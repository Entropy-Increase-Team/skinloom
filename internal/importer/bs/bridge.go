package bs

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// BridgeProtocolVersion is the highest read-only export bridge wire version this
// importer supports. SkinLoom v2 migration plugins will continue consuming v1
// manifests/rows/extensions while evolving optional capabilities additively.
const BridgeProtocolVersion = 1

const (
	bridgeMaxAttempts = 3
	bridgeMaxJSONBody = 32 << 20
)

// BridgeConfig connects SkinLoom to a Blessing Skin site running the standalone
// blessing-skin-export-bridge plugin.
type BridgeConfig struct {
	URL                string
	Token              string
	AllowInsecureLocal bool
	Timezone           string
	HTTPClient         *http.Client
}

// BridgeMail carries optional mail configuration exported by the bridge manifest.
type BridgeMail struct {
	Mailer      string `json:"mailer"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Encryption  string `json:"encryption"`
	FromAddress string `json:"fromAddress"`
	FromName    string `json:"fromName"`
	HostSet     bool   `json:"hostSet"`
}

// BridgeManifest matches GET /plugin/skinloom-bridge/manifest.
type BridgeManifest struct {
	ProtocolVersion int                        `json:"protocolVersion"`
	PluginVersion   string                     `json:"pluginVersion"`
	Capabilities    []string                   `json:"capabilities,omitempty"`
	BSVersion       string                     `json:"bsVersion"`
	Prefix          string                     `json:"prefix"`
	Timezone        string                     `json:"timezone"`
	FSDriver        string                     `json:"fsDriver"`
	WriteFrozen     bool                       `json:"writeFrozen,omitempty"`
	PasswordMethod  string                     `json:"passwordMethod"`
	Salt            string                     `json:"salt"`
	Transition      []Method                   `json:"transition"`
	Mail            *BridgeMail                `json:"mail,omitempty"`
	UUIDTable       bool                       `json:"uuidTable"`
	MissingTables   []string                   `json:"missingTables"`
	OtherTables     []BridgeTableInfo          `json:"otherTables"`
	Extensions      map[string]json.RawMessage `json:"extensions,omitempty"`
	Options         map[string]string          `json:"options"`
	Counts          SourceSummary              `json:"counts"`
}

// BridgeTableInfo describes an unimported BS table reported by the bridge manifest.
type BridgeTableInfo struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// ParseBridgeCredentials extracts URL and token when a user pastes a JSON bundle
// exported by blessing-skin-export-bridge (e.g. {"url":"...","token":"..."}).
func ParseBridgeCredentials(raw string) (bridgeURL, token string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var bundle struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal([]byte(trimmed), &bundle); err == nil && strings.TrimSpace(bundle.URL) != "" {
			return strings.TrimSpace(bundle.URL), strings.TrimSpace(bundle.Token), true
		}
	}
	return "", "", false
}

// ValidateBridgeURL enforces HTTPS for non-private bridge hosts and rejects userinfo.
func ValidateBridgeURL(rawURL string, allowInsecureLocal bool) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("bridge URL is required")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid bridge URL: %w", err)
	}
	if u.User != nil {
		return nil, errors.New("bridge URL must not contain embedded credentials")
	}
	if u.Hostname() == "" {
		return nil, errors.New("bridge URL must include a host")
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "https":
		// Always allowed.
	case "http":
		if !allowInsecureLocal {
			return nil, errors.New("bridge URL must use https:// (enable trusted LAN HTTP only on private networks)")
		}
		if !isPrivateBridgeHost(u.Hostname()) {
			return nil, errors.New("http:// is only allowed for localhost or private network IPs; public domains require https://")
		}
	default:
		return nil, fmt.Errorf("unsupported bridge scheme %q (want https or http)", u.Scheme)
	}
	u.Scheme = scheme
	u.Path = strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/plugin/skinloom-bridge/manifest", "/plugin/skinloom-bridge"} {
		if strings.HasSuffix(u.Path, suffix) {
			u.Path = strings.TrimRight(strings.TrimSuffix(u.Path, suffix), "/")
			break
		}
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func isPrivateBridgeHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
	}
	// Allow Docker / Kubernetes single-label service names and RFC 6762 / 8375 internal domains.
	if !strings.Contains(h, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".internal", ".lan", ".home.arpa", ".localdomain", ".cluster.local", ".svc"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

// BridgeClient talks to the read-only blessing-skin-export-bridge endpoints.
type BridgeClient struct {
	base  *url.URL
	token string
	http  *http.Client
	allow bool
}

// NewBridgeClient validates cfg and prepares a TLS-hardened, pooled HTTP client.
func NewBridgeClient(cfg BridgeConfig) (*BridgeClient, error) {
	rawURL := strings.TrimSpace(cfg.URL)
	rawToken := strings.TrimSpace(cfg.Token)
	if parsedURL, parsedTok, ok := ParseBridgeCredentials(rawURL); ok {
		rawURL = parsedURL
		if rawToken == "" {
			rawToken = parsedTok
		}
	}

	base, err := ValidateBridgeURL(rawURL, cfg.AllowInsecureLocal)
	if err != nil {
		return nil, err
	}
	if rawToken == "" {
		return nil, errors.New("bridge Bearer token is required")
	}

	client := cfg.HTTPClient
	if client == nil {
		transport := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          32,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
		client = &http.Client{
			Timeout:   90 * time.Second,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if _, err := ValidateBridgeURL(req.URL.String(), cfg.AllowInsecureLocal); err != nil {
					return fmt.Errorf("unsafe redirect blocked: %w", err)
				}
				return nil
			},
		}
	}
	return &BridgeClient{
		base:  base,
		token: rawToken,
		http:  client,
		allow: cfg.AllowInsecureLocal,
	}, nil
}

func (c *BridgeClient) endpoint(path string, q url.Values) string {
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + path
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

type bridgeResponse struct {
	status        int
	body          []byte
	headers       http.Header
	contentLength int64
}

func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
			return min(time.Duration(secs)*time.Second, 5*time.Second)
		}
	}
	switch attempt {
	case 1:
		return 200 * time.Millisecond
	case 2:
		return 600 * time.Millisecond
	default:
		return 1400 * time.Millisecond
	}
}

func (c *BridgeClient) doWithRetry(ctx context.Context, method, fullURL, accept string, limit int64) (*bridgeResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= bridgeMaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("User-Agent", "SkinLoom-Importer/1")
		req.Header.Set("X-SkinLoom-Bridge-Client", strconv.Itoa(BridgeProtocolVersion))
		if accept != "" {
			req.Header.Set("Accept", accept)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil || attempt == bridgeMaxAttempts {
				return nil, lastErr
			}
			if waitErr := sleepCtx(ctx, retryDelay(attempt, "")); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		var body []byte
		var readErr error
		if method != http.MethodHead {
			body, readErr = io.ReadAll(io.LimitReader(resp.Body, limit+1))
		}
		resp.Body.Close()

		if readErr != nil {
			lastErr = readErr
			if attempt == bridgeMaxAttempts {
				return nil, lastErr
			}
			if waitErr := sleepCtx(ctx, retryDelay(attempt, "")); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if method != http.MethodHead && int64(len(body)) > limit {
			return nil, fmt.Errorf("bridge response exceeds %d bytes limit on %s", limit, req.URL.Path)
		}

		if method != http.MethodHead && resp.ContentLength >= 0 && int64(len(body)) != resp.ContentLength {
			lastErr = fmt.Errorf("truncated response on %s: got %d bytes, expected %d", req.URL.Path, len(body), resp.ContentLength)
			if attempt == bridgeMaxAttempts {
				return nil, lastErr
			}
			if waitErr := sleepCtx(ctx, retryDelay(attempt, "")); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if isRetryableStatus(resp.StatusCode) && attempt < bridgeMaxAttempts {
			lastErr = fmt.Errorf("bridge HTTP %d on %s", resp.StatusCode, req.URL.Path)
			if waitErr := sleepCtx(ctx, retryDelay(attempt, resp.Header.Get("Retry-After"))); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		return &bridgeResponse{
			status:        resp.StatusCode,
			body:          body,
			headers:       resp.Header,
			contentLength: resp.ContentLength,
		}, nil
	}
	return nil, lastErr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *BridgeClient) doJSON(ctx context.Context, method, fullURL string, dst any) error {
	res, err := c.doWithRetry(ctx, method, fullURL, "application/json", bridgeMaxJSONBody)
	if err != nil {
		return err
	}
	if res.status != http.StatusOK {
		msg := strings.TrimSpace(string(res.body))
		switch res.status {
		case http.StatusUnauthorized:
			return fmt.Errorf("bridge authentication failed (HTTP 401): token missing, invalid, or expired: %s", msg)
		case http.StatusForbidden:
			return fmt.Errorf("bridge access forbidden (HTTP 403): check IP allowlist on Blessing Skin: %s", msg)
		default:
			return fmt.Errorf("bridge HTTP %d: %s", res.status, msg)
		}
	}
	if dst != nil {
		if err := json.Unmarshal(res.body, dst); err != nil {
			return fmt.Errorf("decode bridge JSON: %w", err)
		}
	}
	return nil
}

// Manifest fetches the bridge manifest metadata and validates protocol compatibility.
func (c *BridgeClient) Manifest(ctx context.Context) (*BridgeManifest, error) {
	var m BridgeManifest
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/plugin/skinloom-bridge/manifest", nil), &m); err != nil {
		return nil, err
	}
	if m.ProtocolVersion > BridgeProtocolVersion {
		return nil, fmt.Errorf("bridge plugin uses newer protocolVersion=%d (this SkinLoom supports <= %d); upgrade SkinLoom", m.ProtocolVersion, BridgeProtocolVersion)
	}
	m.Counts.HasYggdrasilKey = strings.TrimSpace(m.Options["ygg_private_key"]) != ""
	return &m, nil
}

// ResolveSource builds a bs.Source carrying password rules, mail and timezone from m.
func (c *BridgeClient) ResolveSource(m *BridgeManifest, tzOverride string) (Source, error) {
	tz := cmp.Or(strings.TrimSpace(tzOverride), strings.TrimSpace(m.Timezone), "Asia/Shanghai")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Source{}, fmt.Errorf("timezone %q: %w", tz, err)
	}
	var mailCfg *Mail
	if m.Mail != nil {
		mailCfg = &Mail{
			Mailer:      strings.ToLower(cmp.Or(strings.TrimSpace(m.Mail.Mailer), "smtp")),
			Host:        strings.TrimSpace(m.Mail.Host),
			Port:        cmp.Or(strings.TrimSpace(m.Mail.Port), "587"),
			Username:    strings.TrimSpace(m.Mail.Username),
			Password:    m.Mail.Password,
			Encryption:  strings.ToLower(strings.TrimSpace(m.Mail.Encryption)),
			FromAddress: strings.TrimSpace(m.Mail.FromAddress),
			FromName:    strings.TrimSpace(m.Mail.FromName),
			HostSet:     m.Mail.HostSet && strings.TrimSpace(m.Mail.Host) != "",
		}
	}
	return Source{
		Driver:         "bridge",
		Prefix:         m.Prefix,
		PasswordMethod: cmp.Or(strings.TrimSpace(m.PasswordMethod), "BCRYPT"),
		Salt:           m.Salt,
		Transition:     m.Transition,
		Location:       loc,
		Mail:           mailCfg,
	}, nil
}

// BridgeSourceReader implements SourceReader over the bridge HTTP endpoints.
type BridgeSourceReader struct {
	Client   *BridgeClient
	Manifest *BridgeManifest
}

type bridgeRowsResp struct {
	Table      string            `json:"table"`
	Rows       []json.RawMessage `json:"rows"`
	NextCursor string            `json:"nextCursor"`
	HasMore    bool              `json:"hasMore"`
}

func (r *BridgeSourceReader) Read(ctx context.Context) (*sourceData, error) {
	m := r.Manifest
	if m == nil {
		var err error
		m, err = r.Client.Manifest(ctx)
		if err != nil {
			return nil, err
		}
		r.Manifest = m
	}

	d := &sourceData{
		Options:    m.Options,
		UUIDTable:  m.UUIDTable,
		Prefix:     m.Prefix,
		Extensions: m.Extensions,
	}
	if d.Options == nil {
		d.Options = map[string]string{}
	}
	missingSet := map[string]bool{}
	for _, t := range m.MissingTables {
		missingSet[t] = true
		d.Missing = append(d.Missing, m.Prefix+t)
	}
	for _, ot := range m.OtherTables {
		d.OtherTables = append(d.OtherTables, srcTable(ot))
	}

	if err := r.eachPage(ctx, "users", func(raw json.RawMessage) error {
		var row struct {
			ID           int64  `json:"id"`
			Email        string `json:"email"`
			Nickname     string `json:"nickname"`
			Password     string `json:"password"`
			IP           string `json:"ip"`
			Locale       string `json:"locale"`
			Score        int64  `json:"score"`
			Avatar       int64  `json:"avatar"`
			Permission   int64  `json:"permission"`
			Verified     bool   `json:"verified"`
			LastSignAt   string `json:"lastSignAt"`
			RegisteredAt string `json:"registeredAt"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		var lastSign, registered wall
		_ = lastSign.Scan(row.LastSignAt)
		_ = registered.Scan(row.RegisteredAt)
		d.Users = append(d.Users, srcUser{
			ID:           row.ID,
			Email:        row.Email,
			Nickname:     row.Nickname,
			Password:     row.Password,
			IP:           row.IP,
			Locale:       row.Locale,
			Score:        row.Score,
			Avatar:       row.Avatar,
			Permission:   row.Permission,
			Verified:     row.Verified,
			LastSignAt:   lastSign,
			RegisteredAt: registered,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	if err := r.eachPage(ctx, "players", func(raw json.RawMessage) error {
		var row struct {
			ID         int64  `json:"id"`
			UserID     int64  `json:"userId"`
			Name       string `json:"name"`
			Skin       int64  `json:"skin"`
			Cape       int64  `json:"cape"`
			ModifiedAt string `json:"modifiedAt"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		var mod wall
		_ = mod.Scan(row.ModifiedAt)
		d.Players = append(d.Players, srcPlayer{
			ID:         row.ID,
			UserID:     row.UserID,
			Name:       row.Name,
			Skin:       row.Skin,
			Cape:       row.Cape,
			ModifiedAt: mod,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	if err := r.eachPage(ctx, "textures", func(raw json.RawMessage) error {
		var row struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			Hash       string `json:"hash"`
			SizeKB     int64  `json:"sizeKB"`
			Uploader   int64  `json:"uploader"`
			Public     bool   `json:"public"`
			UploadedAt string `json:"uploadedAt"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		var up wall
		_ = up.Scan(row.UploadedAt)
		d.Textures = append(d.Textures, srcTexture{
			ID:         row.ID,
			SizeKB:     row.SizeKB,
			Uploader:   row.Uploader,
			Name:       row.Name,
			Type:       row.Type,
			Hash:       row.Hash,
			Public:     row.Public,
			UploadedAt: up,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	if !missingSet["user_closet"] {
		if err := r.eachPage(ctx, "user_closet", func(raw json.RawMessage) error {
			var row struct {
				UserID    int64  `json:"userId"`
				TextureID int64  `json:"textureId"`
				Name      string `json:"name"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			d.Closet = append(d.Closet, srcCloset{
				UserID:    row.UserID,
				TextureID: row.TextureID,
				Name:      row.Name,
			})
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if !missingSet["reports"] {
		if err := r.eachPage(ctx, "reports", func(raw json.RawMessage) error {
			var row struct {
				ID         int64  `json:"id"`
				TextureID  int64  `json:"textureId"`
				Uploader   int64  `json:"uploader"`
				Reporter   int64  `json:"reporter"`
				Status     int64  `json:"status"`
				Reason     string `json:"reason"`
				ReportedAt string `json:"reportedAt"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			var rep wall
			_ = rep.Scan(row.ReportedAt)
			d.Reports = append(d.Reports, srcReport{
				ID:         row.ID,
				TextureID:  row.TextureID,
				Uploader:   row.Uploader,
				Reporter:   row.Reporter,
				Status:     row.Status,
				Reason:     row.Reason,
				ReportedAt: rep,
			})
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if !missingSet["notifications"] {
		if err := r.eachPage(ctx, "notifications", func(raw json.RawMessage) error {
			var row struct {
				NotifiableType string `json:"notifiableType"`
				NotifiableID   int64  `json:"notifiableId"`
				Data           string `json:"data"`
				CreatedAt      string `json:"createdAt"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			if !strings.HasSuffix(row.NotifiableType, "User") {
				return nil
			}
			var created wall
			_ = created.Scan(row.CreatedAt)
			d.Notifications = append(d.Notifications, srcNotification{
				UserID:    row.NotifiableID,
				Data:      row.Data,
				CreatedAt: created,
			})
			return nil
		}); err != nil {
			return nil, err
		}
		// Bridge paginates notifications by UUID primary key; sort stably by CreatedAt to match SQL read.
		slices.SortStableFunc(d.Notifications, func(a, b srcNotification) int {
			if a.CreatedAt.Valid != b.CreatedAt.Valid {
				if !a.CreatedAt.Valid {
					return -1
				}
				return 1
			}
			if a.CreatedAt.t.Before(b.CreatedAt.t) {
				return -1
			}
			if a.CreatedAt.t.After(b.CreatedAt.t) {
				return 1
			}
			return 0
		})
	}

	if d.UUIDTable {
		if err := r.eachPage(ctx, "uuid", func(raw json.RawMessage) error {
			var row struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				UUID string `json:"uuid"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			d.UUIDs = append(d.UUIDs, srcUUID{ID: row.ID, Name: row.Name, UUID: row.UUID})
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if !missingSet["language_lines"] {
		if err := r.eachPage(ctx, "language_lines", func(raw json.RawMessage) error {
			var row struct {
				ID    int64  `json:"id"`
				Group string `json:"group"`
				Key   string `json:"key"`
				Text  string `json:"text"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			d.Lines = append(d.Lines, srcLine{ID: row.ID, Group: row.Group, Key: row.Key, Text: row.Text})
			return nil
		}); err != nil {
			return nil, err
		}
	}

	if !missingSet["oauth_clients"] {
		if err := r.eachPage(ctx, "oauth_clients", func(raw json.RawMessage) error {
			var row struct {
				ID        int64  `json:"id"`
				UserID    int64  `json:"userId"`
				Name      string `json:"name"`
				Secret    string `json:"secret"`
				Redirect  string `json:"redirect"`
				Personal  bool   `json:"personal"`
				Password  bool   `json:"password"`
				Revoked   bool   `json:"revoked"`
				CreatedAt string `json:"createdAt"`
			}
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			var created wall
			_ = created.Scan(row.CreatedAt)
			d.Clients = append(d.Clients, srcClient{
				ID:        row.ID,
				UserID:    row.UserID,
				Name:      row.Name,
				Secret:    row.Secret,
				Redirect:  row.Redirect,
				Personal:  row.Personal,
				Password:  row.Password,
				Revoked:   row.Revoked,
				CreatedAt: created,
			})
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (r *BridgeSourceReader) eachPage(ctx context.Context, table string, fn func(json.RawMessage) error) error {
	after := ""
	for {
		q := url.Values{"table": {table}, "limit": {"500"}}
		if after != "" {
			q.Set("after", after)
		}
		var page bridgeRowsResp
		if err := r.Client.doJSON(ctx, http.MethodGet, r.Client.endpoint("/plugin/skinloom-bridge/rows", q), &page); err != nil {
			return fmt.Errorf("bridge read %s: %w", table, err)
		}
		for _, raw := range page.Rows {
			if err := fn(raw); err != nil {
				return fmt.Errorf("bridge decode %s: %w", table, err)
			}
		}
		if !page.HasMore || len(page.Rows) == 0 {
			return nil
		}
		if page.NextCursor == "" || page.NextCursor == after {
			return fmt.Errorf("bridge pagination stalled on %s cursor %q", table, page.NextCursor)
		}
		after = page.NextCursor
	}
}

// BridgeFileFetcher inspects and fetches texture blobs from the Blessing Skin bridge.
// When CacheDir is set, Inspect downloads valid textures once into CacheDir so Fetch
// avoids repeating network transfers during live import.
type BridgeFileFetcher struct {
	Client      *BridgeClient
	CacheDir    string
	FastInspect bool // dry-run: prefer HEAD headers before falling back to GET
}

func (f *BridgeFileFetcher) Inspect(ctx context.Context, hashes []string, progress func(done, total int)) map[string]fileInfo {
	out := make(map[string]fileInfo, len(hashes))
	if len(hashes) == 0 {
		return out
	}
	var (
		mu       sync.Mutex
		doneCnt  atomic.Int64
		lastTick atomic.Int64
	)
	total := len(hashes)
	lastTick.Store(time.Now().UnixNano())

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for _, h := range hashes {
		hash := h
		g.Go(func() error {
			info := f.inspectOne(gctx, hash)
			mu.Lock()
			out[hash] = info
			mu.Unlock()

			n := int(doneCnt.Add(1))
			if progress != nil {
				now := time.Now().UnixNano()
				prev := lastTick.Load()
				if n == total || n%50 == 0 || (now-prev > int64(200*time.Millisecond) && lastTick.CompareAndSwap(prev, now)) {
					progress(n, total)
				}
			}
			return nil
		})
	}
	_ = g.Wait()
	return out
}

func (f *BridgeFileFetcher) inspectOne(ctx context.Context, hash string) fileInfo {
	if !storage.ValidKey(hash) {
		return fileInfo{Problem: fmt.Sprintf("invalid hash %q", hash)}
	}
	if f.FastInspect {
		if info, handled := f.headOne(ctx, hash); handled {
			return info
		}
	}
	data, headerSha, problem := f.downloadOne(ctx, hash)
	if problem != "" {
		return fileInfo{Problem: problem}
	}
	sum := sha256.Sum256(data)
	actualHash := hex.EncodeToString(sum[:])
	if headerSha != "" && actualHash != headerSha {
		return fileInfo{Problem: fmt.Sprintf("wire checksum mismatch (got %s, header %s)", actualHash, headerSha)}
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return fileInfo{Problem: "not a valid PNG image"}
	}
	if f.CacheDir != "" {
		_ = os.WriteFile(filepath.Join(f.CacheDir, hash), data, 0o600)
	}
	return fileInfo{
		Hash:   actualHash,
		Size:   int64(len(data)),
		Width:  cfg.Width,
		Height: cfg.Height,
	}
}

func (f *BridgeFileFetcher) headOne(ctx context.Context, hash string) (fileInfo, bool) {
	res, err := f.Client.doWithRetry(ctx, http.MethodHead, f.Client.endpoint("/plugin/skinloom-bridge/textures/"+hash, nil), "", 0)
	if err != nil {
		return fileInfo{Problem: err.Error()}, true
	}
	if res.status == http.StatusNotFound {
		return fileInfo{Problem: "missing from Blessing Skin"}, true
	}
	if res.status == http.StatusMethodNotAllowed || res.status != http.StatusOK {
		return fileInfo{}, false
	}
	w, errW := strconv.Atoi(res.headers.Get("X-Texture-Width"))
	h, errH := strconv.Atoi(res.headers.Get("X-Texture-Height"))
	if errW != nil || errH != nil || w <= 0 || h <= 0 || res.contentLength <= 0 {
		return fileInfo{}, false
	}
	if res.contentLength > maxFileSize {
		return fileInfo{Problem: fmt.Sprintf("exceeds %d bytes", maxFileSize)}, true
	}
	sha := strings.ToLower(strings.TrimSpace(res.headers.Get("X-Texture-Sha256")))
	if !storage.ValidKey(sha) {
		sha = hash
	}
	return fileInfo{
		Hash:   sha,
		Size:   res.contentLength,
		Width:  w,
		Height: h,
	}, true
}

func (f *BridgeFileFetcher) downloadOne(ctx context.Context, hash string) ([]byte, string, string) {
	res, err := f.Client.doWithRetry(ctx, http.MethodGet, f.Client.endpoint("/plugin/skinloom-bridge/textures/"+hash, nil), "application/octet-stream", maxFileSize)
	if err != nil {
		return nil, "", err.Error()
	}
	if res.status == http.StatusInternalServerError {
		// Fall back to Blessing Skin's native /textures/{hash} route if an older bridge plugin build failed to stream.
		if fb, ferr := f.Client.doWithRetry(ctx, http.MethodGet, f.Client.endpoint("/textures/"+hash, nil), "image/png, application/octet-stream", maxFileSize); ferr == nil && fb.status == http.StatusOK {
			res = fb
		}
	}
	if res.status == http.StatusNotFound {
		return nil, "", "missing from Blessing Skin"
	}
	if res.status != http.StatusOK {
		return nil, "", fmt.Sprintf("bridge returned HTTP %d", res.status)
	}
	headerSha := strings.ToLower(strings.TrimSpace(res.headers.Get("X-Texture-Sha256")))
	if headerSha != "" && !storage.ValidKey(headerSha) {
		return nil, "", "invalid X-Texture-Sha256 header"
	}
	return res.body, headerSha, ""
}

func (f *BridgeFileFetcher) Fetch(ctx context.Context, hash string) ([]byte, error) {
	if !storage.ValidKey(hash) {
		return nil, fmt.Errorf("invalid hash %q", hash)
	}
	if f.CacheDir != "" {
		if data, err := os.ReadFile(filepath.Join(f.CacheDir, hash)); err == nil {
			return data, nil
		}
	}
	data, headerSha, problem := f.downloadOne(ctx, hash)
	if problem != "" {
		return nil, errors.New(problem)
	}
	if headerSha != "" {
		sum := sha256.Sum256(data)
		if actual := hex.EncodeToString(sum[:]); actual != headerSha {
			return nil, fmt.Errorf("wire checksum mismatch for %s", hash)
		}
	}
	return data, nil
}

func (f *BridgeFileFetcher) SharedWith(_ string) bool {
	return false
}
