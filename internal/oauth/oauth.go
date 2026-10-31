// Package oauth is the site's OAuth 2.0 authorization server and personal access
// tokens (contract docs/design/02 §2.6): applications registered by users sign
// people in with the authorization code flow (PKCE, refresh tokens), and the tokens
// they get, or personal access tokens, call the Blessing Skin compatible resource API.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthclient"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthcode"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthgrant"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthtoken"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Limits on what one user can hold.
const (
	MaxClients        = 25
	MaxRedirectURIs   = 10
	MaxPersonalTokens = 50
	CodeTTL           = 10 * time.Minute
)

// Token prefixes: they tell the kinds apart at a glance and let secret scanners find
// leaked tokens.
const (
	accessPrefix   = "slo_"
	refreshPrefix  = "slr_"
	personalPrefix = "slp_"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInvalidToken = errors.New("invalid or expired token")
	ErrBanned       = errors.New("the account is banned")
)

// Policy is what the service needs from the settings.
type Policy struct {
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// SettingDefs are the oauth.* settings.
func SettingDefs() []settings.Def {
	return []settings.Def{
		{Key: "oauth.access_token_ttl_seconds", Type: settings.TypeInt, Default: 86400, Range: settings.Between(300, 31536000),
			Doc: "How long an application's access token is valid (1 day); it then uses its refresh token. (Blessing Skin: 1 year)"},
		{Key: "oauth.refresh_token_ttl_seconds", Type: settings.TypeInt, Default: 2592000, Range: settings.Between(3600, 31536000),
			Doc: "How long an unused refresh token stays valid (30 days); each refresh starts a new one."},
	}
}

// PolicyFrom reads the policy from a settings snapshot.
func PolicyFrom(s *settings.Snapshot) Policy {
	p := Policy{
		AccessTTL:  time.Duration(settings.Decode[int64](s, "oauth.access_token_ttl_seconds")) * time.Second,
		RefreshTTL: time.Duration(settings.Decode[int64](s, "oauth.refresh_token_ttl_seconds")) * time.Second,
	}
	p.RefreshTTL = max(p.RefreshTTL, p.AccessTTL)
	return p
}

// Service implements the authorization server and personal access tokens.
type Service struct {
	db  *ent.Client
	log *slog.Logger
	now func() time.Time
}

// NewService creates the service.
func NewService(db *ent.Client, log *slog.Logger) *Service {
	return &Service{db: db, log: log, now: time.Now}
}

func randomToken(prefix string) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// ---- Applications ----

// ClientInput is a new or changed application.
type ClientInput struct {
	Name         *string
	RedirectURIs []string // nil keeps them
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", &validate.FieldError{Field: "name", Message: "1 to 64 characters"}
	}
	return name, nil
}

