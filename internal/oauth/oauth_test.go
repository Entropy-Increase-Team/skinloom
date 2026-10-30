package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

var policy = Policy{AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour}

type fixture struct {
	s   *Service
	db  *ent.Client
	now time.Time
}

func newFixture(t *testing.T, d *db.DB) *fixture {
	f := &fixture{s: NewService(d.Ent, quiet), db: d.Ent, now: time.Now()}
	f.s.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) user(t *testing.T, email string, role user.Role) *ent.User {
	t.Helper()
	return f.db.User.Create().SetEmail(email).SetEmailNormalized(account.NormalizeEmail(email)).SetNickname(strings.Split(email, "@")[0]).
		SetPasswordHash("x").SetRole(role).SetLocale("zh-CN").SaveX(context.Background())
}

func ptr[T any](v T) *T { return &v }

func oauthErr(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// codeFrom approves a request and returns the code from the redirect.
func (f *fixture) codeFrom(t *testing.T, u *ent.User, r AuthRequest) string {
	t.Helper()
	to, _, err := f.s.Approve(context.Background(), u, r, true)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.Parse(to)
	if q.Query().Get("state") != r.State {
		t.Fatalf("state lost: %s", to)
	}
	return q.Query().Get("code")
}

func TestScopes(t *testing.T) {
	got, unknown := ParseScopes("Player.ReadWrite User.Read,Closet.ReadWrtie Player.ReadWrite")
	if unknown != "" || strings.Join(got, " ") != "User.Read Player.ReadWrite Closet.ReadWrite" {
		t.Fatalf("parse: %v %q", got, unknown)
	}
	if got, _ := ParseScopes(""); len(got) != 1 || got[0] != DefaultScope {
		t.Fatalf("default: %v", got)
	}
	if _, unknown := ParseScopes("User.Read Nope"); unknown != "Nope" {
		t.Fatal("unknown scope accepted")
	}
	if !HasScope([]string{"Player.ReadWrite"}, "Player.Read") || HasScope([]string{"Player.Read"}, "Player.ReadWrite") {
		t.Fatal("ReadWrite includes Read, not the other way")
	}
	if got := allowedFor([]string{"User.Read", "UsersManagement.Read"}, false); len(got) != 1 {
		t.Fatalf("administration scopes for a user: %v", got)
	}
}

func TestClients(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		bob := f.user(t, "bob@example.com", user.RoleUser)

		for _, bad := range [][]string{{}, {"/relative"}, {"javascript:alert(1)"}, {"https://a.example/cb#frag"}, {"https://"}} {
			if _, _, err := f.s.CreateClient(ctx, alice.ID, ClientInput{Name: ptr("App"), RedirectURIs: bad}, true); len(validate.FieldErrors(err)) == 0 {
				t.Fatalf("redirect %q accepted: %v", bad, err)
			}
		}
		c, secret, err := f.s.CreateClient(ctx, alice.ID, ClientInput{Name: ptr(" Forum "),
			RedirectURIs: []string{"https://forum.example/cb", "https://forum.example/cb", "com.example.app:/cb"}}, true)
		if err != nil || secret == "" || c.Name != "Forum" || len(c.RedirectUris) != 2 || *c.SecretHash != hash(secret) {
			t.Fatalf("create: %+v %q %v", c, secret, err)
		}
		pub, secret2, err := f.s.CreateClient(ctx, alice.ID, ClientInput{Name: ptr("SPA"), RedirectURIs: []string{"http://localhost:5173/cb"}}, false)
		if err != nil || secret2 != "" || pub.SecretHash != nil {
			t.Fatalf("public client: %+v %v", pub, err)
		}
		if _, err := f.s.ResetSecret(ctx, alice.ID, pub.ID); err == nil {
			t.Fatal("a public client has no secret to reset")
		}

		// Only the owner sees and changes it.
		if _, err := f.s.UpdateClient(ctx, bob.ID, c.ID, ClientInput{Name: ptr("Mine")}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("bob renames alice's app: %v", err)
		}
		if err := f.s.DeleteClient(ctx, bob.ID, c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("bob deletes alice's app: %v", err)
		}
		if c, err = f.s.UpdateClient(ctx, alice.ID, c.ID, ClientInput{RedirectURIs: []string{"https://forum.example/new"}}); err != nil || c.RedirectUris[0] != "https://forum.example/new" || c.Name != "Forum" {
			t.Fatalf("update: %+v %v", c, err)
		}
		newSecret, err := f.s.ResetSecret(ctx, alice.ID, c.ID)
		if err != nil || newSecret == secret {
			t.Fatal(err)
		}
		if _, err := f.s.authClient(ctx, itoa(c.ID), secret); !errors.Is(err, errInvalidClient) {
			t.Fatalf("the old secret: %v", err)
		}
		if list, _ := f.s.Clients(ctx, alice.ID); len(list) != 2 {
			t.Fatalf("list: %d", len(list))
		}
	})
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestPrepare(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		owner := f.user(t, "dev@example.com", user.RoleUser)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		admin := f.user(t, "admin@example.com", user.RoleAdmin)
		one, _, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("One"), RedirectURIs: []string{"https://one.example/cb?x=1"}}, true)
		two, _, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("Two"), RedirectURIs: []string{"https://two.example/a", "https://two.example/b"}}, true)
		pub, _, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("SPA"), RedirectURIs: []string{"https://spa.example/cb"}}, false)
		req := func(c *ent.OAuthClient) AuthRequest {
			return AuthRequest{ResponseType: "code", ClientID: itoa(c.ID), State: "xyz"}
		}

		// Errors shown to the user, never redirected.
		var re *RequestError
		for name, r := range map[string]AuthRequest{
			"unknown client":  {ResponseType: "code", ClientID: "999999"},
			"not a number":    {ResponseType: "code", ClientID: "abc"},
			"unregistered":    {ResponseType: "code", ClientID: itoa(one.ID), RedirectURI: "https://evil.example/cb"},
			"which of two":    req(two),
			"prefix of a uri": {ResponseType: "code", ClientID: itoa(one.ID), RedirectURI: "https://one.example/cb"},
		} {
			if _, err := f.s.Prepare(ctx, alice, r); !errors.As(err, &re) || RedirectOf(err) != "" {
				t.Fatalf("%s: %v", name, err)
			}
		}

		// Errors redirected to the application, with the state.
		for name, c := range map[string]struct {
			r    AuthRequest
			code string
		}{
			"token flow":       {AuthRequest{ResponseType: "token", ClientID: itoa(one.ID), State: "s"}, "unsupported_response_type"},
			"unknown scope":    {AuthRequest{ResponseType: "code", ClientID: itoa(one.ID), Scope: "Nope", State: "s"}, "invalid_scope"},
			"admin only":       {AuthRequest{ResponseType: "code", ClientID: itoa(one.ID), Scope: "UsersManagement.Read", State: "s"}, "invalid_scope"},
			"public sans PKCE": {AuthRequest{ResponseType: "code", ClientID: itoa(pub.ID), State: "s"}, "invalid_request"},
			"bad method":       {AuthRequest{ResponseType: "code", ClientID: itoa(pub.ID), State: "s", CodeChallenge: strings.Repeat("a", 43), CodeChallengeMethod: "S512"}, "invalid_request"},
			"short challenge":  {AuthRequest{ResponseType: "code", ClientID: itoa(pub.ID), State: "s", CodeChallenge: "abc", CodeChallengeMethod: "S256"}, "invalid_request"},
		} {
			_, err := f.s.Prepare(ctx, alice, c.r)
			to, _ := url.Parse(RedirectOf(err))
			if to == nil || to.Query().Get("error") != c.code || to.Query().Get("state") != "s" {
				t.Fatalf("%s: %v → %v", name, err, to)
			}
		}

		// The registered URI's own query is kept; the only URI is the default.
		p, err := f.s.Prepare(ctx, alice, req(one))
		if err != nil || p.RedirectURI != "https://one.example/cb?x=1" || p.Granted || strings.Join(p.Scopes, " ") != "User.Read" || p.Owner != "dev" {
			t.Fatalf("prepare: %+v %v", p, err)
		}
		to, _, _ := f.s.Approve(ctx, alice, req(one), true)
		if u, _ := url.Parse(to); u.Query().Get("x") != "1" || u.Query().Get("code") == "" {
			t.Fatalf("redirect: %s", to)
		}
		if p, _ := f.s.Prepare(ctx, alice, req(one)); !p.Granted {
			t.Fatal("asked again for the same scopes: already granted")
		}
		more := req(one)
		more.Scope = "User.Read Player.Read"
		if p, _ := f.s.Prepare(ctx, alice, more); p.Granted {
			t.Fatal("more scopes need consent again")
		}
		// Refusing.
		if to, _, err := f.s.Approve(ctx, alice, more, false); err != nil || !strings.Contains(to, "error=access_denied") {
			t.Fatalf("deny: %s %v", to, err)
		}
		// Administrators may grant administration scopes; users' requests drop them.
		mixed := req(one)
		mixed.Scope = "User.Read UsersManagement.Read"
		if p, _ := f.s.Prepare(ctx, alice, mixed); strings.Join(p.Scopes, " ") != "User.Read" {
			t.Fatalf("user: %v", p.Scopes)
		}
		if p, _ := f.s.Prepare(ctx, admin, mixed); len(p.Scopes) != 2 {
			t.Fatalf("admin: %v", p.Scopes)
		}

		// A banned owner's applications stop working.
		f.db.User.UpdateOne(owner).SetStatus(user.StatusBanned).ExecX(ctx)
		if _, err := f.s.Prepare(ctx, alice, req(one)); !errors.As(err, &re) {
			t.Fatalf("banned owner: %v", err)
		}
	})
}

