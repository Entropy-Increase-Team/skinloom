package cdn

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	cloudflareAPI   = "https://api.cloudflare.com/client/v4"
	aliyunEndpoint  = "https://cdn.aliyuncs.com/"
	tencentEndpoint = "https://cdn.tencentcloudapi.com/"
)

// Per-request URL caps of the providers' purge APIs.
const (
	cloudflareBatch = 30 // the lowest plan's limit
	aliyunBatch     = 100
	tencentBatch    = 100
)

// readError returns the response body (clipped) for an error message.
func readError(res *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return strings.TrimSpace(string(b))
}

// cloudflare purges by URL: POST /zones/{zone}/purge_cache {"files": [...]}.
type cloudflare struct {
	client      *http.Client
	api         string
	zone, token string
}

func (c *cloudflare) Purge(ctx context.Context, urls []string) error {
	for _, batch := range chunks(urls, cloudflareBatch) {
		body, _ := json.Marshal(map[string][]string{"files": batch})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.api+"/zones/"+url.PathEscape(c.zone)+"/purge_cache", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.client.Do(req)
		if err != nil {
			return fmt.Errorf("cloudflare: %w", err)
		}
		var out struct {
			Success bool `json:"success"`
			Errors  []struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
		res.Body.Close()
		if err != nil || !out.Success {
			return fmt.Errorf("cloudflare: HTTP %d %v", res.StatusCode, out.Errors)
		}
	}
	return nil
}

// aliyun calls Alibaba Cloud CDN's RefreshObjectCaches, signed with RPC signature v1
// (HMAC-SHA1 over the sorted, percent-encoded query).
type aliyun struct {
	client        *http.Client
	endpoint      string
	keyID, secret string
	now           func() time.Time
}

func (a *aliyun) Purge(ctx context.Context, urls []string) error {
	for _, batch := range chunks(urls, aliyunBatch) {
		q := map[string]string{
			"Action": "RefreshObjectCaches", "ObjectPath": strings.Join(batch, "\n"), "ObjectType": "File",
			"Format": "JSON", "Version": "2018-05-10", "AccessKeyId": a.keyID,
			"SignatureMethod": "HMAC-SHA1", "SignatureVersion": "1.0", "SignatureNonce": nonce(),
			"Timestamp": a.now().UTC().Format("2006-01-02T15:04:05Z"),
		}
		query := aliyunQuery(q)
		query += "&Signature=" + aliyunEncode(aliyunSign(http.MethodGet, query, a.secret))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint+"?"+query, nil)
		if err != nil {
			return err
		}
		res, err := a.client.Do(req)
		if err != nil {
			return fmt.Errorf("aliyun: %w", err)
		}
		if res.StatusCode != http.StatusOK {
			msg := readError(res)
			res.Body.Close()
			return fmt.Errorf("aliyun: HTTP %d %s", res.StatusCode, msg)
		}
		res.Body.Close()
	}
	return nil
}

// aliyunEncode is the percent-encoding of the signature: RFC 3986, spaces as %20.
func aliyunEncode(s string) string {
	return strings.NewReplacer("+", "%20", "*", "%2A", "%7E", "~").Replace(url.QueryEscape(s))
}

// aliyunQuery is the canonicalized query: parameters sorted by name, each encoded.
func aliyunQuery(q map[string]string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = aliyunEncode(k) + "=" + aliyunEncode(q[k])
	}
	return strings.Join(parts, "&")
}

// aliyunSign signs a canonicalized query: base64(HMAC-SHA1(secret&, METHOD&%2F&encode(query))).
func aliyunSign(method, query, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(method + "&" + aliyunEncode("/") + "&" + aliyunEncode(query)))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// tencent calls Tencent Cloud CDN's PurgeUrlsCache, signed with TC3-HMAC-SHA256.
type tencent struct {
	client   *http.Client
	endpoint string
	id, key  string
	now      func() time.Time
}

func (t *tencent) Purge(ctx context.Context, urls []string) error {
	host := "cdn.tencentcloudapi.com"
	for _, batch := range chunks(urls, tencentBatch) {
		body, _ := json.Marshal(map[string][]string{"Urls": batch})
		now := t.now()
		const contentType = "application/json; charset=utf-8"
		auth := tc3Authorization(t.id, t.key, "cdn", host, "PurgeUrlsCache", contentType, body, now)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Host = host
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("X-TC-Action", "PurgeUrlsCache")
		req.Header.Set("X-TC-Version", "2018-06-06")
		req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
		res, err := t.client.Do(req)
		if err != nil {
			return fmt.Errorf("tencent: %w", err)
		}
		var out struct {
			Response struct {
				Error *struct{ Code, Message string } `json:"Error"`
			} `json:"Response"`
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
		res.Body.Close()
		switch {
		case err != nil || res.StatusCode != http.StatusOK:
			return fmt.Errorf("tencent: HTTP %d", res.StatusCode)
		case out.Response.Error != nil:
			return fmt.Errorf("tencent: %s %s", out.Response.Error.Code, out.Response.Error.Message)
		}
	}
	return nil
}

// tc3Authorization is the Authorization header of a POST / with a JSON body, signing
// the content-type, host and x-tc-action headers (Tencent Cloud API 3.0 signature v3,
// in the form of its documentation's example).
func tc3Authorization(id, key, service, host, action, contentType string, body []byte, now time.Time) string {
	date := now.UTC().Format("2006-01-02")
	scope := date + "/" + service + "/tc3_request"
	toSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(now.Unix(), 10) + "\n" + scope + "\n" +
		sha256Hex([]byte(tc3Canonical(host, action, contentType, body)))
	signing := hmacSHA256(hmacSHA256(hmacSHA256([]byte("TC3"+key), date), service), "tc3_request")
	return "TC3-HMAC-SHA256 Credential=" + id + "/" + scope + ", SignedHeaders=content-type;host;x-tc-action, Signature=" +
		hex.EncodeToString(hmacSHA256(signing, toSign))
}

// tc3Canonical is the canonical request: header names and values in lower case.
func tc3Canonical(host, action, contentType string, body []byte) string {
	return "POST\n/\n\ncontent-type:" + strings.ToLower(contentType) + "\nhost:" + host + "\nx-tc-action:" + strings.ToLower(action) +
		"\n\ncontent-type;host;x-tc-action\n" + sha256Hex(body)
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

// webhook POSTs {"urls", "event"} for CDNs without a built-in provider.
type webhook struct {
	client      *http.Client
	url, secret string
}

func (w *webhook) Purge(ctx context.Context, urls []string) error {
	body, _ := json.Marshal(map[string]any{"urls": urls, "event": eventName(ctx)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.secret != "" {
		m := hmac.New(sha256.New, []byte(w.secret))
		m.Write(body)
		req.Header.Set("X-SkinLoom-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	res, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		// Not the body: the admin who sets the URL would read any internal endpoint's answer.
		return fmt.Errorf("webhook: HTTP %d", res.StatusCode)
	}
	return nil
}

func nonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type eventKey struct{}

// withEvent tells the webhook which event a purge is for.
func withEvent(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, eventKey{}, name)
}

func eventName(ctx context.Context) string {
	s, _ := ctx.Value(eventKey{}).(string)
	return s
}
