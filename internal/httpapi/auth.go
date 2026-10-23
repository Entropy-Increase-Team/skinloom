package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
)

// SessionCookie holds the browser session token.
const SessionCookie = "skinloom_session"

// CSRFHeader must accompany every state-changing API request made with a session
// cookie. Browsers only send custom headers cross-origin after a CORS preflight, which
// SkinLoom never approves, so a forged form or fetch from another site cannot set it.
const CSRFHeader = "X-SkinLoom-Request"

type ctxKey int

const reqInfoKey ctxKey = iota

// reqInfo is attached to every API request by withRequestInfo.
type reqInfo struct {
	client realip.Info
	// viewer resolves the session cookie on first use, so endpoints that do not care
	// who is signed in (bootstrap on every page render) cost no session lookup.
	viewer func() *account.Viewer
}

func requestInfo(ctx context.Context) reqInfo {
	ri, _ := ctx.Value(reqInfoKey).(reqInfo)
	if ri.viewer == nil {
		ri.viewer = func() *account.Viewer { return nil }
	}
	return ri
}

func (ri reqInfo) meta(userAgent string) account.RequestMeta {
	return account.RequestMeta{IP: ri.client.ClientIP, UserAgent: userAgent}
}

// withRequestInfo resolves the client address (trusted proxies only) and the session
// for /api requests, so handlers read both from the context.
func withRequestInfo(d Deps) func(http.Handler) http.Handler {
	resolve := d.ClientInfo
	if resolve == nil {
		direct, _ := realip.New(nil)
		resolve = direct.Resolve
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}
			ri := reqInfo{client: resolve(r)}
			ctx := r.Context()
			ri.viewer = sync.OnceValue(func() *account.Viewer {
				c, err := r.Cookie(SessionCookie)
				if err != nil || d.Accounts == nil {
					return nil
				}
				v, err := d.Accounts.Authenticate(ctx, c.Value)
				if err != nil {
					d.Log.Warn("session lookup failed", "err", err)
				}
				return v
			})
			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, reqInfoKey, ri)))
		})
	}
}

// sessionEndpoints set a session cookie, so they need CSRF protection even before the
// browser has one (login CSRF would sign the victim into the attacker's account).
var sessionEndpoints = map[string]bool{"/api/v1/auth/login": true, "/api/v1/setup": true}

// csrfGuard rejects state-changing /api requests that ride on a session cookie (or
// create one) unless they carry CSRFHeader and, when the browser sends Origin, come
// from this site. Requests authenticated only by a bearer token are not affected.
func csrfGuard(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			_, cookieErr := r.Cookie(SessionCookie)
			if !strings.HasPrefix(r.URL.Path, "/api/") || (cookieErr != nil && !sessionEndpoints[r.URL.Path]) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get(CSRFHeader) == "" {
				writeProblem(w, http.StatusForbidden, "missing "+CSRFHeader+" header")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" && !sameSite(origin, requestInfo(r.Context()).client.Host, d) {
				writeProblem(w, http.StatusForbidden, "cross-site request refused")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sameSite accepts the public host the request was sent to and the configured site.url.
func sameSite(origin, host string, d Deps) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, host) {
		return true
	}
	if d.Settings != nil {
		if site, err := url.Parse(d.Settings.Current().String("site.url")); err == nil && site.Host != "" {
			return strings.EqualFold(u.Host, site.Host)
		}
	}
	return false
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": http.StatusText(status), "status": status, "detail": detail})
}

// UserView is the signed-in user as shown to themselves.
type UserView struct {
	ID            int64  `json:"id"`
	Email         string `json:"email"`
	Nickname      string `json:"nickname"`
	Role          string `json:"role" enum:"user,admin,superadmin"`
	EmailVerified bool   `json:"emailVerified"`
	Locale        string `json:"locale" doc:"Preferred UI language; empty = follow the browser"`
}

