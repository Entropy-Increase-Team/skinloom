package httpapi

import (
	"cmp"
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
)

// LocaleInfo describes a supported UI language.
type LocaleInfo struct {
	Code string `json:"code" example:"zh-CN"`
	Name string `json:"name" example:"简体中文"`
}

// SiteInfo is the public, localized site identity.
type SiteInfo struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	URL          string `json:"url" doc:"Canonical public URL; empty when not configured"`
	Announcement string `json:"announcement" doc:"Markdown"`
	// AnnouncementHTML is Announcement rendered: no raw HTML, no script links.
	AnnouncementHTML string `json:"announcementHtml"`
}

// MenuItem is one navigation entry. Modules and plugins contribute these.
type MenuItem struct {
	ID    string `json:"id"`
	Title string `json:"title" doc:"Message key resolved by the frontend, or literal text"`
	Href  string `json:"href"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order"`
}

// PluginFrontend tells the frontend which plugin modules to load (empty until P2).
type PluginFrontend struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Entry   string `json:"entry"`
	Style   string `json:"style,omitempty"`
}

// Bootstrap is everything the frontend needs to render any page.
type Bootstrap struct {
	Version int64                 `json:"version" doc:"Config version; changes whenever settings, themes or plugins change"`
	Locale  string                `json:"locale"`
	Locales []LocaleInfo          `json:"locales"`
	Site    SiteInfo              `json:"site"`
	Theme   theme.ResolvedTheme   `json:"theme"`
	Menus   map[string][]MenuItem `json:"menus" doc:"Keyed by area: explore, user, admin"`
	Plugins []PluginFrontend      `json:"plugins"`
	Server  ServerInfo            `json:"server"`
	// NeedsSetup is true until the first administrator is created (see /api/v1/setup).
	NeedsSetup bool     `json:"needsSetup"`
	Auth       AuthInfo `json:"auth"`
}

// AuthInfo tells the frontend which account forms to show.
type AuthInfo struct {
	Registration string      `json:"registration" enum:"open,closed"`
	Captcha      CaptchaInfo `json:"captcha"`
	// Mail is true when mail and site.url are configured: password reset and email
	// verification work.
	Mail bool `json:"mail"`
	// Social are the sign-in providers (none until site.url is set).
	Social []SocialProviderInfo `json:"social"`
}

// CaptchaInfo is the public half of the captcha configuration.
type CaptchaInfo struct {
	Provider string `json:"provider" enum:"none,image,turnstile,recaptcha,hcaptcha"`
	SiteKey  string `json:"siteKey,omitempty" doc:"Widget site key (turnstile, recaptcha, hcaptcha)"`
}

// ServerInfo identifies the running backend.
type ServerInfo struct {
	Name    string `json:"name" example:"SkinLoom"`
	Version string `json:"version"`
}

type bootstrapInput struct {
	Lang           string `query:"lang" doc:"Force a UI language"`
	LocaleCookie   string `cookie:"locale"`
	AcceptLanguage string `header:"Accept-Language"`
}

type bootstrapOutput struct {
	CacheControl  string `header:"Cache-Control"`
	ConfigVersion int64  `header:"X-Config-Version"`
	Body          Bootstrap
}

func registerSite(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getBootstrap",
		Method:      http.MethodGet,
		Path:        "/api/v1/site/bootstrap",
		Summary:     "Site bootstrap",
		Description: "Site identity, active theme, menus and plugin frontends for the current locale. Called by the frontend on every page render.",
		Tags:        []string{"site"},
	}, func(ctx context.Context, in *bootstrapInput) (*bootstrapOutput, error) {
		snap := d.Settings.Current()
		locale := PickLocale(in.Lang, in.LocaleCookie, in.AcceptLanguage)
		out := &bootstrapOutput{CacheControl: "no-store", ConfigVersion: snap.Version}
		out.Body = Bootstrap{
			Version: snap.Version,
			Locale:  locale,
			Locales: Locales,
			Site: SiteInfo{
				Name:             snap.Localized("site.name", locale),
				Description:      snap.Localized("site.description", locale),
				URL:              snap.String("site.url"),
				Announcement:     snap.Localized("site.announcement", locale),
				AnnouncementHTML: notify.Markdown(snap.Localized("site.announcement", locale)),
			},
			Theme: d.Themes.Resolve(snap),
			Menus: map[string][]MenuItem{
				"explore": {{ID: "skinlib", Title: "nav.skinlib", Href: "/skinlib", Icon: "shirt", Order: 10}},
				"user":    {},
				"admin":   {},
			},
			Plugins: []PluginFrontend{},
			Server:  ServerInfo{Name: "SkinLoom", Version: buildinfo.Version},
		}
		cc := captcha.ConfigFrom(snap)
		out.Body.Auth = AuthInfo{
			Registration: snap.String("auth.registration"),
			Captcha:      CaptchaInfo{Provider: cmp.Or(cc.Provider, captcha.None), SiteKey: cc.SiteKey},
			Mail:         mailReady(snap),
			Social:       socialProviders(social.PolicyFrom(snap)),
		}
		if d.Accounts != nil {
			needs, err := d.Accounts.NeedsSetup(ctx)
			if err != nil {
				return nil, err
			}
			out.Body.NeedsSetup = needs
		}
		return out, nil
	})
}

// PickLocale chooses the UI language: explicit ?lang, then the locale cookie, then
// Accept-Language. Unknown values are ignored (never persisted), unlike BS issue #684.
func PickLocale(lang, cookie, accept string) string {
	for _, cand := range []string{lang, cookie} {
		if l, ok := matchLocale(cand); ok {
			return l
		}
	}
	for part := range strings.SplitSeq(accept, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		if l, ok := matchLocale(tag); ok {
			return l
		}
	}
	return Locales[0].Code
}

func matchLocale(tag string) (string, bool) {
	tag = strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")
	if tag == "" {
		return "", false
	}
	for _, l := range Locales {
		if strings.EqualFold(l.Code, tag) {
			return l.Code, true
		}
	}
	lang, _, _ := strings.Cut(tag, "-")
	for _, l := range Locales {
		if code, _, _ := strings.Cut(l.Code, "-"); strings.EqualFold(code, lang) {
			return l.Code, true
		}
	}
	return "", false
}
