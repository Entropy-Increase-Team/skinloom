package cdn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The worked example of Alibaba Cloud's RPC signature documentation (ECS DescribeRegions).
func TestAliyunSignatureExample(t *testing.T) {
	q := aliyunQuery(map[string]string{
		"AccessKeyId": "testid", "Action": "DescribeRegions", "Format": "XML",
		"SignatureMethod": "HMAC-SHA1", "SignatureNonce": "3ee8c1b8-83d3-44af-a94f-4e0ad82fd6cf",
		"SignatureVersion": "1.0", "Timestamp": "2016-02-23T12:46:24Z", "Version": "2014-05-26",
	})
	if got := aliyunSign(http.MethodGet, q, "testsecret"); got != "OLeaidS1JvxuMvnyHOwuJ+uX5qY=" {
		t.Fatalf("signature %s", got)
	}
}

// The worked example of Tencent Cloud's signature v3 documentation (CVM DescribeInstances,
// cloud.tencent.com/document/api/228/30978). Its secret key is masked, but every value
// derived from it is printed, so each step after the first HMAC is checked.
func TestTencentSignatureExample(t *testing.T) {
	body := []byte(`{"Limit": 1, "Filters": [{"Values": ["\u672a\u547d\u540d"], "Name": "instance-name"}]}`)
	if got := sha256Hex(body); got != "35e9c5b0e3ae67532d3c9f17ead6c90222632e5b1ff7f6e89887f1398934f064" {
		t.Fatalf("payload hash %s", got)
	}
	canonical := tc3Canonical("cvm.tencentcloudapi.com", "DescribeInstances", "application/json; charset=utf-8", body)
	if got := sha256Hex([]byte(canonical)); got != "7019a55be8395899b900fb5564e4200d984910f34794a27cb3fb7d10ff6a1e84" {
		t.Fatalf("canonical request hash %s of\n%s", got, canonical)
	}
	unhex := func(s string) []byte { b, _ := hex.DecodeString(s); return b }
	date := unhex("da98fb70dcf6b112dc21038d1eeeb3a95c74b4dcb12c1131f864f6066bd02be0")
	if got := hex.EncodeToString(hmacSHA256(date, "cvm")); got != "8d70cbefb03939f929db64d32dc2ba89b1095620119fe3e050e2b18c5bd2752f" {
		t.Fatalf("SecretService %s", got)
	}
	signing := hmacSHA256(hmacSHA256(date, "cvm"), "tc3_request")
	if got := hex.EncodeToString(signing); got != "b596b923aad85185e2d1f6659d2a062e0a86731226e021e61bfe06f7ed05f5af" {
		t.Fatalf("SecretSigning %s", got)
	}
	toSign := "TC3-HMAC-SHA256\n1551113065\n2019-02-25/cvm/tc3_request\n7019a55be8395899b900fb5564e4200d984910f34794a27cb3fb7d10ff6a1e84"
	if got := hex.EncodeToString(hmacSHA256(signing, toSign)); got != "10b1a37a7301a02ca19a647ad722d5e43b4b3cff309d421d85b46093f6ab6c4f" {
		t.Fatalf("signature %s", got)
	}
	auth := tc3Authorization("AKIDEXAMPLE", "key", "cvm", "cvm.tencentcloudapi.com", "DescribeInstances", "application/json; charset=utf-8", body, time.Unix(1551113065, 0))
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKIDEXAMPLE/2019-02-25/cvm/tc3_request, SignedHeaders=content-type;host;x-tc-action, Signature=") {
		t.Fatalf("header %s", auth)
	}
}

func TestProfileURLs(t *testing.T) {
	got := ProfileURLs("https://skin.example.com", "史蒂夫 x")
	if len(got) != 6 || got[0] != "https://skin.example.com/%E5%8F%B2%E8%92%82%E5%A4%AB%20x.json" || got[5] != "https://skin.example.com/avatar/player/%E5%8F%B2%E8%92%82%E5%A4%AB%20x" {
		t.Fatalf("%v", got)
	}
}

