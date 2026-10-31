// Package cdn tells a CDN to drop its copies of a player's profile and avatar when the
// player changes (docs/design/02 F72), so game clients behind the CDN see a new skin
// without waiting for delivery.profile_cache_seconds. Texture files need no purging:
// their URL is their content hash.
package cdn

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Providers.
const (
	None       = "none"
	Cloudflare = "cloudflare"
	Aliyun     = "aliyun"
	Tencent    = "tencent"
	Webhook    = "webhook"
)

// Purger drops cached copies of URLs.
type Purger interface {
	Purge(ctx context.Context, urls []string) error
}

// SettingDefs are the cdn.* settings. Keys and tokens are secrets.
func SettingDefs() []settings.Def {
	str := func(key, doc string) settings.Def {
		return settings.Def{Key: key, Type: settings.TypeString, Default: "", Doc: doc}
	}
	secret := func(key, doc string) settings.Def {
		return settings.Def{Key: key, Type: settings.TypeString, Default: "", Secret: true, Doc: doc}
	}
	return []settings.Def{
		{
			Key: "cdn.provider", Type: settings.TypeString, Default: None,
			Enum: []any{None, Cloudflare, Aliyun, Tencent, Webhook},
			Doc:  "CDN whose cached player profiles and avatars are purged when a player changes: none, cloudflare, aliyun (Alibaba Cloud CDN), tencent (Tencent Cloud CDN) or webhook (POST the URLs to cdn.webhook.url).",
		},
		{
			Key: "cdn.base_url", Type: settings.TypeString, Default: "", Validate: validURL,
			Doc: "Address the CDN serves the site at, e.g. https://skin.example.com; empty = site.url.",
		},
		str("cdn.cloudflare.zone_id", "Cloudflare zone ID (the domain's overview page)."),
		secret("cdn.cloudflare.api_token", "Cloudflare API token with the Zone → Cache Purge permission."),
		str("cdn.aliyun.access_key_id", "Alibaba Cloud AccessKey ID of a RAM user allowed to call cdn:RefreshObjectCaches."),
		secret("cdn.aliyun.access_key_secret", "Alibaba Cloud AccessKey secret."),
		str("cdn.tencent.secret_id", "Tencent Cloud SecretId of a sub-account allowed to call cdn:PurgeUrlsCache."),
		secret("cdn.tencent.secret_key", "Tencent Cloud SecretKey."),
		{
			Key: "cdn.webhook.url", Type: settings.TypeString, Default: "", Validate: validURL,
			Doc: `URL that receives POST {"urls": [...], "event": "..."}.`,
		},
		secret("cdn.webhook.secret", "Signs webhook bodies: X-SkinLoom-Signature: sha256=<hex HMAC-SHA256 of the body>; empty = unsigned."),
	}
}

func validURL(v any) error {
	s := v.(string)
	if s == "" {
		return nil
	}
	if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("want an http(s) URL like https://skin.example.com")
	}
	return nil
}

// Config is the CDN setup in a settings snapshot.
type Config struct {
	Provider string
	BaseURL  string // without a trailing slash

	CloudflareZone, CloudflareToken string
	AliyunKeyID, AliyunKeySecret    string
	TencentID, TencentKey           string
	WebhookURL, WebhookSecret       string
}

// ConfigFrom reads the CDN setup; BaseURL falls back to site.url.
func ConfigFrom(s *settings.Snapshot) Config {
	base := s.String("cdn.base_url")
	if base == "" {
		base = s.String("site.url")
	}
	return Config{
		Provider: s.String("cdn.provider"), BaseURL: strings.TrimRight(base, "/"),
		CloudflareZone: s.String("cdn.cloudflare.zone_id"), CloudflareToken: s.String("cdn.cloudflare.api_token"),
		AliyunKeyID: s.String("cdn.aliyun.access_key_id"), AliyunKeySecret: s.String("cdn.aliyun.access_key_secret"),
		TencentID: s.String("cdn.tencent.secret_id"), TencentKey: s.String("cdn.tencent.secret_key"),
		WebhookURL: s.String("cdn.webhook.url"), WebhookSecret: s.String("cdn.webhook.secret"),
	}
}

// ErrIncomplete: a provider is chosen but a value it needs is missing.
var ErrIncomplete = errors.New("the CDN settings are incomplete")

// Purger builds the provider; nil, nil when purging is off.
func (c Config) Purger(client *http.Client) (Purger, error) {
	if c.Provider == None || c.Provider == "" {
		return nil, nil
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("%w: set cdn.base_url or site.url", ErrIncomplete)
	}
	var (
		p    Purger
		need []string
	)
	switch c.Provider {
	case Cloudflare:
		p, need = &cloudflare{client: client, api: cloudflareAPI, zone: c.CloudflareZone, token: c.CloudflareToken},
			[]string{c.CloudflareZone, c.CloudflareToken}
	case Aliyun:
		p, need = &aliyun{client: client, endpoint: aliyunEndpoint, keyID: c.AliyunKeyID, secret: c.AliyunKeySecret, now: time.Now},
			[]string{c.AliyunKeyID, c.AliyunKeySecret}
	case Tencent:
		p, need = &tencent{client: client, endpoint: tencentEndpoint, id: c.TencentID, key: c.TencentKey, now: time.Now},
			[]string{c.TencentID, c.TencentKey}
	case Webhook:
		p, need = &webhook{client: client, url: c.WebhookURL, secret: c.WebhookSecret}, []string{c.WebhookURL}
	default:
		return nil, fmt.Errorf("unknown CDN provider %q", c.Provider)
	}
	for _, v := range need {
		if v == "" {
			return nil, fmt.Errorf("%w for %s", ErrIncomplete, c.Provider)
		}
	}
	return p, nil
}

// ProfileURLs are the cached URLs that show what player name wears: the profile APIs
// of every game client and the player's head. Variants with a query string (avatar
// sizes) are not listed; they expire after delivery.profile_cache_seconds.
func ProfileURLs(base, name string) []string {
	n := url.PathEscape(name)
	return []string{
		base + "/" + n + ".json",
		base + "/csl/" + n + ".json",
		base + "/usm/" + n + ".json",
		base + "/skin/" + n + ".png",
		base + "/cape/" + n + ".png",
		base + "/avatar/player/" + n,
	}
}

// chunks splits urls into lists of at most n (providers cap one request).
func chunks(urls []string, n int) [][]string {
	var out [][]string
	for len(urls) > n {
		out = append(out, urls[:n])
		urls = urls[n:]
	}
	if len(urls) > 0 {
		out = append(out, urls)
	}
	return out
}
