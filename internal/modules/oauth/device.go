package oauth

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthclient"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthdevicecode"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

const (
	// DeviceGrantType is the RFC 8628 grant_type value.
	DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
	// DeviceCodeTTL is how long a user has to enter a user_code.
	DeviceCodeTTL = 10 * time.Minute
	// DefaultDeviceInterval is the initial polling interval in seconds.
	DefaultDeviceInterval = 5
)

// Unambiguous consonants for human-entered RFC 8628 user codes (no 0/O/1/I or vowels).
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

func randomUserCode() string {
	var raw [8]byte
	var buf [32]byte
	filled := 0
	limit := byte(256 - (256 % len(userCodeAlphabet)))
	for filled < 8 {
		_, _ = rand.Read(buf[:])
		for _, b := range buf {
			if b >= limit {
				continue
			}
			raw[filled] = userCodeAlphabet[int(b)%len(userCodeAlphabet)]
			filled++
			if filled == 8 {
				break
			}
		}
	}
	return string(raw[:4]) + "-" + string(raw[4:])
}

func normalizeUserCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DeviceAuthResponse is returned by POST /oauth/device_authorization (RFC 8628 §3.2).
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DevicePrepared describes a pending device authorization awaiting user consent.
type DevicePrepared struct {
	Client            *ent.OAuthClient
	Owner             string
	Scopes            []string
	NeedsPlayerSelect bool
	Players           []PlayerOption
	ExpiresAt         time.Time
}

