// Package report handles complaints about textures. A user reports a texture they can
// see; an administrator upholds the report (deleting the texture or banning its
// uploader) or rejects it. Score changes follow Blessing Skin's reporter_* settings,
// but every one goes through the ledger, and refunds reverse what was actually paid.
package report

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	entreport "github.com/Entropy-Increase-Team/skinloom/ent/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

var (
	// ErrNotFound means there is no such report.
	ErrNotFound = errors.New("report not found")
	// ErrDuplicate means the user reported this texture already.
	ErrDuplicate = errors.New("you have reported this texture already")
	// ErrOwnTexture refuses a report of the reporter's own upload.
	ErrOwnTexture = errors.New("you cannot report your own texture")
	// ErrReviewed means the report is no longer pending.
	ErrReviewed = errors.New("the report has been reviewed already")
	// ErrNoUploader refuses a ban when the uploader's account is gone.
	ErrNoUploader = errors.New("the uploader's account no longer exists")
)

// MaxReason is the longest reason, in characters.
const MaxReason = 500

// Actions an administrator can take on a report.
const (
	ActionDelete = "delete" // delete the texture
	ActionBan    = "ban"    // ban the uploader
	ActionReject = "reject"
)

// Service implements reports.
type Service struct {
	db       *ent.Client
	textures *texture.Service
	log      *slog.Logger
}

// NewService creates the service.
func NewService(db *ent.Client, textures *texture.Service, log *slog.Logger) *Service {
	return &Service{db: db, textures: textures, log: log}
}

func ref(id int64) string { return strconv.FormatInt(id, 10) }

// Submit files a report about a texture the viewer can see, applying
// r.ReporterModification to the reporter's score: a charge must be affordable
// (score.ErrInsufficient). Reporting a texture twice is refused by the unique index.
func (s *Service) Submit(ctx context.Context, v texture.Viewer, textureID int64, reason string, r score.Rates) (*ent.Report, int64, error) {
	reason = strings.TrimSpace(reason)
	switch n := utf8.RuneCountInString(reason); {
	case n == 0:
		return nil, 0, &validate.FieldError{Field: "reason", Message: "required"}
	case n > MaxReason:
		return nil, 0, &validate.FieldError{Field: "reason", Message: fmt.Sprintf("at most %d characters", MaxReason)}
	}
	t, err := s.textures.Get(ctx, textureID, v) // a private texture of someone else is not there to report
	if err != nil {
		return nil, 0, err
	}
	if t.UploaderID != nil && *t.UploaderID == v.UserID {
		return nil, 0, ErrOwnTexture
	}
	var (
		rep     *ent.Report
		balance int64
	)
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		if err := score.Lock(ctx, tx, v.UserID); err != nil { // before the insert: see Lock
			return err
		}
		var err error
		rep, err = tx.Report.Create().SetTextureID(t.ID).SetNillableUploaderID(t.UploaderID).
			SetReporterID(v.UserID).SetReason(reason).Save(ctx)
		if ent.IsConstraintError(err) {
			return ErrDuplicate
		}
		if err != nil {
			return err
		}
		balance, err = score.Apply(ctx, tx, score.Change{UserID: v.UserID, Delta: r.ReporterModification,
			Reason: score.ReasonReportSubmit, RefType: "report", RefID: ref(rep.ID)})
		return err
	})
	return rep, balance, err
}

// Filter selects reports, newest first.
type Filter struct {
	Status     string // pending | resolved | rejected; empty for all
	ReporterID int64
	UploaderID int64 // reports against one uploader
	TextureID  int64
	Before     int64 // reports with smaller ids (the next page)
	Offset     int   // or skip this many (numbered pages)
	Limit      int   // 1–100, default 20
}

func (f Filter) where() []predicate.Report {
	var where []predicate.Report
	if f.Status != "" {
		where = append(where, entreport.StatusEQ(entreport.Status(f.Status)))
	}
	if f.ReporterID > 0 {
		where = append(where, entreport.ReporterID(f.ReporterID))
	}
	if f.UploaderID > 0 {
		where = append(where, entreport.UploaderID(f.UploaderID))
	}
	if f.TextureID > 0 {
		where = append(where, entreport.TextureID(f.TextureID))
	}
	return where
}

// List returns one page of reports with their texture (and its uploader), reporter,
// reported uploader and handler, and the id to pass as Before for the next page (0 on
// the last page).
func (s *Service) List(ctx context.Context, f Filter) ([]*ent.Report, int64, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := f.where()
	if f.Before > 0 {
		where = append(where, entreport.IDLT(f.Before))
	}
	list, err := s.db.Report.Query().Where(where...).WithTexture(func(q *ent.TextureQuery) { q.WithUploader() }).
		WithReporter().WithUploader().WithHandler().
		Order(ent.Desc(entreport.FieldID)).Offset(max(f.Offset, 0)).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(list) > limit {
		list = list[:limit]
		next = list[limit-1].ID
	}
	return list, next, nil
}

// Count is how many reports the filter matches (Before, Offset and Limit aside).
func (s *Service) Count(ctx context.Context, f Filter) (int, error) {
	return s.db.Report.Query().Where(f.where()...).Count(ctx)
}

// Get returns one report with the same edges as List.
func (s *Service) Get(ctx context.Context, id int64) (*ent.Report, error) {
	rep, err := s.db.Report.Query().Where(entreport.ID(id)).
		WithTexture(func(q *ent.TextureQuery) { q.WithUploader() }).
		WithReporter().WithUploader().WithHandler().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return rep, err
}

