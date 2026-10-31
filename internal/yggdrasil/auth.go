package yggdrasil

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/ent/yggtoken"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
)

// ProfileRef is a profile (player) as authserver lists it.
type ProfileRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserInfo is the "user" part of an authserver answer (requestUser).
type UserInfo struct {
	ID         string         `json:"id"`
	Properties []UserProperty `json:"properties"`
}

// UserProperty is one property of UserInfo.
type UserProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// AuthAnswer answers authenticate and refresh.
type AuthAnswer struct {
	AccessToken       string       `json:"accessToken"`
	ClientToken       string       `json:"clientToken"`
	AvailableProfiles []ProfileRef `json:"availableProfiles,omitempty"`
	SelectedProfile   *ProfileRef  `json:"selectedProfile,omitempty"`
	User              *UserInfo    `json:"user,omitempty"`
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newAccessToken makes a token in Blessing Skin's format, a JWT signed with HS256
// (iss Yggdrasil-Auth, sub the user's UUID, yggt a random id, spr the bound profile).
// Launchers treat it as opaque; only its hash is stored, and the row decides validity.
func newAccessToken(secret []byte, sub, profile string, now, exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	claims := map[string]any{"iss": "Yggdrasil-Auth", "sub": sub, "yggt": randomHex(16),
		"iat": now.Unix(), "exp": exp.Unix()}
	if profile != "" {
		claims["spr"] = profile
	}
	body, _ := json.Marshal(claims)
	unsigned := enc([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned))
	return unsigned + "." + enc(mac.Sum(nil))
}

// resolve finds the account an authserver username names: an email, or (feature
// non_email_login) a player name, which then also picks that player.
func (s *Service) resolve(ctx context.Context, username string) (email string, picked *ent.Player, err error) {
	if strings.Contains(username, "@") {
		return username, nil, nil
	}
	p, err := s.db.Player.Query().Where(entplayer.NameLower(player.Key(username))).WithOwner().Only(ctx)
	if ent.IsNotFound(err) {
		return username, nil, nil // fails like a wrong password, and is rate limited like one
	}
	if err != nil {
		return "", nil, err
	}
	return p.Edges.Owner.Email, p, nil
}

func (s *Service) checkCredentials(ctx context.Context, username, password string, meta account.RequestMeta) (*ent.User, *ent.Player, error) {
	if username == "" || password == "" {
		return nil, nil, illegal("Credentials are null.")
	}
	email, picked, err := s.resolve(ctx, username)
	if err != nil {
		return nil, nil, err
	}
	u, err := s.accounts.VerifyCredentials(ctx, email, password, meta)
	var rl *account.RateLimitError
	switch {
	case errors.As(err, &rl):
		return nil, nil, forbidden("Too many attempts, try again in " + strconv.Itoa(int(rl.RetryAfter.Seconds())+1) + " seconds.")
	case errors.Is(err, account.ErrAccountDisabled):
		return nil, nil, errBanned
	case errors.Is(err, account.ErrInvalidCredentials):
		return nil, nil, errInvalidCredentials
	case err != nil:
		return nil, nil, err
	}
	return u, picked, nil
}

func (s *Service) profilesOf(ctx context.Context, userID int64) ([]ProfileRef, []*ent.Player, error) {
	list, err := s.db.Player.Query().Where(entplayer.UserID(userID)).Order(ent.Asc(entplayer.FieldID)).All(ctx)
	if err != nil {
		return nil, nil, err
	}
	refs := make([]ProfileRef, len(list))
	for i, p := range list {
		refs[i] = ProfileRef{ID: p.UUID, Name: p.Name}
	}
	return refs, list, nil
}

func userInfo(u *ent.User) *UserInfo {
	info := &UserInfo{ID: userUUID(u.Email), Properties: []UserProperty{}}
	if u.Locale != "" {
		info.Properties = append(info.Properties, UserProperty{"preferredLanguage", strings.ReplaceAll(u.Locale, "-", "_")})
	}
	return info
}

// issue stores a new token for the user (bound to bound, if not nil) inside tx and
// revokes the oldest ones beyond the per-user limit. The keys are loaded before the
// transaction: generating them writes through another connection, which SQLite's
// single writer would make wait for this transaction.
func (s *Service) issue(ctx context.Context, tx *ent.Client, k *keys, u *ent.User, clientToken string, bound *ent.Player, p Policy) (string, error) {
	now := s.now()
	profile := ""
	if bound != nil {
		profile = bound.UUID
	}
	token := newAccessToken(k.token, userUUID(u.Email), profile, now, now.Add(p.TokenValid))
	c := tx.YggToken.Create().SetTokenHash(hashToken(token)).SetClientToken(clientToken).SetUserID(u.ID).
		SetCreatedAt(now).SetExpiresAt(now.Add(p.TokenValid)).SetRefreshUntil(now.Add(p.TokenRefresh))
	if bound != nil {
		c.SetPlayerID(bound.ID)
	}
	if err := c.Exec(ctx); err != nil {
		return "", err
	}
	old, err := tx.YggToken.Query().Where(yggtoken.UserID(u.ID)).Order(ent.Desc(yggtoken.FieldID)).
		Offset(p.TokensPerUser).IDs(ctx)
	if err != nil || len(old) == 0 {
		return token, err
	}
	_, err = tx.YggToken.Delete().Where(yggtoken.IDIn(old...)).Exec(ctx)
	return token, err
}

// AuthRequest is POST /authserver/authenticate.
type AuthRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	ClientToken string `json:"clientToken"`
	RequestUser bool   `json:"requestUser"`
}

