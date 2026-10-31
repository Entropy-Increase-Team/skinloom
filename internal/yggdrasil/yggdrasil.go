// Package yggdrasil implements the Yggdrasil API that authlib-injector launchers and
// game servers use for external login (contract docs/design/02 §2.4): launcher
// tokens (authserver), joining servers (sessionserver), profile lookups and texture
// uploads, compatible with Blessing Skin's yggdrasil-api plugin.
package yggdrasil

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/sitesecret"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// Error is an error in the Yggdrasil wire format: {"error": Kind, "errorMessage": ...}.
type Error struct {
	Status  int
	Kind    string
	Message string
}

func (e *Error) Error() string { return e.Kind + ": " + e.Message }

func forbidden(msg string) *Error {
	return &Error{http.StatusForbidden, "ForbiddenOperationException", msg}
}
func illegal(msg string) *Error {
	return &Error{http.StatusBadRequest, "IllegalArgumentException", msg}
}

// The messages authlib-injector and launchers know (authlib-injector's wiki, "Yggdrasil 服务端技术规范").
var (
	errInvalidToken       = forbidden("Invalid token.")
	errInvalidCredentials = forbidden("Invalid credentials. Invalid username or password.")
	errBanned             = forbidden("Your account has been banned.")
	errUnverified         = forbidden("Verify your email address on the website first.")
	errUnauthorized       = &Error{http.StatusUnauthorized, "UnauthorizedException", "Invalid token."}
)

// Policy is what the Yggdrasil API needs from the settings.
type Policy struct {
	TokenValid    time.Duration // a new token is valid this long
	TokenRefresh  time.Duration // and can be refreshed for this long (also how long it is kept)
	TokensPerUser int           // older tokens are revoked beyond this
	SearchMax     int           // names per bulk profile lookup
	SkinDomains   []string      // extra domains textures may come from
	EnableALI     bool          // send X-Authlib-Injector-API-Location
	// RequireVerified: only accounts with a verified email sign in (account.VerificationRequired).
	RequireVerified bool
}

// SettingDefs are the yggdrasil.* settings (yggdrasil.uuid_algorithm belongs to players).
func SettingDefs() []settings.Def {
	return []settings.Def{
		{Key: "yggdrasil.token_valid_seconds", Type: settings.TypeInt, Default: 259200, Range: settings.Between(60, 31536000),
			Doc: "How long a launcher's access token is valid (3 days). (BS ygg_token_expire_1)"},
		{Key: "yggdrasil.token_refresh_seconds", Type: settings.TypeInt, Default: 604800, Range: settings.Between(60, 31536000),
			Doc: "How long an expired access token can still be refreshed (7 days from issue). (BS ygg_token_expire_2)"},
		{Key: "yggdrasil.tokens_per_user", Type: settings.TypeInt, Default: 10, Range: settings.Between(1, 100),
			Doc: "Launcher tokens a user may hold; logging in again revokes the oldest. (BS ygg_tokens_limit)"},
		{Key: "yggdrasil.search_profile_max", Type: settings.TypeInt, Default: 5, Range: settings.Between(1, 100),
			Doc: "Most names in one bulk profile lookup (POST /api/yggdrasil/api/profiles/minecraft). (BS ygg_search_profile_max)"},
		{Key: "yggdrasil.skin_domains", Type: settings.TypeString, Default: "",
			Doc: "Extra domains textures are served from, comma-separated (the site and storage.public_url are always listed). (BS ygg_skin_domain)"},
		{Key: "yggdrasil.enable_ali", Type: settings.TypeBool, Default: true,
			Doc: "Send X-Authlib-Injector-API-Location on every page, so launchers find the API from the site address alone. (BS ygg_enable_ali)"},
	}
}

// PolicyFrom reads the policy from a settings snapshot.
func PolicyFrom(s *settings.Snapshot) Policy {
	p := Policy{
		TokenValid:    time.Duration(settings.Decode[int64](s, "yggdrasil.token_valid_seconds")) * time.Second,
		TokenRefresh:  time.Duration(settings.Decode[int64](s, "yggdrasil.token_refresh_seconds")) * time.Second,
		TokensPerUser: settings.Decode[int](s, "yggdrasil.tokens_per_user"),
		SearchMax:     settings.Decode[int](s, "yggdrasil.search_profile_max"),
		EnableALI:     settings.Decode[bool](s, "yggdrasil.enable_ali"),

		RequireVerified: account.VerificationRequired(s),
	}
	p.TokenRefresh = max(p.TokenRefresh, p.TokenValid)
	for _, d := range strings.Split(s.String("yggdrasil.skin_domains"), ",") {
		if d = strings.TrimSpace(d); d != "" {
			p.SkinDomains = append(p.SkinDomains, d)
		}
	}
	return p
}