func userView(u *ent.User) UserView {
	return UserView{ID: u.ID, Email: u.Email, Nickname: u.Nickname, Role: string(u.Role),
		EmailVerified: u.EmailVerifiedAt != nil, Locale: u.Locale}
}

type userOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      UserView
}

type setupStatusOutput struct {
	Body struct {
		NeedsSetup bool `json:"needsSetup"`
	}
}

type setupInput struct {
	UserAgent string `header:"User-Agent"`
	Session   string `cookie:"skinloom_session"`
	Body      struct {
		Token    string `json:"token" doc:"One-time setup token printed on the server console"`
		Email    string `json:"email" format:"email" maxLength:"255"`
		Nickname string `json:"nickname" maxLength:"128"`
		Password string `json:"password" maxLength:"1024"`
		SiteName string `json:"siteName,omitempty" maxLength:"256" doc:"Site name for every language; can be refined later in the admin panel"`
		SiteURL  string `json:"siteUrl,omitempty" maxLength:"512" doc:"Public URL, e.g. https://skins.example.com"`
	}
}

type loginInput struct {
	UserAgent string `header:"User-Agent"`
	Session   string `cookie:"skinloom_session"`
	Body      struct {
		Email    string `json:"email" maxLength:"255"`
		Password string `json:"password" maxLength:"1024"`
		Remember bool   `json:"remember,omitempty" doc:"Keep the session for 30 days instead of until the browser closes"`
	}
}

