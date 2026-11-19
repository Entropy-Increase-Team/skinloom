package settings

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // time zone names resolve without a system database (Windows, minimal images)
	"unicode/utf8"
)

// coreDefs are the settings available from M0. Later modules add their own
// (auth.*, score.*, texture.*, yggdrasil.* — mapping in docs/design/02 §4).
func coreDefs() []Def {
	return []Def{
		{
			Key: "site.name", Type: TypeLocalized, Public: true,
			Default: map[string]string{"zh-CN": "云织", "en": "SkinLoom"},
			Doc:     "Site name shown in the header, page titles and the launcher metadata.",
		},
		{
			Key: "site.description", Type: TypeLocalized, Public: true,
			Default: map[string]string{
				"zh-CN": "开源、自托管的 Minecraft 皮肤站",
				"en":    "An open-source, self-hosted Minecraft skin site",
			},
			Doc: "One-line description used on the home page and in search results.",
		},
		{
			Key: "site.url", Type: TypeString, Public: true, Default: "",
			Doc:      "Canonical public URL, e.g. https://skin.example.com. Mail links are built only from it (verification and password reset are off without it).",
			Validate: validURLOrEmpty,
		},
		{
			Key: "site.timezone", Type: TypeString, Default: "Asia/Shanghai",
			Doc: "IANA time zone of the site, e.g. Asia/Shanghai or UTC: where \"a new day\" starts for daily check-ins.",
			Validate: func(v any) error {
				if _, err := time.LoadLocation(v.(string)); err != nil || v.(string) == "" {
					return fmt.Errorf("not a time zone name such as Asia/Shanghai or UTC")
				}
				return nil
			},
		},
		{
			Key: "site.announcement", Type: TypeLocalized, Public: true, Default: map[string]string{},
			Doc: "Announcement shown on the home page and dashboard (Markdown).",
		},
		{
			Key: "site.logo", Type: TypeString, Public: true, Default: "", Validate: validImageOrEmpty,
			Doc: "Logo in the header: an image URL or a path on this site; empty = the theme's mark.",
		},
		{
			Key: "site.favicon", Type: TypeString, Public: true, Default: "", Validate: validImageOrEmpty,
			Doc: "Browser tab icon: an image URL or a path on this site; empty = the built-in one. (BS favicon_url)",
		},
		{
			Key: "site.copyright", Type: TypeLocalized, Public: true, Default: map[string]string{},
			Doc: "Footer text (Markdown); empty = © year and the site name. (BS copyright_text)",
		},
		{
			Key: "seo.keywords", Type: TypeLocalized, Public: true, Default: map[string]string{},
			Doc: "Comma-separated keywords for search engines (meta keywords). The description is site.description. (BS meta_keywords)",
		},
		{
			Key: "theme.custom_css", Type: TypeText, Default: "", Superadmin: true,
			Doc: fmt.Sprintf("CSS added after the theme's on every page, at most %d KB; served as its own stylesheet. Super administrators only. (BS custom_css)", MaxCustomCSS/1024),
			Validate: func(v any) error {
				if len(v.(string)) > MaxCustomCSS {
					return fmt.Errorf("at most %d KB", MaxCustomCSS/1024)
				}
				return nil
			},
		},
		{
			// Allowed by the pages' Content-Security-Policy through the nonce of the
			// <script> that loads it (ADR 0039), so it may load further scripts itself.
			Key: "theme.custom_js", Type: TypeText, Default: "", Superadmin: true,
			Doc: fmt.Sprintf("JavaScript run on every page after it is parsed, at most %d KB; served as its own script. "+
				"Super administrators only. (BS custom_js)", MaxCustomJS/1024),
			Validate: func(v any) error {
				if len(v.(string)) > MaxCustomJS {
					return fmt.Errorf("at most %d KB", MaxCustomJS/1024)
				}
				return nil
			},
		},
		{
			Key: "theme.active", Type: TypeString, Public: true, Default: "default",
			Doc: "ID of the active theme.",
		},
		{
			Key: "theme.overrides", Type: TypeJSON, Public: true, Default: map[string]any{},
			Doc: `Token overrides on top of the active theme: {"light": {"primary": "…"}, "dark": {…}, "fonts": {…}}.`,
			Validate: func(v any) error {
				if _, ok := v.(map[string]any); !ok {
					return fmt.Errorf("want an object")
				}
				return nil
			},
		},
		{
			Key: "theme.settings", Type: TypeJSON, Public: true, Default: map[string]any{},
			Doc: `Per-theme settings configured by the administrator: {"brocade": {"accent": "…"}, …}.`,
			Validate: func(v any) error {
				if _, ok := v.(map[string]any); !ok {
					return fmt.Errorf("want an object")
				}
				return nil
			},
		},
		{
			Key: "theme.trusted_web", Type: TypeJSON, Default: []string{}, Superadmin: true,
			Doc: `Theme IDs whose local Level-5 JS/override entry has been explicitly confirmed by a super administrator.`,
			Validate: func(v any) error {
				items, ok := stringListArg(v)
				if !ok {
					return fmt.Errorf("want an array of theme IDs")
				}
				for _, s := range items {
					if s == "" {
						return fmt.Errorf("each entry must be a non-empty theme ID string")
					}
				}
				return nil
			},
		},
		{
			Key: "plugins.trusted_keys", Type: TypeJSON, Default: []string{}, Superadmin: true,
			Doc: `Additional Ed25519 public keys (SPKI PEM, base64, or 64-hex) trusted to sign .slp plugin packages.`,
			Validate: func(v any) error {
				items, ok := stringListArg(v)
				if !ok {
					return fmt.Errorf("want an array of Ed25519 public key strings")
				}
				for _, s := range items {
					if !validEd25519PublicKey(s) {
						return fmt.Errorf("each entry must be a valid Ed25519 SPKI PEM, base64, or hex public key")
					}
				}
				return nil
			},
		},
		{
			Key: "plugin.registries", Type: TypeJSON, Default: DefaultPluginRegistries(), Superadmin: true,
			Doc:      `Plugin and theme market registry sources: [{"id": "official", "name": "SkinLoom Official Registry", "urls": ["https://registry.skinloom.cn", "https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom-registry/dist"], "official": true, "enabled": true}].`,
			Validate: validatePluginRegistries,
		},
		{
			Key: "i18n.overrides", Type: TypeJSON, Default: map[string]any{}, Validate: validateOverrides,
			Doc: `The administrator's wording of interface texts: {"zh-CN": {"nav.skinlib": "…"}, "en": {…}}, by message key. ` +
				`Texts keep the default's {placeholders}. (BS language_lines)`,
		},
		{
			// Checked against the block catalog by the theme module (theme.ValidateHome).
			Key: "home.blocks", Type: TypeJSON, Public: true, Default: nil,
			Doc: `Home page blocks, e.g. [{"type": "hero"}, {"type": "latest-textures", "props": {"limit": 8}}]. null = the theme's own.`,
		},
	}
}

