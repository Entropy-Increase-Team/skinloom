package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
)

// AdminUserView is a user as shown in the admin panel.
type AdminUserView struct {
	ID            int64      `json:"id"`
	Email         string     `json:"email"`
	Nickname      string     `json:"nickname"`
	Role          string     `json:"role" enum:"user,admin,superadmin"`
	Status        string     `json:"status" enum:"active,banned,deleted"`
	Score         int64      `json:"score"`
	EmailVerified bool       `json:"emailVerified"`
	Locale        string     `json:"locale"`
	RegisterIP    string     `json:"registerIp"`
	CreatedAt     time.Time  `json:"createdAt"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty" doc:"Set while a self-deleted account waits out its grace period"`
}

func adminUserView(u *ent.User) AdminUserView {
	return AdminUserView{ID: u.ID, Email: u.Email, Nickname: u.Nickname, Role: string(u.Role), Status: string(u.Status),
		Score: u.Score, EmailVerified: u.EmailVerifiedAt != nil, Locale: u.Locale, RegisterIP: u.RegisterIP,
		CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt, DeletedAt: u.DeletedAt}
}

// UserPage is one page of the user list.
type UserPage struct {
	Users []AdminUserView `json:"users"`
	Next  int64           `json:"next" doc:"Pass as before= for the next page; 0 = last page"`
}

type listUsersInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Query         string `query:"q" maxLength:"255" doc:"Email or nickname fragment, or a user id"`
	Role          string `query:"role" enum:"user,admin,superadmin,"`
	Status        string `query:"status" enum:"active,banned,deleted,"`
	Before        int64  `query:"before" minimum:"0"`
	Limit         int    `query:"limit" minimum:"0" maximum:"100"`
}

type userIDInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
}

type updateUserInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	Body          struct {
		Nickname      *string `json:"nickname,omitempty" maxLength:"128"`
		Email         *string `json:"email,omitempty" maxLength:"255" doc:"A new address is unverified unless emailVerified is also given"`
		Role          *string `json:"role,omitempty" enum:"user,admin,superadmin" doc:"Super administrators only"`
		Status        *string `json:"status,omitempty" enum:"active,banned" doc:"Banning signs the user out everywhere; any change cancels a pending account deletion"`
		Score         *int64  `json:"score,omitempty" minimum:"0"`
		Password      *string `json:"password,omitempty" maxLength:"1024" doc:"Signs the user out everywhere"`
		EmailVerified *bool   `json:"emailVerified,omitempty"`
	}
}

// AuditView is one audit record.
type AuditView struct {
	ID         int64          `json:"id"`
	ActorID    *int64         `json:"actorId,omitempty" doc:"Absent for the system and the admin token"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	IP         string         `json:"ip"`
	Meta       map[string]any `json:"meta,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AuditPage is one page of the audit log.
type AuditPage struct {
	Records []AuditView `json:"records"`
	Next    int64       `json:"next"`
}

type listAuditInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Action        string `query:"action" maxLength:"64" doc:"Exact action, or a prefix ending in a dot (auth.)"`
	Actor         int64  `query:"actor" minimum:"0"`
	Before        int64  `query:"before" minimum:"0"`
	Limit         int    `query:"limit" minimum:"0" maximum:"100"`
}

// adminActor authorizes an admin request and returns who is acting: the session's
// viewer, or nil for the admin token (which acts as a super administrator).
func adminActor(ctx context.Context, d Deps, authorization string) (account.Actor, error) {
	if _, err := checkAdmin(ctx, d, authorization); err != nil {
		return nil, err
	}
	return requestInfo(ctx).viewer(), nil
}

func adminError(err error) error {
	switch {
	case errors.Is(err, account.ErrNotFound):
		return huma.Error404NotFound("no such user")
	case errors.Is(err, account.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, account.ErrLastSuperadmin):
		return huma.Error409Conflict("this is the last super administrator; promote someone else first")
	}
	return apiError(err)
}

func registerAdminUsers(api huma.API, d Deps) {
	op := func(id, method, path, summary string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{"admin"}, Security: adminSecurity, DefaultStatus: status}
	}

	huma.Register(api, op("listUsers", http.MethodGet, "/api/v1/admin/users", "List users", 0),
		func(ctx context.Context, in *listUsersInput) (*struct{ Body UserPage }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			list, next, err := d.Accounts.ListUsers(ctx, account.UserFilter{Query: in.Query, Role: in.Role, Status: in.Status,
				Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			page := UserPage{Users: make([]AdminUserView, 0, len(list)), Next: next}
			for _, u := range list {
				page.Users = append(page.Users, adminUserView(u))
			}
			return &struct{ Body UserPage }{page}, nil
		})

	huma.Register(api, op("getUser", http.MethodGet, "/api/v1/admin/users/{id}", "Get a user", 0),
		func(ctx context.Context, in *userIDInput) (*struct{ Body AdminUserView }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			u, err := d.Accounts.GetUser(ctx, in.ID)
			if err != nil {
				return nil, adminError(err)
			}
			return &struct{ Body AdminUserView }{adminUserView(u)}, nil
		})

	huma.Register(api, op("updateUser", http.MethodPatch, "/api/v1/admin/users/{id}", "Change a user", 0),
		func(ctx context.Context, in *updateUserInput) (*struct{ Body AdminUserView }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			b := in.Body
			u, err := d.Accounts.AdminUpdateUser(ctx, a, in.ID, account.AdminUpdate{
				Nickname: b.Nickname, Email: b.Email, Role: b.Role, Status: b.Status, Score: b.Score,
				Password: b.Password, EmailVerified: b.EmailVerified,
			}, requestInfo(ctx).meta(""))
			if err != nil {
				return nil, adminError(err)
			}
			return &struct{ Body AdminUserView }{adminUserView(u)}, nil
		})

	huma.Register(api, op("deleteUser", http.MethodDelete, "/api/v1/admin/users/{id}", "Delete a user now", http.StatusNoContent),
		func(ctx context.Context, in *userIDInput) (*struct{}, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			return nil, adminError(d.Accounts.AdminDeleteUser(ctx, a, in.ID, requestInfo(ctx).meta("")))
		})

	huma.Register(api, op("listAudit", http.MethodGet, "/api/v1/admin/audit", "Audit log", 0),
		func(ctx context.Context, in *listAuditInput) (*struct{ Body AuditPage }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			list, next, err := d.Accounts.ListAudit(ctx, account.AuditFilter{Action: in.Action, ActorID: in.Actor,
				Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			page := AuditPage{Records: make([]AuditView, 0, len(list)), Next: next}
			for _, r := range list {
				page.Records = append(page.Records, AuditView{ID: r.ID, ActorID: r.ActorID, Action: r.Action,
					TargetType: r.TargetType, TargetID: r.TargetID, IP: r.IP, Meta: r.Meta, CreatedAt: r.CreatedAt})
			}
			return &struct{ Body AuditPage }{page}, nil
		})
}
