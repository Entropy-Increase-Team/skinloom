package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultFetchTimeout      = 10 * time.Second
	DefaultFetchMaxRespBytes = 5 * 1024 * 1024 // 5 MiB
	maxFetchRedirects        = 5
)

var (
	// ErrFetchDenied indicates an outbound HTTP call violated http.allow or SSRF protection.
	ErrFetchDenied = errors.New("outbound fetch denied")
	// ErrFetchTooLarge indicates an outbound HTTP response exceeded 5 MiB.
	ErrFetchTooLarge = errors.New("outbound fetch response exceeds 5 MiB limit")
)

// FetchRequest models JS ctx.fetch(url, init) options.
type FetchRequest struct {
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Timeout int               `json:"timeoutMs,omitempty"`
}

// FetchResponse is returned back into the JS sandbox.
type FetchResponse struct {
	OK         bool              `json:"ok"`
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	URL        string            `json:"url"`
	BodyText   string            `json:"bodyText"`
	BodyBase64 string            `json:"bodyBase64"`
}

// Fetcher executes SSRF-hardened outbound HTTP requests for one plugin.
type Fetcher struct {
	allowHosts  []string
	perms       *PermissionSet
	client      *http.Client
	dialTimeout time.Duration

	// Test hook to override DNS resolution or loopback blocking only in unit tests.
	lookupIPAddr func(ctx context.Context, host string) ([]net.IPAddr, error)
	allowPrivate bool
}

// NewFetcher creates an SSRF-safe outbound fetcher scoped to allowHosts.
func NewFetcher(allowHosts []string, perms *PermissionSet) *Fetcher {
	f := &Fetcher{
		allowHosts:  allowHosts,
		perms:       perms,
		dialTimeout: DefaultFetchTimeout,
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:               nil, // Ignore host HTTP(S)_PROXY env vars to prevent SSRF bypasses.
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return f.safeDial(ctx, dialer, network, addr)
		},
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   DefaultFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFetchRedirects {
				return fmt.Errorf("%w: too many redirects", ErrFetchDenied)
			}
			if err := f.validateURL(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
	return f
}

func (f *Fetcher) validateURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("%w: empty URL", ErrFetchDenied)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: scheme %q not allowed", ErrFetchDenied, u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrFetchDenied)
	}
	if !f.hostAllowed(host) {
		return fmt.Errorf("%w: host %q is not listed in manifest http.allow", ErrFetchDenied, host)
	}
	return nil
}

func (f *Fetcher) hostAllowed(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, pat := range f.allowHosts {
		p := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(pat)), ".")
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "*.") {
			suffix := p[1:] // ".example.com"
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		} else if host == p {
			return true
		}
	}
	return false
}

func (f *Fetcher) safeDial(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid address %q", ErrFetchDenied, addr)
	}
	if !f.allowPrivate && isForbiddenHostname(host) {
		return nil, fmt.Errorf("%w: forbidden host %q", ErrFetchDenied, host)
	}

	var ips []net.IP
	if parsed := net.ParseIP(host); parsed != nil {
		ips = []net.IP{parsed}
	} else {
		lookup := net.DefaultResolver.LookupIPAddr
		if f.lookupIPAddr != nil {
			lookup = f.lookupIPAddr
		}
		addrs, err := lookup(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("%w: DNS lookup failed: %v", ErrFetchDenied, err)
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: no IP addresses resolved for %q", ErrFetchDenied, host)
	}
	for _, ip := range ips {
		if !f.allowPrivate && isBlockedIP(ip) {
			return nil, fmt.Errorf("%w: resolved IP %s (%s) is a blocked private/internal address", ErrFetchDenied, ip.String(), host)
		}
	}

	// Dial each verified IP directly so DNS rebinding between lookup and connect cannot occur.
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func isForbiddenHostname(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	return h == "localhost" || strings.HasSuffix(h, ".localhost") ||
		h == "metadata.google.internal" || strings.HasSuffix(h, ".internal")
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	if addr.Is4() {
		b := addr.As4()
		// 0.0.0.0/8 ("this network")
		if b[0] == 0 {
			return true
		}
		// 100.64.0.0/10 CGNAT shared address space
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
		// 169.254.0.0/16 cloud metadata / link-local
		if b[0] == 169 && b[1] == 254 {
			return true
		}
		// 198.18.0.0/15 benchmarking
		if b[0] == 198 && (b[1] == 18 || b[1] == 19) {
			return true
		}
		// 240.0.0.0/4 reserved
		if b[0] >= 240 {
			return true
		}
	}
	return false
}

// Fetch performs a guarded outbound HTTP call subject to http.fetch, allowlist,
// SSRF IP filtering, timeout, and 5 MiB response cap.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string, opts FetchRequest) (*FetchResponse, error) {
	if err := f.perms.Check(PermHTTPFetch); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid URL: %v", ErrFetchDenied, err)
	}
	if err := f.validateURL(parsed); err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = http.MethodGet
	}

	timeout := DefaultFetchTimeout
	if opts.Timeout > 0 && time.Duration(opts.Timeout)*time.Millisecond < timeout {
		timeout = time.Duration(opts.Timeout) * time.Millisecond
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if opts.Body != "" {
		bodyReader = strings.NewReader(opts.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, parsed.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range opts.Headers {
		if strings.EqualFold(k, "host") {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, DefaultFetchMaxRespBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(rawBody)) > DefaultFetchMaxRespBytes {
		return nil, ErrFetchTooLarge
	}

	headers := make(map[string]string, len(resp.Header))
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			headers[strings.ToLower(k)] = vals[0]
		}
	}

	return &FetchResponse{
		OK:         resp.StatusCode >= 200 && resp.StatusCode < 300,
		Status:     resp.StatusCode,
		StatusText: http.StatusText(resp.StatusCode),
		Headers:    headers,
		URL:        resp.Request.URL.String(),
		BodyText:   string(rawBody),
		BodyBase64: base64.StdEncoding.EncodeToString(rawBody),
	}, nil
}