func urls(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "https://skin.example.com/p" + strings.Repeat("x", i) + ".json"
	}
	return out
}

func TestCloudflare(t *testing.T) {
	var batches [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/zone-1/purge_cache" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`)
			return
		}
		var in struct{ Files []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		batches = append(batches, in.Files)
		_, _ = io.WriteString(w, `{"success":true,"errors":[]}`)
	}))
	defer srv.Close()
	c := &cloudflare{client: srv.Client(), api: srv.URL, zone: "zone-1", token: "tok"}
	if err := c.Purge(context.Background(), urls(31)); err != nil || len(batches) != 2 || len(batches[1]) != 1 {
		t.Fatalf("%v %d", err, len(batches))
	}
	c.token = "wrong"
	if err := c.Purge(context.Background(), urls(1)); err == nil || !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("refused: %v", err)
	}
}

func TestAliyun(t *testing.T) {
	now := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		params := map[string]string{}
		for k := range q {
			if k != "Signature" {
				params[k] = q.Get(k)
			}
		}
		if aliyunSign(http.MethodGet, aliyunQuery(params), "sec") != q.Get("Signature") || q.Get("Action") != "RefreshObjectCaches" ||
			q.Get("Timestamp") != "2026-10-31T08:00:00Z" || len(strings.Split(q.Get("ObjectPath"), "\n")) != 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"Code":"SignatureDoesNotMatch"}`)
			return
		}
		_, _ = io.WriteString(w, `{"RefreshTaskId":"1"}`)
	}))
	defer srv.Close()
	a := &aliyun{client: srv.Client(), endpoint: srv.URL + "/", keyID: "id", secret: "sec", now: func() time.Time { return now }}
	if err := a.Purge(context.Background(), urls(2)); err != nil {
		t.Fatal(err)
	}
	a.secret = "other"
	if err := a.Purge(context.Background(), urls(2)); err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("refused: %v", err)
	}
}

func TestTencent(t *testing.T) {
	now := time.Unix(1793000000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		want := tc3Authorization("sid", "skey", "cdn", "cdn.tencentcloudapi.com", r.Header.Get("X-TC-Action"), r.Header.Get("Content-Type"), body, now)
		if r.Host != "cdn.tencentcloudapi.com" || r.Header.Get("Authorization") != want || r.Header.Get("X-TC-Action") != "PurgeUrlsCache" {
			_, _ = io.WriteString(w, `{"Response":{"Error":{"Code":"AuthFailure.SignatureFailure","Message":"bad"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"Response":{"TaskId":"t","RequestId":"r"}}`)
	}))
	defer srv.Close()
	tc := &tencent{client: srv.Client(), endpoint: srv.URL, id: "sid", key: "skey", now: func() time.Time { return now }}
	if err := tc.Purge(context.Background(), urls(3)); err != nil {
		t.Fatal(err)
	}
	tc.key = "other"
	if err := tc.Purge(context.Background(), urls(1)); err == nil || !strings.Contains(err.Error(), "SignatureFailure") {
		t.Fatalf("refused: %v", err)
	}
}

func TestWebhook(t *testing.T) {
	var got struct {
		URLs  []string `json:"urls"`
		Event string   `json:"event"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m := hmac.New(sha256.New, []byte("s3cret"))
		m.Write(body)
		if r.Header.Get("X-SkinLoom-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.Unmarshal(body, &got)
	}))
	defer srv.Close()
	wh := &webhook{client: srv.Client(), url: srv.URL, secret: "s3cret"}
	if err := wh.Purge(withEvent(context.Background(), "player.updated"), urls(2)); err != nil || len(got.URLs) != 2 || got.Event != "player.updated" {
		t.Fatalf("%v %+v", err, got)
	}
	wh.secret = "wrong"
	if err := wh.Purge(context.Background(), urls(1)); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("refused: %v", err)
	}
}
