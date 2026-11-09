// Package account owns users, browser sessions, one-time tokens, the first-run setup
// and the audit log. HTTP handlers live in internal/httpapi; this package has no
// knowledge of cookies or requests beyond RequestMeta.
package account

import (
	"context"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	auditlog "github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/clip"
)

// RequestMeta describes the client for audit records and rate limits.
type RequestMeta struct {
	IP        string
	UserAgent string
}

// SetHooks sets the bus account events are published to (none before it is set).
func (s *Service) SetHooks(b *hook.Bus) { s.hooks = b }

// Service implements accounts and sessions.
type Service struct {
	db     *ent.Client
	log    *slog.Logger
	hasher *Hasher
	now    func() time.Time

	ipLimit            *Limiter
	clientAccountLimit *Limiter
	accountLimit       *Limiter
	registrations      *Limiter // per address (IPv6 /64) per day; only counted, see Register
	registerAttempts   *Limiter // registration requests per address, successful or not
	forgotIP           *Limiter // password reset requests per address
	forgotAccount      *Limiter // password reset requests per email
	resendMinute       *Limiter // verification mails per user
	resendDay          *Limiter
	passwordChecks     *Limiter // wrong current passwords per user (profile changes)
	emailChanges       *Limiter // email change attempts per user
	setupDone          atomic.Bool
	notify             func()                                          // see SetJobNotifier
	hooks              *hook.Bus                                       // see SetHooks
	onDelete           func(context.Context, *ent.Client, int64) error // see SetBeforeDelete
}

// NewService creates the service. Failed logins are limited per 15 minutes:
//   - 50 per client address (IPv6 per /64), whatever the account;
//   - 10 per account from one address, which stops guessing without letting a
//     single attacker lock the real user (e.g. the administrator) out;
//   - per account from all addresses together: with a captcha provider, 20
//     failures make every client solve a captcha; without one, 100 failures block.
//
// With a captcha provider a client also has to solve one after
// auth.login_captcha_after failures of its own (LoginPolicy).
func NewService(db *ent.Client, log *slog.Logger) *Service {
	return &Service{
		db: db, log: log, hasher: NewHasher(0), now: time.Now,
		ipLimit:            NewLimiter(50, 15*time.Minute),
		clientAccountLimit: NewLimiter(10, 15*time.Minute),
		accountLimit:       NewLimiter(100, 15*time.Minute),
		registrations:      NewLimiter(math.MaxInt, 24*time.Hour),
		registerAttempts:   NewLimiter(30, time.Hour),
		forgotIP:           NewLimiter(10, time.Hour),
		forgotAccount:      NewLimiter(3, time.Hour),
		resendMinute:       NewLimiter(1, time.Minute),
		resendDay:          NewLimiter(5, 24*time.Hour),
		passwordChecks:     NewLimiter(5, 15*time.Minute),
		emailChanges:       NewLimiter(10, time.Hour),
	}
}

// clipped bounds client-supplied metadata to the column sizes; an IPv6 address with a
// zone, or a huge User-Agent, must not fail a login with a database error.
func (m RequestMeta) clipped() RequestMeta {
	return RequestMeta{IP: clip.Bytes(m.IP, 45), UserAgent: clip.Bytes(m.UserAgent, 512)}
}

// Audit records an action by actor (nil = system). Modules call this after a change
// that happened outside the account service, e.g. a settings update.
func (s *Service) Audit(ctx context.Context, actor *int64, action, targetType, targetID, ip string, meta map[string]any) error {
	return audit(ctx, s.db, actor, action, targetType, targetID, ip, meta)
}

func audit(ctx context.Context, tx *ent.Client, actor *int64, action, targetType, targetID, ip string, meta map[string]any) error {
	return auditlog.Write(ctx, tx, &auditlog.Entry{Actor: actor, Action: action, TargetType: targetType, TargetID: targetID, IP: ip, Meta: meta})
}
