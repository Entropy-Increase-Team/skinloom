package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/social"
)

// SocialCookie ties a sign-in with a provider to the browser that started it.
const SocialCookie = "skinloom_social"

const socialCookiePath = "/api/v1/auth/social"

// socialStarts bounds sign-ins started per client (IPv6: per /64) in ten minutes;
// each one is a database row until it is used or cleaned up.
var socialStarts = account.NewLimiter(30, 10*time.Minute)

// SocialProviderInfo is a sign-in provider as shown on the sign-in page.
type SocialProviderInfo struct {
	ID   string `json:"id" example:"github"`
	Name string `json:"name" example:"GitHub"`
	Type string `json:"type" enum:"github,microsoft,oidc"`
}

func socialProviders(p social.Policy) []SocialProviderInfo {
	out := make([]SocialProviderInfo, 0, len(p.Providers))
	for _, c := range p.Providers {
		out = append(out, SocialProviderInfo{ID: c.ID, Name: c.DisplayName(), Type: c.Type})
	}
	return out
}

// SocialLinkView is a provider account linked to the user.
type SocialLinkView struct {
	Provider     string     `json:"provider"`
	ProviderName string     `json:"providerName" doc:"Empty when the provider is no longer configured"`
	ProviderType string     `json:"providerType" doc:"github, microsoft, oidc; empty when no longer configured"`
	Name         string     `json:"name" doc:"The account at the provider (a login or an email)"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
}

// SocialLinks are the user's linked accounts and the providers they can link.
type SocialLinks struct {
	Links     []SocialLinkView     `json:"links"`
	Providers []SocialProviderInfo `json:"providers"`
}

// SocialPending is a provider account that is not linked to any account yet.
type SocialPending struct {
	Provider      string `json:"provider"`
	ProviderName  string `json:"providerName"`
	ProviderType  string `json:"providerType"`
	Name          string `json:"name" doc:"The account at the provider"`
	Email         string `json:"email" doc:"May be empty"`
	EmailVerified bool   `json:"emailVerified" doc:"Keeping this address makes it verified without a mail"`
	Nickname      string `json:"nickname" doc:"Suggested nickname"`
	// Registration says whether a new account can be created; otherwise the person can
	// only link the provider account to an account they sign in to.
	Registration string    `json:"registration" enum:"open,closed"`
	Next         string    `json:"next" doc:"Where to go once signed in; empty = /user"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// SocialRedirect is where to send the browser.
type SocialRedirect struct {
	URL string `json:"url" doc:"The provider's sign-in page"`
}

type socialRedirectOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      SocialRedirect
}

type socialStartInput struct {
	UserAgent string `header:"User-Agent"`
	Provider  string `path:"provider" maxLength:"32"`
	Body      struct {
		// Longer than social.CleanNext keeps is dropped there, not refused: the sign-in
		// page's next can be a long consent URL.
		Next     string `json:"next,omitempty" maxLength:"8192" doc:"A path on this site to go to afterwards; default /user. Ignored when longer than 512 bytes"`
		Remember bool   `json:"remember,omitempty" doc:"Keep the session for 30 days"`
	}
}

type socialProviderInput struct {
	Provider string `path:"provider" maxLength:"64"`
}

type externalAuthVerifyInput struct {
	UserAgent string `header:"User-Agent"`
	Session   string `cookie:"skinloom_session"`
	Provider  string `path:"provider" maxLength:"40"`
	Body      struct {
		Input    map[string]any `json:"input,omitempty" doc:"Provider-specific credentials or callback payload passed to the plugin verifier"`
		Next     string         `json:"next,omitempty" maxLength:"512" doc:"Optional post-login redirect path on this site"`
		Remember bool           `json:"remember,omitempty" doc:"Keep the session for 30 days"`
		Link     bool           `json:"link,omitempty" doc:"Link the verified external identity to the currently signed-in account"`
	}
}

// ExternalAuthVerifyResult reports the outcome of verifying a plugin auth provider payload.
type ExternalAuthVerifyResult struct {
	Status string    `json:"status" enum:"signed_in,signup_required,linked"`
	User   *UserView `json:"user,omitempty"`
	Next   string    `json:"next"`
}

type externalAuthVerifyOutput struct {
	SetCookies []http.Cookie `header:"Set-Cookie"`
	Body       ExternalAuthVerifyResult
}

type socialPendingInput struct {
	Cookie string `cookie:"skinloom_social"`
}