type logoutInput struct {
	Session string `cookie:"skinloom_session"`
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

var sessionSecurity = []map[string][]string{{"session": {}}}

func registerAuth(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getSetupStatus",
		Method:      http.MethodGet,
		Path:        "/api/v1/setup",
		Summary:     "Whether first-run setup is pending",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, _ *struct{}) (*setupStatusOutput, error) {
		needs, err := d.Accounts.NeedsSetup(ctx)
		if err != nil {
			return nil, err
		}
		out := &setupStatusOutput{}
		out.Body.NeedsSetup = needs
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "completeSetup",
		Method:      http.MethodPost,
		Path:        "/api/v1/setup",
		Summary:     "Create the first administrator",
		Description: "Needs the one-time token printed on the server console at startup (`skinloom setup-link` prints a new one). Signs the new administrator in.",
		Tags:        []string{"setup"},
	}, func(ctx context.Context, in *setupInput) (*userOutput, error) {
		ri := requestInfo(ctx)
		changes, err := siteSettings(in.Body.SiteName, in.Body.SiteURL)
		if err != nil {
			return nil, err
		}
		u, sess, err := d.Accounts.CompleteSetup(ctx, account.SetupInput{
			Token: in.Body.Token, Email: in.Body.Email, Nickname: in.Body.Nickname, Password: in.Body.Password,
		}, ri.meta(in.UserAgent))
		switch {
		case errors.Is(err, account.ErrInvalidToken):
			return nil, huma.Error403Forbidden("the setup link is invalid, used or expired; restart SkinLoom or run `skinloom setup-link` for a new one")
		case errors.Is(err, account.ErrSetupDone):
			return nil, huma.Error409Conflict("setup has already been completed")
		case err != nil:
			return nil, apiError(err)
		}
		// The administrator exists now; finish even if the client already went away.
		done := context.WithoutCancel(ctx)
		if len(changes) > 0 {
			if _, err := d.Settings.Set(done, &u.ID, changes); err != nil {
				d.Log.Warn("setup: could not save site settings", "err", err)
			}
		}
		replaceSession(done, d, in.Session)
		return &userOutput{SetCookie: sessionCookie(sess, ri), Body: userView(u)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/login",
		Summary:     "Sign in with email and password",
		Description: "Sets the session cookie. Every failure returns the same 401, whether or not the account exists; repeated failures return 429 with Retry-After.",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, in *loginInput) (*userOutput, error) {
		ri := requestInfo(ctx)
		u, sess, err := d.Accounts.Login(ctx, in.Body.Email, in.Body.Password, in.Body.Remember, ri.meta(in.UserAgent))
		var rl *account.RateLimitError
		switch {
		case errors.As(err, &rl):
			secs := int(rl.RetryAfter.Round(time.Second) / time.Second)
			return nil, huma.ErrorWithHeaders(huma.Error429TooManyRequests("too many failed attempts, try again later"),
				http.Header{"Retry-After": {strconv.Itoa(max(1, secs))}})
		case errors.Is(err, account.ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized("incorrect email or password")
		case errors.Is(err, account.ErrAccountDisabled):
			return nil, huma.Error403Forbidden("this account has been disabled")
		case err != nil:
			return nil, err
		}
		replaceSession(context.WithoutCancel(ctx), d, in.Session)
		return &userOutput{SetCookie: sessionCookie(sess, ri), Body: userView(u)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/logout",
		Summary:       "Sign out",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
		if err := d.Accounts.Logout(ctx, in.Session); err != nil {
			return nil, err
		}
		return &logoutOutput{SetCookie: http.Cookie{
			Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode,
			Secure: requestInfo(ctx).client.Proto == "https",
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getMe",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "The signed-in user",
		Tags:        []string{"auth"},
		Security:    sessionSecurity,
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body UserView }, error) {
		v := requestInfo(ctx).viewer()
		if v == nil {
			return nil, huma.Error401Unauthorized("not signed in")
		}
		return &struct{ Body UserView }{Body: userView(v.User)}, nil
	})
}

// replaceSession revokes the session a browser held before signing in again, so
// switching accounts does not leave the old one valid until it expires.
func replaceSession(ctx context.Context, d Deps, old string) {
	if old == "" {
		return
	}
	if err := d.Accounts.Logout(ctx, old); err != nil {
		d.Log.Warn("revoke replaced session", "err", err)
	}
}

func sessionCookie(s *account.NewSession, ri reqInfo) http.Cookie {
	c := http.Cookie{
		Name: SessionCookie, Value: s.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: ri.client.Proto == "https",
	}
	if s.Persistent {
		c.Expires = s.ExpiresAt
		c.MaxAge = int(time.Until(s.ExpiresAt) / time.Second)
	}
	return c
}

// siteSettings turns the optional setup fields into settings changes, validated up
// front so a bad URL fails before the administrator is created.
func siteSettings(name, siteURL string) (map[string]json.RawMessage, error) {
	changes := map[string]json.RawMessage{}
	var details []error
	if name = strings.TrimSpace(name); name != "" {
		if utf8.RuneCountInString(name) > 64 {
			details = append(details, &huma.ErrorDetail{Location: "body.siteName", Message: "must be at most 64 characters", Value: name})
		}
		changes["site.name"], _ = json.Marshal(map[string]string{"zh-CN": name, "en": name})
	}
	if siteURL = strings.TrimRight(strings.TrimSpace(siteURL), "/"); siteURL != "" {
		u, err := url.Parse(siteURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			details = append(details, &huma.ErrorDetail{Location: "body.siteUrl", Message: "must be an http(s) URL such as https://skins.example.com", Value: siteURL})
		}
		changes["site.url"], _ = json.Marshal(siteURL)
	}
	if len(details) > 0 {
		return nil, huma.Error422UnprocessableEntity("invalid site settings", details...)
	}
	return changes, nil
}

// apiError maps account validation errors to 422 with one detail per field.
func apiError(err error) error {
	if fields := account.FieldErrors(err); len(fields) > 0 {
		details := make([]error, 0, len(fields))
		for _, f := range fields {
			details = append(details, &huma.ErrorDetail{Location: "body." + f.Field, Message: f.Message})
		}
		return huma.Error422UnprocessableEntity("invalid input", details...)
	}
	return err
}