// RequestDeviceCode starts an RFC 8628 Device Authorization Grant flow.
func (s *Service) RequestDeviceCode(ctx context.Context, clientID, clientSecret, rawScope, siteURL string, p Policy) (*DeviceAuthResponse, error) {
	if !p.EnableOIDC {
		return nil, &Error{http.StatusForbidden, "unauthorized_client", "OpenID Connect and Device Authorization Grant are disabled."}
	}
	c, err := s.authClient(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	scopes, unknown := ParseScopes(rawScope)
	if unknown != "" {
		return nil, &Error{http.StatusBadRequest, "invalid_scope", "Unknown scope " + unknown + "."}
	}
	if err := ValidateScopeCombination(scopes, p.EnableOIDC); err != nil {
		return nil, err
	}
	if c.Dynamic {
		if err := ValidateDynamicClientScopes(scopes); err != nil {
			return nil, err
		}
	}
	now := s.now()
	pendingCount, err := s.db.OAuthDeviceCode.Query().
		Where(oauthdevicecode.ClientID(c.ID), oauthdevicecode.StatusEQ(oauthdevicecode.StatusPending), oauthdevicecode.ExpiresAtGT(now)).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	if pendingCount >= 100 {
		return nil, &Error{http.StatusTooManyRequests, "slow_down", "Too many active device authorization requests for this client."}
	}
	expiresAt := now.Add(DeviceCodeTTL)
	deviceCode := randomToken("")
	var userCode string
	for range 5 {
		userCode = randomUserCode()
		err = s.db.OAuthDeviceCode.Create().
			SetDeviceCodeHash(hash(deviceCode)).
			SetUserCodeHash(hash(normalizeUserCode(userCode))).
			SetClientID(c.ID).
			SetScopes(scopes).
			SetStatus(oauthdevicecode.StatusPending).
			SetIntervalSec(DefaultDeviceInterval).
			SetExpiresAt(expiresAt).
			SetCreatedAt(now).
			Exec(ctx)
		if db.IsConstraint(err) {
			continue
		}
		break
	}
	if err != nil {
		return nil, err
	}
	if c.Dynamic {
		_ = s.db.OAuthClient.UpdateOneID(c.ID).SetLastUsedAt(now).Exec(ctx)
	}
	base := strings.TrimRight(siteURL, "/")
	verifyURI := base + "/user/oauth/device"
	return &DeviceAuthResponse{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         verifyURI,
		VerificationURIComplete: verifyURI + "?user_code=" + url.QueryEscape(userCode),
		ExpiresIn:               int(DeviceCodeTTL.Seconds()),
		Interval:                DefaultDeviceInterval,
	}, nil
}

// LookupDeviceCode resolves a user-entered device code for the browser confirmation screen.
func (s *Service) LookupDeviceCode(ctx context.Context, u *ent.User, rawUserCode string, p Policy) (*DevicePrepared, error) {
	if !p.EnableOIDC {
		return nil, &RequestError{"OpenID Connect and Device Authorization are disabled on this site."}
	}
	norm := normalizeUserCode(rawUserCode)
	if len(norm) < 6 {
		return nil, &RequestError{"Invalid or expired device code."}
	}
	row, err := s.db.OAuthDeviceCode.Query().
		Where(oauthdevicecode.UserCodeHash(hash(norm))).
		WithClient(func(q *ent.OAuthClientQuery) {
			q.WithOwner(func(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldNickname, user.FieldStatus) })
		}).Only(ctx)
	if ent.IsNotFound(err) || (err == nil && (row.Status != oauthdevicecode.StatusPending || !s.now().Before(row.ExpiresAt))) {
		return nil, &RequestError{"Invalid or expired device code."}
	}
	if err != nil {
		return nil, err
	}
	c := row.Edges.Client
	if c == nil || (!c.Dynamic && (c.Edges.Owner == nil || c.Edges.Owner.Status != user.StatusActive)) {
		return nil, &RequestError{"This application is no longer active."}
	}
	if c.Dynamic {
		if err := ValidateDynamicClientScopes(row.Scopes); err != nil {
			return nil, &RequestError{err.Description}
		}
	}
	scopes := allowedFor(row.Scopes, isAdmin(u))
	if len(scopes) == 0 {
		return nil, &RequestError{"Only administrators can grant these scopes."}
	}
	if p.RequireVerified && u.EmailVerifiedAt == nil && slices.Contains(scopes, ScopeYggServerJoin) {
		return nil, &RequestError{"Verify your email address before authorizing game server login."}
	}
	owner := ""
	if !c.Dynamic && c.Edges.Owner != nil {
		owner = c.Edges.Owner.Nickname
	}
	out := &DevicePrepared{
		Client:    c,
		Owner:     owner,
		Scopes:    scopes,
		ExpiresAt: row.ExpiresAt,
	}
	if slices.Contains(scopes, ScopeYggProfilesSelect) {
		out.NeedsPlayerSelect = true
		players, err := s.playerOptions(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		out.Players = players
	}
	return out, nil
}

// ApproveDeviceCode approves or denies a pending RFC 8628 device code in the browser.
func (s *Service) ApproveDeviceCode(ctx context.Context, u *ent.User, rawUserCode string, allow bool, playerID *int64, p Policy) (*DevicePrepared, error) {
	prep, err := s.LookupDeviceCode(ctx, u, rawUserCode, p)
	if err != nil {
		return nil, err
	}
	norm := normalizeUserCode(rawUserCode)
	var selectedPlayerID *int64
	if allow && prep.NeedsPlayerSelect {
		if playerID == nil || *playerID <= 0 {
			return nil, &validate.FieldError{Field: "playerId", Message: "select one of your players"}
		}
		ok := false
		for _, pl := range prep.Players {
			if pl.ID == *playerID {
				ok = true
				break
			}
		}
		if !ok {
			return nil, &validate.FieldError{Field: "playerId", Message: "select one of your players"}
		}
		v := *playerID
		selectedPlayerID = &v
	}

	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		now := s.now()
		row, err := tx.OAuthDeviceCode.Query().Where(oauthdevicecode.UserCodeHash(hash(norm))).Only(ctx)
		if ent.IsNotFound(err) || (err == nil && (row.Status != oauthdevicecode.StatusPending || !now.Before(row.ExpiresAt))) {
			return &RequestError{"Invalid or expired device code."}
		}
		if err != nil {
			return err
		}
		upd := tx.OAuthDeviceCode.Update().
			Where(
				oauthdevicecode.ID(row.ID),
				oauthdevicecode.StatusEQ(oauthdevicecode.StatusPending),
				oauthdevicecode.ExpiresAtGT(now),
			).
			SetUserID(u.ID)
		if !allow {
			n, err := upd.SetStatus(oauthdevicecode.StatusDenied).Save(ctx)
			if err != nil {
				return err
			}
			if n == 0 {
				return &RequestError{"Invalid or expired device code."}
			}
			return nil
		}
		if selectedPlayerID != nil {
			upd.SetPlayerID(*selectedPlayerID)
		}
		n, err := upd.SetStatus(oauthdevicecode.StatusApproved).Save(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return &RequestError{"Invalid or expired device code."}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if allow {
		if err := s.recordGrant(ctx, u.ID, &Prepared{Client: prep.Client, Scopes: prep.Scopes}); err != nil {
			s.log.Warn("device flow recordGrant", "err", err)
		}
	}
	return prep, nil
}

func (s *Service) pollDeviceToken(ctx context.Context, c *ent.OAuthClient, r TokenRequest, p Policy) (*TokenResponse, error) {
	if !p.EnableOIDC {
		return nil, &Error{http.StatusBadRequest, "unsupported_grant_type", "Device Authorization Grant is disabled."}
	}
	if strings.TrimSpace(r.DeviceCode) == "" {
		return nil, invalidRequest("device_code is required.")
	}
	now := s.now()
	row, err := s.db.OAuthDeviceCode.Query().
		Where(oauthdevicecode.DeviceCodeHash(hash(r.DeviceCode))).
		WithUser().Only(ctx)
	if ent.IsNotFound(err) || (err == nil && row.ClientID != c.ID) {
		return nil, invalidGrant("Invalid device_code.")
	}
	if err != nil {
		return nil, err
	}
	if !now.Before(row.ExpiresAt) {
		_, _ = s.db.OAuthDeviceCode.Delete().Where(oauthdevicecode.ID(row.ID)).Exec(ctx)
		return nil, &Error{http.StatusBadRequest, "expired_token", "The device code has expired."}
	}
	switch row.Status {
	case oauthdevicecode.StatusPending:
		interval := time.Duration(max(row.IntervalSec, DefaultDeviceInterval)) * time.Second
		if row.LastPolledAt != nil && now.Sub(*row.LastPolledAt) < interval {
			_ = s.db.OAuthDeviceCode.UpdateOneID(row.ID).
				SetLastPolledAt(now).
				SetIntervalSec(row.IntervalSec + 5).Exec(ctx)
			return nil, &Error{http.StatusBadRequest, "slow_down", "Polling too frequently; increase interval by 5 seconds."}
		}
		_ = s.db.OAuthDeviceCode.UpdateOneID(row.ID).SetLastPolledAt(now).Exec(ctx)
		return nil, &Error{http.StatusBadRequest, "authorization_pending", "The user has not completed authorization yet."}
	case oauthdevicecode.StatusDenied:
		_, _ = s.db.OAuthDeviceCode.Delete().Where(oauthdevicecode.ID(row.ID)).Exec(ctx)
		return nil, &Error{http.StatusBadRequest, "access_denied", "The user denied the authorization request."}
	case oauthdevicecode.StatusConsumed:
		return nil, invalidGrant("The device code was already used.")
	case oauthdevicecode.StatusApproved:
		// Pre-load OIDC signing key outside the write transaction to avoid SQLite lock contention.
		if _, err := s.loadOIDCKey(ctx); err != nil {
			return nil, err
		}
	default:
		return nil, invalidGrant("Invalid device code state.")
	}

	var out *TokenResponse
	err = db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		txNow := s.now()
		n, err := tx.OAuthDeviceCode.Update().
			Where(
				oauthdevicecode.ID(row.ID),
				oauthdevicecode.StatusEQ(oauthdevicecode.StatusApproved),
				oauthdevicecode.ExpiresAtGT(txNow),
			).
			SetStatus(oauthdevicecode.StatusConsumed).Save(ctx)
		if err != nil {
			return err
		}
		if n == 0 {
			return invalidGrant("The device code is expired or was already used.")
		}
		if row.UserID == nil {
			return invalidGrant("The account is not active.")
		}
		u, err := tx.User.Get(ctx, *row.UserID)
		if ent.IsNotFound(err) || (err == nil && u.Status != user.StatusActive) {
			return invalidGrant("The account is not active.")
		}
		if err != nil {
			return err
		}
		if c.Dynamic {
			if verr := ValidateDynamicClientScopes(row.Scopes); verr != nil {
				return invalidGrant(verr.Description)
			}
		}
		scopes := allowedFor(row.Scopes, isAdmin(u))
		if p.RequireVerified && u.EmailVerifiedAt == nil && slices.Contains(scopes, ScopeYggServerJoin) {
			return invalidGrant("Verify your email address first.")
		}
		playerName := ""
		if row.PlayerID != nil {
			pl, err := tx.Player.Query().Where(entplayer.ID(*row.PlayerID), entplayer.UserID(u.ID)).Only(ctx)
			if ent.IsNotFound(err) {
				return invalidGrant("The selected player no longer exists.")
			}
			if err != nil {
				return err
			}
			playerName = pl.Name
		}
		if c.Dynamic {
			_ = tx.OAuthClient.Update().Where(oauthclient.ID(c.ID)).SetLastUsedAt(txNow).Exec(ctx)
		}
		out, err = s.issue(ctx, tx, issueParams{
			userID:     u.ID,
			clientID:   c.ID,
			scopes:     scopes,
			playerID:   row.PlayerID,
			playerName: playerName,
			authTime:   txNow,
			issuer:     r.Issuer,
		}, p)
		return err
	})
	if ent.IsNotFound(err) {
		return nil, invalidGrant("The device code is no longer valid.")
	}
	return out, err
}
