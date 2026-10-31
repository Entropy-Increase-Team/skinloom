// Package social signs people in with accounts they have elsewhere (contract
// docs/design/02 F07, F08): GitHub, Microsoft and any OpenID Connect provider, set up
// in auth.social.providers. A provider account is linked to one site account; people
// without one create an account (no password) or link the provider to the account
// they already have.
package social

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/socialaccount"
	"github.com/Entropy-Increase-Team/skinloom/ent/socialflow"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// How long a sign-in may take at the provider, and how long a new account may wait
// for its nickname.
const (
	FlowTTL   = 10 * time.Minute
	SignupTTL = 15 * time.Minute
)

var (
	ErrUnknownProvider = errors.New("unknown sign-in provider")
	ErrNoFlow          = errors.New("the sign-in has expired; start again")
	ErrLinkedElsewhere = errors.New("this provider account is linked to another user")
	ErrAlreadyLinked   = errors.New("an account of this provider is already linked")
	ErrNotLinked       = errors.New("no account of this provider is linked")
	ErrLastSignIn      = errors.New("set a password before unlinking the only way to sign in")
)

// Codes of a failed callback, for the page the browser lands on.
const (
	CodeExpired  = "expired"  // no matching sign-in in progress
	CodeDenied   = "denied"   // the person cancelled at the provider
	CodeFailed   = "failed"   // the provider could not be reached or answered nonsense
	CodeDisabled = "disabled" // the account is banned
	CodeGone     = "gone"     // the account was deleted
	CodeTaken    = "taken"    // link: the provider account belongs to someone else
	CodeAlready  = "already"  // link: the user already linked another account there
)

// FlowError is a failed callback. Link says it was linking an account (the browser
// goes back to the profile page, not the sign-in page).
type FlowError struct {
	Code string
	Link bool
	Err  error
}

func (e *FlowError) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *FlowError) Unwrap() error { return e.Err }

// Service implements social sign-in.
type Service struct {
	db       *ent.Client
	accounts *account.Service
	log      *slog.Logger
	client   *client
	now      func() time.Time
}

// NewService creates the service.
func NewService(db *ent.Client, accounts *account.Service, log *slog.Logger) *Service {
	return &Service{db: db, accounts: accounts, log: log, client: newClient(), now: time.Now}
}

// StartInput begins a sign-in.
type StartInput struct {
	Provider string
	// UserID links the provider account to this user instead of signing in.
	UserID   *int64
	Next     string // a path on this site to return to
	Remember bool
}

// Start records a sign-in in progress and returns the provider URL to send the browser
// to, and the value for the browser's flow cookie.
func (s *Service) Start(ctx context.Context, p Policy, in StartInput) (redirect, cookie string, err error) {
	cfg, ok := p.Provider(in.Provider)
	if !ok {
		return "", "", ErrUnknownProvider
	}
	if in.UserID != nil {
		linked, err := s.db.SocialAccount.Query().Where(socialaccount.UserID(*in.UserID), socialaccount.Provider(cfg.ID)).Exist(ctx)
		if err != nil || linked {
			return "", "", errorOr(err, ErrAlreadyLinked)
		}
	}
	eps, err := s.client.endpoints(ctx, cfg)
	if err != nil {
		return "", "", &FlowError{Code: CodeFailed, Link: in.UserID != nil, Err: err}
	}
	state, verifier, nonce := account.NewToken(), account.NewToken(), account.NewToken()
	purpose := socialflow.PurposeLogin
	if in.UserID != nil {
		purpose = socialflow.PurposeLink
	}
	err = s.db.SocialFlow.Create().
		SetID(account.HashToken(state)).
		SetPurpose(purpose).
		SetProvider(cfg.ID).
		SetNillableUserID(in.UserID).
		SetVerifier(verifier).
		SetNonce(nonce).
		SetNext(CleanNext(in.Next)).
		SetRemember(in.Remember).
		SetCreatedAt(s.now()).
		SetExpiresAt(s.now().Add(FlowTTL)).
		Exec(ctx)
	if err != nil {
		return "", "", err
	}
	return s.client.authURL(cfg, eps, p.CallbackURL(cfg.ID), state, challenge(verifier), nonce), state, nil
}

// CallbackInput is the provider sending the browser back.
type CallbackInput struct {
	Provider string
	State    string
	Code     string
	Error    string // the provider's error parameter
	Cookie   string // the flow cookie
	// ViewerID is who the browser is signed in as, if anyone.
	ViewerID *int64
	Meta     account.RequestMeta
}

