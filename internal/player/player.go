// Package player owns players (in-game characters): names, UUIDs, the skin and cape
// each one wears, and the profiles game clients fetch.
package player

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/scoreentry"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

var (
	ErrNotFound = errors.New("player not found")
	// ErrBanned means the player's owner is banned: game APIs answer 403.
	ErrBanned = errors.New("the player's owner is banned")
)

// Service implements players.
type Service struct {
	db  *ent.Client
	log *slog.Logger
}

// NewService creates the service.
func NewService(db *ent.Client, log *slog.Logger) *Service {
	return &Service{db: db, log: log}
}

// Key is the lookup key of a name: names are unique and found ignoring case.
func Key(name string) string { return strings.ToLower(name) }

var (
	officialName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	cjkName      = regexp.MustCompile(`^[A-Za-z0-9_§\x{4e00}-\x{9fff}]+$`)
)

// ValidateName checks a new name against the site's rule. Whatever the rule, names
// have no spaces or control characters and none of / \ ? # %, which would break the
// URLs game clients build from them (/{name}.json, /skin/{name}.png).
func ValidateName(name string, p Policy) error {
	bad := func(msg string) error { return &validate.FieldError{Field: "name", Message: msg} }
	n := utf8.RuneCountInString(name)
	if !utf8.ValidString(name) || n < p.Min || n > min(p.Max, MaxNameLen) {
		return bad(fmt.Sprintf("must be %d to %d characters", p.Min, min(p.Max, MaxNameLen)))
	}
	for _, r := range name {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) || strings.ContainsRune(`/\?#%`, r) {
			return bad("contains spaces or characters that cannot be part of a name")
		}
	}
	var ok bool
	switch p.Rule {
	case RuleCJK:
		ok = cjkName.MatchString(name)
	case RuleUTF8:
		ok = true
	case RuleCustom:
		ok = p.Custom == nil || p.Custom.MatchString(name)
	default:
		ok = officialName.MatchString(name)
	}
	if !ok {
		return bad("does not follow the site's naming rule")
	}
	return nil
}

// OfflineUUID is the UUID an offline-mode server gives name: a version 3 UUID of
// "OfflinePlayer:" + name (Java's UUID.nameUUIDFromBytes), without dashes.
func OfflineUUID(name string) string {
	b := md5.Sum([]byte("OfflinePlayer:" + name))
	b[6] = b[6]&0x0f | 0x30
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:])
}

// RandomUUID is a version 4 UUID without dashes.
func RandomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b[:])
}

func newUUID(name string, p Policy) string {
	if p.UUIDv4 {
		return RandomUUID()
	}
	return OfflineUUID(name)
}

// List returns a user's players, oldest first, with what they wear.
func (s *Service) List(ctx context.Context, userID int64) ([]*ent.Player, error) {
	return s.db.Player.Query().Where(entplayer.UserID(userID)).
		WithSkin().WithCape().Order(ent.Asc(entplayer.FieldID)).All(ctx)
}

func (s *Service) get(ctx context.Context, userID, id int64) (*ent.Player, error) {
	p, err := s.db.Player.Query().Where(entplayer.ID(id), entplayer.UserID(userID)).WithSkin().WithCape().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return p, err
}

var errNameTaken = &validate.FieldError{Field: "name", Message: "already taken"}

// Create adds a player for a user and charges r.PerPlayer. Whether the name is free is
// decided by the unique index (two people asking for one name at once get one
// player between them, Blessing Skin C01).
func (s *Service) Create(ctx context.Context, userID int64, name string, p Policy, r score.Rates) (*ent.Player, int64, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name, p); err != nil {
		return nil, 0, err
	}
	var (
		created *ent.Player
		balance int64
	)
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
		if err := score.Lock(ctx, tx, userID); err != nil { // before the insert: see Lock
			return err
		}
		var err error
		created, err = tx.Player.Create().
			SetUserID(userID).SetName(name).SetNameLower(Key(name)).SetUUID(newUUID(name, p)).
			Save(ctx)
		if ent.IsConstraintError(err) {
			return errNameTaken
		}
		if err != nil {
			return err
		}
		balance, err = score.Apply(ctx, tx, score.Change{UserID: userID, Delta: -r.PerPlayer,
			Reason: score.ReasonPlayerCreate, RefType: "player", RefID: strconv.FormatInt(created.ID, 10)})
		return err
	})
	return created, balance, err
}

// Rename changes a player's name. With v3 UUIDs the UUID follows the name, as on an
// offline-mode server; v4 UUIDs stay.
func (s *Service) Rename(ctx context.Context, userID, id int64, name string, p Policy) (*ent.Player, error) {
	name = strings.TrimSpace(name)
	if err := ValidateName(name, p); err != nil {
		return nil, err
	}
	pl, err := s.get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	upd := s.db.Player.UpdateOne(pl).SetName(name).SetNameLower(Key(name))
	if !p.UUIDv4 {
		upd.SetUUID(OfflineUUID(name))
	}
	if _, err := upd.Save(ctx); ent.IsConstraintError(err) {
		return nil, errNameTaken
	} else if err != nil {
		return nil, err
	}
	return s.get(ctx, userID, id)
}

