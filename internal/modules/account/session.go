package account

import (
	"context"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/session"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/clip"
)

// Session lifetimes. A "remember me" session lasts 30 days; otherwise the cookie dies
// with the browser and the server forgets the session after a day at the latest.
const (
	SessionTTL         = 24 * time.Hour
	RememberSessionTTL = 30 * 24 * time.Hour
	SetupTokenTTL      = 24 * time.Hour
	// lastSeenEvery throttles the last_seen_at write to once per interval per session.
	lastSeenEvery = 5 * time.Minute
)

// NewSession is a freshly created login. Token goes into the cookie and is never stored.
type NewSession struct {
	Token     string
	ExpiresAt time.Time
	// Persistent sessions ("remember me") get a cookie with an expiry; others a
	// browser-session cookie.
	Persistent bool
}

// Viewer is the authenticated user of a request.
type Viewer struct {
	User      *ent.User
	SessionID string
}

// IsAdmin reports whether the viewer may use the admin API.
func (v *Viewer) IsAdmin() bool {
	return v != nil && (v.User.Role == user.RoleAdmin || v.User.Role == user.RoleSuperadmin)
}

// Authenticate resolves a session cookie. It returns (nil, nil) for anything that is
// not a live session of an active user.
func (s *Service) Authenticate(ctx context.Context, token string) (*Viewer, error) {
	if !wellFormedToken(token) {
		return nil, nil
	}
	id := HashToken(token)
	sess, err := s.db.Session.Query().Where(session.ID(id)).WithUser().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	u := sess.Edges.User
	if sess.RevokedAt != nil || !now.Before(sess.ExpiresAt) || u == nil || u.Status != user.StatusActive {
		return nil, nil
	}
	if now.Sub(sess.LastSeenAt) >= lastSeenEvery {
		if err := s.db.Session.UpdateOneID(id).SetLastSeenAt(now).Exec(ctx); err != nil {
			s.log.Debug("update session last_seen_at", "err", err)
		}
	}
	return &Viewer{User: u, SessionID: id}, nil
}

// Logout revokes the session behind token. Unknown tokens are ignored.
func (s *Service) Logout(ctx context.Context, token string) error {
	if !wellFormedToken(token) {
		return nil
	}
	_, err := s.db.Session.Update().
		Where(session.ID(HashToken(token)), session.RevokedAtIsNil()).
		SetRevokedAt(s.now()).Save(ctx)
	return err
}

func (s *Service) createSession(ctx context.Context, tx *ent.Client, userID int64, remember bool, meta RequestMeta) (*NewSession, error) {
	ttl := SessionTTL
	if remember {
		ttl = RememberSessionTTL
	}
	token := NewToken()
	now := s.now()
	err := tx.Session.Create().
		SetID(HashToken(token)).
		SetUserID(userID).
		SetIP(meta.IP).
		SetUserAgent(clip.Bytes(meta.UserAgent, 512)).
		SetCreatedAt(now).
		SetLastSeenAt(now).
		SetExpiresAt(now.Add(ttl)).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &NewSession{Token: token, ExpiresAt: now.Add(ttl), Persistent: remember}, nil
}
