// Package notify keeps each user's inbox: notifications sent to them, and their copies
// of announcements sent to many. An announcement is one row when sent; a user gets
// their copy the next time they look at their inbox (Blessing Skin wrote one row per
// user at once, which on a big site is a long write while the administrator waits).
package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/announcement"
	"github.com/Entropy-Increase-Team/skinloom/ent/notification"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

// ErrNotFound means the user has no such notification.
var ErrNotFound = errors.New("notification not found")

// Limits, in characters.
const (
	MaxTitle   = 64
	MaxContent = 10000
)

// Audiences of an announcement.
const (
	AudienceAll   = "all"
	AudienceUsers = "users" // ordinary users, not administrators (Blessing Skin's "normal")
)

// Service implements inboxes.
type Service struct {
	db  *ent.Client
	log *slog.Logger
}

// NewService creates the service.
func NewService(db *ent.Client, log *slog.Logger) *Service { return &Service{db: db, log: log} }

func check(title, content string) (string, string, error) {
	title, content = strings.TrimSpace(title), strings.TrimSpace(content)
	var errs []error
	if n := utf8.RuneCountInString(title); n == 0 || n > MaxTitle {
		errs = append(errs, &validate.FieldError{Field: "title", Message: fmt.Sprintf("1 to %d characters", MaxTitle)})
	}
	if utf8.RuneCountInString(content) > MaxContent {
		errs = append(errs, &validate.FieldError{Field: "content", Message: fmt.Sprintf("at most %d characters", MaxContent)})
	}
	return title, content, errors.Join(errs...)
}

// Send puts a notification in one user's inbox.
func (s *Service) Send(ctx context.Context, userID int64, title, content string) (*ent.Notification, error) {
	title, content, err := check(title, content)
	if err != nil {
		return nil, err
	}
	// Announcements the user has not received yet come first, so an inbox in id order
	// is in the order things were sent.
	if err := s.deliver(ctx, userID); err != nil {
		return nil, err
	}
	return s.db.Notification.Create().SetUserID(userID).SetTitle(title).SetContent(content).Save(ctx)
}

// Announce sends a notification to every user of audience who exists now (later
// users do not get it, as with Blessing Skin). sender is nil for the admin token.
func (s *Service) Announce(ctx context.Context, sender *int64, audience, title, content string) (*ent.Announcement, error) {
	title, content, err := check(title, content)
	if err != nil {
		return nil, err
	}
	if audience != AudienceAll && audience != AudienceUsers {
		return nil, &validate.FieldError{Field: "audience", Message: "must be all or users"}
	}
	return s.db.Announcement.Create().SetTitle(title).SetContent(content).
		SetAudience(announcement.Audience(audience)).SetNillableSenderID(sender).Save(ctx)
}

// deliver copies into the user's inbox the announcements meant for them that they do
// not have yet. The unique (user, announcement) index makes concurrent deliveries
// harmless; users.announcements_seen keeps the lookup to new announcements.
func (s *Service) deliver(ctx context.Context, userID int64) error {
	u, err := s.db.User.Query().Where(user.ID(userID)).
		Select(user.FieldCreatedAt, user.FieldRole, user.FieldAnnouncementsSeen).Only(ctx)
	if err != nil {
		return err
	}
	q := s.db.Announcement.Query().Where(announcement.IDGT(u.AnnouncementsSeen), announcement.CreatedAtGTE(u.CreatedAt))
	if u.Role != user.RoleUser {
		q = q.Where(announcement.AudienceEQ(announcement.AudienceAll))
	}
	list, err := q.Order(ent.Asc(announcement.FieldID)).Limit(500).All(ctx)
	if err != nil || len(list) == 0 {
		return err
	}
	return withTx(ctx, s.db, func(tx *ent.Client) error {
		// Claim the delivery on the user's row first: concurrent deliveries for one user
		// queue on that row lock, and the ones that find it delivered insert nothing.
		// Inserting first let them all reach the same notification keys at once, which
		// MySQL (whose DoNothing is ON DUPLICATE KEY UPDATE) resolves with a deadlock.
		last := list[len(list)-1].ID
		n, err := tx.User.Update().Where(user.ID(userID), user.AnnouncementsSeenLT(last)).SetAnnouncementsSeen(last).Save(ctx)
		if err != nil || n == 0 {
			return err
		}
		builders := make([]*ent.NotificationCreate, len(list))
		for i, a := range list {
			builders[i] = tx.Notification.Create().SetUserID(userID).SetTitle(a.Title).SetContent(a.Content).
				SetAnnouncementID(a.ID).SetCreatedAt(a.CreatedAt)
		}
		// A delivery that claimed less (it read the list earlier) may have copied some
		// of these already.
		return tx.Notification.CreateBulk(builders...).
			OnConflictColumns(notification.FieldUserID, notification.FieldAnnouncementID).DoNothing().Exec(ctx)
	})
}

// Filter selects notifications, newest first.
type Filter struct {
	Unread bool
	Before int64 // notifications with smaller ids (the next page)
	Limit  int   // 1–100, default 20
}

// List returns one page of the user's inbox and the id to pass as Before for the next
// page (0 on the last page).
func (s *Service) List(ctx context.Context, userID int64, f Filter) ([]*ent.Notification, int64, error) {
	if err := s.deliver(ctx, userID); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := s.db.Notification.Query().Where(notification.UserID(userID))
	if f.Unread {
		q = q.Where(notification.ReadAtIsNil())
	}
	if f.Before > 0 {
		q = q.Where(notification.IDLT(f.Before))
	}
	list, err := q.Order(ent.Desc(notification.FieldID)).Limit(limit + 1).All(ctx)
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

// Unread is how many unread notifications the user has.
func (s *Service) Unread(ctx context.Context, userID int64) (int, error) {
	if err := s.deliver(ctx, userID); err != nil {
		return 0, err
	}
	return s.db.Notification.Query().Where(notification.UserID(userID), notification.ReadAtIsNil()).Count(ctx)
}

// MarkRead marks one of the user's notifications read (reading it again is fine).
func (s *Service) MarkRead(ctx context.Context, userID, id int64) error {
	ok, err := s.db.Notification.Query().Where(notification.ID(id), notification.UserID(userID)).Exist(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound // Blessing Skin answered an unknown id with a 500 (C15)
	}
	return s.db.Notification.Update().Where(notification.ID(id), notification.ReadAtIsNil()).SetReadAt(time.Now()).Exec(ctx)
}

// MarkAllRead marks all of the user's notifications read and returns how many were
// unread.
func (s *Service) MarkAllRead(ctx context.Context, userID int64) (int, error) {
	return s.db.Notification.Update().Where(notification.UserID(userID), notification.ReadAtIsNil()).SetReadAt(time.Now()).Save(ctx)
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	// Line breaks are kept, as in a chat message. Raw HTML stays out and javascript:
	// links are dropped: goldmark's defaults, which WithUnsafe would turn off.
	goldmark.WithRendererOptions(html.WithHardWraps()),
)

// Markdown renders Markdown to HTML that is safe to put in a page as it is: no raw
// HTML, no script links.
func Markdown(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return buf.String()
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