// Service implements the Yggdrasil API.
type Service struct {
	db       *ent.Client
	accounts *account.Service
	players  *player.Service
	textures *texture.Service
	log      *slog.Logger
	now      func() time.Time

	// KeyBits is the size of a generated signing key: 4096 as the contract asks
	// (tests use less: generating 4096 bits takes seconds).
	KeyBits int

	keyMu sync.Mutex
	key   *keys

	sigMu sync.Mutex
	sigs  map[string]string // signed property value → signature
}

// NewService creates the service.
func NewService(db *ent.Client, accounts *account.Service, players *player.Service, textures *texture.Service, log *slog.Logger) *Service {
	return &Service{db: db, accounts: accounts, players: players, textures: textures, log: log, now: time.Now,
		KeyBits: 4096, sigs: map[string]string{}}
}

// keys are the site's Yggdrasil secrets, kept in site_secrets (shared by every
// instance, backed up with the database).
type keys struct {
	signing   *rsa.PrivateKey
	publicPEM string // for the metadata (signaturePublickey)
	publicDER []byte // for /minecraftservices/publickeys
	token     []byte // HMAC key of access tokens
}

const (
	signingKeyName = "yggdrasil.signing_key"
	tokenKeyName   = "yggdrasil.token_secret"
)

// keys loads the secrets, generating any that are missing. Several instances starting
// together may each generate one; the first stored wins and every instance reads it.
func (s *Service) keys(ctx context.Context) (*keys, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.key != nil {
		return s.key, nil
	}
	signingPEM, err := s.secret(ctx, signingKeyName, func() (string, error) {
		k, err := rsa.GenerateKey(rand.Reader, s.KeyBits)
		if err != nil {
			return "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			return "", err
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
	})
	if err != nil {
		return nil, err
	}
	tokenHex, err := s.secret(ctx, tokenKeyName, func() (string, error) {
		b := make([]byte, 32)
		_, err := rand.Read(b)
		return hex.EncodeToString(b), err
	})
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(signingPEM))
	if block == nil {
		return nil, errors.New("yggdrasil signing key: not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("yggdrasil signing key: %w", err)
	}
	signing, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("yggdrasil signing key: not RSA")
	}
	pub, err := x509.MarshalPKIXPublicKey(&signing.PublicKey)
	if err != nil {
		return nil, err
	}
	token, err := hex.DecodeString(tokenHex)
	if err != nil {
		return nil, fmt.Errorf("yggdrasil token secret: %w", err)
	}
	s.key = &keys{signing: signing, publicDER: pub, token: token,
		publicPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))}
	return s.key, nil
}

func (s *Service) secret(ctx context.Context, name string, generate func() (string, error)) (string, error) {
	row, err := s.db.SiteSecret.Get(ctx, name)
	if err == nil {
		return row.Value, nil
	}
	if !ent.IsNotFound(err) {
		return "", err
	}
	value, err := generate()
	if err != nil {
		return "", err
	}
	// Another instance may have stored its own meanwhile: keep the first. What the insert
	// reports on a conflict differs by dialect, so reading the row back decides.
	insertErr := s.db.SiteSecret.Create().SetID(name).SetValue(value).
		OnConflictColumns(sitesecret.FieldID).DoNothing().Exec(ctx)
	row, err = s.db.SiteSecret.Get(ctx, name)
	if err != nil {
		return "", errors.Join(insertErr, err)
	}
	return row.Value, nil
}

// Warm loads (or generates) the keys ahead of the first request.
func (s *Service) Warm(ctx context.Context) error {
	_, err := s.keys(ctx)
	return err
}

// PublicKey is the signature public key: PEM (metadata) and DER (publickeys API).
func (s *Service) PublicKey(ctx context.Context) (pemText string, der []byte, err error) {
	k, err := s.keys(ctx)
	if err != nil {
		return "", nil, err
	}
	return k.publicPEM, k.publicDER, nil
}

// userUUID is the Yggdrasil id of a user: a version 5 UUID of the email in the DNS
// namespace, as Blessing Skin's plugin made it (so migrated launchers see the same
// id), always of the normalized address (BS used the stored case in one place and
// lowercase in another, issue #212).
func userUUID(email string) string {
	ns := []byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	h := sha1.Sum(append(ns, []byte(account.NormalizeEmail(email))...))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return hex.EncodeToString(h[:16])
}

// normalizeUUID accepts a UUID with or without dashes, any case.
func normalizeUUID(id string) (string, bool) {
	id = strings.ToLower(strings.ReplaceAll(id, "-", ""))
	if len(id) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", false
	}
	return id, true
}
