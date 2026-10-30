package social

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/socialaccount"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

var meta = account.RequestMeta{IP: "192.0.2.1", UserAgent: "test"}

// fakeIdP is an OpenID Connect provider: each sign-in returns the claims set last.
type fakeIdP struct {
	srv      *httptest.Server
	clientID string
	secret   string

	mu    sync.Mutex
	codes map[string]map[string]any // code -> claims, with the PKCE challenge
	// Overrides of the ID token claims, to test their checks.
	tamper func(map[string]any)
}

func newIdP(t *testing.T) *fakeIdP {
	f := &fakeIdP{clientID: "skinloom", secret: "s3cret", codes: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"userinfo_endpoint": f.srv.URL + "/userinfo", "token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		id, secret, ok := r.BasicAuth()
		if !ok || id != f.clientID || secret != f.secret {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		f.mu.Lock()
		c, found := f.codes[r.FormValue("code")]
		delete(f.codes, r.FormValue("code"))
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if !found || base64.RawURLEncoding.EncodeToString(sum[:]) != c["challenge"] || r.FormValue("redirect_uri") != c["redirect_uri"] {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		claims := map[string]any{"iss": f.srv.URL, "aud": f.clientID, "exp": time.Now().Add(time.Hour).Unix(), "nonce": c["nonce"]}
		for k, v := range c["claims"].(map[string]any) {
			claims[k] = v
		}
		if f.tamper != nil {
			f.tamper(claims)
		}
		payload, _ := json.Marshal(claims)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-" + c["claims"].(map[string]any)["sub"].(string),
			"id_token": "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer at-")
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": sub, "email": sub + "@userinfo.example", "email_verified": "true"})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// authorize plays the person signing in at the provider: it returns the code the
// provider would send back for the sign-in URL.
func (f *fakeIdP) authorize(t *testing.T, redirect string, claims map[string]any) (state, code string) {
	t.Helper()
	u, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != f.clientID || q.Get("code_challenge_method") != "S256" || !strings.Contains(q.Get("scope"), "openid") {
		t.Fatalf("authorize request: %s", redirect)
	}
	code = account.NewToken()
	f.mu.Lock()
	f.codes[code] = map[string]any{"challenge": q.Get("code_challenge"), "nonce": q.Get("nonce"),
		"redirect_uri": q.Get("redirect_uri"), "claims": claims}
	f.mu.Unlock()
	return q.Get("state"), code
}

type env struct {
	t        *testing.T
	s        *Service
	accounts *account.Service
	idp      *fakeIdP
	policy   Policy
	db       *ent.Client
}

func newEnv(t *testing.T, d *db.DB) *env {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	accounts := account.NewService(d.Ent, log)
	ctx := context.Background()
	// A shared server database keeps the setup marker of the previous test.
	if _, err := d.Ent.SystemState.Delete().Exec(ctx); err != nil {
		t.Fatal(err)
	}
	token, _ := accounts.NewSetupToken(ctx)
	if _, _, err := accounts.CompleteSetup(ctx, account.SetupInput{Token: token, Email: "admin@example.com", Nickname: "Admin", Password: "admin-pass-1"}, meta); err != nil {
		t.Fatal(err)
	}
	idp := newIdP(t)
	return &env{t: t, s: NewService(d.Ent, accounts, log), accounts: accounts, idp: idp, db: d.Ent, policy: Policy{
		SiteURL:   "https://skins.example.com",
		Providers: []ProviderConfig{{ID: "corp", Type: TypeOIDC, Issuer: idp.srv.URL, ClientID: idp.clientID, ClientSecret: idp.secret}},
	}}
}

// signIn runs a whole sign-in (or link, with userID) with the claims.
func (e *env) signIn(userID *int64, claims map[string]any) (*Result, error) {
	e.t.Helper()
	ctx := context.Background()
	redirect, cookie, err := e.s.Start(ctx, e.policy, StartInput{Provider: "corp", UserID: userID, Next: "/skinlib", Remember: true})
	if err != nil {
		e.t.Fatal(err)
	}
	state, code := e.idp.authorize(e.t, redirect, claims)
	if state != cookie {
		e.t.Fatal("the state is the flow cookie")
	}
	return e.s.Callback(ctx, e.policy, CallbackInput{Provider: "corp", State: state, Code: code, Cookie: cookie, ViewerID: userID, Meta: meta})
}

func flowCode(err error) string {
	var fe *FlowError
	if errors.As(err, &fe) {
		return fe.Code
	}
	return ""
}

func TestSignUpAndSignIn(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		e, ctx := newEnv(t, d), context.Background()
		rp := account.RegisterPolicy{Open: true, InitialScore: 100}
		claims := map[string]any{"sub": "u-1", "email": "Alice@Example.com", "email_verified": true, "name": "Alice", "preferred_username": "alice"}

		res, err := e.signIn(nil, claims)
		if err != nil || res.Signup == "" || res.Next != "/skinlib" {
			t.Fatalf("someone new: %+v %v", res, err)
		}
		f, err := e.s.Pending(ctx, res.Signup)
		if err != nil || f.Email != "Alice@Example.com" || !f.EmailVerified || f.Nickname != "Alice" || f.Name != "alice" || !f.Remember {
			t.Fatalf("pending: %+v %v", f, err)
		}
		if _, err := e.s.Pending(ctx, "nope"); !errors.Is(err, ErrNoFlow) {
			t.Fatalf("unknown cookie: %v", err)
		}
		// Registration closed: no account, the pending sign-in stays for linking.
		if _, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: f.Email, Nickname: "Alice"}, account.RegisterPolicy{}, account.MailContext{}, meta); !errors.Is(err, account.ErrRegistrationClosed) {
			t.Fatalf("closed: %v", err)
		}
		if _, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: f.Email, Nickname: ""}, rp, account.MailContext{}, meta); len(account.FieldErrors(err)) == 0 {
			t.Fatalf("no nickname: %v", err)
		}
		u, sess, next, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: "alice@example.com", Nickname: "Alice"}, rp, account.MailContext{}, meta)
		if err != nil || sess == nil || !sess.Persistent || next != "/skinlib" {
			t.Fatalf("register: %v", err)
		}
		if u.EmailVerifiedAt == nil || account.HasPassword(u) || u.Score != 100 {
			t.Fatalf("the provider's verified address is verified, no password: %+v", u)
		}
		if _, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: "a2@example.com", Nickname: "A"}, rp, account.MailContext{}, meta); !errors.Is(err, ErrNoFlow) {
			t.Fatalf("the pending sign-in works once: %v", err)
		}
		// Without a password, a password login fails like a wrong one.
		if _, _, err := e.accounts.Login(ctx, account.LoginInput{Email: "alice@example.com", Password: "anything-1"}, account.LoginPolicy{}, meta); !errors.Is(err, account.ErrInvalidCredentials) {
			t.Fatalf("password login: %v", err)
		}

		// Next time: signed in at once.
		res, err = e.signIn(nil, claims)
		if err != nil || res.User == nil || res.User.ID != u.ID || res.Session == nil {
			t.Fatalf("sign in again: %+v %v", res, err)
		}
		acct, _ := e.db.SocialAccount.Query().Where(socialaccount.UserID(u.ID)).Only(ctx)
		if acct.LastUsedAt == nil || acct.Name != "alice" {
			t.Fatalf("link: %+v", acct)
		}

		// Another address than the provider's is not verified.
		res, _ = e.signIn(nil, map[string]any{"sub": "u-2", "email": "bob@example.com", "email_verified": true, "name": "Bob"})
		bob, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: "robert@example.com", Nickname: "Bob"}, rp, account.MailContext{}, meta)
		if err != nil || bob.EmailVerifiedAt != nil {
			t.Fatalf("changed address: %+v %v", bob, err)
		}

		// The address of an existing account: taken.
		res, _ = e.signIn(nil, map[string]any{"sub": "u-3", "email": "carol@example.com", "email_verified": false})
		if _, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: "Alice@example.com", Nickname: "C"}, rp, account.MailContext{}, meta); len(account.FieldErrors(err)) == 0 {
			t.Fatalf("taken address: %v", err)
		}

		// A banned account cannot sign in.
		_ = e.db.User.UpdateOneID(u.ID).SetStatus(user.StatusBanned).Exec(ctx)
		if _, err := e.signIn(nil, claims); flowCode(err) != CodeDisabled {
			t.Fatalf("banned: %v", err)
		}
		// Deleted within the grace period: signing in restores it, like a password login.
		_ = e.db.User.UpdateOneID(u.ID).SetStatus(user.StatusDeleted).SetDeletedAt(time.Now()).Exec(ctx)
		if res, err := e.signIn(nil, claims); err != nil || res.Session == nil {
			t.Fatalf("deleted within the grace period: %v", err)
		}
		if got, _ := e.db.User.Get(ctx, u.ID); got.Status != user.StatusActive {
			t.Fatalf("restored: %s", got.Status)
		}
		_ = e.db.User.UpdateOneID(u.ID).SetStatus(user.StatusDeleted).SetDeletedAt(time.Now().Add(-account.DeleteGrace - time.Hour)).Exec(ctx)
		if _, err := e.signIn(nil, claims); flowCode(err) != CodeGone {
			t.Fatalf("deleted for good: %v", err)
		}
	})
}

