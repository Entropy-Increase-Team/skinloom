package httpapi

import (
	"cmp"
	"context"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/sync/singleflight"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/stats"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

const themePreviewCookie = "skinloom_theme_preview"

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
	Logo             string `json:"logo" doc:"Header logo URL; empty = the theme's mark"`
	Favicon          string `json:"favicon" doc:"Tab icon URL; empty = the built-in one"`
	// CopyrightHTML is the footer text (site.copyright) rendered like the announcement;
	// empty = © year and the site name.
	CopyrightHTML string `json:"copyrightHtml"`
	Keywords      string `json:"keywords" doc:"meta keywords"`
}

// SiteStats are the public totals of the home page's stats block.
type SiteStats struct {
	Users    int `json:"users" doc:"Accounts that are not deleted"`
	Players  int `json:"players"`
	Textures int `json:"textures" doc:"Public textures"`
}

// statsTTL is how long the totals are reused: the home page may be busy, and they do
// not need to be exact.
const statsTTL = time.Minute

type statsCache struct {
	mu     sync.Mutex
	at     time.Time
	totals stats.Totals
	count  singleflight.Group
}

func (c *statsCache) get(ctx context.Context, d Deps) (stats.Totals, error) {
	c.mu.Lock()
	if !c.at.IsZero() && time.Since(c.at) < statsTTL {
		t := c.totals
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	// One count at a time. The requests that arrive meanwhile share its result, and
	// each stops waiting when its own request ends; the count is not tied to any one
	// of them.
	ch := c.count.DoChan("", func() (any, error) {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		t, err := stats.CountTotals(cctx, d.DB.Ent)
		if err == nil {
			c.mu.Lock()
			c.totals, c.at = t, time.Now()
			c.mu.Unlock()
		}
		return t, err
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return stats.Totals{}, r.Err
		}
		return r.Val.(stats.Totals), nil
	case <-ctx.Done():
		return stats.Totals{}, ctx.Err()
	}
}

// MenuItem is one navigation entry. Modules and plugins contribute these.
type MenuItem struct {
	ID    string `json:"id"`
	Title string `json:"title" doc:"Message key resolved by the frontend, or literal text"`
	Href  string `json:"href"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order"`
}

// PluginFrontend tells the frontend which enabled plugin bundles and metadata to load.
type PluginFrontend struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind,omitempty"`
	Version   string                 `json:"version"`
	Rev       string                 `json:"rev,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Entry     string                 `json:"entry,omitempty"`
	Style     string                 `json:"style,omitempty"`
	Slots     []string               `json:"slots,omitempty"`
	Blocks    []plugin.FrontendBlock `json:"blocks,omitempty"`
	Overrides []string               `json:"overrides,omitempty"`
	Settings  map[string]any         `json:"settings,omitempty"`
	Messages  map[string]string      `json:"messages,omitempty"`
}

// PluginFormField describes an extension field declared by a plugin for a host form scope.
type PluginFormField struct {
	PluginID    string   `json:"pluginId"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	MaxLength   int      `json:"maxLength,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Bootstrap is everything the frontend needs to render any page.
type Bootstrap struct {
	Version int64                        `json:"version" doc:"Config version; changes whenever settings, themes or plugins change"`
	Locale  string                       `json:"locale"`
	Locales []LocaleInfo                 `json:"locales"`
	Site    SiteInfo                     `json:"site"`
	Theme   theme.ResolvedTheme          `json:"theme"`
	Menus   map[string][]MenuItem        `json:"menus" doc:"Keyed by area: explore, user, admin"`
	Plugins []PluginFrontend             `json:"plugins"`
	Forms   map[string][]PluginFormField `json:"forms,omitempty" doc:"Declarative form fields grouped by scope"`
	Server  ServerInfo                   `json:"server"`
	// NeedsSetup is true until the first administrator is created (see /api/v1/setup).
	NeedsSetup bool     `json:"needsSetup"`
	Auth       AuthInfo `json:"auth"`
	// Messages are the administrator's wording of interface texts in this locale
	// (i18n.overrides), by message key; the frontend lays them over its own.
	Messages map[string]string `json:"messages"`
}

// AuthInfo tells the frontend which account forms to show.
type AuthInfo struct {
	Registration string      `json:"registration" enum:"open,closed"`
	Captcha      CaptchaInfo `json:"captcha"`
	// Mail is true when mail and site.url are configured: password reset and email
	// verification work.
	Mail bool `json:"mail"`
	// RequireVerification: accounts must verify their email before managing players,
	// uploading, reporting or signing in to launchers (auth.require_email_verification).
	RequireVerification bool `json:"requireVerification"`
	// Social are the sign-in providers (none until site.url is set).
	Social []SocialProviderInfo `json:"social"`
}

// CaptchaInfo is the public half of the captcha configuration.
type CaptchaInfo struct {
	Provider string `json:"provider" doc:"Active captcha provider (none, image, turnstile, recaptcha, hcaptcha, or a plugin-registered provider ID)"`
	SiteKey  string `json:"siteKey,omitempty" doc:"Widget site key (turnstile, recaptcha, hcaptcha)"`
}

// ServerInfo identifies the running backend.
type ServerInfo struct {
	Name    string `json:"name" example:"SkinLoom"`
	Version string `json:"version"`
}

type bootstrapInput struct {
	Lang               string `query:"lang" doc:"Force a UI language"`
	LocaleCookie       string `cookie:"locale"`
	PreviewThemeCookie string `cookie:"skinloom_theme_preview"`
	AcceptLanguage     string `header:"Accept-Language"`
}

type bootstrapOutput struct {
	CacheControl  string `header:"Cache-Control"`
	ConfigVersion int64  `header:"X-Config-Version"`
	Body          Bootstrap
}

// localized caches the parts of the bootstrap that take work to build, the rendered
// home blocks and the text overrides, per config version and locale.
type localized struct {
	mu      sync.Mutex
	version int64
	parts   map[string]localizedParts
}

type localizedParts struct {
	home     []theme.Block
	messages map[string]string
}

func (c *localized) get(version int64, locale string, build func() localizedParts) localizedParts {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.parts == nil || c.version != version {
		c.version, c.parts = version, map[string]localizedParts{}
	}
	p, ok := c.parts[locale]
	if !ok {
		p = build()
		c.parts[locale] = p
	}
	return p
}

func registerSite(api huma.API, d Deps) {
	var cache localized
	huma.Register(api, huma.Operation{
		OperationID: "getBootstrap",
		Method:      http.MethodGet,
		Path:        "/api/v1/site/bootstrap",
		Summary:     "Site bootstrap",
		Description: "Site identity, active theme, menus and plugin frontends for the current locale. Called by the frontend on every page render.",
		Tags:        []string{"site"},
	}, func(ctx context.Context, in *bootstrapInput) (*bootstrapOutput, error) {
		var snap *settings.Snapshot
		if d.Settings != nil {
			snap = d.Settings.Current()
		} else {
			reg := d.SettingsReg
			if reg == nil {
				reg = settings.NewRegistry()
			}
			snap = settings.NewSnapshot(reg, 0, nil)
		}
		locale := PickLocale(in.Lang, in.LocaleCookie, in.AcceptLanguage)
		viewer := requestInfo(ctx).viewer()
		trustFn := trustThemeWebFn(d)
		resolved := theme.ResolvedTheme{
			ID:          "none",
			ColorScheme: "both",
			Tokens:      theme.Tokens{Light: map[string]string{}, Dark: map[string]string{}},
			CSS:         []string{},
			JS:          []string{},
			Layout:      map[string]string{},
			Home:        []theme.Block{},
		}
		if d.Themes != nil {
			previewID := ""
			if in.PreviewThemeCookie != "" && viewer != nil && viewer.User != nil && viewer.User.Role != user.RoleUser {
				if pt, ok := d.Themes.Get(in.PreviewThemeCookie); ok && pt.Error == "" {
					previewID = pt.ID
				}
			}
			resolved = d.Themes.ResolveTheme(snap, previewID, trustFn)
			if previewID != "" && resolved.ID == previewID {
				resolved.Previewing = true
				if pt, ok := d.Themes.Get(previewID); ok {
					resolved.PreviewName = cmp.Or(settings.PickLocalized(pt.Name, locale), pt.ID)
				}
			}
		}
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
				Logo:             snap.String("site.logo"),
				Favicon:          snap.String("site.favicon"),
				CopyrightHTML:    notify.Markdown(snap.Localized("site.copyright", locale)),
				Keywords:         snap.Localized("seo.keywords", locale),
			},
			Theme: resolved,
			Menus: map[string][]MenuItem{
				"explore": {{ID: "skinlib", Title: "nav.skinlib", Href: "/skinlib", Icon: "layout-grid", Order: 10}},
				"user":    {},
				"admin":   {},
			},
			Plugins: []PluginFrontend{},
			Server:  ServerInfo{Name: "SkinLoom", Version: buildinfo.Version},
		}
		if resolved.Previewing {
			out.Body.Theme.Home = theme.LocalizeBlocks(out.Body.Theme.Home, locale, notify.Markdown)
			out.Body.Messages = settings.Decode[map[string]map[string]string](snap, "i18n.overrides")[locale]
			if out.Body.Messages == nil {
				out.Body.Messages = map[string]string{}
			}
		} else {
			parts := cache.get(snap.Version, locale, func() localizedParts {
				p := localizedParts{
					home:     theme.LocalizeBlocks(out.Body.Theme.Home, locale, notify.Markdown),
					messages: settings.Decode[map[string]map[string]string](snap, "i18n.overrides")[locale],
				}
				if p.messages == nil {
					p.messages = map[string]string{}
				}
				return p
			})
			out.Body.Theme.Home, out.Body.Messages = parts.home, parts.messages
		}
		if d.Plugins != nil {
			populatePluginBootstrap(&out.Body, d.Plugins.FrontendPlugins(locale), viewer)
		}
		cc := captcha.ConfigFrom(snap)
		out.Body.Auth = AuthInfo{
			Registration: cmp.Or(snap.String("auth.registration"), account.RegistrationClosed),
			Captcha:      CaptchaInfo{Provider: cmp.Or(cc.Provider, captcha.None), SiteKey: cc.SiteKey},
			Mail:         mailReady(snap),
			Social:       socialProviders(social.PolicyFrom(snap)),

			RequireVerification: account.VerificationRequired(snap),
		}
		if d.Setup != nil && d.DB == nil {
			out.Body.NeedsSetup = true
			out.Body.Auth.Registration = account.RegistrationClosed
		} else if d.Accounts != nil {
			needs, err := d.Accounts.NeedsSetup(ctx)
			if err != nil {
				return nil, err
			}
			out.Body.NeedsSetup = needs
		}
		return out, nil
	})

	var totals statsCache
	huma.Register(api, huma.Operation{
		OperationID: "getSiteStats",
		Method:      http.MethodGet,
		Path:        "/api/v1/site/stats",
		Summary:     "Site totals",
		Description: "Accounts, players and public textures, for the home page's stats block; up to a minute old.",
		Tags:        []string{"site"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		CacheControl string `header:"Cache-Control"`
		Body         SiteStats
	}, error) {
		t, err := totals.get(ctx, d)
		if err != nil {
			return nil, err
		}
		return &struct {
			CacheControl string `header:"Cache-Control"`
			Body         SiteStats
		}{"public, max-age=60", SiteStats{Users: t.Users, Players: t.Players, Textures: t.PublicTextures}}, nil
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

func populatePluginBootstrap(boot *Bootstrap, list []plugin.FrontendPlugin, viewer *account.Viewer) {
	if len(list) == 0 {
		return
	}
	forms := map[string][]PluginFormField{}
	isAdmin := viewer != nil && viewer.User != nil && viewer.User.Role != user.RoleUser
	isSuperadmin := viewer != nil && viewer.User != nil && viewer.User.Role == user.RoleSuperadmin

	for _, fp := range list {
		isActiveThemePkg := fp.Kind == plugin.KindTheme
		if isActiveThemePkg {
			if fp.ID != boot.Theme.ID {
				continue
			}
			// If a kind:theme package overrides the currently active/previewed theme, attach its Level-5 web entry
			// when trusted without double-loading the bundle as a regular JS/CSS plugin.
			if fp.Entry != "" && boot.Theme.WebEntry == "" {
				boot.Theme.WebEntry = fp.Entry
				boot.Theme.Overrides = slices.Clone(fp.Overrides)
				if fp.Style != "" && !slices.Contains(boot.Theme.CSS, fp.Style) {
					boot.Theme.CSS = append(boot.Theme.CSS, fp.Style)
				}
			}
			fp.Entry = ""
			fp.Style = ""
			fp.Overrides = nil
		}
		boot.Plugins = append(boot.Plugins, PluginFrontend{
			ID:        fp.ID,
			Kind:      fp.Kind,
			Version:   fp.Version,
			Rev:       fp.Rev,
			Name:      fp.Name,
			Entry:     fp.Entry,
			Style:     fp.Style,
			Slots:     fp.Slots,
			Blocks:    fp.Blocks,
			Overrides: fp.Overrides,
			Settings:  fp.Settings,
			Messages:  fp.Messages,
		})
		for _, m := range fp.Menus {
			clean := "/" + strings.TrimPrefix(path.Clean("/"+m.Path), "/")
			if clean == "/" {
				clean = ""
			}
			if (m.Role == "admin" && !isAdmin) || (m.Role == "superadmin" && !isSuperadmin) {
				continue
			}
			var prefix string
			switch m.Area {
			case "explore":
				prefix = "/p/" + fp.ID
			case "user":
				prefix = "/user/p/" + fp.ID
			case "admin":
				if !isAdmin {
					continue
				}
				prefix = "/admin/p/" + fp.ID
			default:
				continue
			}
			boot.Menus[m.Area] = append(boot.Menus[m.Area], MenuItem{
				ID:    "plugin:" + fp.ID + clean,
				Title: m.Title,
				Href:  prefix + clean,
				Icon:  m.Icon,
				Order: m.Order,
			})
		}
		for scope, defs := range fp.Forms {
			for _, d := range defs {
				forms[scope] = append(forms[scope], PluginFormField{
					PluginID:    fp.ID,
					Name:        d.Name,
					Type:        d.Type,
					Label:       d.Label,
					Description: d.Description,
					Placeholder: d.Placeholder,
					Required:    d.Required,
					MaxLength:   d.MaxLength,
					Options:     d.Options,
				})
			}
		}
	}
	if len(forms) > 0 {
		boot.Forms = forms
	}
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
