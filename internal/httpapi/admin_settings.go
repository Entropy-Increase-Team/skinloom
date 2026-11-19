package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
)

// SettingView is one setting as shown in the admin panel.
type SettingView struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Value   any    `json:"value" doc:"null for a secret setting; secret parts of other values are replaced by ********"`
	Default any    `json:"default"`
	IsSet   bool   `json:"isSet" doc:"false when the default is in effect"`
	Public  bool   `json:"public"`
	Secret  bool   `json:"secret" doc:"Write-only: the value is never sent back, leave the key out to keep it"`
	// Superadmin settings put code into every page; other administrators see them read-only.
	Superadmin bool   `json:"superadmin"`
	Enum       []any  `json:"enum,omitempty" doc:"The allowed values"`
	Min        *int64 `json:"min,omitempty"`
	Max        *int64 `json:"max,omitempty"`
	Doc        string `json:"doc"`
}

// SettingsList is the admin view of all settings.
type SettingsList struct {
	Version  int64         `json:"version"`
	Settings []SettingView `json:"settings"`
}

// The header is optional in the schema so a missing token yields 401 (checked in the
// handler) instead of a 422 validation error.
type adminAuth struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
}

type settingsOutput struct {
	Body SettingsList
}

// Huma does not read parameter tags from embedded structs, so the header is repeated here.
type updateSettingsInput struct {
	Authorization string         `header:"Authorization" doc:"Bearer <admin token>"`
	Body          map[string]any `doc:"Keys to change. A null value resets the key to its default."`
}