func TestLinkByVerifiedEmail(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		e, ctx := newEnv(t, d), context.Background()
		admin, _ := e.db.User.Query().Only(ctx)
		_ = e.db.User.UpdateOneID(admin.ID).ClearEmailVerifiedAt().Exec(ctx)

		// The administrator's address is not verified: someone else could have
		// registered it, so no automatic link.
		claims := map[string]any{"sub": "a-1", "email": "ADMIN@example.com", "email_verified": true}
		if res, err := e.signIn(nil, claims); err != nil || res.Signup == "" {
			t.Fatalf("unverified account: %+v %v", res, err)
		}
		_ = e.db.User.UpdateOneID(admin.ID).SetEmailVerifiedAt(time.Now()).Exec(ctx)
		// The provider did not verify it: no link either.
		if res, err := e.signIn(nil, map[string]any{"sub": "a-1", "email": "admin@example.com", "email_verified": false}); err != nil || res.Signup == "" {
			t.Fatalf("unverified at the provider: %+v %v", res, err)
		}
		// Both verified: the same person.
		res, err := e.signIn(nil, claims)
		if err != nil || res.User == nil || res.User.ID != admin.ID {
			t.Fatalf("both verified: %+v %v", res, err)
		}
		// email_verified as a string, and the address from userinfo when the ID token
		// has none.
		res, err = e.signIn(nil, map[string]any{"sub": "u-9"})
		if err != nil || res.Signup == "" {
			t.Fatal(err)
		}
		if f, _ := e.s.Pending(ctx, res.Signup); f.Email != "u-9@userinfo.example" || !f.EmailVerified {
			t.Fatalf("userinfo: %+v", f)
		}
	})
}

