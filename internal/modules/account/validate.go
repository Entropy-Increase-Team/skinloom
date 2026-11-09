package account

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
)

var (
	// ErrInvalidCredentials is the only answer to a failed login, whether the account
	// exists or not (S05).
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrAccountDisabled is returned only after the correct password was given.
	ErrAccountDisabled = errors.New("account is disabled")
	ErrSetupDone       = errors.New("setup has already been completed")
	ErrInvalidToken    = errors.New("invalid, used or expired token")
	// ErrCaptchaRequired means this login attempt must come with a solved captcha.
	ErrCaptchaRequired    = errors.New("captcha required")
	ErrRegistrationClosed = errors.New("registration is closed")
	// ErrSetupPending refuses registration before the first administrator exists.
	ErrSetupPending = errors.New("the site has not been set up yet")
	// ErrMailDisabled means the action needs mail, and no mail driver is configured.
	ErrMailDisabled    = errors.New("mail is not configured on this site")
	ErrAlreadyVerified = errors.New("the email address is already verified")
)

// FieldError rejects one input field; the API reports it as a validation error.
type FieldError = validate.FieldError

// RateLimitError means too many failed attempts; retry after RetryAfter.
type RateLimitError = validate.RateLimitError

// FieldErrors returns every FieldError in err (validation joins one per bad field).
func FieldErrors(err error) []*FieldError { return validate.FieldErrors(err) }

// NormalizeEmail is the uniqueness key: trimmed and lower-cased (BS issue #581 made
// addresses case-sensitive). Provider-specific rules such as Gmail dots are not applied.
func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// ValidateEmail accepts a bare address (no display name) of at most 255 bytes.
func ValidateEmail(s string) error {
	s = strings.TrimSpace(s)
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || len(s) > 255 || !strings.Contains(s[strings.LastIndexByte(s, '@'):], ".") {
		return &FieldError{Field: "email", Message: "not a valid email address"}
	}
	return nil
}

// MaxNicknameLen is counted in characters.
const MaxNicknameLen = 32

// ValidateNickname allows 1–32 printable characters after trimming.
func ValidateNickname(s string) error {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n == 0 || n > MaxNicknameLen {
		return &FieldError{Field: "nickname", Message: fmt.Sprintf("must be 1 to %d characters", MaxNicknameLen)}
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return &FieldError{Field: "nickname", Message: "contains invisible or control characters"}
		}
	}
	return nil
}

// tokenLen is the length of a token as sent to clients: 32 random bytes, base64url.
const tokenLen = 43

// NewToken returns a random secret for a cookie or link. Only HashToken(token) is stored.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is the database key of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func wellFormedToken(token string) bool {
	if len(token) != tokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}
