// Package captcha verifies that a form was filled in by a person. The provider is a
// runtime setting (hot reload): the built-in image captcha (no third-party service,
// works in mainland China), Cloudflare Turnstile, Google reCAPTCHA (via
// recaptcha.net, reachable from China) or hCaptcha.
package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Providers.
const (
	None      = "none"
	Image     = "image"
	Turnstile = "turnstile"
	ReCAPTCHA = "recaptcha"
	HCaptcha  = "hcaptcha"
)

var (
	// ErrInvalid means the answer is wrong, expired or already used.
	ErrInvalid = errors.New("captcha answer is wrong or expired")
	// ErrUnavailable means the third-party service could not be asked. Forms fail
	// closed: no captcha check, no registration.
	ErrUnavailable = errors.New("captcha service unavailable")
)

// Config is the active provider and its keys (from settings).
type Config struct {
	Provider  string
	SiteKey   string // public, given to the browser widget
	SecretKey string // server side only
}

// Enabled reports whether forms must carry a captcha.
func (c Config) Enabled() bool { return c.Provider != "" && c.Provider != None }

// Answer is what the browser sends: for the image captcha the challenge id and the
// typed text, for widget providers only the token in Value.
type Answer struct {
	ID    string `json:"id,omitempty" doc:"Image captcha challenge id"`
	Value string `json:"value" maxLength:"4096" doc:"Typed text (image captcha) or the widget token"`
}

// Default siteverify endpoints.
var defaultEndpoints = map[string]string{
	Turnstile: "https://challenges.cloudflare.com/turnstile/v0/siteverify",
	ReCAPTCHA: "https://www.recaptcha.net/recaptcha/api/siteverify",
	HCaptcha:  "https://api.hcaptcha.com/siteverify",
}

// Options configure a Service; the zero value is fine.
type Options struct {
	HTTPClient *http.Client
	// Endpoints overrides siteverify URLs per provider (tests, self-hosted proxies).
	Endpoints map[string]string
	Log       *slog.Logger
}

// Service issues image challenges and verifies answers for every provider.
type Service struct {
	images    *store
	client    *http.Client
	endpoints map[string]string
	log       *slog.Logger
}

// NewService creates a Service.
func NewService(o Options) *Service {
	s := &Service{images: newStore(), client: o.HTTPClient, endpoints: map[string]string{}, log: o.Log}
	if s.client == nil {
		s.client = &http.Client{Timeout: 8 * time.Second}
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	for k, v := range defaultEndpoints {
		s.endpoints[k] = v
	}
	for k, v := range o.Endpoints {
		s.endpoints[k] = v
	}
	return s
}

// NewImage creates an image challenge. The answer stays on the server.
func (s *Service) NewImage() (id string, png []byte, err error) {
	text := randomText(answerLen)
	png, err = render(text)
	if err != nil {
		return "", nil, err
	}
	return s.images.put(text), png, nil
}

// Verify checks an answer against the configured provider. With the provider off it
// always succeeds.
func (s *Service) Verify(ctx context.Context, c Config, a Answer, remoteIP string) error {
	switch c.Provider {
	case "", None:
		return nil
	case Image:
		if !s.images.take(a.ID, a.Value) {
			return ErrInvalid
		}
		return nil
	case Turnstile, ReCAPTCHA, HCaptcha:
		if strings.TrimSpace(a.Value) == "" {
			return ErrInvalid
		}
		if c.SecretKey == "" {
			s.log.Warn("captcha secret key is not set", "provider", c.Provider)
			return ErrUnavailable
		}
		return s.siteverify(ctx, c, a.Value, remoteIP)
	default:
		return fmt.Errorf("unknown captcha provider %q", c.Provider)
	}
}

// siteverify implements the protocol Turnstile, reCAPTCHA and hCaptcha share: a
// form POST answered with {"success": bool, "error-codes": [...]}.
func (s *Service) siteverify(ctx context.Context, c Config, token, remoteIP string) error {
	form := url.Values{"secret": {c.SecretKey}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	if c.Provider == HCaptcha && c.SiteKey != "" {
		form.Set("sitekey", c.SiteKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoints[c.Provider], strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.client.Do(req)
	if err != nil {
		s.log.Warn("captcha siteverify failed", "provider", c.Provider, "err", err)
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		s.log.Warn("captcha siteverify failed", "provider", c.Provider, "status", res.Status)
		return ErrUnavailable
	}
	var out struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out); err != nil {
		s.log.Warn("captcha siteverify returned an unexpected body", "provider", c.Provider, "err", err)
		return ErrUnavailable
	}
	if !out.Success {
		// Misconfiguration on our side is not the visitor's fault: tell the admin.
		for _, code := range out.ErrorCodes {
			if strings.Contains(code, "secret") || strings.Contains(code, "sitekey") {
				s.log.Warn("captcha keys are rejected by the provider", "provider", c.Provider, "codes", out.ErrorCodes)
				return ErrUnavailable
			}
		}
		return ErrInvalid
	}
	return nil
}
