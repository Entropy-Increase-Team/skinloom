package oauth

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

const (
	dcrLimitPerHour = 15
	dcrWindow       = time.Hour
)

var validURISchemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]{0,63}$`)

type windowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	hits    map[string][]time.Time
}

func newWindowLimiter(limit int, window time.Duration, maxKeys int) *windowLimiter {
	return &windowLimiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		hits:    make(map[string][]time.Time),
	}
}

func (l *windowLimiter) allow(key string, now time.Time) bool {
	if key == "" {
		key = "unknown"
	}
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = make(map[string][]time.Time)
	}
	hits := l.hits[key]
	kept := hits[:0]
	for _, ts := range hits {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	if _, exists := l.hits[key]; !exists && len(l.hits) >= l.maxKeys {
		// Evict expired keys first; if still at capacity under a distinct-key flood,
		// evict the oldest entries so memory stays strictly bounded.
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
		if len(l.hits) >= l.maxKeys {
			var oldestKey string
			var oldestTS time.Time
			for k, v := range l.hits {
				last := v[len(v)-1]
				if oldestKey == "" || last.Before(oldestTS) {
					oldestKey = k
					oldestTS = last
				}
			}
			if oldestKey != "" {
				delete(l.hits, oldestKey)
			}
		}
	}
	l.hits[key] = append(kept, now)
	return true
}

func validOptionalPort(u *url.URL) bool {
	p := u.Port()
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}

// DCRRequest is a subset of RFC 7591 Dynamic Client Registration metadata.
type DCRRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// DCRResponse is returned by POST /oauth/register (RFC 7591 §3.2.1).
type DCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// checkDCRRedirects validates redirect_uris for dynamically registered native clients:
// only loopback HTTP (http://127.0.0.1, http://localhost, http://[::1]) or custom
// non-web native application URI schemes are allowed, preventing open web redirect abuse.
func checkDCRRedirects(list []string) ([]string, *Error) {
	bad := func(msg string) *Error {
		return &Error{Status: http.StatusBadRequest, Code: "invalid_redirect_uri", Description: msg}
	}
	if len(list) == 0 {
		return nil, bad("At least one redirect_uri is required.")
	}
	out := make([]string, 0, len(list))
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || len(raw) > 2048 {
			return nil, bad("Not an absolute URI: " + raw)
		}
		if u.Fragment != "" || strings.Contains(raw, "#") {
			return nil, bad("Redirect URI must not contain a fragment.")
		}
		if u.User != nil {
			return nil, bad("Redirect URI must not contain userinfo.")
		}
		scheme := strings.ToLower(u.Scheme)
		if !validURISchemeRe.MatchString(scheme) {
			return nil, bad("Invalid URI scheme: " + raw)
		}
		switch scheme {
		case "http":
			if u.Opaque != "" || !isLoopbackHost(u.Hostname()) || !validOptionalPort(u) {
				return nil, bad("Dynamic clients may only use loopback HTTP redirect URIs (127.0.0.1, localhost, [::1]) or custom native app schemes.")
			}
		case "https", "javascript", "data", "vbscript", "file", "about", "blob", "mailto", "tel",
			"ftp", "ftps", "ws", "wss", "smb", "ssh", "view-source", "chrome", "edge", "intent", "ms-msdt", "search-ms":
			return nil, bad("Scheme not permitted for dynamic public clients: " + scheme)
		default:
			// Custom native app URI scheme (RFC 8252 §7.1): require at least one character after scheme.
			if u.Opaque == "" && u.Host == "" && u.Path == "" {
				return nil, bad("Incomplete custom redirect URI: " + raw)
			}
		}
		if !contains(out, raw) {
			out = append(out, raw)
		}
	}
	if len(out) == 0 {
		return nil, bad("At least one redirect_uri is required.")
	}
	if len(out) > MaxRedirectURIs {
		return nil, bad("Too many redirect_uris.")
	}
	return out, nil
}

func (s *Service) allowDCR(clientIP string) bool {
	if s.dcrLimiter == nil {
		s.dcrLimiter = newWindowLimiter(dcrLimitPerHour, dcrWindow, 4096)
	}
	return s.dcrLimiter.allow(clientIP, s.now())
}

// AllowDeviceRequest applies per-IP rate limiting to POST /oauth/device_authorization.
func (s *Service) AllowDeviceRequest(clientIP string) bool {
	if s.devReqLimiter == nil {
		s.devReqLimiter = newWindowLimiter(20, 10*time.Minute, 4096)
	}
	return s.devReqLimiter.allow(clientIP, s.now())
}

// AllowDeviceLookup applies per-user/IP rate limiting to GET/POST /api/v1/oauth/device.
func (s *Service) AllowDeviceLookup(key string) bool {
	if s.devUsrLimiter == nil {
		s.devUsrLimiter = newWindowLimiter(30, 10*time.Minute, 4096)
	}
	return s.devUsrLimiter.allow(key, s.now())
}

// RegisterDynamicClient implements restricted RFC 7591 registration for public PKCE
// native launchers (e.g. HMCL / PCL2 / Prism).
func (s *Service) RegisterDynamicClient(ctx context.Context, in DCRRequest, clientIP string, p Policy) (*DCRResponse, error) {
	if !p.EnableOIDC || !p.EnableDCR {
		return nil, &Error{Status: http.StatusForbidden, Code: "registration_disabled", Description: "Dynamic client registration is disabled on this site."}
	}
	if !s.allowDCR(clientIP) {
		return nil, &Error{Status: http.StatusTooManyRequests, Code: "slow_down", Description: "Too many dynamic client registrations from this address; try again later."}
	}
	authMethod := strings.TrimSpace(in.TokenEndpointAuthMethod)
	if authMethod != "" && authMethod != "none" {
		return nil, &Error{Status: http.StatusBadRequest, Code: "invalid_client_metadata", Description: "Dynamic registration only supports public PKCE clients (token_endpoint_auth_method=none)."}
	}
	redirects, rerr := checkDCRRedirects(in.RedirectURIs)
	if rerr != nil {
		return nil, rerr
	}
	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		name = "Minecraft Launcher"
	}
	if utf8.RuneCountInString(name) > 64 {
		runes := []rune(name)
		name = string(runes[:64])
	}

	// Attach the FK to an active site administrator so SQLite schema requires no table
	// rebuild while Dynamic=true hides it from personal apps and decouples lifecycle.
	owner, err := s.db.User.Query().
		Where(user.RoleEQ(user.RoleSuperadmin), user.StatusEQ(user.StatusActive)).
		Order(ent.Asc(user.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		owner, err = s.db.User.Query().
			Where(user.RoleIn(user.RoleSuperadmin, user.RoleAdmin), user.StatusEQ(user.StatusActive)).
			Order(ent.Asc(user.FieldID)).First(ctx)
	}
	if err != nil {
		return nil, &Error{Status: http.StatusServiceUnavailable, Code: "server_error", Description: "Site setup is not complete."}
	}

	now := s.now()
	row, err := s.db.OAuthClient.Create().
		SetUserID(owner.ID).
		SetName(name).
		SetRedirectUris(redirects).
		SetDynamic(true).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return &DCRResponse{
		ClientID:                strconv.FormatInt(row.ID, 10),
		ClientName:              row.Name,
		RedirectURIs:            row.RedirectUris,
		GrantTypes:              []string{"authorization_code", "refresh_token", DeviceGrantType},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		ClientIDIssuedAt:        now.Unix(),
	}, nil
}