// Result of a callback: one of signed in (User, Session), linked, or a new account
// to set up (Signup is the new flow cookie).
type Result struct {
	User    *ent.User
	Session *account.NewSession
	Linked  bool
	Signup  string
	Next    string
}

// Callback finishes a sign-in. Errors are *FlowError, apart from database failures.
func (s *Service) Callback(ctx context.Context, p Policy, in CallbackInput) (*Result, error) {
	// The state must be the one this browser started with (RFC 6749 §10.12): otherwise
	// another site could send a visitor here with a code for the attacker's account.
	if in.State == "" || subtle.ConstantTimeCompare([]byte(in.State), []byte(in.Cookie)) != 1 {
		return nil, &FlowError{Code: CodeExpired, Err: ErrNoFlow}
	}
	f, err := s.spend(ctx, in.State, in.Provider, socialflow.PurposeLogin, socialflow.PurposeLink)
	if err != nil {
		return nil, err
	}
	linking := f.Purpose == socialflow.PurposeLink
	fail := func(code string, err error) (*Result, error) {
		return nil, &FlowError{Code: code, Link: linking, Err: err}
	}
	if linking && (in.ViewerID == nil || f.UserID == nil || *in.ViewerID != *f.UserID) {
		return fail(CodeExpired, errors.New("signed in as someone else meanwhile"))
	}
	switch in.Error {
	case "":
	case "access_denied":
		return fail(CodeDenied, errors.New("cancelled at the provider"))
	default:
		return fail(CodeFailed, errors.New("provider error "+truncate(in.Error, 64)))
	}
	cfg, ok := p.Provider(f.Provider)
	if !ok || in.Code == "" {
		return fail(CodeFailed, ErrUnknownProvider)
	}
	eps, err := s.client.endpoints(ctx, cfg)
	if err != nil {
		return fail(CodeFailed, err)
	}
	id, err := s.client.identity(ctx, cfg, eps, p.CallbackURL(cfg.ID), in.Code, f.Verifier, f.Nonce)
	if err != nil {
		s.log.Warn("social sign-in failed", "provider", cfg.ID, "err", err)
		return fail(CodeFailed, err)
	}

	if linking {
		switch err := s.link(ctx, *f.UserID, cfg.ID, id, in.Meta); {
		case errors.Is(err, ErrLinkedElsewhere):
			return fail(CodeTaken, err)
		case errors.Is(err, ErrAlreadyLinked):
			return fail(CodeAlready, err)
		case err != nil:
			return nil, err
		}
		return &Result{Linked: true, Next: f.Next}, nil
	}

	acct, err := s.db.SocialAccount.Query().
		Where(socialaccount.Provider(cfg.ID), socialaccount.Subject(id.Subject)).WithUser().Only(ctx)
	switch {
	case err == nil:
		return s.signIn(ctx, acct.Edges.User, f, id, in.Meta)
	case !ent.IsNotFound(err):
		return nil, err
	}
	// A verified address of an account whose address is verified too: the same
	// person, link them (Blessing Skin users who signed in with a provider have no
	// other link). An unverified side could be someone else's: an attacker who
	// registered the victim's address first, or a provider that lets anyone claim one.
	if id.EmailVerified {
		u, err := s.db.User.Query().
			Where(user.EmailNormalized(account.NormalizeEmail(id.Email)), user.EmailVerifiedAtNotNil()).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
		if u != nil && u.Status != user.StatusBanned {
			switch err := s.link(ctx, u.ID, cfg.ID, id, in.Meta); {
			case err == nil:
				return s.signIn(ctx, u, f, id, in.Meta)
			case !errors.Is(err, ErrAlreadyLinked) && !errors.Is(err, ErrLinkedElsewhere):
				return nil, err
			}
		} else if u != nil {
			return fail(CodeDisabled, account.ErrAccountDisabled)
		}
	}
	// Someone new: they pick a nickname, or link an account they already have.
	signup := account.NewToken()
	err = s.db.SocialFlow.Create().
		SetID(account.HashToken(signup)).
		SetPurpose(socialflow.PurposeSignup).
		SetProvider(cfg.ID).
		SetNext(f.Next).
		SetRemember(f.Remember).
		SetSubject(id.Subject).
		SetEmail(truncate(id.Email, 255)).
		SetEmailVerified(id.EmailVerified).
		SetName(truncate(id.Name, 255)).
		SetNickname(truncate(id.Nickname, 255)).
		SetCreatedAt(s.now()).
		SetExpiresAt(s.now().Add(SignupTTL)).
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &Result{Signup: signup, Next: f.Next}, nil
}