func TestCodeFlow(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		owner := f.user(t, "dev@example.com", user.RoleUser)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		c, secret, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("Forum"), RedirectURIs: []string{"https://forum.example/cb"}}, true)
		other, otherSecret, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("Other"), RedirectURIs: []string{"https://other.example/cb"}}, true)
		r := AuthRequest{ResponseType: "code", ClientID: itoa(c.ID), RedirectURI: "https://forum.example/cb", Scope: "User.Read Player.ReadWrite", State: "st"}

		code := f.codeFrom(t, alice, r)
		exchange := TokenRequest{GrantType: "authorization_code", Code: code, RedirectURI: r.RedirectURI, ClientID: itoa(c.ID), ClientSecret: secret}

		// Client authentication comes first.
		bad := exchange
		bad.ClientSecret = "wrong"
		if _, err := f.s.Token(ctx, bad, policy); !oauthErr(err, "invalid_client") {
			t.Fatalf("wrong secret: %v", err)
		}
		bad.ClientSecret = ""
		if _, err := f.s.Token(ctx, bad, policy); !oauthErr(err, "invalid_client") {
			t.Fatalf("no secret: %v", err)
		}
		// Another application cannot use the code (and spends it).
		stolen := exchange
		stolen.ClientID, stolen.ClientSecret = itoa(other.ID), otherSecret
		if _, err := f.s.Token(ctx, stolen, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("another client: %v", err)
		}
		if _, err := f.s.Token(ctx, exchange, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("a code tried by another client is spent: %v", err)
		}

		code = f.codeFrom(t, alice, r)
		exchange.Code = code
		wrongURI := exchange
		wrongURI.RedirectURI = "https://forum.example/other"
		if _, err := f.s.Token(ctx, wrongURI, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("redirect_uri mismatch: %v", err)
		}
		exchange.Code = f.codeFrom(t, alice, r)
		tok, err := f.s.Token(ctx, exchange, policy)
		if err != nil || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || !strings.HasPrefix(tok.AccessToken, accessPrefix) ||
			!strings.HasPrefix(tok.RefreshToken, refreshPrefix) || tok.Scope != "User.Read Player.ReadWrite" {
			t.Fatalf("token: %+v %v", tok, err)
		}
		if _, err := f.s.Token(ctx, exchange, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("a code works once: %v", err)
		}

		// The access token acts for alice with the granted scopes.
		pr, err := f.s.Authenticate(ctx, tok.AccessToken)
		if err != nil || pr.User.ID != alice.ID || !pr.Can("Player.Read") || pr.Can("Closet.Read") || *pr.ClientID != c.ID {
			t.Fatalf("authenticate: %+v %v", pr, err)
		}
		if _, err := f.s.Authenticate(ctx, tok.RefreshToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("a refresh token is not an access token")
		}
		// The application's owner banned: its tokens stop working; unbanned, they work again.
		f.db.User.UpdateOne(owner).SetStatus(user.StatusBanned).ExecX(ctx)
		if _, err := f.s.Authenticate(ctx, tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("owner banned: %v", err)
		}
		f.db.User.UpdateOne(owner).SetStatus(user.StatusActive).ExecX(ctx)
		if _, err := f.s.Authenticate(ctx, tok.AccessToken); err != nil {
			t.Fatalf("owner unbanned: %v", err)
		}

		// Refresh: rotation, narrowing, no widening, only by its client.
		refresh := TokenRequest{GrantType: "refresh_token", RefreshToken: tok.RefreshToken, ClientID: itoa(c.ID), ClientSecret: secret, Scope: "User.Read Closet.Read"}
		if _, err := f.s.Token(ctx, refresh, policy); !oauthErr(err, "invalid_scope") {
			t.Fatalf("widening: %v", err)
		}
		byOther := refresh
		byOther.ClientID, byOther.ClientSecret, byOther.Scope = itoa(other.ID), otherSecret, ""
		if _, err := f.s.Token(ctx, byOther, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("another client's refresh: %v", err)
		}
		refresh.Scope = "User.Read"
		next, err := f.s.Token(ctx, refresh, policy)
		if err != nil || next.Scope != "User.Read" {
			t.Fatalf("refresh: %+v %v", next, err)
		}
		if _, err := f.s.Authenticate(ctx, tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("the old access token is revoked")
		}
		if _, err := f.s.Token(ctx, refresh, policy); !oauthErr(err, "invalid_grant") {
			t.Fatal("the old refresh token is revoked")
		}

		// Expiry.
		f.now = f.now.Add(policy.AccessTTL)
		if _, err := f.s.Authenticate(ctx, next.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("expired access token")
		}
		later := TokenRequest{GrantType: "refresh_token", RefreshToken: next.RefreshToken, ClientID: itoa(c.ID), ClientSecret: secret}
		if next, err = f.s.Token(ctx, later, policy); err != nil {
			t.Fatalf("refresh after the access token expired: %v", err)
		}
		f.now = f.now.Add(policy.RefreshTTL)
		later.RefreshToken = next.RefreshToken
		if _, err := f.s.Token(ctx, later, policy); !oauthErr(err, "invalid_grant") {
			t.Fatal("expired refresh token")
		}
		f.now = time.Now()

		// Unsupported grants.
		if _, err := f.s.Token(ctx, TokenRequest{GrantType: "password", ClientID: itoa(c.ID), ClientSecret: secret}, policy); !oauthErr(err, "unsupported_grant_type") {
			t.Fatalf("password grant: %v", err)
		}
	})
}

