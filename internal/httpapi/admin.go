package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
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
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Name        map[string]string `json:"name"`
	Description map[string]string `json:"description,omitempty"`
	Source      string            `json:"source"`
	Active      bool              `json:"active"`
	Tokens      theme.Tokens      `json:"tokens"`
	Layout      map[string]string `json:"layout"`
	Home        []theme.Block     `json:"home" doc:"The theme's home page, used while home.blocks is null"`
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
		active := d.Settings.Current().String("theme.active")
		var out []ThemeView
		for _, t := range d.Themes.List() {
			home := t.Theme.Home
			if home == nil {
				home = []theme.Block{}
			}
			out = append(out, ThemeView{ID: t.ID, Version: t.Version, Name: t.Name, Description: t.Description,
				Source: t.Source, Active: t.ID == active, Tokens: t.Theme.Tokens, Layout: t.Theme.Layout, Home: home})
		}
		return &themesOutput{Body: out}, nil
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
		return &blockTypesOutput{Body: theme.BlockTypes}, nil
	})
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
			IsSet: snap.IsSet(def.Key), Public: def.Public, Secret: def.Secret, Superadmin: def.Superadmin, Enum: def.Enum, Doc: def.Doc,
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
