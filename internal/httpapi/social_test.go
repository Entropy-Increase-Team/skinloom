package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeOIDC is an OpenID Connect provider whose code is the subject's claims (JSON).
func fakeOIDC(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
				"token_endpoint": srv.URL + "/token", "token_endpoint_auth_methods_supported": []string{"client_secret_post"}})
		case "/token":
			if r.FormValue("client_secret") != "secret" || r.FormValue("code_verifier") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var c struct{ Claims, Nonce string }
			_ = json.Unmarshal([]byte(r.FormValue("code")), &c)
			claims := map[string]any{"iss": srv.URL, "aud": "skinloom", "exp": time.Now().Add(time.Hour).Unix(), "nonce": c.Nonce}
			_ = json.Unmarshal([]byte(c.Claims), &claims)
			payload, _ := json.Marshal(claims)
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "x", "id_token": "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// socialRequest is a browser request carrying the flow cookie too.
func socialRequest(b *browser, method, path, body, flow string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CSRFHeader, "1")
	if b.cookie != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: b.cookie})
	}
	if flow != "" {
		req.AddCookie(&http.Cookie{Name: SocialCookie, Value: flow})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			b.cookie = c.Value
		}
	}
	return rec
}

func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestSocialSignIn(t *testing.T) {
	d := newTestDeps(t)
	idp := fakeOIDC(t)
	setSettings(t, d, `{"captcha.provider": "none", "auth.registrations_per_ip": 0, "auth.social.providers": [
		{"id": "corp", "type": "oidc", "name": "Corp SSO", "issuer": "`+idp.URL+`", "clientId": "skinloom", "clientSecret": "secret"}]}`)
	h, _ := New(d)
	root := &browser{t: t, h: h}
	setupSite(t, d, root)

	// No site.url, no callback URL: no providers.
	var boot Bootstrap
	_ = json.Unmarshal(root.do("GET", "/api/v1/site/bootstrap", "").Body.Bytes(), &boot)
	if len(boot.Auth.Social) != 0 {
		t.Fatalf("without site.url: %+v", boot.Auth.Social)
	}
	if rec := root.do("POST", "/api/v1/auth/social/corp/start", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("start without site.url: %d", rec.Code)
	}
	setSettings(t, d, `{"site.url": "https://skins.example.com"}`)
	_ = json.Unmarshal(root.do("GET", "/api/v1/site/bootstrap", "").Body.Bytes(), &boot)
	if len(boot.Auth.Social) != 1 || boot.Auth.Social[0] != (SocialProviderInfo{ID: "corp", Name: "Corp SSO", Type: "oidc"}) {
		t.Fatalf("providers: %+v", boot.Auth.Social)
	}
	if strings.Contains(root.do("GET", "/api/v1/site/bootstrap", "").Body.String(), "secret") {
		t.Fatal("the client secret is public")
	}

	// signIn starts a sign-in in b and comes back from the provider as claims.
	signIn := func(b *browser, startPath, claims string) *httptest.ResponseRecorder {
		t.Helper()
		rec := socialRequest(b, "POST", startPath, `{"next":"/skinlib","remember":true}`, "")
		var out SocialRedirect
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		flow := cookieOf(rec, SocialCookie)
		if rec.Code != http.StatusOK || flow == nil || !flow.HttpOnly || flow.Path != "/api/v1/auth/social" {
			t.Fatalf("start: %d %s", rec.Code, rec.Body)
		}
		u, _ := url.Parse(out.URL)
		q := u.Query()
		if !strings.HasPrefix(out.URL, idp.URL+"/authorize?") || q.Get("redirect_uri") != "https://skins.example.com/api/v1/auth/social/corp/callback" {
			t.Fatalf("authorize url: %s", out.URL)
		}
		code, _ := json.Marshal(map[string]string{"Claims": claims, "Nonce": q.Get("nonce")})
		return socialRequest(b, "GET", "/api/v1/auth/social/corp/callback?state="+url.QueryEscape(q.Get("state"))+"&code="+url.QueryEscape(string(code)), "", flow.Value)
	}

	// A next too long to keep is dropped, not refused (the sign-in page's next can be
	// a long consent URL).
	long := `{"next":"/user/oauth/authorize?state=` + strings.Repeat("x", 600) + `"}`
	if rec := socialRequest(&browser{t: t, h: h}, "POST", "/api/v1/auth/social/corp/start", long, ""); rec.Code != http.StatusOK {
		t.Fatalf("long next: %d %s", rec.Code, rec.Body)
	}

	// Without the CSRF header no sign-in starts.
	if rec := app(t, h, "POST", "/api/v1/auth/social/corp/start", "application/json", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("start without the header: %d", rec.Code)
	}

	// Someone new.
	alice := &browser{t: t, h: h}
	rec := signIn(alice, "/api/v1/auth/social/corp/start", `{"sub":"1","email":"alice@example.com","email_verified":true,"name":"Alice"}`)
	signup := cookieOf(rec, SocialCookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/social" || signup == nil || signup.Value == "" || alice.cookie != "" {
		t.Fatalf("callback for someone new: %d %v", rec.Code, rec.Header())
	}
	var pending SocialPending
	rec = socialRequest(alice, "GET", "/api/v1/auth/social/pending", "", signup.Value)
	_ = json.Unmarshal(rec.Body.Bytes(), &pending)
	if rec.Code != http.StatusOK || pending.ProviderName != "Corp SSO" || pending.Email != "alice@example.com" || !pending.EmailVerified ||
		pending.Nickname != "Alice" || pending.Registration != "open" || pending.Next != "/skinlib" {
		t.Fatalf("pending: %d %s", rec.Code, rec.Body)
	}
	if rec := socialRequest(alice, "GET", "/api/v1/auth/social/pending", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("no flow cookie: %d", rec.Code)
	}
	rec = socialRequest(alice, "POST", "/api/v1/auth/social/pending/register", `{"email":"alice@example.com","nickname":"Alice"}`, signup.Value)
	var me UserView
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if rec.Code != http.StatusOK || alice.cookie == "" || me.HasPassword || !me.EmailVerified {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	// Setting a first password needs no current one; she can then sign in with it.
	if rec := alice.do("POST", "/api/v1/me/password", `{"currentPassword":"","newPassword":"alice-pass-1"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("first password: %d %s", rec.Code, rec.Body)
	}
	if rec := alice.do("POST", "/api/v1/me/password", `{"currentPassword":"","newPassword":"alice-pass-2"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second password without the current one: %d", rec.Code)
	}

	// Signing in again: straight to where she was going.
	again := &browser{t: t, h: h}
	rec = signIn(again, "/api/v1/auth/social/corp/start", `{"sub":"1"}`)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/skinlib" || again.cookie == "" {
		t.Fatalf("sign in: %d %v", rec.Code, rec.Header())
	}
	if c := cookieOf(rec, SessionCookie); c.MaxAge <= 0 {
		t.Fatalf("remember me: %+v", c)
	}
	rec = again.do("GET", "/api/v1/me/social", "")
	var links SocialLinks
	_ = json.Unmarshal(rec.Body.Bytes(), &links)
	if len(links.Links) != 1 || links.Links[0].ProviderName != "Corp SSO" || len(links.Providers) != 1 {
		t.Fatalf("links: %s", rec.Body)
	}

	// The administrator links an account from the profile page.
	rec = signIn(root, "/api/v1/me/social/corp", `{"sub":"2"}`)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/user/profile?social_linked=corp" {
		t.Fatalf("link: %d %v", rec.Code, rec.Header())
	}
	if rec := root.do("POST", "/api/v1/me/social/corp", ""); rec.Code != http.StatusConflict {
		t.Fatalf("second link: %d", rec.Code)
	}
	if rec := root.do("DELETE", "/api/v1/me/social/corp", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unlink: %d %s", rec.Code, rec.Body)
	}
	rec = signIn(root, "/api/v1/me/social/corp", `{"sub":"1"}`)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/user/profile?social_error=taken" {
		t.Fatalf("someone else's: %d %v", rec.Code, rec.Header())
	}

	// A pending sign-in claimed by signing in with a password.
	bob := registerUser(t, h, "bob@example.com")
	rec = signIn(&browser{t: t, h: h}, "/api/v1/auth/social/corp/start", `{"sub":"4","email":"bob@example.com"}`)
	flow := cookieOf(rec, SocialCookie).Value
	if rec := socialRequest(&browser{t: t, h: h}, "POST", "/api/v1/auth/social/pending/register", `{"email":"bob@example.com","nickname":"B"}`, flow); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("taken address: %d %s", rec.Code, rec.Body)
	}
	if rec := socialRequest(bob, "POST", "/api/v1/auth/social/pending/link", "", flow); rec.Code != http.StatusOK {
		t.Fatalf("link pending: %d %s", rec.Code, rec.Body)
	}
	if rec := signIn(&browser{t: t, h: h}, "/api/v1/auth/social/corp/start", `{"sub":"4"}`); rec.Header().Get("Location") != "/skinlib" {
		t.Fatalf("bob signs in: %v", rec.Header())
	}

	// Failures land on the sign-in page.
	if rec := socialRequest(&browser{t: t, h: h}, "GET", "/api/v1/auth/social/corp/callback?state=x&code=y", "", "x"); rec.Header().Get("Location") != "/auth/login?social_error=expired" {
		t.Fatalf("unknown state: %v", rec.Header())
	}
	rec = signIn(&browser{t: t, h: h}, "/api/v1/auth/social/corp/start", `{"sub":""}`)
	if rec.Header().Get("Location") != "/auth/login?social_error=failed" {
		t.Fatalf("bad id token: %v", rec.Header())
	}

	// Registration closed: the pending account can still be linked, not created.
	setSettings(t, d, `{"auth.registration": "closed"}`)
	rec = signIn(&browser{t: t, h: h}, "/api/v1/auth/social/corp/start", `{"sub":"5"}`)
	flow = cookieOf(rec, SocialCookie).Value
	if rec := socialRequest(&browser{t: t, h: h}, "POST", "/api/v1/auth/social/pending/register", `{"email":"e@example.com","nickname":"E"}`, flow); rec.Code != http.StatusForbidden {
		t.Fatalf("closed: %d", rec.Code)
	}

	if body := root.do("GET", "/api/v1/admin/audit?action=account.", "").Body.String(); !strings.Contains(body, "account.social_linked") || !strings.Contains(body, "account.social_unlinked") {
		t.Fatalf("audit: %s", body)
	}
}
