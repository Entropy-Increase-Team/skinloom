package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthclient"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthcode"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthgrant"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// Error is an OAuth error (RFC 6749 §4.1.2.1, §5.2).
type Error struct {
	Status      int
	Code        string
	Description string
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func invalidRequest(desc string) *Error {
	return &Error{http.StatusBadRequest, "invalid_request", desc}
}
func invalidGrant(desc string) *Error { return &Error{http.StatusBadRequest, "invalid_grant", desc} }

var errInvalidClient = &Error{http.StatusUnauthorized, "invalid_client", "Client authentication failed"}

// RequestError is an authorization request that cannot be answered by redirecting
// back to the application (unknown client, unregistered redirect URI): the user sees
// it instead, so a forged request cannot bounce people to arbitrary addresses.
type RequestError struct{ Description string }

func (e *RequestError) Error() string { return e.Description }

// AuthRequest is an authorization request (GET /oauth/authorize).
type AuthRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// Prepared is a valid authorization request, ready for the consent screen.
type Prepared struct {
	Client      *ent.OAuthClient
	Owner       string   // the application's owner (nickname)
	RedirectURI string   // where the answer goes
	Scopes      []string // what would be granted (administration scopes only to administrators)
	Granted     bool     // the user already allowed all of it: no need to ask again
}

// redirectError is an error answered by redirecting to the application.
type redirectError struct {
	uri, state string
	err        *Error
}

func (e *redirectError) Error() string { return e.err.Error() }

// RedirectOf returns where to send the browser for an error from Prepare or Approve
// that goes back to the application, or "" for any other error.
func RedirectOf(err error) string {
	var re *redirectError
	if !errors.As(err, &re) {
		return ""
	}
	return withQuery(re.uri, map[string]string{"error": re.err.Code, "error_description": re.err.Description, "state": re.state})
}

func withQuery(uri string, params map[string]string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// pkceVerifier is RFC 7636's code_verifier (and plain challenge) alphabet and length.
var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func (s *Service) client(ctx context.Context, id string) (*ent.OAuthClient, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return nil, ErrNotFound
	}
	c, err := s.db.OAuthClient.Query().Where(oauthclient.ID(n)).
		WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldNickname, user.FieldStatus) }).Only(ctx)
	if ent.IsNotFound(err) || (err == nil && c.Edges.Owner.Status != user.StatusActive) {
		return nil, ErrNotFound // a banned or deleted owner's applications stop working
	}
	return c, err
}

// Prepare checks an authorization request for a signed-in user.
func (s *Service) Prepare(ctx context.Context, u *ent.User, r AuthRequest) (*Prepared, error) {
	c, err := s.client(ctx, r.ClientID)
	if errors.Is(err, ErrNotFound) {
		return nil, &RequestError{"Unknown application (client_id)."}
	}
	if err != nil {
		return nil, err
	}
	redirect := r.RedirectURI
	switch {
	case redirect == "" && len(c.RedirectUris) == 1:
		redirect = c.RedirectUris[0]
	case redirect == "":
		return nil, &RequestError{"The application has several redirect URIs and did not say which (redirect_uri)."}
	case !contains(c.RedirectUris, redirect):
		return nil, &RequestError{"The redirect URI is not registered for this application."}
	}
	back := func(e *Error) error { return &redirectError{uri: redirect, state: r.State, err: e} }

	if r.ResponseType != "code" {
		return nil, back(&Error{http.StatusBadRequest, "unsupported_response_type", "Only response_type=code is supported."})
	}
	scopes, unknown := ParseScopes(r.Scope)
	if unknown != "" {
		return nil, back(&Error{http.StatusBadRequest, "invalid_scope", "Unknown scope " + unknown + "."})
	}
	if scopes = allowedFor(scopes, isAdmin(u)); len(scopes) == 0 {
		return nil, back(&Error{http.StatusBadRequest, "invalid_scope", "Only administrators can grant these scopes."})
	}
	method := r.CodeChallengeMethod
	if r.CodeChallenge != "" && method == "" {
		method = "plain"
	}
	switch {
	case r.CodeChallenge == "" && method != "":
		return nil, back(invalidRequest("code_challenge_method without code_challenge."))
	case r.CodeChallenge == "" && c.SecretHash == nil:
		return nil, back(invalidRequest("Public clients must use PKCE (code_challenge)."))
	case method != "" && method != "S256" && method != "plain":
		return nil, back(invalidRequest("code_challenge_method must be S256 or plain."))
	case r.CodeChallenge != "" && !pkceVerifier.MatchString(r.CodeChallenge):
		return nil, back(invalidRequest("Malformed code_challenge."))
	}

	p := &Prepared{Client: c, Owner: c.Edges.Owner.Nickname, RedirectURI: redirect, Scopes: scopes}
	g, err := s.db.OAuthGrant.Query().Where(oauthgrant.UserID(u.ID), oauthgrant.ClientID(c.ID)).Only(ctx)
	switch {
	case err == nil:
		p.Granted = covers(g.Scopes, scopes)
	case !ent.IsNotFound(err):
		return nil, err
	}
	return p, nil
}