// Largest theme.custom_css and theme.custom_js, in bytes.
const (
	MaxCustomCSS = 64 * 1024
	MaxCustomJS  = 64 * 1024
)

// Limits of i18n.overrides.
const (
	MaxTextOverrides = 3000
	maxOverrideKey   = 200
	maxOverrideText  = 2000
)

var (
	overrideLocale = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	overrideKey    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+)*$`)
)

// validateOverrides checks i18n.overrides: {locale: {message key: text}}. Each text must
// parse as an ICU message the way the browser parses it (checkMessage), so an override
// cannot leave a page showing the message key; that its placeholders match the
// default's is up to the admin panel, which knows them.
func validateOverrides(v any) error {
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf(`want {"zh-CN": {"message.key": "text"}, …}`)
	}
	n := 0
	for locale, texts := range m {
		if !overrideLocale.MatchString(locale) {
			return fmt.Errorf("%q is not a language code like zh-CN", locale)
		}
		tm, ok := texts.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want an object of texts by message key", locale)
		}
		for key, t := range tm {
			s, ok := t.(string)
			switch {
			case len(key) > maxOverrideKey || !overrideKey.MatchString(key):
				return fmt.Errorf("%s: %q is not a message key like nav.skinlib", locale, key)
			case !ok || utf8.RuneCountInString(s) > maxOverrideText:
				return fmt.Errorf("%s.%s: want text of at most %d characters", locale, key, maxOverrideText)
			}
			if err := checkMessage(s); err != nil {
				return fmt.Errorf("%s.%s: %v", locale, key, err)
			}
			n++
		}
	}
	if n > MaxTextOverrides {
		return fmt.Errorf("at most %d texts", MaxTextOverrides)
	}
	return nil
}

// validImageOrEmpty accepts an http(s) URL, a path on this site (/…, not //…) or "".
func validImageOrEmpty(v any) error {
	s := v.(string)
	if s == "" || (strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && !strings.ContainsAny(s, " \\")) {
		return nil
	}
	return validURLOrEmpty(v)
}

func validURLOrEmpty(v any) error {
	s := v.(string)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("want an http(s) URL like https://skin.example.com")
	}
	return nil
}

// Location is the site's time zone (site.timezone), or UTC if it cannot be loaded.
func Location(s *Snapshot) *time.Location {
	loc, err := time.LoadLocation(s.String("site.timezone"))
	if err != nil {
		return time.UTC
	}
	return loc
}

func stringListArg(v any) ([]string, bool) {
	switch list := v.(type) {
	case []string:
		return list, true
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

// ParseEd25519PublicKey decodes a PEM SPKI, base64 SPKI/raw, or 64-hex Ed25519 public key.
func ParseEd25519PublicKey(spec string) (ed25519.PublicKey, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return nil, errors.New("empty public key")
	}
	if strings.Contains(s, "BEGIN PUBLIC KEY") {
		blk, _ := pem.Decode([]byte(s))
		if blk == nil {
			return nil, errors.New("invalid PEM block")
		}
		pubAny, err := x509.ParsePKIXPublicKey(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKIX public key: %w", err)
		}
		pub, ok := pubAny.(ed25519.PublicKey)
		if !ok || len(pub) != ed25519.PublicKeySize {
			return nil, errors.New("public key is not Ed25519")
		}
		return pub, nil
	}
	if len(s) == ed25519.PublicKeySize*2 {
		if raw, err := hex.DecodeString(s); err == nil && len(raw) == ed25519.PublicKeySize {
			return ed25519.PublicKey(raw), nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		raw, err := enc.DecodeString(s)
		if err != nil {
			continue
		}
		if len(raw) == ed25519.PublicKeySize {
			return ed25519.PublicKey(raw), nil
		}
		if pubAny, err := x509.ParsePKIXPublicKey(raw); err == nil {
			if pub, ok := pubAny.(ed25519.PublicKey); ok && len(pub) == ed25519.PublicKeySize {
				return pub, nil
			}
		}
	}
	return nil, errors.New("unsupported Ed25519 public key format")
}

func validEd25519PublicKey(spec string) bool {
	_, err := ParseEd25519PublicKey(spec)
	return err == nil
}

// RegistrySource configures one plugin/theme market registry in plugin.registries.
type RegistrySource struct {
	ID        string   `json:"id"`
	Name      string   `json:"name,omitempty"`
	URLs      []string `json:"urls"`
	PublicKey string   `json:"publicKey,omitempty"`
	Official  bool     `json:"official,omitempty"`
	Enabled   bool     `json:"enabled"`
}

var registryIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// DefaultPluginRegistries returns the default official market registry configuration
// with the domestic CDN primary mirror and GitHub failover mirror.
func DefaultPluginRegistries() []RegistrySource {
	return []RegistrySource{
		{
			ID:   "official",
			Name: "SkinLoom Official Registry",
			URLs: []string{
				"https://registry.skinloom.cn",
				"https://raw.githubusercontent.com/Entropy-Increase-Team/skinloom-registry/dist",
			},
			Official: true,
			Enabled:  true,
		},
	}
}

// ParsePluginRegistries validates and normalizes a plugin.registries setting value.
func ParsePluginRegistries(v any) ([]RegistrySource, error) {
	if v == nil {
		return nil, errors.New("want an array of registry objects")
	}
	if typed, ok := v.([]RegistrySource); ok {
		return validateNormalizedRegistries(typed)
	}
	rawList, ok := v.([]any)
	if !ok {
		return nil, errors.New("want an array of registry objects")
	}
	if len(rawList) > 32 {
		return nil, errors.New("at most 32 registry sources may be configured")
	}
	out := make([]RegistrySource, 0, len(rawList))
	for i, item := range rawList {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("registry[%d]: want an object", i)
		}
		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		pubKey, _ := m["publicKey"].(string)
		pubKey = strings.TrimSpace(pubKey)

		var urls []string
		if rawURLs, hasURLs := m["urls"]; hasURLs {
			list, ok := stringListArg(rawURLs)
			if !ok {
				return nil, fmt.Errorf("registry[%d].urls: want an array of http(s) URLs", i)
			}
			urls = append(urls, list...)
		}
		if singleURL, ok := m["url"].(string); ok && strings.TrimSpace(singleURL) != "" {
			urls = append(urls, strings.TrimSpace(singleURL))
		}

		enabled := true
		if rawEnabled, hasEnabled := m["enabled"]; hasEnabled {
			b, ok := rawEnabled.(bool)
			if !ok {
				return nil, fmt.Errorf("registry[%d].enabled: want a boolean", i)
			}
			enabled = b
		}
		official := id == "official"
		if rawOfficial, hasOfficial := m["official"]; hasOfficial {
			b, ok := rawOfficial.(bool)
			if !ok {
				return nil, fmt.Errorf("registry[%d].official: want a boolean", i)
			}
			official = b || id == "official"
		}

		out = append(out, RegistrySource{
			ID:        id,
			Name:      name,
			URLs:      urls,
			PublicKey: pubKey,
			Official:  official,
			Enabled:   enabled,
		})
	}
	return validateNormalizedRegistries(out)
}

func validateNormalizedRegistries(list []RegistrySource) ([]RegistrySource, error) {
	if len(list) > 32 {
		return nil, errors.New("at most 32 registry sources may be configured")
	}
	seenIDs := make(map[string]bool, len(list))
	out := make([]RegistrySource, 0, len(list))
	for i, src := range list {
		src.ID = strings.TrimSpace(src.ID)
		if !registryIDPattern.MatchString(src.ID) {
			return nil, fmt.Errorf("registry[%d].id: %q must match %s", i, src.ID, registryIDPattern.String())
		}
		if seenIDs[src.ID] {
			return nil, fmt.Errorf("duplicate registry id %q", src.ID)
		}
		seenIDs[src.ID] = true
		if src.Name == "" {
			src.Name = src.ID
		}
		if len(src.URLs) == 0 {
			return nil, fmt.Errorf("registry %q: requires at least one http(s) URL in urls", src.ID)
		}
		cleanURLs := make([]string, 0, len(src.URLs))
		for _, rawU := range src.URLs {
			u := strings.TrimRight(strings.TrimSpace(rawU), "/")
			if u == "" {
				return nil, fmt.Errorf("registry %q: URL must not be empty", src.ID)
			}
			if err := validURLOrEmpty(u); err != nil {
				return nil, fmt.Errorf("registry %q URL %q: %w", src.ID, u, err)
			}
			cleanURLs = append(cleanURLs, u)
		}
		src.URLs = cleanURLs
		if src.PublicKey != "" && !validEd25519PublicKey(src.PublicKey) {
			return nil, fmt.Errorf("registry %q: publicKey must be a valid Ed25519 SPKI PEM, base64, or hex public key", src.ID)
		}
		if src.ID == "official" {
			src.Official = true
		}
		out = append(out, src)
	}
	return out, nil
}

func validatePluginRegistries(v any) error {
	_, err := ParsePluginRegistries(v)
	return err
}