type socialRegisterInput struct {
	UserAgent string `header:"User-Agent"`
	Session   string `cookie:"skinloom_session"`
	Cookie    string `cookie:"skinloom_social"`
	Body      struct {
		Email    string         `json:"email" maxLength:"255"`
		Nickname string         `json:"nickname" maxLength:"255"`
		Form     map[string]any `json:"form,omitempty" doc:"Optional plugin extension form fields"`
	}
}

func socialFlowCookie(value string, ttl time.Duration, ri reqInfo) http.Cookie {
	c := http.Cookie{Name: SocialCookie, Value: value, Path: socialCookiePath, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: ri.client.Proto == "https", MaxAge: int(ttl / time.Second)}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

func socialError(err error) error {
	var fe *social.FlowError
	switch {
	case errors.Is(err, social.ErrUnknownProvider):
		return huma.Error404NotFound("no such sign-in provider")
	case errors.Is(err, social.ErrNoFlow):
		return huma.Error404NotFound("no sign-in in progress, or it has expired; start again")
	case errors.Is(err, social.ErrAlreadyLinked):
		return huma.Error409Conflict("an account of this provider is already linked; unlink it first")
	case errors.Is(err, social.ErrLinkedElsewhere):
		return huma.Error409Conflict("this provider account is linked to another user")
	case errors.Is(err, social.ErrLastSignIn):
		return huma.Error409Conflict("set a password before unlinking your only way to sign in")
	case errors.Is(err, social.ErrNotLinked):
		return huma.Error404NotFound("no account of this provider is linked")
	case errors.As(err, &fe) && fe.Code == social.CodeFailed:
		return huma.Error502BadGateway("the sign-in provider could not be reached; try again later")
	}
	return err
}

func registerSocial(api huma.API, d Deps) {
	op := func(id, method, path, summary, desc string, status int, tags ...string) huma.Operation {
		o := huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary, Description: desc,
			Tags: tags, DefaultStatus: status}
		if tags[0] == "me" {
			o.Security = sessionSecurity
		}
		return o
	}
	start := func(ctx context.Context, provider string, userID *int64, next string, remember bool) (*socialRedirectOutput, error) {
		ri := requestInfo(ctx)
		client := account.ClientKey(ri.client.ClientIP)
		if wait, blocked := socialStarts.Take(client); blocked {
			return nil, rateLimitError(&account.RateLimitError{RetryAfter: wait}, "too many sign-ins started, try again later")
		}
		redirect, cookie, err := d.Social.Start(ctx, social.PolicyFrom(d.Settings.Current()),
			social.StartInput{Provider: provider, UserID: userID, Next: next, Remember: remember})
		if err != nil {
			return nil, socialError(err)
		}
		return &socialRedirectOutput{SetCookie: socialFlowCookie(cookie, social.FlowTTL, ri), Body: SocialRedirect{URL: redirect}}, nil
	}

	huma.Register(api, op("startSocialLogin", http.MethodPost, "/api/v1/auth/social/{provider}/start", "Sign in with a provider",
		"Returns the provider's sign-in page to send the browser to. The provider sends it back to "+
			"/api/v1/auth/social/{provider}/callback, which signs in (or, for someone new, leads to /auth/social).", 0, "auth"),
		func(ctx context.Context, in *socialStartInput) (*socialRedirectOutput, error) {
			return start(ctx, in.Provider, nil, in.Body.Next, in.Body.Remember)
		})

	huma.Register(api, op("linkSocial", http.MethodPost, "/api/v1/me/social/{provider}", "Link a provider account",
		"Like startSocialLogin, but the callback links the provider account to yours and returns to /user/profile.", 0, "me"),
		func(ctx context.Context, in *socialProviderInput) (*socialRedirectOutput, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return start(ctx, in.Provider, &v.User.ID, "", false)
		})

	huma.Register(api, op("getSocialPending", http.MethodGet, "/api/v1/auth/social/pending", "The provider account waiting for an account",
		"After a sign-in with a provider account no site account is linked to (cookie "+SocialCookie+").", 0, "auth"),
		func(ctx context.Context, in *socialPendingInput) (*struct{ Body SocialPending }, error) {
			f, err := d.Social.Pending(ctx, in.Cookie)
			if err != nil {
				return nil, socialError(err)
			}
			snap := d.Settings.Current()
			name, typ := resolveProviderLabel(d, social.PolicyFrom(snap), f.Provider)
			return &struct{ Body SocialPending }{SocialPending{Provider: f.Provider, ProviderName: name, ProviderType: typ, Name: f.Name,
				Email: f.Email, EmailVerified: f.EmailVerified, Nickname: f.Nickname,
				Registration: snap.String("auth.registration"), Next: f.Next, ExpiresAt: f.ExpiresAt}}, nil
		})

	huma.Register(api, op("registerSocial", http.MethodPost, "/api/v1/auth/social/pending/register", "Create an account for the provider account",
		"The account has no password until you set one. Keeping the address the provider verified makes it verified. "+
			"Needs auth.registration = open; counts against auth.registrations_per_ip.", 0, "auth"),
		func(ctx context.Context, in *socialRegisterInput) (*userOutput, error) {
			ri := requestInfo(ctx)
			snap := d.Settings.Current()
			policy, _ := account.PoliciesFrom(snap, captcha.ConfigFrom(snap).Enabled())
			u, sess, _, err := d.Social.Register(ctx, social.PolicyFrom(snap),
				social.RegisterInput{Cookie: in.Cookie, Email: in.Body.Email, Nickname: in.Body.Nickname, Form: in.Body.Form},
				policy, mailContext(d, ri), ri.meta(in.UserAgent))
			switch {
			case errors.Is(err, account.ErrRegistrationClosed):
				return nil, huma.Error403Forbidden("registration is closed")
			case errors.Is(err, account.ErrSetupPending):
				return nil, huma.Error403Forbidden("the site has not been set up yet")
			case isRateLimit(err):
				return nil, rateLimitError(err, "too many accounts registered from this address, try again later")
			case errors.Is(err, social.ErrNoFlow), errors.Is(err, social.ErrUnknownProvider), errors.Is(err, social.ErrLinkedElsewhere):
				return nil, socialError(err)
			case err != nil:
				return nil, apiError(err)
			}
			replaceSession(context.WithoutCancel(ctx), d, in.Session)
			return &userOutput{SetCookie: sessionCookie(sess, ri), Body: userViewWithBadges(ctx, d, u)}, nil
		})

	huma.Register(api, op("linkSocialPending", http.MethodPost, "/api/v1/auth/social/pending/link", "Link the provider account to yours",
		"For someone who signed in with a provider account no site account is linked to, then signed in with their password.", 0, "me"),
		func(ctx context.Context, in *socialPendingInput) (*struct{ Body SocialLinks }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := d.Social.LinkPending(ctx, in.Cookie, v.User.ID, requestInfo(ctx).meta("")); err != nil {
				return nil, socialError(err)
			}
			return socialLinks(ctx, d, v.User.ID)
		})

	huma.Register(api, op("listSocial", http.MethodGet, "/api/v1/me/social", "Your linked provider accounts", "", 0, "me"),
		func(ctx context.Context, _ *struct{}) (*struct{ Body SocialLinks }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return socialLinks(ctx, d, v.User.ID)
		})

	huma.Register(api, op("unlinkSocial", http.MethodDelete, "/api/v1/me/social/{provider}", "Unlink a provider account",
		"An account without a password cannot unlink its last provider account that is still configured (409).", http.StatusNoContent, "me"),
		func(ctx context.Context, in *socialProviderInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, socialError(d.Social.Unlink(ctx, social.PolicyFrom(d.Settings.Current()), v.User, in.Provider, requestInfo(ctx).meta("")))
		})

	huma.Register(api, op("verifyExternalAuth", http.MethodPost, "/api/v1/auth/providers/{provider}/verify", "Verify a plugin authentication provider",
		"Verifies credentials or a callback payload through an active plugin registered via ctx.auth.registerProvider, "+
			"then signs in (setting skinloom_session), links the current user, or opens a pending signup flow at /auth/social.", 0, "auth"),
		func(ctx context.Context, in *externalAuthVerifyInput) (*externalAuthVerifyOutput, error) {
			ri := requestInfo(ctx)
			if wait, blocked := socialStarts.Take(account.ClientKey(ri.client.ClientIP)); blocked {
				return nil, rateLimitError(&account.RateLimitError{RetryAfter: wait}, "too many sign-in attempts, try again later")
			}
			if d.Accounts == nil || d.Social == nil {
				return nil, huma.Error404NotFound("no such sign-in provider")
			}
			ap, ok := d.Accounts.LookupAuthProvider(in.Provider)
			if !ok || ap.Verify == nil {
				return nil, huma.Error404NotFound("no such sign-in provider")
			}
			var linkUID *int64
			if in.Body.Link {
				v, err := signedIn(ctx)
				if err != nil {
					return nil, err
				}
				linkUID = &v.User.ID
			}
			payload := in.Body.Input
			if payload == nil {
				payload = map[string]any{}
			}
			info, err := ap.Verify(ctx, payload)
			if err != nil {
				if rej, ok := hook.AsRejection(err); ok {
					return nil, huma.Error422UnprocessableEntity(rej.Reason)
				}
				return nil, huma.Error401Unauthorized("external authentication failed")
			}
			res, err := d.Social.CompleteExternal(ctx, social.ExternalAuthInput{
				Provider: ap.ID,
				Identity: social.Identity{
					Subject:       info.Subject,
					Email:         info.Email,
					EmailVerified: info.EmailVerified,
					Name:          cmp.Or(info.Nickname, info.Email),
					Nickname:      info.Nickname,
				},
				LinkUserID: linkUID,
				Next:       cmp.Or(info.Next, in.Body.Next),
				Remember:   in.Body.Remember,
				Meta:       ri.meta(in.UserAgent),
			})
			if err != nil {
				var fe *social.FlowError
				if errors.As(err, &fe) {
					switch fe.Code {
					case social.CodeDisabled:
						return nil, huma.Error403Forbidden("account is disabled")
					case social.CodeGone:
						return nil, huma.Error403Forbidden("account is deleted")
					case social.CodeTaken:
						return nil, huma.Error409Conflict("this provider account is linked to another user")
					case social.CodeAlready:
						return nil, huma.Error409Conflict("an account of this provider is already linked; unlink it first")
					}
				}
				return nil, socialError(err)
			}
			out := &externalAuthVerifyOutput{}
			switch {
			case res.Linked:
				out.Body = ExternalAuthVerifyResult{
					Status: "linked",
					Next:   cmp.Or(res.Next, "/user/profile"),
				}
			case res.Session != nil:
				replaceSession(context.WithoutCancel(ctx), d, in.Session)
				uv := userViewWithBadges(ctx, d, res.User)
				out.SetCookies = []http.Cookie{sessionCookie(res.Session, ri)}
				out.Body = ExternalAuthVerifyResult{
					Status: "signed_in",
					User:   &uv,
					Next:   cmp.Or(res.Next, "/user"),
				}
			default:
				out.SetCookies = []http.Cookie{socialFlowCookie(res.Signup, social.SignupTTL, ri)}
				out.Body = ExternalAuthVerifyResult{
					Status: "signup_required",
					Next:   "/auth/social",
				}
			}
			return out, nil
		})
}