// Approve answers an authorization request the user allowed (or refused): it records
// the grant and returns the address to send the browser to, with a code.
func (s *Service) Approve(ctx context.Context, u *ent.User, r AuthRequest, allow bool) (string, *Prepared, error) {
	p, err := s.Prepare(ctx, u, r)
	if err != nil {
		return "", nil, err
	}
	if !allow {
		return withQuery(p.RedirectURI, map[string]string{"error": "access_denied",
			"error_description": "The user refused.", "state": r.State}), p, nil
	}
	if err := s.recordGrant(ctx, u.ID, p); err != nil {
		return "", nil, err
	}
	method := r.CodeChallengeMethod
	if r.CodeChallenge != "" && method == "" {
		method = "plain"
	}
	code := randomToken("")
	err = s.db.OAuthCode.Create().SetCodeHash(hash(code)).SetClientID(p.Client.ID).SetUserID(u.ID).
		SetRedirectURI(r.RedirectURI).SetScopes(p.Scopes).SetCodeChallenge(r.CodeChallenge).
		SetCodeChallengeMethod(method).SetExpiresAt(s.now().Add(CodeTTL)).Exec(ctx)
	if err != nil {
		return "", nil, err
	}
	return withQuery(p.RedirectURI, map[string]string{"code": code, "state": r.State}), p, nil
}

// recordGrant adds the approved scopes to the user's grant for the application.
func (s *Service) recordGrant(ctx context.Context, userID int64, p *Prepared) error {
	for range 2 {
		g, err := s.db.OAuthGrant.Query().Where(oauthgrant.UserID(userID), oauthgrant.ClientID(p.Client.ID)).Only(ctx)
		switch {
		case ent.IsNotFound(err):
			err = s.db.OAuthGrant.Create().SetUserID(userID).SetClientID(p.Client.ID).SetScopes(p.Scopes).Exec(ctx)
			if ent.IsConstraintError(err) {
				continue // allowed in another tab at the same moment: add to that one
			}
			return err
		case err != nil:
			return err
		case covers(g.Scopes, p.Scopes):
			return g.Update().Exec(ctx) // touch updated_at: the list shows recent use first
		}
		union := map[string]bool{}
		for _, sc := range append(g.Scopes, p.Scopes...) {
			union[sc] = true
		}
		return g.Update().SetScopes(ordered(union)).Exec(ctx)
	}
	return errors.New("oauth grant: concurrent updates")
}

// TokenRequest is a token request (POST /oauth/token).
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	CodeVerifier string
	RefreshToken string
	Scope        string
	ClientID     string
	ClientSecret string
}

// TokenResponse is a successful token response (RFC 6749 §5.1).
type TokenResponse struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// authClient authenticates the client of a token or revocation request: confidential
// clients by their secret, public ones by their id alone.
func (s *Service) authClient(ctx context.Context, id, secret string) (*ent.OAuthClient, error) {
	c, err := s.client(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, errInvalidClient
	}
	if err != nil {
		return nil, err
	}
	if c.SecretHash == nil {
		if secret != "" {
			return nil, errInvalidClient
		}
		return c, nil
	}
	if secret == "" || subtle.ConstantTimeCompare([]byte(hash(secret)), []byte(*c.SecretHash)) != 1 {
		return nil, errInvalidClient
	}
	return c, nil
}

// Token answers a token request: an authorization code or a refresh token for new
// tokens. Refresh tokens rotate: the old pair stops working.
func (s *Service) Token(ctx context.Context, r TokenRequest, p Policy) (*TokenResponse, error) {
	c, err := s.authClient(ctx, r.ClientID, r.ClientSecret)
	if err != nil {
		return nil, err
	}
	switch r.GrantType {
	case "authorization_code":
		return s.exchangeCode(ctx, c, r, p)
	case "refresh_token":
		return s.refresh(ctx, c, r, p)
	case "":
		return nil, invalidRequest("grant_type is required.")
	default:
		return nil, &Error{http.StatusBadRequest, "unsupported_grant_type", "Supported: authorization_code, refresh_token."}
	}
}

func checkVerifier(code *ent.OAuthCode, verifier string) bool {
	if code.CodeChallenge == "" {
		return verifier == ""
	}
	if !pkceVerifier.MatchString(verifier) {
		return false
	}
	want := verifier
	if code.CodeChallengeMethod == "S256" {
		sum := sha256.Sum256([]byte(verifier))
		want = base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(code.CodeChallenge)) == 1
}