// Review settles a pending report as an administrator (handler; nil for the admin
// token):
//
//   - reject: a submit reward is taken back (if the reporter can cover it); a charge
//     is kept, as in Blessing Skin.
//   - delete: the texture is deleted through the one deletion path (closets refunded,
//     the file reference-counted, Blessing Skin C06), in the same transaction as the
//     report. The reporter gets a charge back and r.ReporterReward. Other pending
//     reports of the texture are settled with it: charges back, no reward (the first
//     report reviewed is the one rewarded, as in Blessing Skin). If the uploader had
//     deleted the texture already, the charge comes back without a reward.
//   - ban: ban(uploader) runs first and decides whether the administrator may; then
//     the reporter is settled as for delete.
//
// Only one review of a report can win; a later one gets ErrReviewed. refund is
// score.return_on_delete, for the texture deletion.
func (s *Service) Review(ctx context.Context, handler *int64, id int64, action string, r score.Rates, refund bool, ban func(ctx context.Context, userID int64) error) (*ent.Report, error) {
	rep, err := s.db.Report.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if rep.Status != entreport.StatusPending {
		return nil, ErrReviewed
	}
	switch action {
	case ActionBan:
		if rep.UploaderID == nil {
			return nil, ErrNoUploader
		}
		if err := ban(ctx, *rep.UploaderID); err != nil {
			return nil, err
		}
	case ActionDelete, ActionReject:
	default:
		return nil, &validate.FieldError{Field: "action", Message: "must be delete, ban or reject"}
	}
	deleted := false
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		now := time.Now()
		claim := func(id int64, status entreport.Status) error {
			n, err := tx.Report.Update().Where(entreport.ID(id), entreport.StatusEQ(entreport.StatusPending)).
				SetStatus(status).SetAction(entreport.Action(action)).SetHandledAt(now).
				SetNillableHandlerID(handler).Save(ctx)
			if err == nil && n == 0 {
				return ErrReviewed
			}
			return err
		}
		if action == ActionReject {
			if err := claim(rep.ID, entreport.StatusRejected); err != nil {
				return err
			}
			return s.takeBackReward(ctx, tx, rep)
		}
		if err := claim(rep.ID, entreport.StatusResolved); err != nil {
			return err
		}
		reward := action == ActionBan
		if action == ActionDelete && rep.TextureID != nil {
			others, err := tx.Report.Query().Where(entreport.TextureID(*rep.TextureID), entreport.IDNEQ(rep.ID),
				entreport.StatusEQ(entreport.StatusPending)).All(ctx)
			if err != nil {
				return err
			}
			for _, o := range others {
				if err := claim(o.ID, entreport.StatusResolved); err != nil {
					return err
				}
				if err := s.settle(ctx, tx, o, false, r); err != nil {
					return err
				}
			}
			switch err := s.textures.DeleteIn(ctx, tx, *rep.TextureID, refund); {
			case err == nil:
				reward, deleted = true, true
			case !errors.Is(err, texture.ErrNotFound): // gone already: no reward
				return err
			}
		}
		return s.settle(ctx, tx, rep, reward, r)
	})
	if err != nil {
		return nil, err
	}
	if deleted {
		s.textures.Kick()
	}
	return s.Get(ctx, rep.ID)
}

// settle gives an upheld report's reporter back a submit charge and, with reward, the
// reporter reward.
func (s *Service) settle(ctx context.Context, tx *ent.Client, rep *ent.Report, reward bool, r score.Rates) error {
	if rep.ReporterID == nil {
		return nil // the reporter's account is gone
	}
	paid, err := score.Last(ctx, tx, *rep.ReporterID, score.ReasonReportSubmit, "report", ref(rep.ID))
	if err != nil {
		return err
	}
	if paid != nil && paid.Delta < 0 {
		if _, err := score.Apply(ctx, tx, score.Change{UserID: *rep.ReporterID, Delta: -paid.Delta,
			Reason: score.ReasonReportRefund, RefType: "report", RefID: ref(rep.ID)}); err != nil {
			return err
		}
	}
	if reward && r.ReporterReward > 0 {
		_, err := score.Apply(ctx, tx, score.Change{UserID: *rep.ReporterID, Delta: r.ReporterReward,
			Reason: score.ReasonReportReward, RefType: "report", RefID: ref(rep.ID)})
		return err
	}
	return nil
}

// takeBackReward reverses a submit reward of a rejected report, if the reporter can
// still cover it.
func (s *Service) takeBackReward(ctx context.Context, tx *ent.Client, rep *ent.Report) error {
	if rep.ReporterID == nil {
		return nil
	}
	got, err := score.Last(ctx, tx, *rep.ReporterID, score.ReasonReportSubmit, "report", ref(rep.ID))
	if err != nil || got == nil || got.Delta <= 0 {
		return err
	}
	_, err = score.Apply(ctx, tx, score.Change{UserID: *rep.ReporterID, Delta: -got.Delta,
		Reason: score.ReasonReportReject, RefType: "report", RefID: ref(rep.ID)})
	if errors.Is(err, score.ErrInsufficient) {
		return nil // spent already: let it be
	}
	return err
}

// PendingCount is how many reports wait for review.
func (s *Service) PendingCount(ctx context.Context) (int, error) {
	return s.db.Report.Query().Where(entreport.StatusEQ(entreport.StatusPending)).Count(ctx)
}

func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return tx.Commit()
}
