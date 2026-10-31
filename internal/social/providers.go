package social

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Provider types.
const (
	TypeGitHub    = "github"
	TypeMicrosoft = "microsoft"
	TypeOIDC      = "oidc"
)

// ProviderConfig is one entry of the auth.social.providers setting.
type ProviderConfig struct {
	// ID names the provider in URLs and links accounts to it: changing it unlinks
	// everyone, so pick it once (github, microsoft, google…).
	ID   string `json:"id"`
	Type string `json:"type"`
	// Name is shown on the sign-in button; empty = GitHub, Microsoft or the id.
	Name         string `json:"name,omitempty"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	// Issuer is the OpenID Connect issuer URL (type oidc), e.g. https://accounts.google.com.
	Issuer string `json:"issuer,omitempty"`
	// Tenant limits who can sign in with Microsoft: common (default; personal, work and
	// school accounts), consumers (personal only), organizations, or a tenant id.
	Tenant string `json:"tenant,omitempty"`
	// Scopes are requested besides openid email profile (type oidc).
	Scopes []string `json:"scopes,omitempty"`
}

// DisplayName is the name on the sign-in button.
func (c ProviderConfig) DisplayName() string {
	switch {
	case c.Name != "":
		return c.Name
	case c.Type == TypeGitHub:
		return "GitHub"
	case c.Type == TypeMicrosoft:
		return "Microsoft"
	}
	return c.ID
}

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	tenantPattern = regexp.MustCompile(`^[A-Za-z0-9.-]{1,128}$`)
	scopePattern  = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]{1,128}$`) // RFC 6749 scope-token
)

// SettingDefs are the social sign-in settings.
func SettingDefs() []settings.Def {
	return []settings.Def{{
		Key: "auth.social.providers", Type: settings.TypeJSON, Default: []any{},
		Doc: `Sign-in providers, a list of {"id", "type": "github" | "microsoft" | "oidc", "clientId", "clientSecret", ` +
			`"name"?, "issuer" (oidc), "tenant"? (microsoft), "scopes"? (oidc)}. The callback URL to register with the provider is ` +
			`<site.url>/api/v1/auth/social/<id>/callback; providers show up once site.url is set. Secrets stay on the server: ` +
			`a saved clientSecret shows as ` + settings.Redacted + `, and leaving it so keeps it.`,
		Validate: func(v any) error {
			_, err := parseProviders(v)
			return err
		},
		Redact:  redactSecrets,
		Restore: restoreSecrets,
	}}
}

// redactSecrets replaces each provider's saved client secret with settings.Redacted.
func redactSecrets(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, len(list))
	for i, item := range list {
		m, _ := item.(map[string]any)
		if s, _ := m["clientSecret"].(string); s == "" {
			out[i] = item
			continue
		}
		c := maps.Clone(m)
		c["clientSecret"] = settings.Redacted
		out[i] = c
	}
	return out
}

// restoreSecrets puts the saved secret back into each edited provider whose secret was
// left redacted, matched by id, but only while the secret still goes where it went:
// a provider with a new id, type, issuer or tenant gets none, so an edit cannot send
// the saved secret to a token endpoint of the editor's choosing.
func restoreSecrets(stored, edited any) any {
	saved := map[string]map[string]any{}
	list, _ := stored.([]any)
	for _, item := range list {
		m, _ := item.(map[string]any)
		id, _ := m["id"].(string)
		saved[id] = m
	}
	list, ok := edited.([]any)
	if !ok {
		return edited
	}
	out := make([]any, len(list))
	for i, item := range list {
		m, _ := item.(map[string]any)
		if s, _ := m["clientSecret"].(string); s != settings.Redacted {
			out[i] = item
			continue
		}
		c := maps.Clone(m)
		id, _ := m["id"].(string)
		old := saved[id]
		secret, _ := old["clientSecret"].(string)
		if secret != "" && sameEndpoint(old, m) {
			c["clientSecret"] = secret
		} else {
			delete(c, "clientSecret")
		}
		out[i] = c
	}
	return out
}

