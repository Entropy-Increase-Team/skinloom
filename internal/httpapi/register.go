package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
)

type registerInput struct {
	UserAgent string `header:"User-Agent"`
	Session   string `cookie:"skinloom_session"`
	Body      struct {
		Email    string          `json:"email" maxLength:"255"`
		Nickname string          `json:"nickname" maxLength:"128"`
		Password string          `json:"password" maxLength:"1024"`
		Captcha  *captcha.Answer `json:"captcha,omitempty" doc:"Required unless captcha.provider is none"`
	}
}

// CaptchaImage is a fresh image challenge.
type CaptchaImage struct {
	ID    string `json:"id"`
	Image string `json:"image" doc:"data:image/png;base64,…"`
}

type captchaImageOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         CaptchaImage
}

// captchaImagesPerClient bounds image challenges per client (IPv6: per /64) in ten
// minutes: rendering costs CPU, and a flood would push other visitors' unanswered
// challenges out of the store. A person needs a handful.
const captchaImagesPerClient = 300

func registerAccounts(api huma.API, d Deps) {
	images := account.NewLimiter(captchaImagesPerClient, 10*time.Minute)
	huma.Register(api, huma.Operation{
		OperationID: "getCaptchaImage",
		Method:      http.MethodGet,
		Path:        "/api/v1/captcha/image",
		Summary:     "New image captcha",
		Description: "Only when captcha.provider is image. Each challenge works once and expires after 5 minutes.",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*captchaImageOutput, error) {
		if captcha.ConfigFrom(d.Settings.Current()).Provider != captcha.Image {
			return nil, huma.Error404NotFound("the image captcha is not enabled")
		}
		client := account.ClientKey(requestInfo(ctx).client.ClientIP)
		if wait, blocked := images.Blocked(client); blocked {
			return nil, rateLimitError(&account.RateLimitError{RetryAfter: wait}, "too many captcha images, try again later")
		}
		images.Fail(client)
		id, png, err := d.Captcha.NewImage()
		if err != nil {
			return nil, err
		}
		return &captchaImageOutput{CacheControl: "no-store", Body: CaptchaImage{
			ID: id, Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "register",
		Method:      http.MethodPost,
		Path:        "/api/v1/auth/register",
		Summary:     "Create an account",
		Description: "Signs the new user in. Needs auth.registration = open and, unless captcha.provider is none, a solved captcha.",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, in *registerInput) (*userOutput, error) {
		ri := requestInfo(ctx)
		snap := d.Settings.Current()
		cc := captcha.ConfigFrom(snap)
		policy, _ := account.PoliciesFrom(snap, cc.Enabled())
		if !policy.Open {
			return nil, huma.Error403Forbidden("registration is closed")
		}
		b := in.Body
		// Field mistakes first: a captcha answer is single-use, so do not spend it on a
		// form that would be rejected anyway.
		if err := account.ValidateNewAccount(b.Email, b.Nickname, b.Password); err != nil {
			return nil, apiError(err)
		}
		if cc.Enabled() {
			if err := checkCaptcha(ctx, d, cc, b.Captcha, ri.client.ClientIP); err != nil {
				return nil, err
			}
		}
		u, sess, err := d.Accounts.Register(ctx, account.RegisterInput{Email: b.Email, Nickname: b.Nickname, Password: b.Password},
			policy, mailContext(d, ri), ri.meta(in.UserAgent))
		switch {
		case errors.Is(err, account.ErrRegistrationClosed):
			return nil, huma.Error403Forbidden("registration is closed")
		case errors.Is(err, account.ErrSetupPending):
			return nil, huma.Error403Forbidden("the site has not been set up yet")
		case isRateLimit(err):
			return nil, rateLimitError(err, "too many accounts registered from this address, try again later")
		case err != nil:
			return nil, apiError(err)
		}
		replaceSession(context.WithoutCancel(ctx), d, in.Session)
		return &userOutput{SetCookie: sessionCookie(sess, ri), Body: userView(u)}, nil
	})
}

// checkCaptcha verifies a captcha answer: 422 on body.captcha when it is missing or
// wrong (the form shows or refreshes the captcha), 503 when the provider is down.
func checkCaptcha(ctx context.Context, d Deps, c captcha.Config, a *captcha.Answer, ip string) error {
	if a == nil || a.Value == "" {
		return captchaError("required")
	}
	switch err := d.Captcha.Verify(ctx, c, *a, ip); {
	case errors.Is(err, captcha.ErrInvalid):
		return captchaError("wrong or expired")
	case errors.Is(err, captcha.ErrUnavailable):
		return huma.Error503ServiceUnavailable("the captcha service is unavailable, try again later")
	default:
		return err
	}
}

func captchaError(msg string) error {
	return huma.Error422UnprocessableEntity("captcha "+msg, &huma.ErrorDetail{Location: "body.captcha", Message: msg})
}

func isRateLimit(err error) bool {
	var rl *account.RateLimitError
	return errors.As(err, &rl)
}

func rateLimitError(err error, msg string) error {
	var rl *account.RateLimitError
	errors.As(err, &rl)
	secs := int(rl.RetryAfter.Round(time.Second) / time.Second)
	return huma.ErrorWithHeaders(huma.Error429TooManyRequests(msg), http.Header{"Retry-After": {strconv.Itoa(max(1, secs))}})
}
