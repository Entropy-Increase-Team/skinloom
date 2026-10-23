package captcha

import (
	"fmt"
	"slices"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

var providers = []string{None, Image, Turnstile, ReCAPTCHA, HCaptcha}

// SettingDefs are the captcha settings. Site keys are public (the widget needs them);
// secret keys never leave the server.
func SettingDefs() []settings.Def {
	defs := []settings.Def{{
		Key: "captcha.provider", Type: settings.TypeString, Public: true, Default: Image,
		Doc: "Captcha on registration and after failed logins: none | image (built in) | turnstile | recaptcha | hcaptcha.",
		Validate: func(v any) error {
			if !slices.Contains(providers, v.(string)) {
				return fmt.Errorf("want one of %v", providers)
			}
			return nil
		},
	}}
	for _, p := range []struct{ id, name string }{
		{Turnstile, "Cloudflare Turnstile"}, {ReCAPTCHA, "Google reCAPTCHA v2"}, {HCaptcha, "hCaptcha"},
	} {
		defs = append(defs,
			settings.Def{Key: "captcha." + p.id + ".site_key", Type: settings.TypeString, Public: true, Default: "",
				Doc: p.name + " site key (public)."},
			settings.Def{Key: "captcha." + p.id + ".secret_key", Type: settings.TypeString, Default: "",
				Doc: p.name + " secret key (server only)."},
		)
	}
	return defs
}

// ConfigFrom reads the active provider and its keys from a settings snapshot.
func ConfigFrom(s *settings.Snapshot) Config {
	p := s.String("captcha.provider")
	c := Config{Provider: p}
	if p == Turnstile || p == ReCAPTCHA || p == HCaptcha {
		c.SiteKey = s.String("captcha." + p + ".site_key")
		c.SecretKey = s.String("captcha." + p + ".secret_key")
	}
	return c
}