// sameEndpoint: a and b send their secret to the same identity provider.
func sameEndpoint(a, b map[string]any) bool {
	for _, k := range []string{"type", "issuer", "tenant"} {
		x, _ := a[k].(string)
		y, _ := b[k].(string)
		if x != y {
			return false
		}
	}
	return true
}

func parseProviders(v any) ([]ProviderConfig, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var list []ProviderConfig
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("want a list of providers: %w", err)
	}
	seen := map[string]bool{}
	for i, p := range list {
		if err := p.validate(); err != nil {
			return nil, fmt.Errorf("provider %d (%q): %w", i, p.ID, err)
		}
		if seen[p.ID] {
			return nil, fmt.Errorf("provider id %q is used twice", p.ID)
		}
		seen[p.ID] = true
	}
	return list, nil
}

func (c ProviderConfig) validate() error {
	if !idPattern.MatchString(c.ID) || c.ID == "pending" { // /api/v1/auth/social/pending is taken
		return errors.New("id must be 1 to 32 lowercase letters, digits or dashes (not pending)")
	}
	if c.ClientID == "" || len(c.ClientID) > 255 || len(c.ClientSecret) > 1024 || len([]rune(c.Name)) > 32 {
		return errors.New("clientId is required (clientId at most 255 bytes, clientSecret 1024, name 32 characters)")
	}
	switch c.Type {
	case TypeGitHub:
		if c.ClientSecret == "" {
			return errors.New("GitHub needs a clientSecret")
		}
	case TypeMicrosoft:
		if c.Tenant != "" && !tenantPattern.MatchString(c.Tenant) {
			return errors.New("tenant must be common, consumers, organizations or a tenant id")
		}
	case TypeOIDC:
		if err := checkEndpoint(c.Issuer); err != nil {
			return fmt.Errorf("issuer: %w", err)
		}
		if len(c.Scopes) > 10 {
			return errors.New("at most 10 scopes")
		}
		for _, s := range c.Scopes {
			if !scopePattern.MatchString(s) {
				return fmt.Errorf("invalid scope %q", s)
			}
		}
	default:
		return errors.New(`type must be "github", "microsoft" or "oidc"`)
	}
	if (c.Type != TypeMicrosoft && c.Tenant != "") || (c.Type != TypeOIDC && (c.Issuer != "" || len(c.Scopes) > 0)) {
		return errors.New("tenant is for microsoft, issuer and scopes for oidc")
	}
	return nil
}

// checkEndpoint accepts https URLs, and http ones on this machine (a local identity
// provider while testing): codes and tokens must not cross the network in the clear.
func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return errors.New("must be an absolute URL")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if host := u.Hostname(); host == "localhost" || net.ParseIP(host).IsLoopback() {
			return nil
		}
	}
	return errors.New("must use https")
}

// Policy is what the service needs from the settings.
type Policy struct {
	// SiteURL is site.url without a trailing slash; without it there is no callback URL
	// to give providers, so no provider is offered.
	SiteURL   string
	Providers []ProviderConfig
}

// PolicyFrom reads the policy from a settings snapshot.
func PolicyFrom(s *settings.Snapshot) Policy {
	p := Policy{SiteURL: strings.TrimRight(s.String("site.url"), "/")}
	if p.SiteURL == "" {
		return p
	}
	var v any
	if err := json.Unmarshal(s.Raw("auth.social.providers"), &v); err == nil {
		p.Providers, _ = parseProviders(v)
	}
	return p
}

// Provider returns the configured provider id.
func (p Policy) Provider(id string) (ProviderConfig, bool) {
	for _, c := range p.Providers {
		if c.ID == id {
			return c, true
		}
	}
	return ProviderConfig{}, false
}

// CallbackURL is where provider id sends people back to.
func (p Policy) CallbackURL(id string) string {
	return p.SiteURL + "/api/v1/auth/social/" + id + "/callback"
}