func TestPKCE(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		owner := f.user(t, "dev@example.com", user.RoleUser)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		pub, _, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("SPA"), RedirectURIs: []string{"https://spa.example/cb"}}, false)
		verifier := strings.Repeat("v3rifier-", 6)
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])
		r := AuthRequest{ResponseType: "code", ClientID: itoa(pub.ID), CodeChallenge: challenge, CodeChallengeMethod: "S256"}
		exchange := TokenRequest{GrantType: "authorization_code", ClientID: itoa(pub.ID), CodeVerifier: strings.Repeat("x", 43)}

		// A wrong verifier spends the code.
		exchange.Code = f.codeFrom(t, alice, r)
		if _, err := f.s.Token(ctx, exchange, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("wrong verifier: %v", err)
		}
		exchange.CodeVerifier = verifier
		if _, err := f.s.Token(ctx, exchange, policy); !oauthErr(err, "invalid_grant") {
			t.Fatalf("the code was spent: %v", err)
		}
		exchange.Code = f.codeFrom(t, alice, r)
		withSecret := exchange
		withSecret.ClientSecret = "anything"
		if _, err := f.s.Token(ctx, withSecret, policy); !oauthErr(err, "invalid_client") {
			t.Fatalf("a public client with a secret: %v", err)
		}
		if tok, err := f.s.Token(ctx, exchange, policy); err != nil || tok.AccessToken == "" {
			t.Fatalf("S256: %+v %v", tok, err)
		}
		// plain, the default method.
		plain := AuthRequest{ResponseType: "code", ClientID: itoa(pub.ID), CodeChallenge: verifier}
		exchange.Code = f.codeFrom(t, alice, plain)
		if _, err := f.s.Token(ctx, exchange, policy); err != nil {
			t.Fatalf("plain: %v", err)
		}
	})
}