// ThemeView is one installed theme, with what it brings before the administrator's
// overrides: the appearance page shows these as the defaults.
type ThemeView struct {
	ID          string                   `json:"id"`
	Version     string                   `json:"version"`
	Name        map[string]string        `json:"name"`
	Description map[string]string        `json:"description,omitempty"`
	Source      string                   `json:"source"`
	Active      bool                     `json:"active"`
	Extends     string                   `json:"extends,omitempty"`
	Screenshot  string                   `json:"screenshot,omitempty"`
	ColorScheme string                   `json:"colorScheme"`
	Tokens      theme.Tokens             `json:"tokens"`
	Layout      map[string]string        `json:"layout"`
	Home        []theme.Block            `json:"home" doc:"The theme's home page, used while home.blocks is null"`
	Overrides   []string                 `json:"overrides,omitempty"`
	HasWeb      bool                     `json:"hasWeb,omitempty"`
	WebTrusted  bool                     `json:"webTrusted,omitempty"`
	Settings    map[string]theme.Setting `json:"settings,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

type themePreviewInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Body          struct {
		ThemeID string `json:"themeId" doc:"Theme ID to preview for this administrator session; empty clears preview"`
	}
}

type themePreviewOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		PreviewThemeID string `json:"previewThemeId"`
	}
}

type blockTypesOutput struct {
	Body []theme.BlockType
}

type themesOutput struct {
	Body []ThemeView
}

var adminSecurity = []map[string][]string{{"session": {}}, {"adminToken": {}}}

func registerAdmin(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "listSettings",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/settings",
		Summary:     "List runtime settings",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *adminAuth) (*settingsOutput, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		return &settingsOutput{Body: settingsList(d.Settings)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateSettings",
		Method:      http.MethodPatch,
		Path:        "/api/v1/admin/settings",
		Summary:     "Update runtime settings",
		Description: "Changes apply immediately on every node and in every open browser tab (hot reload).",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *updateSettingsInput) (*settingsOutput, error) {
		actor, err := checkAdmin(ctx, d, in.Authorization)
		if err != nil {
			return nil, err
		}
		if err := checkSuperadminKeys(ctx, d, in.Body); err != nil {
			return nil, err
		}
		changes := make(map[string]json.RawMessage, len(in.Body))
		for k, v := range in.Body {
			raw, err := json.Marshal(v)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("invalid value for " + k)
			}
			changes[k] = raw
		}
		rec := &audit.Entry{Actor: actor, Action: "settings.update", TargetType: "settings", IP: requestInfo(ctx).client.ClientIP}
		if _, err := d.Settings.Set(ctx, actor, changes, rec); err != nil {
			var verr *settings.ValidationError
			if errors.As(err, &verr) {
				details := make([]error, 0, len(verr.Fields))
				for k, msg := range verr.Fields {
					details = append(details, &huma.ErrorDetail{Location: "body." + k, Message: msg, Value: in.Body[k]})
				}
				return nil, huma.Error422UnprocessableEntity("invalid settings", details...)
			}
			return nil, err
		}
		return &settingsOutput{Body: settingsList(d.Settings)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listThemes",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/themes",
		Summary:     "List installed themes",
		Description: "Switch themes by setting theme.active.",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *adminAuth) (*themesOutput, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		return &themesOutput{Body: themeViews(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "reloadThemes",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/themes/reload",
		Summary:     "Rescan local themes",
		Description: "Reloads themes from <data>/themes and broadcasts a config version bump.",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *adminAuth) (*themesOutput, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		d.Themes.Reload()
		if _, err := d.Settings.Touch(ctx); err != nil {
			return nil, err
		}
		return &themesOutput{Body: themeViews(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listBlockTypes",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/blocks",
		Summary:     "Block types home.blocks can use",
		Description: "Each type with its properties; home.blocks is checked against these.",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *adminAuth) (*blockTypesOutput, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		return &blockTypesOutput{Body: theme.AllBlockTypes()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "setThemePreview",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/themes/preview",
		Summary:     "Set or clear administrator session theme preview",
		Description: "Stores the previewed theme ID in an HttpOnly cookie affecting only this administrator session.",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *themePreviewInput) (*themePreviewOutput, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		id := in.Body.ThemeID
		secure := requestInfo(ctx).client.Proto == "https"
		var cookie http.Cookie
		if id == "" {
			cookie = http.Cookie{
				Name:     themePreviewCookie,
				Value:    "",
				Path:     "/",
				HttpOnly: true,
				Secure:   secure,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   -1,
			}
		} else {
			if d.Themes == nil {
				return nil, huma.Error404NotFound("theme registry unavailable")
			}
			th, ok := d.Themes.Get(id)
			if !ok {
				return nil, huma.Error404NotFound("unknown theme " + id)
			}
			if th.Error != "" {
				return nil, huma.Error422UnprocessableEntity("theme has errors: " + th.Error)
			}
			cookie = http.Cookie{
				Name:     themePreviewCookie,
				Value:    id,
				Path:     "/",
				HttpOnly: true,
				Secure:   secure,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400,
			}
		}
		var out themePreviewOutput
		out.SetCookie = cookie.String()
		out.Body.PreviewThemeID = id
		return &out, nil
	})
}

func themeViews(d Deps) []ThemeView {
	active := d.Settings.Current().String("theme.active")
	if _, ok := d.Themes.Get(active); !ok {
		active = theme.DefaultID
	}
	trustFn := trustThemeWebFn(d)
	var out []ThemeView
	for _, t := range d.Themes.List() {
		home := t.Theme.Home
		if home == nil {
			home = []theme.Block{}
		}
		cs := t.Theme.ColorScheme
		if cs == "" {
			cs = "both"
		}
		hasWeb := t.WebEntry != ""
		trusted := hasWeb && (t.Source == "builtin" || trustFn(t.ID))
		out = append(out, ThemeView{
			ID:          t.ID,
			Version:     t.Version,
			Name:        t.Name,
			Description: t.Description,
			Source:      t.Source,
			Active:      t.Error == "" && t.ID == active,
			Extends:     t.Extends,
			Screenshot:  t.ScreenshotURL,
			ColorScheme: cs,
			Tokens:      t.Theme.Tokens,
			Layout:      t.Theme.Layout,
			Home:        home,
			Overrides:   slices.Clone(t.Overrides),
			HasWeb:      hasWeb,
			WebTrusted:  trusted,
			Settings:    t.Theme.Settings,
			Error:       t.Error,
		})
	}
	return out
}

// checkSuperadminKeys refuses an administrator who is not a super administrator to
// change a Superadmin setting (custom CSS reaches every page, BS S16). The admin token
// and the CLI belong to whoever runs the server and may.
func checkSuperadminKeys(ctx context.Context, d Deps, body map[string]any) error {
	v := requestInfo(ctx).viewer()
	if v == nil || v.User.Role == user.RoleSuperadmin {
		return nil
	}
	var details []error
	for k := range body {
		if def, ok := d.Settings.Registry().Def(k); ok && def.Superadmin {
			details = append(details, &huma.ErrorDetail{Location: "body." + k, Message: "only super administrators change this"})
		}
	}
	if len(details) > 0 {
		return huma.Error403Forbidden("only super administrators change these settings", details...)
	}
	return nil
}

func settingsList(s *settings.Service) SettingsList {
	snap := s.Current()
	list := SettingsList{Version: snap.Version}
	for _, def := range s.Registry().All() {
		var value any
		_ = json.Unmarshal(snap.Raw(def.Key), &value)
		v := SettingView{
			Key: def.Key, Type: string(def.Type), Value: def.Shown(value), Default: def.Shown(def.Default),
			IsSet: snap.IsSet(def.Key), Public: def.Public, Secret: def.Secret, Superadmin: def.Superadmin, Enum: def.AllowedEnum(), Doc: def.Doc,
		}
		if r := def.Range; r != nil {
			lo, hi := r.Min, r.Max
			v.Min = &lo
			if hi != math.MaxInt64 {
				v.Max = &hi
			}
		}
		list.Settings = append(list.Settings, v)
	}
	return list
}
