package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// SettingView is one setting as shown in the admin panel.
type SettingView struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Value   any    `json:"value"`
	Default any    `json:"default"`
	IsSet   bool   `json:"isSet" doc:"false when the default is in effect"`
	Public  bool   `json:"public"`
	Doc     string `json:"doc"`
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

// ThemeView is one installed theme.
type ThemeView struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Name        map[string]string `json:"name"`
	Description map[string]string `json:"description,omitempty"`
	Source      string            `json:"source"`
	Active      bool              `json:"active"`
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
			out = append(out, ThemeView{ID: t.ID, Version: t.Version, Name: t.Name, Description: t.Description,
				Source: t.Source, Active: t.ID == active})
		}
		return &themesOutput{Body: out}, nil
	})
}

func settingsList(s *settings.Service) SettingsList {
	snap := s.Current()
	list := SettingsList{Version: snap.Version}
	for _, def := range s.Registry().All() {
		var value any
		_ = json.Unmarshal(snap.Raw(def.Key), &value)
		list.Settings = append(list.Settings, SettingView{
			Key: def.Key, Type: string(def.Type), Value: value, Default: def.Default,
			IsSet: snap.IsSet(def.Key), Public: def.Public, Doc: def.Doc,
		})
	}
	return list
}