// Two exchanges of one code at once: exactly one gets tokens.
func TestCodeUsedOnceConcurrently(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		owner := f.user(t, "dev@example.com", user.RoleUser)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		c, secret, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("Forum"), RedirectURIs: []string{"https://forum.example/cb"}}, true)
		code := f.codeFrom(t, alice, AuthRequest{ResponseType: "code", ClientID: itoa(c.ID)})
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		for range 5 {
			wg.Go(func() {
				_, err := f.s.Token(ctx, TokenRequest{GrantType: "authorization_code", Code: code, ClientID: itoa(c.ID), ClientSecret: secret}, policy)
				if err == nil {
					mu.Lock()
					ok++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if ok != 1 {
			t.Fatalf("%d exchanges succeeded", ok)
		}
	})
}

func TestGrantsRevokeAndPersonalTokens(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		owner := f.user(t, "dev@example.com", user.RoleUser)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		c, secret, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("Forum"), RedirectURIs: []string{"https://forum.example/cb"}}, true)
		r := AuthRequest{ResponseType: "code", ClientID: itoa(c.ID)}
		tok, err := f.s.Token(ctx, TokenRequest{GrantType: "authorization_code", Code: f.codeFrom(t, alice, r), ClientID: itoa(c.ID), ClientSecret: secret}, policy)
		if err != nil {
			t.Fatal(err)
		}
		grants, err := f.s.Grants(ctx, alice.ID)
		if err != nil || len(grants) != 1 || grants[0].Client.Name != "Forum" || grants[0].Owner != "dev" {
			t.Fatalf("grants: %+v %v", grants, err)
		}

		// RFC 7009 revocation by the client, of the refresh token: the pair goes.
		if err := f.s.Revoke(ctx, itoa(c.ID), secret, tok.RefreshToken); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Authenticate(ctx, tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("revoked")
		}
		if err := f.s.Revoke(ctx, itoa(c.ID), secret, "unknown"); err != nil {
			t.Fatalf("unknown tokens are not an error: %v", err)
		}

		// Revoking the grant signs the application out and asks for consent again.
		tok, _ = f.s.Token(ctx, TokenRequest{GrantType: "authorization_code", Code: f.codeFrom(t, alice, r), ClientID: itoa(c.ID), ClientSecret: secret}, policy)
		pending := f.codeFrom(t, alice, r)
		if err := f.s.RevokeGrant(ctx, alice.ID, c.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Authenticate(ctx, tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("tokens go with the grant")
		}
		if _, err := f.s.Token(ctx, TokenRequest{GrantType: "authorization_code", Code: pending, ClientID: itoa(c.ID), ClientSecret: secret}, policy); !oauthErr(err, "invalid_grant") {
			t.Fatal("pending codes go with the grant")
		}
		if p, _ := f.s.Prepare(ctx, alice, r); p.Granted {
			t.Fatal("consent again")
		}
		if err := f.s.RevokeGrant(ctx, alice.ID, c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("twice")
		}

		// Personal access tokens.
		if _, _, err := f.s.CreatePersonalToken(ctx, alice, "ci", []string{"UsersManagement.Read"}, 0); len(validate.FieldErrors(err)) == 0 {
			t.Fatal("administration scope for a user")
		}
		if _, _, err := f.s.CreatePersonalToken(ctx, alice, "ci", nil, 0); len(validate.FieldErrors(err)) == 0 {
			t.Fatal("no scopes")
		}
		row, pat, err := f.s.CreatePersonalToken(ctx, alice, "ci", []string{"Closet.ReadWrtie", "User.Read"}, 24*time.Hour)
		if err != nil || !strings.HasPrefix(pat, personalPrefix) || strings.Join(row.Scopes, " ") != "User.Read Closet.ReadWrite" {
			t.Fatalf("pat: %+v %v", row, err)
		}
		forever, pat2, _ := f.s.CreatePersonalToken(ctx, alice, "forever", []string{"Player.Read"}, 0)
		pr, err := f.s.Authenticate(ctx, pat)
		if err != nil || pr.ClientID != nil || !pr.Can("Closet.Read") || pr.Can("Player.Read") {
			t.Fatalf("pat principal: %+v %v", pr, err)
		}
		if got := f.db.OAuthToken.GetX(ctx, row.ID); got.LastUsedAt == nil {
			t.Fatal("last use recorded")
		}
		f.now = f.now.Add(25 * time.Hour)
		if _, err := f.s.Authenticate(ctx, pat); !errors.Is(err, ErrInvalidToken) {
			t.Fatal("expired pat")
		}
		if _, err := f.s.Authenticate(ctx, pat2); err != nil {
			t.Fatalf("a token without expiry: %v", err)
		}
		if n, err := f.s.Cleanup(ctx); err != nil || n < 1 {
			t.Fatalf("cleanup: %d %v", n, err)
		}
		f.now = time.Now()
		if list, _ := f.s.PersonalTokens(ctx, alice.ID); len(list) != 1 || list[0].ID != forever.ID {
			t.Fatalf("after cleanup: %v", list)
		}
		if err := f.s.DeletePersonalToken(ctx, owner.ID, forever.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("someone else's token")
		}

		// Banned and deleted accounts.
		f.db.User.UpdateOne(alice).SetStatus(user.StatusBanned).ExecX(ctx)
		if _, err := f.s.Authenticate(ctx, pat2); !errors.Is(err, ErrBanned) {
			t.Fatalf("banned: %v", err)
		}
		f.db.User.UpdateOne(alice).SetStatus(user.StatusDeleted).ExecX(ctx)
		if _, err := f.s.Authenticate(ctx, pat2); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("deleted: %v", err)
		}
	})
}

