package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
)

// The OAuth 2.0 endpoints (/oauth/*) follow RFC 6749 rather than the /api/v1
// conventions, as applications written for Blessing Skin (Laravel Passport) expect:
// form or JSON bodies, {"error", "error_description"} errors, no CSRF header.

const oauthMaxBody = 64 << 10

// openCORS lets browser applications (public clients with PKCE) call an endpoint from
// any origin. These endpoints read no cookies, so this exposes nothing a server
// could not already fetch.
func openCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// discoveryCORS allows read-only cross-origin discovery/metadata fetches (GET, OPTIONS).
func discoveryCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Accept, Content-Type")
		h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		h.Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// oidcIssuerFromBase builds the canonical OIDC / OAuth 2.0 issuer URL (<siteBase>/oauth)
// from a pre-resolved siteBase. Hosting the issuer under /oauth places OIDC discovery at
// /oauth/.well-known/openid-configuration (OpenID Connect Discovery 1.0 §4), keeping it clear
// of panel-locked root `location /.well-known` ACME intercepts (e.g. BT Panel Docker sites)
// while preserving strict issuer equality.
func oidcIssuerFromBase(base string) string {
	b := strings.TrimRight(base, "/")
	if b == "" {
		return ""
	}
	return b + "/oauth"
}

func mountOAuth(r chi.Router, d Deps) {
	// The consent screen is a page of the frontend.
	r.Get("/oauth/authorize", func(w http.ResponseWriter, req *http.Request) {
		to := "/user/oauth/authorize"
		if req.URL.RawQuery != "" {
			to += "?" + req.URL.RawQuery
		}
		http.Redirect(w, req, to, http.StatusFound)
	})
	r.With(openCORS).Post("/oauth/token", func(w http.ResponseWriter, req *http.Request) {
		form, basic, err := oauthForm(w, req)
		if err != nil {
			oauthAnswer(w, d, nil, err, basic)
			return
		}
		snap := d.Settings.Current()
		in := oauth.TokenRequest{
			GrantType:    form.Get("grant_type"),
			Code:         form.Get("code"),
			RedirectURI:  form.Get("redirect_uri"),
			CodeVerifier: form.Get("code_verifier"),
			RefreshToken: form.Get("refresh_token"),
			DeviceCode:   form.Get("device_code"),
			Scope:        form.Get("scope"),
			ClientID:     form.Get("client_id"),
			ClientSecret: form.Get("client_secret"),
			Issuer:       oidcIssuerFromBase(siteBaseFrom(req, snap)),
		}
		out, err := d.OAuth.Token(req.Context(), in, oauth.PolicyFrom(snap))
		oauthAnswer(w, d, out, err, basic)
	})
	r.With(openCORS).Options("/oauth/token", func(http.ResponseWriter, *http.Request) {})
	r.With(openCORS).Post("/oauth/revoke", func(w http.ResponseWriter, req *http.Request) {
		form, basic, err := oauthForm(w, req)
		if err == nil {
			err = d.OAuth.Revoke(req.Context(), form.Get("client_id"), form.Get("client_secret"), form.Get("token"))
		}
		if err == nil {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			return
		}
		oauthAnswer(w, d, nil, err, basic)
	})
	r.With(openCORS).Options("/oauth/revoke", func(http.ResponseWriter, *http.Request) {})

	// OpenID Connect & Yggdrasil Connect protocol endpoints.
	r.With(openCORS).Get("/oauth/jwks", func(w http.ResponseWriter, req *http.Request) {
		if !oauth.PolicyFrom(d.Settings.Current()).EnableOIDC {
			http.NotFound(w, req)
			return
		}
		jwks, err := d.OAuth.JWKS(req.Context())
		if err != nil {
			oauthAnswer(w, d, nil, err, false)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	r.With(openCORS).Options("/oauth/jwks", func(http.ResponseWriter, *http.Request) {})

	serveUserInfo := func(w http.ResponseWriter, req *http.Request) {
		if !oauth.PolicyFrom(d.Settings.Current()).EnableOIDC {
			http.NotFound(w, req)
			return
		}
		token := oauthBearer(req)
		if token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="openid"`)
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusUnauthorized, Code: "invalid_token", Description: "Missing bearer token."}, false)
			return
		}
		info, err := d.OAuth.UserInfo(req.Context(), token, siteBase(req, d))
		switch {
		case errors.Is(err, oauth.ErrInvalidToken):
			w.Header().Set("WWW-Authenticate", `Bearer realm="openid", error="invalid_token"`)
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusUnauthorized, Code: "invalid_token", Description: "The access token is invalid, expired, or its bound profile changed."}, false)
			return
		case errors.Is(err, oauth.ErrInsufficientScope):
			w.Header().Set("WWW-Authenticate", `Bearer realm="openid", error="insufficient_scope", scope="openid"`)
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusForbidden, Code: "insufficient_scope", Description: "The openid scope is required."}, false)
			return
		case errors.Is(err, oauth.ErrBanned):
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusForbidden, Code: "access_denied", Description: "The account is banned."}, false)
			return
		case err != nil:
			oauthAnswer(w, d, nil, err, false)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(info)
	}
	r.With(openCORS).Get("/oauth/userinfo", serveUserInfo)
	r.With(openCORS).Post("/oauth/userinfo", serveUserInfo)
	r.With(openCORS).Options("/oauth/userinfo", func(http.ResponseWriter, *http.Request) {})

	r.With(openCORS).Post("/oauth/device_authorization", func(w http.ResponseWriter, req *http.Request) {
		if !d.OAuth.AllowDeviceRequest(requestInfo(req.Context()).client.ClientIP) {
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusTooManyRequests, Code: "slow_down", Description: "Too many device authorization requests from this address; try again later."}, false)
			return
		}
		form, basic, err := oauthForm(w, req)
		if err != nil {
			oauthAnswer(w, d, nil, err, basic)
			return
		}
		out, err := d.OAuth.RequestDeviceCode(req.Context(), form.Get("client_id"), form.Get("client_secret"),
			form.Get("scope"), siteBase(req, d), oauth.PolicyFrom(d.Settings.Current()))
		oauthAnswer(w, d, out, err, basic)
	})
	r.With(openCORS).Options("/oauth/device_authorization", func(http.ResponseWriter, *http.Request) {})

	r.With(openCORS).Post("/oauth/register", func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, oauthMaxBody))
		if err != nil {
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_request", Description: "Malformed request body."}, false)
			return
		}
		var in oauth.DCRRequest
		if err := json.Unmarshal(body, &in); err != nil {
			oauthAnswer(w, d, nil, &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_client_metadata", Description: "Invalid JSON body."}, false)
			return
		}
		out, err := d.OAuth.RegisterDynamicClient(req.Context(), in, requestInfo(req.Context()).client.ClientIP, oauth.PolicyFrom(d.Settings.Current()))
		if err != nil {
			oauthAnswer(w, d, nil, err, false)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(out)
	})
	r.With(openCORS).Options("/oauth/register", func(http.ResponseWriter, *http.Request) {})

	discoveryDoc := func(req *http.Request, snap *settings.Snapshot, oidc bool) map[string]any {
		pol := oauth.PolicyFrom(snap)
		base := siteBaseFrom(req, snap)
		names := make([]string, 0, len(oauth.Scopes))
		for _, s := range oauth.Scopes {
			if !pol.EnableOIDC && s.OIDC {
				continue
			}
			names = append(names, s.Name)
		}
		grants := []string{"authorization_code", "refresh_token"}
		if pol.EnableOIDC {
			grants = append(grants, oauth.DeviceGrantType)
		}
		doc := map[string]any{
			"issuer":                                oidcIssuerFromBase(base),
			"authorization_endpoint":                base + "/oauth/authorize",
			"token_endpoint":                        base + "/oauth/token",
			"revocation_endpoint":                   base + "/oauth/revoke",
			"scopes_supported":                      names,
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 grants,
			"code_challenge_methods_supported":      []string{"S256", "plain"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		}
		if pol.EnableOIDC {
			doc["device_authorization_endpoint"] = base + "/oauth/device_authorization"
			doc["jwks_uri"] = base + "/oauth/jwks"
			doc["userinfo_endpoint"] = base + "/oauth/userinfo"
			if pol.EnableDCR {
				doc["registration_endpoint"] = base + "/oauth/register"
			}
		}
		if oidc {
			doc["subject_types_supported"] = []string{"public"}
			doc["id_token_signing_alg_values_supported"] = []string{"RS256"}
			doc["claims_supported"] = []string{
				"sub", "iss", "aud", "exp", "iat", "auth_time", "nonce",
				"name", "nickname", "preferred_username", "picture",
				"email", "email_verified", "selectedProfile", "availableProfiles",
			}
		}
		return doc
	}
	setDiscoveryCache := func(w http.ResponseWriter, snap *settings.Snapshot) {
		if strings.TrimSpace(snap.String("site.url")) != "" {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		} else {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Vary", "Host, X-Forwarded-Host, X-Forwarded-Proto")
		}
	}
	requireOIDC := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !oauth.PolicyFrom(d.Settings.Current()).EnableOIDC {
				http.NotFound(w, req)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
	serveOAuthMeta := func(w http.ResponseWriter, req *http.Request) {
		snap := d.Settings.Current()
		w.Header().Set("Content-Type", "application/json")
		setDiscoveryCache(w, snap)
		_ = json.NewEncoder(w).Encode(discoveryDoc(req, snap, false))
	}
	serveOIDCConfig := func(w http.ResponseWriter, req *http.Request) {
		snap := d.Settings.Current()
		if !oauth.PolicyFrom(snap).EnableOIDC {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		setDiscoveryCache(w, snap)
		_ = json.NewEncoder(w).Encode(discoveryDoc(req, snap, true))
	}
	// Canonical discovery endpoints for issuer <siteBase>/oauth:
	//   - OIDC Discovery 1.0 §4: /oauth/.well-known/openid-configuration
	//   - RFC 8414 §3 path-insertion: /.well-known/oauth-authorization-server/oauth (+ /.well-known/openid-configuration/oauth)
	//   - /oauth/.well-known/oauth-authorization-server (co-located path-scoped metadata)
	// Root /.well-known/* aliases are kept as best-effort fallbacks for non-strict clients;
	// strict OIDC / RFC 8414 §3.3 clients MUST configure <siteBase>/oauth as their issuer.
	for _, p := range []string{
		"/oauth/.well-known/oauth-authorization-server",
		"/.well-known/oauth-authorization-server/oauth",
		"/.well-known/oauth-authorization-server",
	} {
		r.With(discoveryCORS).Get(p, serveOAuthMeta)
		r.With(discoveryCORS).Options(p, func(http.ResponseWriter, *http.Request) {})
	}
	for _, p := range []string{
		"/oauth/.well-known/openid-configuration",
		"/.well-known/openid-configuration/oauth",
		"/.well-known/openid-configuration",
	} {
		r.With(requireOIDC, discoveryCORS).Get(p, serveOIDCConfig)
		r.With(requireOIDC, discoveryCORS).Options(p, func(http.ResponseWriter, *http.Request) {})
	}
}

// oauthForm reads a token or revocation request: a form (RFC 6749) or, as Laravel
// accepted, JSON. Client credentials may come in an HTTP Basic header instead.
func oauthForm(w http.ResponseWriter, r *http.Request) (url.Values, bool, error) {
	bad := &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_request", Description: "Malformed request body."}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, oauthMaxBody))
	if err != nil {
		return nil, false, bad
	}
	form := url.Values{}
	ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ctype == "application/json" {
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, false, bad
		}
		for k, v := range m {
			switch x := v.(type) {
			case string:
				form.Set(k, x)
			case float64:
				form.Set(k, strconv.FormatFloat(x, 'f', -1, 64)) // client_id as a number
			}
		}
	} else if form, err = url.ParseQuery(string(body)); err != nil {
		return nil, false, bad
	}
	id, secret, basic := r.BasicAuth()
	if basic {
		// RFC 6749 §2.3.1: both are form-encoded before Basic encoding.
		if id, err = url.QueryUnescape(id); err == nil {
			secret, err = url.QueryUnescape(secret)
		}
		if err != nil || (form.Get("client_id") != "" && form.Get("client_id") != id) {
			return nil, true, &oauth.Error{Status: http.StatusUnauthorized, Code: "invalid_client", Description: "Client authentication failed"}
		}
		form.Set("client_id", id)
		form.Set("client_secret", secret)
	}
	return form, basic, nil
}

// oauthAnswer writes a token endpoint answer: the tokens, or an RFC 6749 §5.2 error
// (with Laravel's "message" too, which Passport clients read).
func oauthAnswer(w http.ResponseWriter, d Deps, out any, err error, basic bool) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
	if err == nil {
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	var oe *oauth.Error
	if !errors.As(err, &oe) {
		d.Log.Error("oauth token endpoint", "err", err)
		oe = &oauth.Error{Status: http.StatusInternalServerError, Code: "server_error", Description: "The server could not answer."}
	}
	if oe.Status == http.StatusUnauthorized && basic {
		h.Set("WWW-Authenticate", `Basic realm="oauth"`)
	}
	w.WriteHeader(oe.Status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": oe.Code, "error_description": oe.Description, "message": oe.Description})
}

// ---- The account API: consent, applications, grants, personal access tokens ----

// OAuthClientRef is an application as users see it.
type OAuthClientRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Owner string `json:"owner" doc:"The nickname of who registered it"`
}

// OAuthClientView is one of your applications.
type OAuthClientView struct {
	ID           int64     `json:"id" doc:"The client_id"`
	Name         string    `json:"name"`
	RedirectURIs []string  `json:"redirectUris"`
	Confidential bool      `json:"confidential" doc:"Has a secret; public clients must use PKCE"`
	CreatedAt    time.Time `json:"createdAt"`
}

func oauthClientView(c *ent.OAuthClient) OAuthClientView {
	return OAuthClientView{ID: c.ID, Name: c.Name, RedirectURIs: c.RedirectUris, Confidential: c.SecretHash != nil, CreatedAt: c.CreatedAt}
}

// OAuthClientList is your applications.
type OAuthClientList struct {
	Clients []OAuthClientView `json:"clients"`
}

// OAuthClientSecret is a new application or secret: the secret is shown only now.
type OAuthClientSecret struct {
	Client OAuthClientView `json:"client"`
	Secret string          `json:"secret" doc:"Empty for a public client"`
}

// OAuthGrantView is an application you allowed.
type OAuthGrantView struct {
	Client    OAuthClientRef `json:"client"`
	Scopes    []string       `json:"scopes"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt" doc:"Last allowed"`
}

// OAuthGrantList is the applications you allowed.
type OAuthGrantList struct {
	Grants []OAuthGrantView `json:"grants"`
}

// PersonalTokenView is one of your personal access tokens.
type PersonalTokenView struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt" doc:"null: never"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func personalTokenView(t *ent.OAuthToken) PersonalTokenView {
	return PersonalTokenView{ID: t.ID, Name: t.Name, Scopes: t.Scopes, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt}
}

// PersonalTokenList is your personal access tokens.
type PersonalTokenList struct {
	Tokens []PersonalTokenView `json:"tokens"`
}

// NewPersonalToken is a created token: the token itself is shown only now.
type NewPersonalToken struct {
	Info  PersonalTokenView `json:"info"`
	Token string            `json:"token"`
}

// OAuthScopes are the scopes applications and tokens can be given.
type OAuthScopes struct {
	Scopes []oauth.Scope `json:"scopes"`
}

// AuthorizeInfo answers a look at an authorization request, for the consent screen:
// exactly one of error, redirect or client is set.
type AuthorizeInfo struct {
	Error             string               `json:"error,omitempty" doc:"The request is broken and cannot go back to the application: show this"`
	Redirect          string               `json:"redirect,omitempty" doc:"Send the browser here (an error the application handles)"`
	Client            *OAuthClientRef      `json:"client,omitempty"`
	RedirectURI       string               `json:"redirectUri,omitempty"`
	Scopes            []string             `json:"scopes,omitempty" doc:"What would be granted"`
	Granted           bool                 `json:"granted" doc:"You already allowed all of it: approve without asking"`
	NeedsPlayerSelect bool                 `json:"needsPlayerSelect,omitempty" doc:"The application asks you to pick a Minecraft character"`
	Players           []oauth.PlayerOption `json:"players,omitempty" doc:"Your characters when needsPlayerSelect is true"`
}

// AuthorizeResult is where to send the browser after approving or refusing.
type AuthorizeResult struct {
	Redirect string `json:"redirect"`
}

type authorizeQuery struct {
	ResponseType        string `query:"response_type" maxLength:"32"`
	ClientID            string `query:"client_id" maxLength:"20"`
	RedirectURI         string `query:"redirect_uri" maxLength:"2048"`
	Scope               string `query:"scope" maxLength:"1024"`
	State               string `query:"state" maxLength:"1024"`
	Nonce               string `query:"nonce" maxLength:"256"`
	CodeChallenge       string `query:"code_challenge" maxLength:"128"`
	CodeChallengeMethod string `query:"code_challenge_method" maxLength:"8"`
}

func (q authorizeQuery) request() oauth.AuthRequest {
	return oauth.AuthRequest{ResponseType: q.ResponseType, ClientID: q.ClientID, RedirectURI: q.RedirectURI, Scope: q.Scope,
		State: q.State, Nonce: q.Nonce, CodeChallenge: q.CodeChallenge, CodeChallengeMethod: q.CodeChallengeMethod}
}

type approveInput struct {
	Body struct {
		ResponseType        string `json:"response_type" maxLength:"32"`
		ClientID            string `json:"client_id" maxLength:"20"`
		RedirectURI         string `json:"redirect_uri,omitempty" maxLength:"2048"`
		Scope               string `json:"scope,omitempty" maxLength:"1024"`
		State               string `json:"state,omitempty" maxLength:"1024"`
		Nonce               string `json:"nonce,omitempty" maxLength:"256"`
		CodeChallenge       string `json:"code_challenge,omitempty" maxLength:"128"`
		CodeChallengeMethod string `json:"code_challenge_method,omitempty" maxLength:"8"`
		PlayerID            *int64 `json:"playerId,omitempty"`
		Allow               bool   `json:"allow"`
	}
}

// DeviceCodeInfo describes a pending RFC 8628 device authorization for the consent UI.
type DeviceCodeInfo struct {
	Error             string               `json:"error,omitempty"`
	Client            *OAuthClientRef      `json:"client,omitempty"`
	Scopes            []string             `json:"scopes,omitempty"`
	NeedsPlayerSelect bool                 `json:"needsPlayerSelect,omitempty"`
	Players           []oauth.PlayerOption `json:"players,omitempty"`
	ExpiresAt         *time.Time           `json:"expiresAt,omitempty"`
}

type deviceLookupQuery struct {
	UserCode string `query:"user_code" maxLength:"32"`
}

type deviceApproveInput struct {
	Body struct {
		UserCode string `json:"user_code" maxLength:"32"`
		PlayerID *int64 `json:"playerId,omitempty"`
		Allow    bool   `json:"allow"`
	}
}

type deviceApproveResult struct {
	Approved bool `json:"approved"`
}

type clientIDInput struct {
	ID int64 `path:"id"`
}

type createClientInput struct {
	Body struct {
		Name         string   `json:"name" maxLength:"256"`
		RedirectURIs []string `json:"redirectUris" maxItems:"10"`
		Confidential bool     `json:"confidential" doc:"A server-side application that keeps a secret (false: browser or native app, PKCE)"`
	}
}

type updateClientInput struct {
	ID   int64 `path:"id"`
	Body struct {
		Name         *string  `json:"name,omitempty" maxLength:"256"`
		RedirectURIs []string `json:"redirectUris,omitempty" maxItems:"10"`
	}
}

type createTokenInput struct {
	Body struct {
		Name          string   `json:"name" maxLength:"256"`
		Scopes        []string `json:"scopes" maxItems:"32"`
		ExpiresInDays int      `json:"expiresInDays" minimum:"0" maximum:"366" doc:"0: never"`
	}
}

type tokenIDInput struct {
	ID int64 `path:"id"`
}

func oauthError(err error) error {
	if errors.Is(err, oauth.ErrNotFound) {
		return huma.Error404NotFound("not found")
	}
	return apiError(err)
}

func registerOAuthAccount(api huma.API, d Deps) {
	op := func(id, method, path, summary string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{"oauth"}, Security: sessionSecurity, DefaultStatus: status}
	}

	huma.Register(api, huma.Operation{OperationID: "listOAuthScopes", Method: http.MethodGet, Path: "/api/v1/oauth/scopes",
		Summary: "Scopes applications and personal access tokens can be given", Tags: []string{"oauth"}},
		func(context.Context, *struct{}) (*struct{ Body OAuthScopes }, error) {
			return &struct{ Body OAuthScopes }{OAuthScopes{oauth.Scopes}}, nil
		})

	huma.Register(api, op("getAuthorization", http.MethodGet, "/api/v1/oauth/authorize", "Check an authorization request for the consent screen", 0),
		func(ctx context.Context, in *authorizeQuery) (*struct{ Body AuthorizeInfo }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			p, err := d.OAuth.Prepare(ctx, v.User, in.request(), oauth.PolicyFrom(d.Settings.Current()))
			var re *oauth.RequestError
			switch {
			case errors.As(err, &re):
				return &struct{ Body AuthorizeInfo }{AuthorizeInfo{Error: re.Description}}, nil
			case oauth.RedirectOf(err) != "":
				return &struct{ Body AuthorizeInfo }{AuthorizeInfo{Redirect: oauth.RedirectOf(err)}}, nil
			case err != nil:
				return nil, err
			}
			return &struct{ Body AuthorizeInfo }{AuthorizeInfo{
				Client:            &OAuthClientRef{ID: p.Client.ID, Name: p.Client.Name, Owner: p.Owner},
				RedirectURI:       p.RedirectURI,
				Scopes:            p.Scopes,
				Granted:           p.Granted,
				NeedsPlayerSelect: p.NeedsPlayerSelect,
				Players:           p.Players,
			}}, nil
		})

	huma.Register(api, op("authorize", http.MethodPost, "/api/v1/oauth/authorize", "Allow or refuse an application", 0),
		func(ctx context.Context, in *approveInput) (*struct{ Body AuthorizeResult }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			b := in.Body
			req := oauth.AuthRequest{
				ResponseType:        b.ResponseType,
				ClientID:            b.ClientID,
				RedirectURI:         b.RedirectURI,
				Scope:               b.Scope,
				State:               b.State,
				Nonce:               b.Nonce,
				CodeChallenge:       b.CodeChallenge,
				CodeChallengeMethod: b.CodeChallengeMethod,
				PlayerID:            b.PlayerID,
			}
			to, p, err := d.OAuth.Approve(ctx, v.User, req, b.Allow, oauth.PolicyFrom(d.Settings.Current()))
			var re *oauth.RequestError
			switch {
			case errors.As(err, &re):
				return nil, huma.Error400BadRequest(re.Description)
			case oauth.RedirectOf(err) != "":
				return &struct{ Body AuthorizeResult }{AuthorizeResult{oauth.RedirectOf(err)}}, nil
			case err != nil:
				return nil, apiError(err)
			}
			if b.Allow {
				if err := d.Accounts.Audit(ctx, &v.User.ID, "oauth.authorize", "oauth_client", strconv.FormatInt(p.Client.ID, 10),
					requestInfo(ctx).client.ClientIP, map[string]any{"scopes": p.Scopes}); err != nil {
					d.Log.Warn("audit", "action", "oauth.authorize", "err", err)
				}
			}
			return &struct{ Body AuthorizeResult }{AuthorizeResult{to}}, nil
		})

	huma.Register(api, op("lookupDeviceCode", http.MethodGet, "/api/v1/oauth/device", "Look up an RFC 8628 device authorization user_code", 0),
		func(ctx context.Context, in *deviceLookupQuery) (*struct{ Body DeviceCodeInfo }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			key := strconv.FormatInt(v.User.ID, 10) + "|" + requestInfo(ctx).client.ClientIP
			if !d.OAuth.AllowDeviceLookup(key) {
				return nil, huma.Error429TooManyRequests("Too many device code attempts; please try again later.")
			}
			prep, err := d.OAuth.LookupDeviceCode(ctx, v.User, in.UserCode, oauth.PolicyFrom(d.Settings.Current()))
			var re *oauth.RequestError
			if errors.As(err, &re) {
				return &struct{ Body DeviceCodeInfo }{DeviceCodeInfo{Error: re.Description}}, nil
			}
			if err != nil {
				return nil, err
			}
			exp := prep.ExpiresAt
			return &struct{ Body DeviceCodeInfo }{DeviceCodeInfo{
				Client:            &OAuthClientRef{ID: prep.Client.ID, Name: prep.Client.Name, Owner: prep.Owner},
				Scopes:            prep.Scopes,
				NeedsPlayerSelect: prep.NeedsPlayerSelect,
				Players:           prep.Players,
				ExpiresAt:         &exp,
			}}, nil
		})

	huma.Register(api, op("approveDeviceCode", http.MethodPost, "/api/v1/oauth/device", "Approve or deny an RFC 8628 device authorization", 0),
		func(ctx context.Context, in *deviceApproveInput) (*struct{ Body deviceApproveResult }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			key := strconv.FormatInt(v.User.ID, 10) + "|" + requestInfo(ctx).client.ClientIP
			if !d.OAuth.AllowDeviceLookup(key) {
				return nil, huma.Error429TooManyRequests("Too many device code attempts; please try again later.")
			}
			prep, err := d.OAuth.ApproveDeviceCode(ctx, v.User, in.Body.UserCode, in.Body.Allow, in.Body.PlayerID, oauth.PolicyFrom(d.Settings.Current()))
			var re *oauth.RequestError
			if errors.As(err, &re) {
				return nil, huma.Error400BadRequest(re.Description)
			}
			if err != nil {
				return nil, apiError(err)
			}
			if in.Body.Allow {
				if err := d.Accounts.Audit(ctx, &v.User.ID, "oauth.device_authorize", "oauth_client", strconv.FormatInt(prep.Client.ID, 10),
					requestInfo(ctx).client.ClientIP, map[string]any{"scopes": prep.Scopes}); err != nil {
					d.Log.Warn("audit", "action", "oauth.device_authorize", "err", err)
				}
			}
			return &struct{ Body deviceApproveResult }{deviceApproveResult{Approved: in.Body.Allow}}, nil
		})

	huma.Register(api, op("listOAuthClients", http.MethodGet, "/api/v1/me/oauth/clients", "Your applications", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body OAuthClientList }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, err := d.OAuth.Clients(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			out := OAuthClientList{Clients: make([]OAuthClientView, len(list))}
			for i, c := range list {
				out.Clients[i] = oauthClientView(c)
			}
			return &struct{ Body OAuthClientList }{out}, nil
		})

	huma.Register(api, op("createOAuthClient", http.MethodPost, "/api/v1/me/oauth/clients", "Register an application", http.StatusCreated),
		func(ctx context.Context, in *createClientInput) (*struct{ Body OAuthClientSecret }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			c, secret, err := d.OAuth.CreateClient(ctx, v.User.ID, oauth.ClientInput{Name: &in.Body.Name, RedirectURIs: in.Body.RedirectURIs}, in.Body.Confidential)
			if err != nil {
				return nil, oauthError(err)
			}
			return &struct{ Body OAuthClientSecret }{OAuthClientSecret{Client: oauthClientView(c), Secret: secret}}, nil
		})

	huma.Register(api, op("updateOAuthClient", http.MethodPatch, "/api/v1/me/oauth/clients/{id}", "Rename an application or change its redirect URIs", 0),
		func(ctx context.Context, in *updateClientInput) (*struct{ Body OAuthClientView }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			c, err := d.OAuth.UpdateClient(ctx, v.User.ID, in.ID, oauth.ClientInput{Name: in.Body.Name, RedirectURIs: in.Body.RedirectURIs})
			if err != nil {
				return nil, oauthError(err)
			}
			return &struct{ Body OAuthClientView }{oauthClientView(c)}, nil
		})

	huma.Register(api, op("resetOAuthClientSecret", http.MethodPost, "/api/v1/me/oauth/clients/{id}/secret", "Give an application a new secret", 0),
		func(ctx context.Context, in *clientIDInput) (*struct{ Body OAuthClientSecret }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			secret, err := d.OAuth.ResetSecret(ctx, v.User.ID, in.ID)
			if err != nil {
				return nil, oauthError(err)
			}
			list, err := d.OAuth.Clients(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			out := OAuthClientSecret{Secret: secret}
			for _, c := range list {
				if c.ID == in.ID {
					out.Client = oauthClientView(c)
				}
			}
			return &struct{ Body OAuthClientSecret }{out}, nil
		})

	huma.Register(api, op("deleteOAuthClient", http.MethodDelete, "/api/v1/me/oauth/clients/{id}", "Delete an application", http.StatusNoContent),
		func(ctx context.Context, in *clientIDInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, oauthError(d.OAuth.DeleteClient(ctx, v.User.ID, in.ID))
		})

	huma.Register(api, op("listOAuthGrants", http.MethodGet, "/api/v1/me/oauth/grants", "Applications you allowed", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body OAuthGrantList }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, err := d.OAuth.Grants(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			out := OAuthGrantList{Grants: make([]OAuthGrantView, len(list))}
			for i, g := range list {
				out.Grants[i] = OAuthGrantView{Client: OAuthClientRef{ID: g.Client.ID, Name: g.Client.Name, Owner: g.Owner},
					Scopes: g.Scopes, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
			}
			return &struct{ Body OAuthGrantList }{out}, nil
		})

	huma.Register(api, op("revokeOAuthGrant", http.MethodDelete, "/api/v1/me/oauth/grants/{id}", "Take an application's access away", http.StatusNoContent),
		func(ctx context.Context, in *clientIDInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, oauthError(d.OAuth.RevokeGrant(ctx, v.User.ID, in.ID))
		})

	huma.Register(api, op("listPersonalTokens", http.MethodGet, "/api/v1/me/tokens", "Your personal access tokens", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body PersonalTokenList }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, err := d.OAuth.PersonalTokens(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			out := PersonalTokenList{Tokens: make([]PersonalTokenView, len(list))}
			for i, t := range list {
				out.Tokens[i] = personalTokenView(t)
			}
			return &struct{ Body PersonalTokenList }{out}, nil
		})

	huma.Register(api, op("createPersonalToken", http.MethodPost, "/api/v1/me/tokens", "Create a personal access token", http.StatusCreated),
		func(ctx context.Context, in *createTokenInput) (*struct{ Body NewPersonalToken }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			row, token, err := d.OAuth.CreatePersonalToken(ctx, v.User, in.Body.Name, in.Body.Scopes, time.Duration(in.Body.ExpiresInDays)*24*time.Hour)
			if err != nil {
				return nil, oauthError(err)
			}
			if err := d.Accounts.Audit(ctx, &v.User.ID, "token.create", "oauth_token", strconv.FormatInt(row.ID, 10),
				requestInfo(ctx).client.ClientIP, map[string]any{"scopes": row.Scopes}); err != nil {
				d.Log.Warn("audit", "action", "token.create", "err", err)
			}
			return &struct{ Body NewPersonalToken }{NewPersonalToken{Info: personalTokenView(row), Token: token}}, nil
		})

	huma.Register(api, op("deletePersonalToken", http.MethodDelete, "/api/v1/me/tokens/{id}", "Revoke a personal access token", http.StatusNoContent),
		func(ctx context.Context, in *tokenIDInput) (*struct{}, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			return nil, oauthError(d.OAuth.DeletePersonalToken(ctx, v.User.ID, in.ID))
		})
}

// oauthBearer reads "Authorization: Bearer <token>".
func oauthBearer(r *http.Request) string {
	hdr := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(hdr) < 7 || !strings.EqualFold(hdr[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(hdr[7:])
}