func TestLinkAndUnlink(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		e, ctx := newEnv(t, d), context.Background()
		rp := account.RegisterPolicy{Open: true}
		res, _ := e.signIn(nil, map[string]any{"sub": "s-1", "email": "x@example.com"})
		alice, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: "alice@example.com", Nickname: "Alice"}, rp, account.MailContext{}, meta)
		if err != nil {
			t.Fatal(err)
		}
		admin, _ := e.db.User.Query().Where(user.RoleEQ(user.RoleSuperadmin)).Only(ctx)

		// The administrator links a provider account: someone else's fails.
		if _, err := e.signIn(&admin.ID, map[string]any{"sub": "s-1"}); flowCode(err) != CodeTaken {
			t.Fatalf("someone else's: %v", err)
		}
		res, err = e.signIn(&admin.ID, map[string]any{"sub": "s-2", "preferred_username": "root"})
		if err != nil || !res.Linked {
			t.Fatalf("link: %+v %v", res, err)
		}
		if _, _, err := e.s.Start(ctx, e.policy, StartInput{Provider: "corp", UserID: &admin.ID}); !errors.Is(err, ErrAlreadyLinked) {
			t.Fatalf("a second account of the provider: %v", err)
		}
		// Signed in as someone else by the time the provider sends the browser back.
		_ = e.s.Unlink(ctx, e.policy, admin, "corp", meta)
		redirect, cookie, _ := e.s.Start(ctx, e.policy, StartInput{Provider: "corp", UserID: &admin.ID})
		state, code := e.idp.authorize(t, redirect, map[string]any{"sub": "s-2"})
		if _, err := e.s.Callback(ctx, e.policy, CallbackInput{Provider: "corp", State: state, Code: code, Cookie: cookie, ViewerID: &alice.ID, Meta: meta}); flowCode(err) != CodeExpired {
			t.Fatalf("other viewer: %v", err)
		}

		// A pending sign-in linked after signing in with a password.
		res, _ = e.signIn(nil, map[string]any{"sub": "s-3"})
		if _, err := e.s.LinkPending(ctx, res.Signup, admin.ID, meta); err != nil {
			t.Fatal(err)
		}
		if _, err := e.s.LinkPending(ctx, res.Signup, admin.ID, meta); !errors.Is(err, ErrNoFlow) {
			t.Fatalf("twice: %v", err)
		}
		if res, err := e.signIn(nil, map[string]any{"sub": "s-3"}); err != nil || res.User.ID != admin.ID {
			t.Fatalf("sign in with the linked account: %v", err)
		}

		// Alice has no password: her only link stays.
		if err := e.s.Unlink(ctx, e.policy, alice, "corp", meta); !errors.Is(err, ErrLastSignIn) {
			t.Fatalf("last way to sign in: %v", err)
		}
		if err := e.s.Unlink(ctx, e.policy, alice, "github", meta); !errors.Is(err, ErrNotLinked) {
			t.Fatalf("not linked: %v", err)
		}
		// A link to a provider the site no longer offers is no way to sign in: it does
		// not let her unlink the last working one, and it can always be removed.
		oldLink := func() {
			e.db.SocialAccount.Create().SetProvider("old").SetSubject("o-1").SetUserID(alice.ID).ExecX(ctx)
		}
		oldLink()
		if err := e.s.Unlink(ctx, e.policy, alice, "corp", meta); !errors.Is(err, ErrLastSignIn) {
			t.Fatalf("only a removed provider left: %v", err)
		}
		if err := e.s.Unlink(ctx, e.policy, alice, "old", meta); err != nil {
			t.Fatalf("removing a removed provider's link: %v", err)
		}
		oldLink()
		withOld := e.policy
		withOld.Providers = append([]ProviderConfig{{ID: "old", Type: TypeGitHub, ClientID: "c", ClientSecret: "s"}}, e.policy.Providers...)
		if err := e.s.Unlink(ctx, withOld, alice, "corp", meta); err != nil {
			t.Fatalf("another configured provider left: %v", err)
		}
		// Two unlinks at once leave one link.
		res, _ = e.signIn(nil, map[string]any{"sub": "s-9"})
		bob, _, _, err := e.s.Register(ctx, e.policy, RegisterInput{Cookie: res.Signup, Email: "bob@example.com", Nickname: "Bob"}, rp, account.MailContext{}, meta)
		if err != nil {
			t.Fatal(err)
		}
		e.db.SocialAccount.Create().SetProvider("old").SetSubject("o-2").SetUserID(bob.ID).ExecX(ctx)
		var wg sync.WaitGroup
		for _, prov := range []string{"corp", "old"} {
			wg.Add(1)
			go func() { defer wg.Done(); _ = e.s.Unlink(ctx, withOld, bob, prov, meta) }()
		}
		wg.Wait()
		if links, _ := e.s.Links(ctx, bob.ID); len(links) != 1 {
			t.Fatalf("concurrent unlinks left %d links", len(links))
		}
		if err := e.s.Unlink(ctx, e.policy, admin, "corp", meta); err != nil {
			t.Fatalf("with a password: %v", err)
		}
		if links, _ := e.s.Links(ctx, admin.ID); len(links) != 0 {
			t.Fatalf("links: %v", links)
		}
	})
}