// Changing the password (or signing out other devices) revokes applications' tokens
// and personal access tokens with the sessions.
func TestRevokedWithSessions(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		accounts := account.NewService(d.Ent, quiet)
		owner := f.user(t, "dev@example.com", user.RoleUser)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		c, secret, _ := f.s.CreateClient(ctx, owner.ID, ClientInput{Name: ptr("Forum"), RedirectURIs: []string{"https://forum.example/cb"}}, true)
		tok, _ := f.s.Token(ctx, TokenRequest{GrantType: "authorization_code", Code: f.codeFrom(t, alice, AuthRequest{ResponseType: "code", ClientID: itoa(c.ID)}),
			ClientID: itoa(c.ID), ClientSecret: secret}, policy)
		_, pat, _ := f.s.CreatePersonalToken(ctx, alice, "ci", []string{"User.Read"}, 0)
		if err := accounts.RevokeOtherSessions(ctx, &account.Viewer{User: alice}); err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{tok.AccessToken, pat} {
			if _, err := f.s.Authenticate(ctx, token); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("%s survived", token[:4])
			}
		}
		if n := f.db.OAuthToken.Query().Where(oauthtoken.UserID(alice.ID)).CountX(ctx); n != 0 {
			t.Fatalf("%d tokens left", n)
		}
	})
}

// Names are up to 64 characters, not bytes: a Chinese name of 30 characters is 90
// bytes and must be accepted for applications and personal access tokens alike.
func TestNamesCountCharacters(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		f := newFixture(t, d)
		alice := f.user(t, "alice@example.com", user.RoleUser)
		name := strings.Repeat("皮", 30)
		c, _, err := f.s.CreateClient(ctx, alice.ID, ClientInput{Name: ptr(name), RedirectURIs: []string{"https://a.example/cb"}}, true)
		if err != nil || c.Name != name {
			t.Fatalf("create: %v", err)
		}
		if _, err := f.s.UpdateClient(ctx, alice.ID, c.ID, ClientInput{Name: ptr(strings.Repeat("肤", 64))}); err != nil {
			t.Fatalf("rename: %v", err)
		}
		if _, _, err := f.s.CreatePersonalToken(ctx, alice, name, []string{"User.Read"}, 0); err != nil {
			t.Fatalf("personal token: %v", err)
		}
		if _, _, err := f.s.CreatePersonalToken(ctx, alice, strings.Repeat("肤", 65), []string{"User.Read"}, 0); len(validate.FieldErrors(err)) == 0 {
			t.Fatalf("65 characters: %v", err)
		}
	})
}
