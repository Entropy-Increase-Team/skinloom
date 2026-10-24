package account

import (
	"fmt"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Registration modes.
const (
	RegistrationOpen   = "open"
	RegistrationClosed = "closed"
)

// SettingDefs are the account settings (Blessing Skin equivalents in docs/design/02 §4).
func SettingDefs() []settings.Def {
	nonNegative := func(v any) error {
		if toInt(v) < 0 {
			return fmt.Errorf("must be 0 or more")
		}
		return nil
	}
	return []settings.Def{
		{
			Key: "auth.registration", Type: settings.TypeString, Public: true, Default: RegistrationOpen,
			Doc: "open: anyone can register; closed: only administrators create accounts.",
			Validate: func(v any) error {
				if s := v.(string); s != RegistrationOpen && s != RegistrationClosed {
					return fmt.Errorf("want open or closed")
				}
				return nil
			},
		},
		{
			Key: "auth.registrations_per_ip", Type: settings.TypeInt, Default: 3, Validate: nonNegative,
			Doc: "Accounts that can be registered from one address (IPv6: one /64) per 24 hours; 0 = unlimited. (BS regs_per_ip)",
		},
		{
			Key: "auth.login_captcha_after", Type: settings.TypeInt, Default: 3, Validate: nonNegative,
			Doc: "Failed logins for an account from one client before a captcha is required; 0 = always. Ignored when captcha.provider is none.",
		},
		{
			Key: "score.initial", Type: settings.TypeInt, Default: 1000, Validate: nonNegative,
			Doc: "Score a new account starts with. (BS user_initial_score)",
		},
	}
}

// RegisterPolicy is what registration needs from the settings.
type RegisterPolicy struct {
	Open         bool
	PerIPPerDay  int // 0 = unlimited
	InitialScore int64
}

// LoginPolicy is what login needs from the settings.
type LoginPolicy struct {
	CaptchaEnabled bool
	// CaptchaAfter failures of one client on one account require a captcha.
	CaptchaAfter int
}

// PoliciesFrom reads the policies from a settings snapshot. captchaEnabled says
// whether a captcha provider is configured.
func PoliciesFrom(s *settings.Snapshot, captchaEnabled bool) (RegisterPolicy, LoginPolicy) {
	return RegisterPolicy{
		Open:         s.String("auth.registration") == RegistrationOpen,
		PerIPPerDay:  settings.Decode[int](s, "auth.registrations_per_ip"),
		InitialScore: settings.Decode[int64](s, "score.initial"),
	}, LoginPolicy{
		CaptchaEnabled: captchaEnabled,
		CaptchaAfter:   settings.Decode[int](s, "auth.login_captcha_after"),
	}
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
