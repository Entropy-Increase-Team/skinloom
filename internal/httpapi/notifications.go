package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// NotificationView is one message in the signed-in user's inbox.
type NotificationView struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	ContentHTML  string    `json:"contentHtml" doc:"Rendered from Markdown: no raw HTML, no script links"`
	Read         bool      `json:"read"`
	Announcement bool      `json:"announcement" doc:"A copy of a message sent to many users"`
	CreatedAt    time.Time `json:"createdAt"`
}

func notificationView(n *ent.Notification) NotificationView {
	return NotificationView{ID: n.ID, Title: n.Title, ContentHTML: notify.Markdown(n.Content), Read: n.ReadAt != nil,
		Announcement: n.AnnouncementID != nil, CreatedAt: n.CreatedAt}
}

// NotificationPage is one page of the inbox.
type NotificationPage struct {
	Notifications []NotificationView `json:"notifications"`
	Next          int64              `json:"next" doc:"Pass as before= for the next page; 0 = last page"`
	Unread        int                `json:"unread"`
}

// UnreadCount is how many notifications are unread.
type UnreadCount struct {
	Unread int `json:"unread"`
}

// MarkedRead answers marking everything read.
type MarkedRead struct {
	Count int `json:"count" doc:"How many were unread"`
}

// SentNotification answers an administrator's send.
type SentNotification struct {
	Kind string `json:"kind" enum:"announcement,notification" doc:"announcement: delivered to each user as they next open their inbox"`
	ID   int64  `json:"id"`
}

type listNotificationsInput struct {
	Unread bool  `query:"unread" doc:"Only unread ones"`
	Before int64 `query:"before" minimum:"0"`
	Limit  int   `query:"limit" minimum:"0" maximum:"100"`
}

type notificationIDInput struct {
	ID int64 `path:"id"`
}

type sendNotificationInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Body          struct {
		To      string `json:"to" enum:"all,users,user" doc:"all users, ordinary users (not administrators), or one user"`
		UserID  int64  `json:"userId,omitempty" minimum:"0" doc:"For to=user: the user's id, or give email"`
		Email   string `json:"email,omitempty" maxLength:"255" doc:"For to=user: the user's address"`
		Title   string `json:"title" maxLength:"256" doc:"Up to 64 characters"`
		Content string `json:"content" maxLength:"40000" doc:"Markdown, up to 10000 characters"`
	}
}

type setAvatarInput struct {
	Body struct {
		TextureID int64 `json:"textureId" minimum:"0" doc:"A skin you can see; 0 removes the avatar"`
	}
}

func notifyError(err error) error {
	if errors.Is(err, notify.ErrNotFound) {
		return huma.Error404NotFound("no such notification")
	}
	return apiError(err)
}

func registerNotifications(api huma.API, d Deps) {
	op := func(id, method, path, summary string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{"me"}, Security: sessionSecurity, DefaultStatus: status}
	}

	huma.Register(api, op("listNotifications", http.MethodGet, "/api/v1/me/notifications", "Your notifications, newest first", 0),
		func(ctx context.Context, in *listNotificationsInput) (*struct{ Body NotificationPage }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, next, err := d.Notify.List(ctx, v.User.ID, notify.Filter{Unread: in.Unread, Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			unread, err := d.Notify.Unread(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			page := NotificationPage{Notifications: make([]NotificationView, 0, len(list)), Next: next, Unread: unread}
			for _, n := range list {
				page.Notifications = append(page.Notifications, notificationView(n))
			}
			return &struct{ Body NotificationPage }{page}, nil
		})

	huma.Register(api, op("countUnreadNotifications", http.MethodGet, "/api/v1/me/notifications/unread", "How many notifications are unread", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body UnreadCount }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			n, err := d.Notify.Unread(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body UnreadCount }{UnreadCount{n}}, nil
		})

	huma.Register(api, op("readNotification", http.MethodPost, "/api/v1/me/notifications/{id}/read", "Mark a notification read", http.StatusNoContent),
		func(ctx context.Context, in *notificationIDInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, notifyError(d.Notify.MarkRead(ctx, v.User.ID, in.ID))
		})

	huma.Register(api, op("readAllNotifications", http.MethodPost, "/api/v1/me/notifications/read-all", "Mark every notification read", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body MarkedRead }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			n, err := d.Notify.MarkAllRead(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			return &struct{ Body MarkedRead }{MarkedRead{n}}, nil
		})

	huma.Register(api, op("setAvatar", http.MethodPut, "/api/v1/me/avatar", "Choose a skin whose head is your avatar", http.StatusNoContent),
		func(ctx context.Context, in *setAvatarInput) (*struct{}, error) {
			if _, err := signedIn(ctx); err != nil {
				return nil, err
			}
			err := d.Textures.SetAvatar(ctx, textureViewer(ctx), in.Body.TextureID)
			if errors.Is(err, texture.ErrNotSkin) {
				return nil, apiError(&account.FieldError{Field: "textureId", Message: "not a skin"})
			}
			if err != nil {
				return nil, textureError(d, err)
			}
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "sendNotification", Method: http.MethodPost, Path: "/api/v1/admin/notifications",
		Summary: "Send a notification", Tags: []string{"admin"}, Security: adminSecurity,
		Description: "to=all or users writes one announcement, which each user receives when they next open their inbox " +
			"(users who register later do not get it); to=user sends to one user, by userId or email."},
		func(ctx context.Context, in *sendNotificationInput) (*struct{ Body SentNotification }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			b := in.Body
			var out SentNotification
			switch b.To {
			case "user":
				var u *ent.User
				switch {
				case b.UserID > 0:
					u, err = d.Accounts.GetUser(ctx, b.UserID)
				case b.Email != "":
					u, err = d.Accounts.GetUserByEmail(ctx, b.Email)
				default:
					err = account.ErrNotFound
				}
				if errors.Is(err, account.ErrNotFound) {
					return nil, apiError(&account.FieldError{Field: "userId", Message: "no such user"})
				}
				if err != nil {
					return nil, err
				}
				n, err := d.Notify.Send(ctx, u.ID, b.Title, b.Content)
				if err != nil {
					return nil, apiError(err)
				}
				out = SentNotification{Kind: "notification", ID: n.ID}
			default:
				ann, err := d.Notify.Announce(ctx, actorIDOf(a), b.To, b.Title, b.Content)
				if err != nil {
					return nil, apiError(err)
				}
				out = SentNotification{Kind: "announcement", ID: ann.ID}
			}
			if err := d.Accounts.Audit(ctx, actorIDOf(a), "admin.notification_sent", out.Kind, strconv.FormatInt(out.ID, 10),
				requestInfo(ctx).client.ClientIP, map[string]any{"to": b.To, "title": b.Title}); err != nil {
				d.Log.Warn("audit", "action", "admin.notification_sent", "err", err)
			}
			return &struct{ Body SentNotification }{out}, nil
		})
}