func TestCallbackChecks(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		e, ctx := newEnv(t, d), context.Background()
		start := func() (string, string) {
			redirect, cookie, err := e.s.Start(ctx, e.policy, StartInput{Provider: "corp"})
			if err != nil {
				t.Fatal(err)
			}
			state, code := e.idp.authorize(t, redirect, map[string]any{"sub": "c-1"})
			if state != cookie {
				t.Fatal("state")
			}
			return state, code
		}
		cb := func(in CallbackInput) error {
			in.Meta = meta
			if in.Provider == "" {
				in.Provider = "corp"
			}
			_, err := e.s.Callback(ctx, e.policy, in)
			return err
		}

		state, code := start()
		if err := cb(CallbackInput{State: state, Code: code, Cookie: "another-browser"}); flowCode(err) != CodeExpired {
			t.Fatalf("cookie mismatch: %v", err)
		}
		if err := cb(CallbackInput{State: state, Code: code}); flowCode(err) != CodeExpired {
			t.Fatalf("no cookie: %v", err)
		}
		if err := cb(CallbackInput{State: state, Code: code, Cookie: state}); err != nil {
			t.Fatalf("the real one: %v", err)
		}
		if err := cb(CallbackInput{State: state, Code: code, Cookie: state}); flowCode(err) != CodeExpired {
			t.Fatalf("used twice: %v", err)
		}
		state, code = start()
		if err := cb(CallbackInput{Provider: "other", State: state, Code: code, Cookie: state}); flowCode(err) != CodeExpired {
			t.Fatalf("another provider's callback: %v", err)
		}
		state, _ = start()
		if err := cb(CallbackInput{State: state, Error: "access_denied", Cookie: state}); flowCode(err) != CodeDenied {
			t.Fatalf("cancelled: %v", err)
		}
		state, code = start()
		e.s.now = func() time.Time { return time.Now().Add(FlowTTL + time.Minute) }
		if err := cb(CallbackInput{State: state, Code: code, Cookie: state}); flowCode(err) != CodeExpired {
			t.Fatalf("too late: %v", err)
		}
		if n, _ := e.s.Cleanup(ctx); n != 0 {
			t.Fatalf("cleanup after spending: %d", n)
		}
		e.s.now = time.Now
		start()
		e.s.now = func() time.Time { return time.Now().Add(FlowTTL + time.Minute) }
		if n, _ := e.s.Cleanup(ctx); n != 1 {
			t.Fatalf("cleanup: %d", n)
		}
		e.s.now = time.Now

		for name, tamper := range map[string]func(map[string]any){
			"issuer":   func(c map[string]any) { c["iss"] = "https://evil.example" },
			"audience": func(c map[string]any) { c["aud"] = "someone-else" },
			"azp":      func(c map[string]any) { c["aud"] = []string{e.idp.clientID, "other"} },
			"expired":  func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
			"nonce":    func(c map[string]any) { c["nonce"] = "replayed" },
			"subject":  func(c map[string]any) { c["sub"] = "" },
		} {
			e.idp.tamper = tamper
			state, code := start()
			if err := cb(CallbackInput{State: state, Code: code, Cookie: state}); flowCode(err) != CodeFailed {
				t.Fatalf("%s: %v", name, err)
			}
		}
		e.idp.tamper = nil
		e.policy.Providers[0].ClientSecret = "wrong"
		state, code = start()
		if err := cb(CallbackInput{State: state, Code: code, Cookie: state}); flowCode(err) != CodeFailed {
			t.Fatalf("wrong secret: %v", err)
		}
		if _, _, err := e.s.Start(ctx, e.policy, StartInput{Provider: "nope"}); !errors.Is(err, ErrUnknownProvider) {
			t.Fatalf("unknown provider: %v", err)
		}
	})
}

