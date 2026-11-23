package oauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entplayer "github.com/Entropy-Increase-Team/skinloom/ent/player"
	"github.com/Entropy-Increase-Team/skinloom/ent/sitesecret"
)

const oidcSigningKeyName = "oidc.signing_key"

type oidcSigningKey struct {
	signing *rsa.PrivateKey
	kid     string
	jwk     map[string]any
}

func (s *Service) loadOIDCKey(ctx context.Context) (*oidcSigningKey, error) {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if s.oidcKey != nil {
		return s.oidcKey, nil
	}
	bits := s.KeyBits
	if bits < 2048 {
		bits = 2048
	}
	pemText, err := s.secret(ctx, oidcSigningKeyName, func() (string, error) {
		k, err := rsa.GenerateKey(rand.Reader, bits)
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
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return nil, errors.New("oidc signing key: not PEM")
	}
	var rk *rsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		var ok bool
		rk, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("oidc signing key: not RSA")
		}
	} else if parsed1, err1 := x509.ParsePKCS1PrivateKey(block.Bytes); err1 == nil {
		rk = parsed1
	} else {
		return nil, fmt.Errorf("oidc signing key: %w", err)
	}

	nB64 := base64.RawURLEncoding.EncodeToString(rk.N.Bytes())
	eB64 := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rk.E)).Bytes())
	// RFC 7638 canonical JWK thumbprint for kid.
	thumbInput := fmt.Sprintf(`{"e":%q,"kty":"RSA","n":%q}`, eB64, nB64)
	thumb := sha256.Sum256([]byte(thumbInput))
	kid := base64.RawURLEncoding.EncodeToString(thumb[:])

	s.oidcKey = &oidcSigningKey{
		signing: rk,
		kid:     kid,
		jwk: map[string]any{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": kid,
			"n":   nB64,
			"e":   eB64,
		},
	}
	return s.oidcKey, nil
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
	insertErr := s.db.SiteSecret.Create().SetID(name).SetValue(value).
		OnConflictColumns(sitesecret.FieldID).DoNothing().Exec(ctx)
	row, err = s.db.SiteSecret.Get(ctx, name)
	if err != nil {
		return "", errors.Join(insertErr, err)
	}
	return row.Value, nil
}

// JWKS returns the public JSON Web Key Set for verifying OIDC id_tokens.
func (s *Service) JWKS(ctx context.Context) (map[string]any, error) {
	k, err := s.loadOIDCKey(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"keys": []any{k.jwk}}, nil
}

// mintIDToken signs an OpenID Connect ID Token with RS256.
func (s *Service) mintIDToken(ctx context.Context, issuer, clientID, sub, nonce string, authTime time.Time, ttl time.Duration) (string, error) {
	k, err := s.loadOIDCKey(ctx)
	if err != nil {
		return "", err
	}
	now := s.now()
	if authTime.IsZero() {
		authTime = now
	}
	header, _ := json.Marshal(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": k.kid,
	})
	claims := map[string]any{
		"iss":       strings.TrimRight(issuer, "/"),
		"sub":       sub,
		"aud":       clientID,
		"iat":       now.Unix(),
		"exp":       now.Add(ttl).Unix(),
		"auth_time": authTime.Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString
	signingInput := enc(header) + "." + enc(payload)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k.signing, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + enc(sig), nil
}

// UserInfo resolves the OpenID Connect & Yggdrasil Connect UserInfo response for a
// Bearer access token.
func (s *Service) UserInfo(ctx context.Context, token, siteURL string) (map[string]any, error) {
	p, err := s.Authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(p.Scopes, ScopeOpenID) {
		return nil, ErrInsufficientScope
	}
	base := strings.TrimRight(siteURL, "/")
	uidStr := strconv.FormatInt(p.User.ID, 10)
	out := map[string]any{
		"sub": uidStr,
	}
	if slices.Contains(p.Scopes, ScopeProfile) {
		out["name"] = p.User.Nickname
		out["nickname"] = p.User.Nickname
		out["preferred_username"] = p.User.Nickname
		if base != "" {
			out["picture"] = base + "/avatar/user/" + uidStr
		}
	}
	if slices.Contains(p.Scopes, ScopeEmail) {
		out["email"] = p.User.Email
		out["email_verified"] = p.User.EmailVerifiedAt != nil
	}
	if slices.Contains(p.Scopes, ScopeYggProfilesSelect) {
		if p.PlayerID == nil {
			return nil, ErrInvalidToken
		}
		pl, err := s.db.Player.Query().Where(entplayer.ID(*p.PlayerID), entplayer.UserID(p.User.ID)).Only(ctx)
		if ent.IsNotFound(err) {
			return nil, ErrInvalidToken
		}
		if err != nil {
			return nil, err
		}
		// Yggdrasil Connect #231: renaming a bound player invalidates the access token
		// so launchers refresh and receive the updated profile name.
		if p.PlayerNameAtIssue != "" && pl.Name != p.PlayerNameAtIssue {
			return nil, ErrInvalidToken
		}
		out["selectedProfile"] = map[string]string{
			"id":   pl.UUID,
			"name": pl.Name,
		}
	}
	if slices.Contains(p.Scopes, ScopeYggProfilesRead) {
		players, err := s.db.Player.Query().Where(entplayer.UserID(p.User.ID)).
			Order(ent.Asc(entplayer.FieldID)).All(ctx)
		if err != nil {
			return nil, err
		}
		avail := make([]map[string]string, len(players))
		for i, pl := range players {
			avail[i] = map[string]string{"id": pl.UUID, "name": pl.Name}
		}
		out["availableProfiles"] = avail
	}
	return out, nil
}
