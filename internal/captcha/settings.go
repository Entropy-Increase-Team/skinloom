package captcha

import (
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// SettingDefs are the captcha settings. Site keys are public (the widget needs them);
// secret keys never leave the server.
func SettingDefs() []settings.Def {
	defs := []settings.Def{{
		Key: "captcha.provider", Type: settings.TypeString, Public: true, Default: Image,
		Enum: []any{None, Image, Turnstile, ReCAPTCHA, HCaptcha},
		Doc:  "Captcha on registration and after failed logins: none | image (built in) | turnstile | recaptcha | hcaptcha.",
	}}
	for _, p := range []struct{ id, name string }{
		{Turnstile, "Cloudflare Turnstile"}, {ReCAPTCHA, "Google reCAPTCHA v2"}, {HCaptcha, "hCaptcha"},
	} {
		defs = append(defs,
			settings.Def{Key: "captcha." + p.id + ".site_key", Type: settings.TypeString, Public: true, Default: "",
				Doc: p.name + " site key (public)."},
			settings.Def{Key: "captcha." + p.id + ".secret_key", Type: settings.TypeString, Default: "", Secret: true,
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
