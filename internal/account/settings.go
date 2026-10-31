package account

import (
	"errors"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Registration modes.
const (
	RegistrationOpen   = "open"
	RegistrationClosed = "closed"
)

// SettingDefs are the account settings (Blessing Skin equivalents in docs/design/02 §4).
func SettingDefs() []settings.Def {
	return []settings.Def{
		{
			Key: "auth.registration", Type: settings.TypeString, Public: true, Default: RegistrationOpen,
			Enum: []any{RegistrationOpen, RegistrationClosed},
			Doc:  "open: anyone can register; closed: only administrators create accounts.",
		},
		{
			Key: "auth.registrations_per_ip", Type: settings.TypeInt, Default: 3, Range: settings.AtLeast(0),
			Doc: "Accounts that can be registered from one address (IPv6: one /64) per 24 hours; 0 = unlimited. (BS regs_per_ip)",
		},
		{
			Key: "auth.login_captcha_after", Type: settings.TypeInt, Default: 3, Range: settings.AtLeast(0),
			Doc: "Failed logins for an account from one client before a captcha is required; 0 = always. Ignored when captcha.provider is none.",
		},
		{
			Key: "auth.require_email_verification", Type: settings.TypeBool, Public: true, Default: false,
			Doc: "Accounts must verify their email before they manage players, upload or change textures, report, " +
				"register OAuth applications or sign in to launchers. Needs mail.driver and site.url. (BS require_verification)",
		},
		{
			Key: "score.initial", Type: settings.TypeInt, Default: 1000, Range: settings.Between(0, 1_000_000_000),
			Doc: "Score a new account starts with. (BS user_initial_score)",
		},
	}
}

// ErrEmailNotVerified: the site requires a verified email address and the account has
// not verified its own.
var ErrEmailNotVerified = errors.New("verify your email address first")

// VerificationRequired reports whether the site requires a verified email address
// (auth.require_email_verification) before managing players, uploading or changing
// textures, reporting, registering OAuth applications and signing in to launchers, the
// actions behind Blessing Skin's verified middleware.
func VerificationRequired(s *settings.Snapshot) bool {
	return settings.Decode[bool](s, "auth.require_email_verification")
}

// CheckVerified returns ErrEmailNotVerified if the site requires a verified email
// address and u has not verified its own.
func CheckVerified(s *settings.Snapshot, u *ent.User) error {
	if VerificationRequired(s) && u.EmailVerifiedAt == nil {
		return ErrEmailNotVerified
	}
	return nil
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
