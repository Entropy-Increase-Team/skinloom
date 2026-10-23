package mail

import (
	"fmt"
	"net/mail"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// SettingDefs are the mail settings.
func SettingDefs() []settings.Def {
	oneOf := func(allowed ...string) func(v any) error {
		return func(v any) error {
			for _, a := range allowed {
				if v.(string) == a {
					return nil
				}
			}
			return fmt.Errorf("want one of %v", allowed)
		}
	}
	return []settings.Def{
		{
			Key: "mail.driver", Type: settings.TypeString, Default: DriverNone, Validate: oneOf(DriverNone, DriverSMTP, DriverLog),
			Doc: "none: no mail (no email verification or password reset); smtp; log: write mail to the server log instead of sending (development).",
		},
		{
			Key: "mail.from", Type: settings.TypeString, Default: "",
			Doc: `Sender, e.g. "SkinLoom <noreply@skins.example.com>".`,
			Validate: func(v any) error {
				if s := v.(string); s != "" {
					if _, err := mail.ParseAddress(s); err != nil {
						return fmt.Errorf("want an address like SkinLoom <noreply@example.com>")
					}
				}
				return nil
			},
		},
		{Key: "mail.smtp.host", Type: settings.TypeString, Default: "", Doc: "SMTP server, e.g. smtp.qq.com."},
		{
			Key: "mail.smtp.port", Type: settings.TypeInt, Default: 465, Doc: "SMTP port: 465 for tls, 587 for starttls.",
			Validate: func(v any) error {
				var n float64
				switch x := v.(type) {
				case float64:
					n = x
				case int:
					n = float64(x)
				}
				if n < 1 || n > 65535 {
					return fmt.Errorf("want 1-65535")
				}
				return nil
			},
		},
		{Key: "mail.smtp.username", Type: settings.TypeString, Default: ""},
		{Key: "mail.smtp.password", Type: settings.TypeString, Default: "", Doc: "SMTP password or app-specific authorization code (server only)."},
		{
			Key: "mail.smtp.encryption", Type: settings.TypeString, Default: "tls", Validate: oneOf("tls", "starttls", "none"),
			Doc: "tls: encrypted from the start (port 465); starttls: upgrade after connecting (587); none: only for a relay on the same host.",
		},
	}
}

// ConfigFrom reads the mail setup from a settings snapshot.
func ConfigFrom(s *settings.Snapshot) Config {
	return Config{
		Driver:     s.String("mail.driver"),
		From:       s.String("mail.from"),
		Host:       s.String("mail.smtp.host"),
		Port:       settings.Decode[int](s, "mail.smtp.port"),
		Username:   s.String("mail.smtp.username"),
		Password:   s.String("mail.smtp.password"),
		Encryption: s.String("mail.smtp.encryption"),
	}
}