func resolveProviderLabel(d Deps, p social.Policy, provider string) (name, typ string) {
	if c, ok := p.Provider(provider); ok {
		return c.DisplayName(), c.Type
	}
	if rest, ok := strings.CutPrefix(provider, social.PluginProviderPrefix); ok && d.Accounts != nil {
		if ap, exists := d.Accounts.LookupAuthProvider(rest); exists {
			return ap.Name, "plugin"
		}
	}
	return "", ""
}

func socialLinks(ctx context.Context, d Deps, userID int64) (*struct{ Body SocialLinks }, error) {
	links, err := d.Social.Links(ctx, userID)
	if err != nil {
		return nil, err
	}
	p := social.PolicyFrom(d.Settings.Current())
	out := SocialLinks{Links: make([]SocialLinkView, 0, len(links)), Providers: socialProviders(p)}
	for _, l := range links {
		out.Links = append(out.Links, socialLinkView(d, l, p))
	}
	return &struct{ Body SocialLinks }{out}, nil
}

func socialLinkView(d Deps, l *ent.SocialAccount, p social.Policy) SocialLinkView {
	v := SocialLinkView{Provider: l.Provider, Name: cmp.Or(l.Name, l.Email), CreatedAt: l.CreatedAt, LastUsedAt: l.LastUsedAt}
	v.ProviderName, v.ProviderType = resolveProviderLabel(d, p, l.Provider)
	return v
}

