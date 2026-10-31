package settings

import (
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

// MaxCustomCSS is the largest theme.custom_css, in bytes.
const MaxCustomCSS = 64 * 1024

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

// validateOverrides checks i18n.overrides: {locale: {message key: text}}. Braces must
// pair up, so an override cannot break the message format of the texts around it;
// that its placeholders match the default's is up to the admin panel, which knows them.
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
			case !balancedBraces(s):
				return fmt.Errorf("%s.%s: { and } do not pair up", locale, key)
			}
			n++
		}
	}
	if n > MaxTextOverrides {
		return fmt.Errorf("at most %d texts", MaxTextOverrides)
	}
	return nil
}

func balancedBraces(s string) bool {
	depth := 0
	for _, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			if depth--; depth < 0 {
				return false
			}
		}
	}
	return depth == 0
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