// Delete removes a player. With refund, the score its creation cost is given back:
// the amount recorded in the ledger then, not today's price (Blessing Skin returned
// the current score_per_player, C05).
func (s *Service) Delete(ctx context.Context, userID, id int64, refund bool) (int64, error) {
	pl, err := s.get(ctx, userID, id)
	if err != nil {
		return 0, err
	}
	var returned int64
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		if err := tx.Player.DeleteOne(pl).Exec(ctx); err != nil {
			return err
		}
		if !refund {
			return nil
		}
		paid, err := tx.ScoreEntry.Query().Where(scoreentry.UserID(userID), scoreentry.Reason(score.ReasonPlayerCreate),
			scoreentry.RefType("player"), scoreentry.RefID(strconv.FormatInt(id, 10))).First(ctx)
		if ent.IsNotFound(err) || (err == nil && paid.Delta >= 0) {
			return nil // created for free (or imported): nothing to give back
		}
		if err != nil {
			return err
		}
		returned = -paid.Delta
		_, err = score.Apply(ctx, tx, score.Change{UserID: userID, Delta: returned,
			Reason: score.ReasonPlayerDelete, RefType: "player", RefID: strconv.FormatInt(id, 10)})
		return err
	})
	return returned, err
}

// Wear sets what a player wears. For each of skin and cape: nil leaves it, 0 takes it
// off, a texture id puts it on. The texture must be in the owner's closet and of the
// right kind: a cape is not a skin (Blessing Skin let any closet item go anywhere, C07).
func (s *Service) Wear(ctx context.Context, userID, id int64, skin, cape *int64) (*ent.Player, error) {
	pl, err := s.get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	upd := s.db.Player.UpdateOne(pl)
	var errs []error
	for _, slot := range []struct {
		field string
		id    *int64
		types []tex.Type
		set   func(int64) *ent.PlayerUpdateOne
		clear func() *ent.PlayerUpdateOne
	}{
		{"skin", skin, []tex.Type{tex.TypeSteve, tex.TypeAlex}, upd.SetSkinID, upd.ClearSkinID},
		{"cape", cape, []tex.Type{tex.TypeCape}, upd.SetCapeID, upd.ClearCapeID},
	} {
		switch {
		case slot.id == nil:
		case *slot.id == 0:
			slot.clear()
		default:
			t, err := s.db.Texture.Get(ctx, *slot.id)
			if ent.IsNotFound(err) {
				errs = append(errs, &validate.FieldError{Field: slot.field, Message: "no such texture"})
				continue
			}
			if err != nil {
				return nil, err
			}
			ok := false
			for _, want := range slot.types {
				ok = ok || t.Type == want
			}
			if !ok {
				errs = append(errs, &validate.FieldError{Field: slot.field, Message: "not a " + slot.field})
				continue
			}
			inCloset, err := s.db.ClosetItem.Query().Where(closetitem.UserID(userID), closetitem.TextureID(t.ID)).Exist(ctx)
			if err != nil {
				return nil, err
			}
			if !inCloset {
				errs = append(errs, &validate.FieldError{Field: slot.field, Message: "not in your closet"})
				continue
			}
			slot.set(t.ID)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if _, err := upd.Save(ctx); err != nil {
		return nil, err
	}
	return s.get(ctx, userID, id)
}

// Profile is what game clients get for a name.
type Profile struct {
	Name     string // as stored, whatever case was asked for
	UUID     string
	Slim     bool   // the skin is an Alex (slim arms) skin
	Skin     string // hash; "" = none
	Cape     string // hash; "" = none
	Modified time.Time
}

// Model is the skin model name game clients use: "default" or "slim".
func (p *Profile) Model() string {
	if p.Slim {
		return "slim"
	}
	return "default"
}

// Profile looks a name up for the game APIs, ignoring case. Players of deleted
// accounts are not found; players of banned accounts give ErrBanned.
func (s *Service) Profile(ctx context.Context, name string) (*Profile, error) {
	pl, err := s.db.Player.Query().Where(entplayer.NameLower(Key(name))).
		WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldStatus) }).
		WithSkin(func(q *ent.TextureQuery) { q.Select(tex.FieldID, tex.FieldHash, tex.FieldType) }).
		WithCape(func(q *ent.TextureQuery) { q.Select(tex.FieldID, tex.FieldHash) }).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	switch pl.Edges.Owner.Status {
	case user.StatusDeleted:
		return nil, ErrNotFound
	case user.StatusBanned:
		return nil, ErrBanned
	}
	out := &Profile{Name: pl.Name, UUID: pl.UUID, Modified: pl.UpdatedAt}
	if t := pl.Edges.Skin; t != nil {
		out.Skin, out.Slim = t.Hash, t.Type == tex.TypeAlex
	}
	if t := pl.Edges.Cape; t != nil {
		out.Cape = t.Hash
	}
	return out, nil
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
