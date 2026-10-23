package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// mailReady reports whether mail with links can be sent: a mail driver and site.url.
// Links never come from the request's Host header: anyone could then get a genuine
// reset mail whose link points at their own server (password reset poisoning).
func mailReady(snap *settings.Snapshot) bool {
	return mail.ConfigFrom(snap).Enabled() && snap.String("site.url") != ""
}

// mailContext tells the account service how to write mail for this request.
func mailContext(d Deps, ri reqInfo) account.MailContext {
	snap := d.Settings.Current()
	return account.MailContext{
		Enabled: mailReady(snap),
		BaseURL: strings.TrimRight(snap.String("site.url"), "/"),
		Locale:  ri.locale,
		Site:    snap.Localized("site.name", ri.locale),
	}
}

// mailUnavailable explains why a mail could not be sent.
func mailUnavailable(d Deps) error {
	if mail.ConfigFrom(d.Settings.Current()).Enabled() {
		return huma.Error503ServiceUnavailable("site.url is not set, so mail links cannot be built; contact the administrator")
	}
	return huma.Error503ServiceUnavailable("mail is not configured on this site; contact the administrator")
}

type forgotInput struct {
	Body struct {
		Email   string          `json:"email" maxLength:"255"`
		Captcha *captcha.Answer `json:"captcha,omitempty" doc:"Required unless captcha.provider is none"`
	}
}

type resetInput struct {
	Body struct {
		Token    string `json:"token" maxLength:"64"`
		Password string `json:"password" maxLength:"1024"`
	}
}

type verifyInput struct {
	Body struct {
		Token string `json:"token" maxLength:"64"`
	}
}

type mailTestInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Body          struct {
		To string `json:"to,omitempty" maxLength:"255" doc:"Recipient; default: the signed-in administrator"`
	}
}

func registerMail(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "forgotPassword",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/password/forgot",
		Summary:       "Mail a password reset link",
		Description:   "Always 202, whether or not the address belongs to an account. The link is valid for 30 minutes and works once.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *forgotInput) (*struct{}, error) {
		ri := requestInfo(ctx)
		mc := mailContext(d, ri)
		if !mc.Enabled {
			return nil, mailUnavailable(d)
		}
		if err := account.ValidateEmail(in.Body.Email); err != nil {
			return nil, apiError(err)
		}
		if cc := captcha.ConfigFrom(d.Settings.Current()); cc.Enabled() {
			if err := checkCaptcha(ctx, d, cc, in.Body.Captcha, ri.client.ClientIP); err != nil {
				return nil, err
			}
		}
		switch err := d.Accounts.RequestPasswordReset(ctx, in.Body.Email, mc, ri.meta("")); {
		case isRateLimit(err):
			return nil, rateLimitError(err, "too many reset requests, try again later")
		case err != nil:
			return nil, err
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "resetPassword",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/password/reset",
		Summary:       "Set a new password with a reset link",
		Description:   "Signs the account out everywhere. The link stops working once used.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *resetInput) (*struct{}, error) {
		switch err := d.Accounts.ResetPassword(ctx, in.Body.Token, in.Body.Password, requestInfo(ctx).meta("")); {
		case errors.Is(err, account.ErrInvalidToken):
			return nil, huma.Error403Forbidden("the reset link is invalid, used or expired; ask for a new one")
		case err != nil:
			return nil, apiError(err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "verifyEmail",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/email/verify",
		Summary:       "Confirm an email address with a verification link",
		Description:   "A POST from the verification page (not a GET on the link), so mail scanners that open links do not use them up.",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *verifyInput) (*struct{}, error) {
		if _, err := d.Accounts.VerifyEmail(ctx, in.Body.Token, requestInfo(ctx).meta("")); err != nil {
			if errors.Is(err, account.ErrInvalidToken) {
				return nil, huma.Error403Forbidden("the verification link is invalid, used or expired")
			}
			return nil, err
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "resendVerification",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/email/resend",
		Summary:       "Send a new verification link to the signed-in user",
		Tags:          []string{"auth"},
		Security:      sessionSecurity,
		DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		ri := requestInfo(ctx)
		v := ri.viewer()
		if v == nil {
			return nil, huma.Error401Unauthorized("not signed in")
		}
		switch err := d.Accounts.ResendVerification(ctx, v.User.ID, mailContext(d, ri)); {
		case errors.Is(err, account.ErrMailDisabled):
			return nil, mailUnavailable(d)
		case errors.Is(err, account.ErrAlreadyVerified):
			return nil, huma.Error409Conflict("the email address is already verified")
		case isRateLimit(err):
			return nil, rateLimitError(err, "a link was sent recently, try again later")
		case err != nil:
			return nil, err
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "sendTestMail",
		Method:        http.MethodPost,
		Path:          "/api/v1/admin/mail/test",
		Summary:       "Send a test email with the current mail settings",
		Description:   "Sent right away (not queued). On failure the SMTP error is returned, for administrators only.",
		Tags:          []string{"admin"},
		Security:      adminSecurity,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *mailTestInput) (*struct{}, error) {
		if _, err := checkAdmin(ctx, d, in.Authorization); err != nil {
			return nil, err
		}
		ri := requestInfo(ctx)
		to := in.Body.To
		if to == "" {
			if v := ri.viewer(); v != nil {
				to = v.User.Email
			}
		}
		if err := account.ValidateEmail(to); err != nil {
			return nil, apiError(&account.FieldError{Field: "to", Message: "a recipient is required"})
		}
		snap := d.Settings.Current()
		msg := mail.Render(mail.KindTest, ri.locale, to, mail.Data{Site: snap.Localized("site.name", ri.locale)})
		if err := d.Mailer.Send(ctx, mail.ConfigFrom(snap), msg); err != nil {
			if errors.Is(err, mail.ErrDisabled) {
				return nil, huma.Error409Conflict("mail.driver is none; set it to smtp (or log) first")
			}
			return nil, huma.NewError(http.StatusBadGateway, "sending failed: "+err.Error())
		}
		return nil, nil
	})
}