// checkRedirects validates redirect URIs: absolute, no fragment, not a scheme that
// runs in the browser. Plain http and app schemes are allowed, as Blessing Skin did,
// for development and native applications.
func checkRedirects(list []string) ([]string, error) {
	bad := func(msg string) error { return &validate.FieldError{Field: "redirectUris", Message: msg} }
	out := []string{}
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || len(raw) > 2048 {
			return nil, bad("not an absolute URL: " + raw)
		}
		if u.Fragment != "" || strings.Contains(raw, "#") {
			return nil, bad("must not have a fragment: " + raw)
		}
		switch strings.ToLower(u.Scheme) {
		case "javascript", "data", "vbscript", "file", "about", "blob":
			return nil, bad("scheme not allowed: " + raw)
		case "http", "https":
			if u.Host == "" {
				return nil, bad("no host: " + raw)
			}
		}
		if !contains(out, raw) {
			out = append(out, raw)
		}
	}
	if len(out) == 0 {
		return nil, bad("at least one is required")
	}
	if len(out) > MaxRedirectURIs {
		return nil, bad(fmt.Sprintf("at most %d", MaxRedirectURIs))
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Clients lists a user's applications.
func (s *Service) Clients(ctx context.Context, userID int64) ([]*ent.OAuthClient, error) {
	return s.db.OAuthClient.Query().Where(oauthclient.UserID(userID)).Order(ent.Asc(oauthclient.FieldID)).All(ctx)
}

// CreateClient registers an application. Confidential ones get a secret, returned
// only here.
func (s *Service) CreateClient(ctx context.Context, userID int64, in ClientInput, confidential bool) (*ent.OAuthClient, string, error) {
	var name string
	var err error
	if in.Name == nil {
		err = &validate.FieldError{Field: "name", Message: "required"}
	} else {
		name, err = checkName(*in.Name)
	}
	redirects, rerr := checkRedirects(in.RedirectURIs)
	if err := errors.Join(err, rerr); err != nil {
		return nil, "", err
	}
	n, err := s.db.OAuthClient.Query().Where(oauthclient.UserID(userID)).Count(ctx)
	if err != nil {
		return nil, "", err
	}
	if n >= MaxClients {
		return nil, "", &validate.FieldError{Field: "name", Message: fmt.Sprintf("at most %d applications", MaxClients)}
	}
	c := s.db.OAuthClient.Create().SetUserID(userID).SetName(name).SetRedirectUris(redirects)
	secret := ""
	if confidential {
		secret = randomToken("")
		c.SetSecretHash(hash(secret))
	}
	row, err := c.Save(ctx)
	return row, secret, err
}

func (s *Service) ownClient(ctx context.Context, userID, id int64) (*ent.OAuthClient, error) {
	c, err := s.db.OAuthClient.Query().Where(oauthclient.ID(id), oauthclient.UserID(userID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return c, err
}

// UpdateClient renames an application or changes its redirect URIs.
func (s *Service) UpdateClient(ctx context.Context, userID, id int64, in ClientInput) (*ent.OAuthClient, error) {
	c, err := s.ownClient(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	upd := c.Update()
	if in.Name != nil {
		name, err := checkName(*in.Name)
		if err != nil {
			return nil, err
		}
		upd.SetName(name)
	}
	if in.RedirectURIs != nil {
		redirects, err := checkRedirects(in.RedirectURIs)
		if err != nil {
			return nil, err
		}
		upd.SetRedirectUris(redirects)
	}
	return upd.Save(ctx)
}

// ResetSecret gives a confidential application a new secret; the old one stops
// working at once.
func (s *Service) ResetSecret(ctx context.Context, userID, id int64) (string, error) {
	c, err := s.ownClient(ctx, userID, id)
	if err != nil {
		return "", err
	}
	if c.SecretHash == nil {
		return "", &validate.FieldError{Field: "id", Message: "a public application has no secret"}
	}
	secret := randomToken("")
	return secret, c.Update().SetSecretHash(hash(secret)).Exec(ctx)
}

// DeleteClient deletes an application with everything it was granted.
func (s *Service) DeleteClient(ctx context.Context, userID, id int64) error {
	n, err := s.db.OAuthClient.Delete().Where(oauthclient.ID(id), oauthclient.UserID(userID)).Exec(ctx)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

// ---- What users allowed ----

// Grant is an application a user allowed, with its owner's nickname.
type Grant struct {
	Client    *ent.OAuthClient
	Owner     string
	Scopes    []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Grants lists the applications a user allowed.
func (s *Service) Grants(ctx context.Context, userID int64) ([]Grant, error) {
	rows, err := s.db.OAuthGrant.Query().Where(oauthgrant.UserID(userID)).
		WithClient(func(q *ent.OAuthClientQuery) {
			q.WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldNickname) })
		}).
		Order(ent.Desc(oauthgrant.FieldUpdatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Grant, len(rows))
	for i, g := range rows {
		out[i] = Grant{Client: g.Edges.Client, Owner: g.Edges.Client.Edges.Owner.Nickname,
			Scopes: g.Scopes, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
	}
	return out, nil
}

// RevokeGrant takes an application's access away: the grant, its tokens and pending
// codes. Asking again shows the consent screen.
func (s *Service) RevokeGrant(ctx context.Context, userID, clientID int64) error {
	return withTx(ctx, s.db, func(tx *ent.Client) error {
		n, err := tx.OAuthGrant.Delete().Where(oauthgrant.UserID(userID), oauthgrant.ClientID(clientID)).Exec(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if _, err := tx.OAuthToken.Delete().Where(oauthtoken.UserID(userID), oauthtoken.ClientID(clientID)).Exec(ctx); err != nil {
			return err
		}
		_, err = tx.OAuthCode.Delete().Where(oauthcode.UserID(userID), oauthcode.ClientID(clientID)).Exec(ctx)
		return err
	})
}

// ---- Personal access tokens ----

// CreatePersonalToken makes a token for the user's own scripts. ttl 0 never expires.
// The token is returned only here.
func (s *Service) CreatePersonalToken(ctx context.Context, u *ent.User, name string, scopes []string, ttl time.Duration) (*ent.OAuthToken, string, error) {
	name, err := checkName(name)
	if err != nil {
		return nil, "", err
	}
	asked := map[string]bool{}
	for _, sc := range scopes {
		known, ok := lookupScope(sc)
		if !ok {
			return nil, "", &validate.FieldError{Field: "scopes", Message: "unknown scope " + sc}
		}
		if known.Admin && !isAdmin(u) {
			return nil, "", &validate.FieldError{Field: "scopes", Message: known.Name + " is for administrators"}
		}
		asked[known.Name] = true
	}
	if len(asked) == 0 {
		return nil, "", &validate.FieldError{Field: "scopes", Message: "choose at least one"}
	}
	if ttl < 0 || ttl > 366*24*time.Hour {
		return nil, "", &validate.FieldError{Field: "expiresInDays", Message: "0 (never) to 366 days"}
	}
	n, err := s.db.OAuthToken.Query().Where(oauthtoken.UserID(u.ID), oauthtoken.KindEQ(oauthtoken.KindPersonal)).Count(ctx)
	if err != nil {
		return nil, "", err
	}
	if n >= MaxPersonalTokens {
		return nil, "", &validate.FieldError{Field: "name", Message: fmt.Sprintf("at most %d tokens", MaxPersonalTokens)}
	}
	token := randomToken(personalPrefix)
	c := s.db.OAuthToken.Create().SetKind(oauthtoken.KindPersonal).SetTokenHash(hash(token)).SetUserID(u.ID).
		SetName(name).SetScopes(ordered(asked)).SetCreatedAt(s.now())
	if ttl > 0 {
		c.SetExpiresAt(s.now().Add(ttl))
	}
	row, err := c.Save(ctx)
	return row, token, err
}

// PersonalTokens lists a user's personal access tokens, newest first.
func (s *Service) PersonalTokens(ctx context.Context, userID int64) ([]*ent.OAuthToken, error) {
	return s.db.OAuthToken.Query().Where(oauthtoken.UserID(userID), oauthtoken.KindEQ(oauthtoken.KindPersonal)).
		Order(ent.Desc(oauthtoken.FieldID)).All(ctx)
}

// DeletePersonalToken revokes one of the user's personal access tokens.
func (s *Service) DeletePersonalToken(ctx context.Context, userID, id int64) error {
	n, err := s.db.OAuthToken.Delete().Where(oauthtoken.ID(id), oauthtoken.UserID(userID),
		oauthtoken.KindEQ(oauthtoken.KindPersonal)).Exec(ctx)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func isAdmin(u *ent.User) bool { return u.Role == user.RoleAdmin || u.Role == user.RoleSuperadmin }

// ---- Using a token ----

// Principal is who a resource API request acts for, and what it may do.
type Principal struct {
	User     *ent.User
	Scopes   []string
	TokenID  int64
	ClientID *int64 // nil for a personal access token
}

// Can reports whether the token allows scope (administration scopes also need the
// user to still be an administrator).
func (p *Principal) Can(scope string) bool {
	if s, ok := lookupScope(scope); ok && s.Admin && !isAdmin(p.User) {
		return false
	}
	return HasScope(p.Scopes, scope)
}

// Authenticate resolves a bearer token (an application's access token or a personal
// access token). An application's tokens stop working when its owner is banned or
// deleted, like its sign-ins.
func (s *Service) Authenticate(ctx context.Context, token string) (*Principal, error) {
	if !strings.HasPrefix(token, accessPrefix) && !strings.HasPrefix(token, personalPrefix) {
		return nil, ErrInvalidToken
	}
	row, err := s.db.OAuthToken.Query().Where(oauthtoken.TokenHash(hash(token))).WithUser().
		WithClient(func(q *ent.OAuthClientQuery) {
			q.Select(oauthclient.FieldID, oauthclient.FieldUserID).
				WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldStatus) })
		}).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if row.ExpiresAt != nil && !now.Before(*row.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	if c := row.Edges.Client; row.ClientID != nil && (c == nil || c.Edges.Owner == nil || c.Edges.Owner.Status != user.StatusActive) {
		return nil, ErrInvalidToken
	}
	switch row.Edges.User.Status {
	case user.StatusDeleted:
		return nil, ErrInvalidToken
	case user.StatusBanned:
		return nil, ErrBanned
	}
	// Last use is shown to the user; once a minute is precise enough and keeps busy
	// tokens from writing on every request.
	if row.LastUsedAt == nil || now.Sub(*row.LastUsedAt) > time.Minute {
		if err := s.db.OAuthToken.UpdateOneID(row.ID).SetLastUsedAt(now).Exec(ctx); err != nil && !ent.IsNotFound(err) {
			s.log.Warn("oauth token last use", "err", err)
		}
	}
	return &Principal{User: row.Edges.User, Scopes: row.Scopes, TokenID: row.ID, ClientID: row.ClientID}, nil
}

// Cleanup deletes codes and tokens that can no longer be used.
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	now := s.now()
	a, err := s.db.OAuthCode.Delete().Where(oauthcode.ExpiresAtLTE(now)).Exec(ctx)
	if err != nil {
		return a, err
	}
	b, err := s.db.OAuthToken.Delete().Where(oauthtoken.Or(
		oauthtoken.And(oauthtoken.KindEQ(oauthtoken.KindOauth), oauthtoken.RefreshUntilLTE(now)),
		oauthtoken.And(oauthtoken.KindEQ(oauthtoken.KindPersonal), oauthtoken.ExpiresAtLTE(now)),
	)).Exec(ctx)
	return a + b, err
}