func (s *Service) exchangeCode(ctx context.Context, c *ent.OAuthClient, r TokenRequest, p Policy) (*TokenResponse, error) {
	if r.Code == "" {
		return nil, invalidRequest("code is required.")
	}
	var out *TokenResponse
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
		code, err := tx.OAuthCode.Query().Where(oauthcode.CodeHash(hash(r.Code))).WithUser().Only(ctx)
		if ent.IsNotFound(err) {
			return invalidGrant("The code is invalid, expired or already used.")
		}
		if err != nil {
			return err
		}
		// One use: whoever deletes it gets the tokens.
		if n, err := tx.OAuthCode.Delete().Where(oauthcode.ID(code.ID)).Exec(ctx); err != nil || n == 0 {
			if err == nil {
				err = invalidGrant("The code is invalid, expired or already used.")
			}
			return err
		}
		switch {
		case code.ClientID != c.ID || !s.now().Before(code.ExpiresAt):
			return invalidGrant("The code is invalid, expired or already used.")
		case r.RedirectURI != code.RedirectURI:
			return invalidGrant("redirect_uri does not match the authorization request.")
		case !checkVerifier(code, r.CodeVerifier):
			return invalidGrant("PKCE verification failed (code_verifier).")
		case code.Edges.User.Status != user.StatusActive:
			return invalidGrant("The account is not active.")
		}
		out, err = s.issue(ctx, tx, code.UserID, c.ID, code.Scopes, p)
		return err
	})
	return out, s.keepCodeDeleted(ctx, r.Code, err)
}

// keepCodeDeleted spends a code that failed a check: the rollback undid its deletion,
// and a code must work once at most (a stolen code tried with a wrong verifier must
// not stay usable for another guess).
func (s *Service) keepCodeDeleted(ctx context.Context, code string, err error) error {
	var oe *Error
	if errors.As(err, &oe) && oe.Code == "invalid_grant" {
		if _, derr := s.db.OAuthCode.Delete().Where(oauthcode.CodeHash(hash(code))).Exec(ctx); derr != nil {
			return errors.Join(err, derr)
		}
	}
	return err
}

func (s *Service) refresh(ctx context.Context, c *ent.OAuthClient, r TokenRequest, p Policy) (*TokenResponse, error) {
	if !strings.HasPrefix(r.RefreshToken, refreshPrefix) {
		return nil, invalidGrant("The refresh token is invalid or expired.")
	}
	var out *TokenResponse
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
		old, err := tx.OAuthToken.Query().Where(oauthtoken.RefreshHash(hash(r.RefreshToken))).WithUser().Only(ctx)
		if ent.IsNotFound(err) {
			return invalidGrant("The refresh token is invalid or expired.")
		}
		if err != nil {
			return err
		}
		if old.ClientID == nil || *old.ClientID != c.ID || old.RefreshUntil == nil || !s.now().Before(*old.RefreshUntil) ||
			old.Edges.User.Status != user.StatusActive {
			return invalidGrant("The refresh token is invalid or expired.")
		}
		scopes := old.Scopes
		if strings.TrimSpace(r.Scope) != "" {
			asked, unknown := ParseScopes(r.Scope)
			if unknown != "" || !covers(old.Scopes, asked) {
				return &Error{http.StatusBadRequest, "invalid_scope", "A refresh can only keep or narrow the scopes."}
			}
			scopes = asked
		}
		if n, err := tx.OAuthToken.Delete().Where(oauthtoken.ID(old.ID)).Exec(ctx); err != nil || n == 0 {
			if err == nil {
				err = invalidGrant("The refresh token is invalid or expired.")
			}
			return err
		}
		out, err = s.issue(ctx, tx, old.UserID, c.ID, scopes, p)
		return err
	})
	return out, err
}

func (s *Service) issue(ctx context.Context, tx *ent.Client, userID, clientID int64, scopes []string, p Policy) (*TokenResponse, error) {
	now := s.now()
	access, refresh := randomToken(accessPrefix), randomToken(refreshPrefix)
	err := tx.OAuthToken.Create().SetKind(oauthtoken.KindOauth).SetTokenHash(hash(access)).SetRefreshHash(hash(refresh)).
		SetUserID(userID).SetClientID(clientID).SetScopes(scopes).SetCreatedAt(now).
		SetExpiresAt(now.Add(p.AccessTTL)).SetRefreshUntil(now.Add(p.RefreshTTL)).Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &TokenResponse{TokenType: "Bearer", ExpiresIn: int64(p.AccessTTL.Seconds()), AccessToken: access,
		RefreshToken: refresh, Scope: strings.Join(scopes, " ")}, nil
}

// Revoke revokes an access or refresh token of the client (RFC 7009). Unknown tokens
// are not an error.
func (s *Service) Revoke(ctx context.Context, clientID, secret, token string) error {
	c, err := s.authClient(ctx, clientID, secret)
	if err != nil {
		return err
	}
	h := hash(token)
	_, err = s.db.OAuthToken.Delete().Where(oauthtoken.ClientID(c.ID),
		oauthtoken.Or(oauthtoken.TokenHash(h), oauthtoken.RefreshHash(h))).Exec(ctx)
	return err
}