func (s *Service) signIn(ctx context.Context, u *ent.User, f *ent.SocialFlow, id *Identity, meta account.RequestMeta) (*Result, error) {
	sess, err := s.accounts.SignIn(ctx, u, f.Remember, "social:"+f.Provider, meta)
	switch {
	case errors.Is(err, account.ErrAccountDisabled):
		return nil, &FlowError{Code: CodeDisabled, Err: err}
	case errors.Is(err, account.ErrAccountGone):
		return nil, &FlowError{Code: CodeGone, Err: err}
	case err != nil:
		return nil, err
	}
	err = s.db.SocialAccount.Update().
		Where(socialaccount.Provider(f.Provider), socialaccount.Subject(id.Subject)).
		SetEmail(truncate(id.Email, 255)).SetName(truncate(id.Name, 255)).SetLastUsedAt(s.now()).
		Exec(ctx)
	if err != nil {
		s.log.Debug("update social account", "err", err)
	}
	return &Result{User: u, Session: sess, Next: f.Next}, nil
}

// link connects the provider account id to userID. Checked first, inserted alone:
// after a failed insert PostgreSQL refuses the rest of a transaction.
func (s *Service) link(ctx context.Context, userID int64, provider string, id *Identity, meta account.RequestMeta) error {
	existing, err := s.db.SocialAccount.Query().Where(socialaccount.Provider(provider), socialaccount.Subject(id.Subject)).Only(ctx)
	switch {
	case err == nil && existing.UserID == userID:
		return nil
	case err == nil:
		return ErrLinkedElsewhere
	case !ent.IsNotFound(err):
		return err
	}
	if has, err := s.db.SocialAccount.Query().Where(socialaccount.UserID(userID), socialaccount.Provider(provider)).Exist(ctx); err != nil || has {
		return errorOr(err, ErrAlreadyLinked)
	}
	if err := s.createLink(ctx, s.db, userID, provider, id); err != nil {
		return err
	}
	return s.audit(ctx, userID, "account.social_linked", provider, meta)
}

func (s *Service) createLink(ctx context.Context, client *ent.Client, userID int64, provider string, id *Identity) error {
	err := client.SocialAccount.Create().
		SetProvider(provider).
		SetSubject(id.Subject).
		SetUserID(userID).
		SetEmail(truncate(id.Email, 255)).
		SetName(truncate(id.Name, 255)).
		SetCreatedAt(s.now()).
		SetLastUsedAt(s.now()).
		Exec(ctx)
	if db.IsConstraint(err) {
		return ErrLinkedElsewhere // a second tab won the race
	}
	return err
}

// spend deletes the live flow behind cookie, so it works once.
func (s *Service) spend(ctx context.Context, cookie, provider string, purposes ...socialflow.Purpose) (*ent.SocialFlow, error) {
	expired := &FlowError{Code: CodeExpired, Err: ErrNoFlow}
	if cookie == "" {
		return nil, expired
	}
	f, err := s.db.SocialFlow.Get(ctx, account.HashToken(cookie))
	if ent.IsNotFound(err) {
		return nil, expired
	}
	if err != nil {
		return nil, err
	}
	n, err := s.db.SocialFlow.Delete().Where(socialflow.ID(f.ID)).Exec(ctx)
	if err != nil {
		return nil, err
	}
	expired.Link = f.Purpose == socialflow.PurposeLink
	if n == 0 || !s.now().Before(f.ExpiresAt) || (provider != "" && f.Provider != provider) || !slices.Contains(purposes, f.Purpose) {
		return nil, expired
	}
	return f, nil
}

// Pending is the provider account waiting to become (or join) a site account.
func (s *Service) Pending(ctx context.Context, cookie string) (*ent.SocialFlow, error) {
	if cookie == "" {
		return nil, ErrNoFlow
	}
	f, err := s.db.SocialFlow.Get(ctx, account.HashToken(cookie))
	if ent.IsNotFound(err) || (err == nil && (f.Purpose != socialflow.PurposeSignup || !s.now().Before(f.ExpiresAt))) {
		return nil, ErrNoFlow
	}
	return f, err
}

// RegisterInput creates an account for the pending provider account.
type RegisterInput struct {
	Cookie   string
	Email    string
	Nickname string
}

