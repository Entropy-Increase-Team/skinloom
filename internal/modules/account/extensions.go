package account

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

var extVerifierSem = make(chan struct{}, 16)

// PasswordVerifier verifies a stored password hash produced by an external algorithm
// (for example a legacy forum hash checked by a plugin).
type PasswordVerifier func(ctx context.Context, storedHash, password string) (bool, error)

// ExternalUserInfo is the verified identity returned by a plugin auth provider.
type ExternalUserInfo struct {
	Subject       string `json:"subject"`
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"emailVerified,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
	Next          string `json:"next,omitempty"`
}

// AuthProviderDef registers a plugin-provided login or identity provider.
type AuthProviderDef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Icon    string `json:"icon,omitempty"`
	AuthURL string `json:"authUrl,omitempty"`
	Mode    string `json:"mode,omitempty"` // credentials | redirect
	Owner   string `json:"-"`

	Verify func(ctx context.Context, input map[string]any) (*ExternalUserInfo, error) `json:"-"`
}

type verifierEntry struct {
	owner string
	fn    PasswordVerifier
}

func isBuiltinPasswordAlgo(algo string) bool {
	switch algo {
	case AlgoArgon2id, AlgoArgon2i, AlgoBcrypt,
		AlgoMD5, AlgoSalted2MD5,
		AlgoSHA256, AlgoSalted2SHA256,
		AlgoSHA512, AlgoSalted2SHA512:
		return true
	default:
		return false
	}
}

// RegisterPasswordVerifier registers an external password verification algorithm owned
// by owner (typically "plugin:<id>"). Built-in algorithms cannot be overridden.
func (s *Service) RegisterPasswordVerifier(owner, algo string, fn PasswordVerifier) error {
	algo = strings.TrimSpace(algo)
	if algo == "" || fn == nil {
		return fmt.Errorf("invalid password verifier %q", algo)
	}
	if isBuiltinPasswordAlgo(algo) {
		return fmt.Errorf("cannot override built-in password verifier %q", algo)
	}
	s.extMu.Lock()
	defer s.extMu.Unlock()
	if s.verifiers == nil {
		s.verifiers = map[string]verifierEntry{}
	}
	if existing, exists := s.verifiers[algo]; exists && existing.owner != owner {
		return fmt.Errorf("password verifier %q is already registered by %s", algo, existing.owner)
	}
	s.verifiers[algo] = verifierEntry{owner: owner, fn: fn}
	return nil
}

// UnregisterPasswordVerifiers removes every password verifier registered by owner.
func (s *Service) UnregisterPasswordVerifiers(owner string) {
	s.extMu.Lock()
	defer s.extMu.Unlock()
	for k, v := range s.verifiers {
		if v.owner == owner {
			delete(s.verifiers, k)
		}
	}
}

// RegisterAuthProvider registers a plugin login provider owned by owner.
func (s *Service) RegisterAuthProvider(owner string, def AuthProviderDef) error {
	def.ID = strings.TrimSpace(def.ID)
	if def.ID == "" {
		return fmt.Errorf("auth provider id is required")
	}
	if def.Name == "" {
		def.Name = def.ID
	}
	def.Owner = owner
	s.extMu.Lock()
	defer s.extMu.Unlock()
	if s.authProviders == nil {
		s.authProviders = map[string]AuthProviderDef{}
	}
	if existing, exists := s.authProviders[def.ID]; exists && existing.Owner != owner {
		return fmt.Errorf("auth provider %q is already registered by %s", def.ID, existing.Owner)
	}
	s.authProviders[def.ID] = def
	return nil
}

// UnregisterAuthProviders removes every external auth provider owned by owner.
func (s *Service) UnregisterAuthProviders(owner string) {
	s.extMu.Lock()
	defer s.extMu.Unlock()
	for k, p := range s.authProviders {
		if p.Owner == owner {
			delete(s.authProviders, k)
		}
	}
}

// AuthProviders returns a sorted snapshot of registered external auth providers.
func (s *Service) AuthProviders() []AuthProviderDef {
	s.extMu.RLock()
	defer s.extMu.RUnlock()
	out := make([]AuthProviderDef, 0, len(s.authProviders))
	for _, p := range s.authProviders {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LookupAuthProvider finds a registered external auth provider by ID.
func (s *Service) LookupAuthProvider(id string) (AuthProviderDef, bool) {
	s.extMu.RLock()
	defer s.extMu.RUnlock()
	p, ok := s.authProviders[id]
	return p, ok
}

func (s *Service) verifyUserPassword(ctx context.Context, algo, stored, pw string) (ok, rehash bool, err error) {
	s.extMu.RLock()
	v, hasExt := s.verifiers[algo]
	s.extMu.RUnlock()
	if hasExt && v.fn != nil {
		// Use a dedicated concurrency semaphore and tight timeout for external plugin
		// verifiers so a slow or hung plugin cannot starve Argon2id logins.
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		select {
		case extVerifierSem <- struct{}{}:
		case <-callCtx.Done():
			return false, false, callCtx.Err()
		}
		defer func() { <-extVerifierSem }()

		matched, callErr := safeCallPasswordVerifier(callCtx, v.fn, stored, pw)
		if callErr != nil {
			return false, false, callErr
		}
		return matched, matched, nil
	}
	return s.hasher.Verify(ctx, algo, stored, pw)
}

func safeCallPasswordVerifier(ctx context.Context, fn PasswordVerifier, stored, pw string) (matched bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			matched = false
			err = fmt.Errorf("external password verifier panic: %v", r)
		}
	}()
	return fn(ctx, stored, pw)
}