// mountSocial serves the callback providers send the browser back to. It always
// ends on a page of the site: signed in, the profile (linked), /auth/social (someone
// new) or the sign-in or profile page with ?social_error=<code>.
func mountSocial(r chi.Router, d Deps) {
	r.Get(socialCookiePath+"/{provider}/callback", func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		ri := requestInfo(ctx)
		q := req.URL.Query()
		in := social.CallbackInput{Provider: chi.URLParam(req, "provider"), State: q.Get("state"), Code: q.Get("code"),
			Error: q.Get("error"), Meta: ri.meta(req.UserAgent())}
		if c, err := req.Cookie(SocialCookie); err == nil {
			in.Cookie = c.Value
		}
		if v := ri.viewer(); v != nil {
			in.ViewerID = &v.User.ID
		}
		res, err := d.Social.Callback(ctx, social.PolicyFrom(d.Settings.Current()), in)
		w.Header().Set("Cache-Control", "no-store")
		go302 := func(target string) { http.Redirect(w, req, target, http.StatusFound) }
		var fe *social.FlowError
		switch {
		case errors.As(err, &fe):
			cookie := socialFlowCookie("", 0, ri)
			http.SetCookie(w, &cookie)
			page := "/auth/login"
			if fe.Link {
				page = "/user/profile"
			}
			go302(page + "?social_error=" + url.QueryEscape(fe.Code))
		case err != nil:
			d.Log.Error("social sign-in callback", "err", err)
			go302("/auth/login?social_error=" + social.CodeFailed)
		case res.Session != nil:
			cookie := socialFlowCookie("", 0, ri)
			http.SetCookie(w, &cookie)
			if old, err := req.Cookie(SessionCookie); err == nil {
				replaceSession(context.WithoutCancel(ctx), d, old.Value)
			}
			sess := sessionCookie(res.Session, ri)
			http.SetCookie(w, &sess)
			go302(cmp.Or(res.Next, "/user"))
		case res.Linked:
			cookie := socialFlowCookie("", 0, ri)
			http.SetCookie(w, &cookie)
			go302("/user/profile?social_linked=" + url.QueryEscape(in.Provider))
		default:
			cookie := socialFlowCookie(res.Signup, social.SignupTTL, ri)
			http.SetCookie(w, &cookie)
			go302("/auth/social")
		}
	})

	// Browser redirect callback for plugin-registered external auth providers
	// (ctx.auth.registerProvider with mode="redirect").
	r.Get("/api/v1/auth/providers/{provider}/callback", func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		ri := requestInfo(ctx)
		w.Header().Set("Cache-Control", "no-store")
		go302 := func(target string) { http.Redirect(w, req, target, http.StatusFound) }

		if _, blocked := socialStarts.Take(account.ClientKey(ri.client.ClientIP)); blocked {
			go302("/auth/login?social_error=" + social.CodeFailed)
			return
		}
		if d.Accounts == nil || d.Social == nil {
			go302("/auth/login?social_error=" + social.CodeFailed)
			return
		}
		provider := chi.URLParam(req, "provider")
		ap, ok := d.Accounts.LookupAuthProvider(provider)
		if !ok || ap.Verify == nil {
			go302("/auth/login?social_error=" + social.CodeFailed)
			return
		}
		q := req.URL.Query()
		if q.Get("error") == "access_denied" {
			go302("/auth/login?social_error=" + social.CodeDenied)
			return
		}
		input := make(map[string]any, len(q))
		for k, vals := range q {
			if len(vals) > 0 {
				input[k] = vals[0]
			}
		}
		info, err := ap.Verify(ctx, input)
		if err != nil {
			d.Log.Warn("plugin auth provider verify failed", "provider", provider, "err", err)
			go302("/auth/login?social_error=" + social.CodeFailed)
			return
		}
		remember := q.Get("remember") == "1" || q.Get("remember") == "true"
		res, err := d.Social.CompleteExternal(ctx, social.ExternalAuthInput{
			Provider: ap.ID,
			Identity: social.Identity{
				Subject:       info.Subject,
				Email:         info.Email,
				EmailVerified: info.EmailVerified,
				Name:          cmp.Or(info.Nickname, info.Email),
				Nickname:      info.Nickname,
			},
			Next:     cmp.Or(info.Next, q.Get("next")),
			Remember: remember,
			Meta:     ri.meta(req.UserAgent()),
		})
		var fe *social.FlowError
		switch {
		case errors.As(err, &fe):
			go302("/auth/login?social_error=" + url.QueryEscape(fe.Code))
		case err != nil:
			d.Log.Error("plugin auth callback completion failed", "provider", provider, "err", err)
			go302("/auth/login?social_error=" + social.CodeFailed)
		case res.Session != nil:
			if old, err := req.Cookie(SessionCookie); err == nil {
				replaceSession(context.WithoutCancel(ctx), d, old.Value)
			}
			sess := sessionCookie(res.Session, ri)
			http.SetCookie(w, &sess)
			go302(cmp.Or(res.Next, "/user"))
		default:
			cookie := socialFlowCookie(res.Signup, social.SignupTTL, ri)
			http.SetCookie(w, &cookie)
			go302("/auth/social")
		}
	})
}