// Register creates an account for the pending provider account and signs it in. The
// address counts as verified if it is the one the provider verified.
func (s *Service) Register(ctx context.Context, p Policy, in RegisterInput, rp account.RegisterPolicy, mc account.MailContext, meta account.RequestMeta) (*ent.User, *account.NewSession, string, error) {
	f, err := s.Pending(ctx, in.Cookie)
	if err != nil {
		return nil, nil, "", err
	}
	if _, ok := p.Provider(f.Provider); !ok {
		return nil, nil, "", ErrUnknownProvider
	}
	id := &Identity{Subject: f.Subject, Email: f.Email, Name: f.Name}
	verified := f.EmailVerified && account.NormalizeEmail(in.Email) == account.NormalizeEmail(f.Email)
	u, sess, err := s.accounts.RegisterExternal(ctx, account.ExternalInput{
		Email: in.Email, Nickname: in.Nickname, EmailVerified: verified, Remember: f.Remember, Method: "social:" + f.Provider,
		Created: func(ctx context.Context, tx *ent.Client, u *ent.User) error {
			if n, err := tx.SocialFlow.Delete().Where(socialflow.ID(f.ID)).Exec(ctx); err != nil || n == 0 {
				return errorOr(err, ErrNoFlow) // used by another request meanwhile
			}
			return s.createLink(ctx, tx, u.ID, f.Provider, id)
		},
	}, rp, mc, meta)
	if err != nil {
		return nil, nil, "", err
	}
	return u, sess, f.Next, nil
}

// LinkPending links the pending provider account to userID, who signed in with their
// password to claim it.
func (s *Service) LinkPending(ctx context.Context, cookie string, userID int64, meta account.RequestMeta) (string, error) {
	f, err := s.spend(ctx, cookie, "", socialflow.PurposeSignup)
	if err != nil {
		return "", ErrNoFlow
	}
	return f.Provider, s.link(ctx, userID, f.Provider, &Identity{Subject: f.Subject, Email: f.Email, Name: f.Name}, meta)
}

// Links are the provider accounts linked to userID.
func (s *Service) Links(ctx context.Context, userID int64) ([]*ent.SocialAccount, error) {
	return s.db.SocialAccount.Query().Where(socialaccount.UserID(userID)).Order(ent.Asc(socialaccount.FieldCreatedAt)).All(ctx)
}

// Unlink removes u's link to provider. An account without a password keeps its last
// link to a provider that is still configured: it would have no way to sign in. Links
// to removed providers do not count, and can always be removed.
func (s *Service) Unlink(ctx context.Context, p Policy, u *ent.User, provider string, meta account.RequestMeta) error {
	err := withTx(ctx, s.db, func(tx *ent.Client) error {
		// Lock the user's row first, so two unlinks at once cannot both see the other
		// link still there and leave none.
		if err := tx.User.UpdateOneID(u.ID).SetUpdatedAt(s.now()).Exec(ctx); err != nil {
			return err
		}
		links, err := tx.SocialAccount.Query().Where(socialaccount.UserID(u.ID)).All(ctx)
		if err != nil {
			return err
		}
		found, usable, others := false, false, 0
		for _, l := range links {
			_, ok := p.Provider(l.Provider)
			if l.Provider == provider {
				found, usable = true, ok
			} else if ok {
				others++
			}
		}
		switch {
		case !found:
			return ErrNotLinked
		case usable && others == 0 && !account.HasPassword(u):
			return ErrLastSignIn
		}
		_, err = tx.SocialAccount.Delete().Where(socialaccount.UserID(u.ID), socialaccount.Provider(provider)).Exec(ctx)
		return err
	})
	if err != nil {
		return err
	}
	return s.audit(ctx, u.ID, "account.social_unlinked", provider, meta)
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

// Cleanup deletes sign-ins nobody finished.
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	return s.db.SocialFlow.Delete().Where(socialflow.ExpiresAtLT(s.now())).Exec(ctx)
}

func (s *Service) audit(ctx context.Context, userID int64, action, provider string, meta account.RequestMeta) error {
	return s.accounts.Audit(ctx, &userID, action, "user", strconv.FormatInt(userID, 10), meta.IP, map[string]any{"provider": provider})
}

// CleanNext keeps next if it is a path on this site, else returns "".
func CleanNext(next string) string {
	if len(next) > 512 || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsRune(next, '\\') ||
		strings.ContainsFunc(next, unicode.IsControl) {
		return ""
	}
	return next
}

// challenge is the PKCE S256 code challenge of verifier (RFC 7636 §4.2).
func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func errorOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
