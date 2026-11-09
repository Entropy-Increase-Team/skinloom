// Package sitesettings assembles the definitions of every module's settings into one
// registry, so the server and the Blessing Skin importer check settings alike.
package sitesettings

import (
	"fmt"
	"slices"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

// Builtin is Registry with only the built-in themes, for tools that run without the
// server (the BS importer).
func Builtin() (*settings.Registry, error) {
	themeReg := theme.NewRegistry()
	if err := themeReg.LoadDir(themes.Builtin, "builtin"); err != nil {
		return nil, fmt.Errorf("load built-in themes: %w", err)
	}
	return Registry(themeReg)
}

// Registry returns the core settings and every module's, with the validators that need
// the theme registry.
func Registry(themes *theme.Registry) (*settings.Registry, error) {
	reg := settings.NewRegistry()
	defs := slices.Concat(account.SettingDefs(), captcha.SettingDefs(), mail.SettingDefs(),
		score.SettingDefs(), texture.SettingDefs(), player.SettingDefs(), yggdrasil.SettingDefs(), oauth.SettingDefs(),
		social.SettingDefs(), cdn.SettingDefs())
	if err := reg.Register(defs...); err != nil {
		return nil, err
	}
	reg.SetValidator("theme.active", themes.ValidateActive)
	reg.SetValidator("theme.overrides", theme.ValidateOverrides)
	reg.SetValidator("home.blocks", theme.ValidateHome)
	return reg, nil
}
