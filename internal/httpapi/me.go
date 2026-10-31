package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
)

type updateMeInput struct {
	Body struct {
		Nickname *string `json:"nickname,omitempty" maxLength:"128"`
		Locale   *string `json:"locale,omitempty" maxLength:"16" doc:"One of the site's languages, or empty to follow the browser"`
	}
}

type changePasswordInput struct {
	Body struct {
		CurrentPassword string `json:"currentPassword" maxLength:"1024"`
		NewPassword     string `json:"newPassword" maxLength:"1024"`
	}
}

type changeEmailInput struct {
	Body struct {
		Password string `json:"password" maxLength:"1024"`
		Email    string `json:"email" maxLength:"255"`
	}
}

// SessionView is one signed-in device.
type SessionView struct {
	ID         string    `json:"id"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"userAgent"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Current    bool      `json:"current" doc:"The session making this request"`
}

type sessionIDInput struct {
	ID string `path:"id" maxLength:"64"`
}

type deleteMeInput struct {
	Body struct {
		Password string `json:"password" maxLength:"1024"`
	}
}

// signedIn returns the viewer or a 401.
func signedIn(ctx context.Context) (*account.Viewer, error) {
	if v := requestInfo(ctx).viewer(); v != nil {
		return v, nil
	}
	return nil, huma.Error401Unauthorized("not signed in")
}

// ProblemEmailNotVerified is the problem type (RFC 9457) of a refusal because the site
// requires a verified email address, so the frontend can ask the user to verify instead
// of showing what else a 403 means there.
const ProblemEmailNotVerified = "tag:skinloom.cn,2026:email-not-verified"

// verifiedViewer returns the viewer, or a 403 of type ProblemEmailNotVerified when the
// site requires a verified email address and theirs is not (account.CheckVerified).
func verifiedViewer(ctx context.Context, d Deps) (*account.Viewer, error) {
	v, err := signedIn(ctx)
	if err != nil {
		return nil, err
	}
	if err := account.CheckVerified(d.Settings.Current(), v.User); err != nil {
		return nil, &huma.ErrorModel{Type: ProblemEmailNotVerified, Status: http.StatusForbidden,
			Title: http.StatusText(http.StatusForbidden), Detail: err.Error()}
	}
	return v, nil
}

func registerMe(api huma.API, d Deps) {
	op := func(id, method, path, summary string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{"me"}, Security: sessionSecurity, DefaultStatus: status}
	}
	locales := make([]string, len(Locales))
	for i, l := range Locales {
		locales[i] = l.Code
	}

	huma.Register(api, op("updateMe", http.MethodPatch, "/api/v1/me", "Change your nickname or language", 0),
		func(ctx context.Context, in *updateMeInput) (*struct{ Body UserView }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			u, err := d.Accounts.UpdateProfile(ctx, v.User.ID, account.ProfileUpdate{Nickname: in.Body.Nickname, Locale: in.Body.Locale}, locales)
			if err != nil {
				return nil, apiError(err)
			}
			return &struct{ Body UserView }{userView(u)}, nil
		})

	huma.Register(api, op("changePassword", http.MethodPost, "/api/v1/me/password", "Change your password", http.StatusNoContent),
		func(ctx context.Context, in *changePasswordInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, profileError(d.Accounts.ChangePassword(ctx, v, in.Body.CurrentPassword, in.Body.NewPassword, requestInfo(ctx).meta("")))
		})

	huma.Register(api, op("changeEmail", http.MethodPost, "/api/v1/me/email", "Change your email address", 0),
		func(ctx context.Context, in *changeEmailInput) (*struct{ Body UserView }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			ri := requestInfo(ctx)
			u, err := d.Accounts.ChangeEmail(ctx, v, in.Body.Password, in.Body.Email, mailContext(d, ri), ri.meta(""))
			if err != nil {
				return nil, profileError(err)
			}
			return &struct{ Body UserView }{userView(u)}, nil
		})

	huma.Register(api, op("listSessions", http.MethodGet, "/api/v1/me/sessions", "Your signed-in devices", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body []SessionView }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, err := d.Accounts.ListSessions(ctx, v)
			if err != nil {
				return nil, err
			}
			out := make([]SessionView, 0, len(list))
			for _, x := range list {
				out = append(out, SessionView{ID: x.ID, IP: x.IP, UserAgent: x.UserAgent, CreatedAt: x.CreatedAt,
					LastSeenAt: x.LastSeenAt, Current: x.Current})
			}
			return &struct{ Body []SessionView }{out}, nil
		})

	huma.Register(api, op("revokeSession", http.MethodDelete, "/api/v1/me/sessions/{id}", "Sign one of your devices out", http.StatusNoContent),
		func(ctx context.Context, in *sessionIDInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			if err := d.Accounts.RevokeSession(ctx, v, in.ID); errors.Is(err, account.ErrNotFound) {
				return nil, huma.Error404NotFound("no such session")
			} else if err != nil {
				return nil, err
			}
			return nil, nil
		})

	huma.Register(api, op("revokeOtherSessions", http.MethodPost, "/api/v1/me/sessions/revoke-others", "Sign out all your other devices", http.StatusNoContent),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, d.Accounts.RevokeOtherSessions(ctx, v)
		})

	huma.Register(api, op("deleteMe", http.MethodPost, "/api/v1/me/delete", "Delete your account",
		http.StatusOK),
		func(ctx context.Context, in *deleteMeInput) (*logoutOutput, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			if err := d.Accounts.DeleteAccount(ctx, v, in.Body.Password, requestInfo(ctx).meta("")); err != nil {
				if errors.Is(err, account.ErrAdminCannotDelete) {
					return nil, huma.Error403Forbidden("administrators cannot delete their own account; ask a super administrator to change your role first")
				}
				return nil, profileError(err)
			}
			return &logoutOutput{SetCookie: clearedSessionCookie(requestInfo(ctx))}, nil
		})
}

// profileError maps errors of password-confirmed changes.
func profileError(err error) error {
	if isRateLimit(err) {
		return rateLimitError(err, "too many wrong passwords, try again later")
	}
	return apiError(err)
}
