package settings

import (
	"fmt"
	"net/url"
	"time"
	_ "time/tzdata" // time zone names resolve without a system database (Windows, minimal images)
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
			Key: "home.blocks", Type: TypeJSON, Public: true, Default: nil,
			Doc: `Home page blocks, e.g. [{"type": "hero"}, {"type": "launcher-guide"}]. null = use the theme's default layout.`,
			Validate: func(v any) error {
				if v == nil {
					return nil
				}
				list, ok := v.([]any)
				if !ok {
					return fmt.Errorf("want a list of blocks or null")
				}
				for i, b := range list {
					m, ok := b.(map[string]any)
					if !ok {
						return fmt.Errorf("block %d: want an object", i)
					}
					if t, _ := m["type"].(string); t == "" {
						return fmt.Errorf("block %d: missing type", i)
					}
				}
				return nil
			},
		},
	}
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