func TestGitHub(t *testing.T) {
	var verifier string
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.FormValue("client_secret") != "gh-secret" || r.FormValue("code") != "good" {
			_, _ = w.Write([]byte(`{"error":"bad_verification_code","error_description":"The code passed is incorrect"}`))
			return
		}
		verifier = r.FormValue("code_verifier")
		_, _ = w.Write([]byte(`{"access_token":"gho_x","token_type":"bearer"}`))
	})
	emails := `[{"email":"old@example.com","primary":false,"verified":true},{"email":"octo@example.com","primary":true,"verified":true}]`
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gho_x" || r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":583231,"login":"octocat","name":"","email":"public@example.com"}`))
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, _ *http.Request) {
		if emails == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(emails))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newClient()
	c.githubWeb, c.githubAPI = srv.URL, srv.URL
	p := ProviderConfig{ID: "github", Type: TypeGitHub, ClientID: "gh", ClientSecret: "gh-secret"}
	ctx := context.Background()
	eps, _ := c.endpoints(ctx, p)
	if u := c.authURL(p, eps, "https://s/cb", "st", "ch", "n"); !strings.HasPrefix(u, srv.URL+"/login/oauth/authorize?") || !strings.Contains(u, "scope=read%3Auser+user%3Aemail") {
		t.Fatalf("authorize: %s", u)
	}
	id, err := c.identity(ctx, p, eps, "https://s/cb", "good", "the-verifier", "")
	if err != nil || id.Subject != "583231" || id.Email != "octo@example.com" || !id.EmailVerified || id.Name != "octocat" || id.Nickname != "octocat" || verifier != "the-verifier" {
		t.Fatalf("identity: %+v %v", id, err)
	}
	emails = "" // user:email not granted: the public address, unverified
	if id, err := c.identity(ctx, p, eps, "https://s/cb", "good", "v", ""); err != nil || id.Email != "public@example.com" || id.EmailVerified {
		t.Fatalf("without user:email: %+v %v", id, err)
	}
	if _, err := c.identity(ctx, p, eps, "https://s/cb", "bad", "v", ""); err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Fatalf("bad code: %v", err)
	}
}

func TestMicrosoft(t *testing.T) {
	var claims map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token") || r.FormValue("client_secret") != "ms-secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(claims)
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "x", "id_token": "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newClient()
	c.microsoftLogin = srv.URL
	ctx := context.Background()
	work := "72f988bf-86f1-41af-91ab-2d7cd011db47"
	signIn := func(tenant, tid string, edov bool) (*Identity, error) {
		p := ProviderConfig{ID: "ms", Type: TypeMicrosoft, ClientID: "app", ClientSecret: "ms-secret", Tenant: tenant}
		eps, _ := c.endpoints(ctx, p)
		claims = map[string]any{"iss": srv.URL + "/" + tid + "/v2.0", "aud": "app", "exp": time.Now().Add(time.Hour).Unix(), "nonce": "n",
			"sub": "pairwise-sub", "tid": tid, "email": "someone@example.com", "name": "Some One", "xms_edov": edov}
		return c.identity(ctx, p, eps, "https://s/cb", "code", "v", "n")
	}
	if id, err := signIn("", microsoftConsumers, false); err != nil || !id.EmailVerified || id.Nickname != "Some One" {
		t.Fatalf("personal account: %+v %v", id, err)
	}
	// nOAuth: a work account's email claim is whatever its administrator typed in.
	if id, err := signIn("common", work, false); err != nil || id.EmailVerified {
		t.Fatalf("work account: %+v %v", id, err)
	}
	if id, err := signIn("organizations", work, true); err != nil || !id.EmailVerified {
		t.Fatalf("work account, verified domain: %+v %v", id, err)
	}
	for _, tc := range []struct{ tenant, tid string }{{"consumers", work}, {"organizations", microsoftConsumers}, {"11111111-2222-3333-4444-555555555555", work}} {
		if _, err := signIn(tc.tenant, tc.tid, false); err == nil {
			t.Fatalf("tenant %s let %s in", tc.tenant, tc.tid)
		}
	}
	if _, err := signIn(work, work, false); err != nil {
		t.Fatalf("own tenant: %v", err)
	}
}

func TestProviderSettings(t *testing.T) {
	for _, bad := range []string{
		`{}`,
		`[{"id":"GitHub","type":"github","clientId":"a","clientSecret":"b"}]`,
		`[{"id":"pending","type":"github","clientId":"a","clientSecret":"b"}]`,
		`[{"id":"gh","type":"github","clientId":"a"}]`,
		`[{"id":"gh","type":"gitlab","clientId":"a","clientSecret":"b"}]`,
		`[{"id":"o","type":"oidc","clientId":"a","issuer":"http://idp.example.com"}]`,
		`[{"id":"o","type":"oidc","clientId":"a","issuer":"https://idp.example.com","scopes":["a b"]}]`,
		`[{"id":"gh","type":"github","clientId":"a","clientSecret":"b","issuer":"https://x"}]`,
		`[{"id":"ms","type":"microsoft","clientId":"a","tenant":"a/b"}]`,
		`[{"id":"gh","type":"github","clientId":"a","clientSecret":"b"},{"id":"gh","type":"github","clientId":"c","clientSecret":"d"}]`,
		`[{"id":"gh","type":"github","clientId":"a","clientSecret":"b","colour":"red"}]`,
	} {
		var v any
		_ = json.Unmarshal([]byte(bad), &v)
		if _, err := parseProviders(v); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	var v any
	_ = json.Unmarshal([]byte(`[{"id":"github","type":"github","clientId":"a","clientSecret":"b"},
		{"id":"ms","type":"microsoft","clientId":"a","tenant":"consumers"},
		{"id":"local","type":"oidc","clientId":"a","issuer":"http://127.0.0.1:8080/realms/x","name":"Keycloak","scopes":["offline_access"]}]`), &v)
	list, err := parseProviders(v)
	if err != nil || len(list) != 3 || list[0].DisplayName() != "GitHub" || list[1].DisplayName() != "Microsoft" || list[2].DisplayName() != "Keycloak" {
		t.Fatalf("good list: %+v %v", list, err)
	}

	for in, want := range map[string]string{"/skinlib?x=1": "/skinlib?x=1", "//evil.example": "", "/\\evil": "", "https://evil.example": "",
		"": "", "/a\nb": "", "user": ""} {
		if got := CleanNext(in); got != want {
			t.Errorf("CleanNext(%q) = %q", in, got)
		}
	}
}