// Authenticate logs a launcher in with an email or a player name. The token is bound
// to the player named at login (Blessing Skin left it unbound, issue #123), or to the
// only player of the account.
func (s *Service) Authenticate(ctx context.Context, in AuthRequest, p Policy, meta account.RequestMeta) (*AuthAnswer, error) {
	u, picked, err := s.checkCredentials(ctx, in.Username, in.Password, meta)
	if err != nil {
		return nil, err
	}
	if p.RequireVerified && u.EmailVerifiedAt == nil { // as Blessing Skin's yggdrasil-api plugin
		return nil, errUnverified
	}
	refs, players, err := s.profilesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	var bound *ent.Player
	switch {
	case picked != nil:
		bound = picked
	case len(players) == 1:
		bound = players[0]
	}
	clientToken := strings.TrimSpace(in.ClientToken)
	if clientToken == "" || len(clientToken) > 128 {
		clientToken = randomHex(16)
	}
	k, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	var token string
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		token, err = s.issue(ctx, tx, k, u, clientToken, bound, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := &AuthAnswer{AccessToken: token, ClientToken: clientToken, AvailableProfiles: refs}
	if out.AvailableProfiles == nil {
		out.AvailableProfiles = []ProfileRef{}
	}
	if bound != nil {
		out.SelectedProfile = &ProfileRef{ID: bound.UUID, Name: bound.Name}
	}
	if in.RequestUser {
		out.User = userInfo(u)
	}
	if err := s.accounts.Audit(ctx, &u.ID, "yggdrasil.login", "user", strconv.FormatInt(u.ID, 10), meta.IP, nil); err != nil {
		s.log.Warn("audit", "action", "yggdrasil.login", "err", err)
	}
	return out, nil
}

// found is a stored token with its user and bound player.
type found struct {
	row    *ent.YggToken
	user   *ent.User
	player *ent.Player // nil when unbound
}

// find looks a token up. refreshable accepts an expired token that can still be
// refreshed; otherwise it must be valid. Tokens of accounts that are no longer active
// are never found.
func (s *Service) find(ctx context.Context, token, clientToken string, refreshable bool) (*found, error) {
	if token == "" {
		return nil, errInvalidToken
	}
	row, err := s.db.YggToken.Query().Where(yggtoken.TokenHash(hashToken(token))).WithUser().WithPlayer().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, errInvalidToken
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	limit := row.ExpiresAt
	if refreshable {
		limit = row.RefreshUntil
	}
	if !now.Before(limit) || (clientToken != "" && clientToken != row.ClientToken) {
		return nil, errInvalidToken
	}
	switch row.Edges.User.Status {
	case user.StatusBanned:
		return nil, errBanned
	case user.StatusDeleted:
		return nil, errInvalidToken
	}
	f := &found{row: row, user: row.Edges.User, player: row.Edges.Player}
	if f.player != nil && f.player.UserID != f.user.ID {
		f.player = nil // an admin gave the player to someone else: no longer this token's
	}
	return f, nil
}

// RefreshRequest is POST /authserver/refresh.
type RefreshRequest struct {
	AccessToken     string      `json:"accessToken"`
	ClientToken     string      `json:"clientToken"`
	RequestUser     bool        `json:"requestUser"`
	SelectedProfile *ProfileRef `json:"selectedProfile"`
}

// Refresh replaces a token (valid, or expired but still refreshable) with a new one,
// optionally binding a profile to a token that has none.
func (s *Service) Refresh(ctx context.Context, in RefreshRequest, p Policy) (*AuthAnswer, error) {
	t, err := s.find(ctx, in.AccessToken, in.ClientToken, true)
	if err != nil {
		return nil, err
	}
	// A refresh issues a new refresh window: without this check a token taken before
	// verification was required (or before the email changed) would never run out.
	if p.RequireVerified && t.user.EmailVerifiedAt == nil {
		return nil, errUnverified
	}
	refs, players, err := s.profilesOf(ctx, t.user.ID)
	if err != nil {
		return nil, err
	}
	bound := t.player
	if in.SelectedProfile != nil {
		if t.player != nil {
			return nil, illegal("Access token already has a profile assigned.")
		}
		id, _ := normalizeUUID(in.SelectedProfile.ID)
		bound = nil
		for _, pl := range players {
			if pl.UUID == id {
				bound = pl
			}
		}
		if bound == nil {
			return nil, forbidden("Invalid profile.")
		}
	}
	k, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	var token string
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		n, err := tx.YggToken.Delete().Where(yggtoken.ID(t.row.ID)).Exec(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return errInvalidToken // refreshed by a concurrent request: one wins
		}
		token, err = s.issue(ctx, tx, k, t.user, t.row.ClientToken, bound, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := &AuthAnswer{AccessToken: token, ClientToken: t.row.ClientToken, AvailableProfiles: refs}
	if out.AvailableProfiles == nil {
		out.AvailableProfiles = []ProfileRef{}
	}
	if bound != nil {
		out.SelectedProfile = &ProfileRef{ID: bound.UUID, Name: bound.Name}
	}
	if in.RequestUser {
		out.User = userInfo(t.user)
	}
	return out, nil
}

// Validate checks that a token is valid (and belongs to clientToken, if given).
func (s *Service) Validate(ctx context.Context, token, clientToken string) error {
	_, err := s.find(ctx, token, clientToken, false)
	return err
}

// Invalidate revokes a token; an unknown one is not an error.
func (s *Service) Invalidate(ctx context.Context, token string) error {
	_, err := s.db.YggToken.Delete().Where(yggtoken.TokenHash(hashToken(token))).Exec(ctx)
	return err
}

// Signout revokes every token of the account, given its credentials. Blessing Skin
// revoked nothing when the username was a player name or an email in another case.
func (s *Service) Signout(ctx context.Context, username, password string, meta account.RequestMeta) error {
	u, _, err := s.checkCredentials(ctx, username, password, meta)
	if err != nil {
		return err
	}
	_, err = s.db.YggToken.Delete().Where(yggtoken.UserID(u.ID)).Exec(ctx)
	return err
}

// Tokens is how many launcher tokens a user holds (for their profile page).
func (s *Service) Tokens(ctx context.Context, userID int64) (int, error) {
	return s.db.YggToken.Query().Where(yggtoken.UserID(userID), yggtoken.RefreshUntilGT(s.now())).Count(ctx)
}

// RevokeAll signs every launcher of a user out.
func (s *Service) RevokeAll(ctx context.Context, userID int64) (int, error) {
	return s.db.YggToken.Delete().Where(yggtoken.UserID(userID)).Exec(ctx)
}

func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return tx.Commit()
}
