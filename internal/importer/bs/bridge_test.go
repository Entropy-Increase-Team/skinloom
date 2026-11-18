package bs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeTestPNG(t *testing.T, w, h int) ([]byte, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestValidateBridgeURL(t *testing.T) {
	if _, err := ValidateBridgeURL("http://skins.example.com", false); err == nil {
		t.Fatal("expected rejection for http:// without allowInsecureLocal")
	}
	if _, err := ValidateBridgeURL("http://skins.example.com", true); err == nil {
		t.Fatal("expected rejection for public hostname over http:// even when allowInsecureLocal is true")
	}
	if u, err := ValidateBridgeURL("http://192.168.1.25:8080/bs/", true); err != nil || u.String() != "http://192.168.1.25:8080/bs" {
		t.Fatalf("expected private LAN URL acceptance, got u=%v err=%v", u, err)
	}
	if u, err := ValidateBridgeURL("http://blessing-skin:8080/plugin/skinloom-bridge/manifest", true); err != nil || u.String() != "http://blessing-skin:8080" {
		t.Fatalf("expected Docker service name and suffix strip acceptance, got u=%v err=%v", u, err)
	}
	if u, err := ValidateBridgeURL("https://skins.example.com/sub/plugin/skinloom-bridge/", false); err != nil || u.String() != "https://skins.example.com/sub" {
		t.Fatalf("expected https URL with stripped bridge path acceptance, got u=%v err=%v", u, err)
	}
}

func TestBridgeImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	pngBytes, texHash := makeTestPNG(t, 64, 64)
	const wantToken = "secret-bridge-token-123"

	mux := http.NewServeMux()
	authOK := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/plugin/skinloom-bridge/manifest", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"pluginVersion":  "1.0.0",
			"prefix":         "bs_",
			"timezone":       "UTC",
			"passwordMethod": "BCRYPT",
			"uuidTable":      false,
			"missingTables":  []string{"reports", "notifications", "language_lines", "oauth_clients"},
			"otherTables":    []map[string]any{{"name": "custom_plugin", "rows": 2}},
			"options": map[string]string{
				"site_name": "BridgeBS",
			},
			"mail": map[string]any{
				"mailer":      "smtp",
				"host":        "smtp.bridge.example.com",
				"port":        "465",
				"username":    "noreply@bridge.example.com",
				"password":    "smtp-secret",
				"encryption":  "ssl",
				"fromAddress": "noreply@bridge.example.com",
				"fromName":    "BridgeBS",
				"hostSet":     true,
			},
			"counts": map[string]any{
				"users":    2,
				"players":  1,
				"textures": 1,
				"closet":   1,
			},
		})
	})
	mux.HandleFunc("/plugin/skinloom-bridge/rows", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(w, r) {
			return
		}
		table := r.URL.Query().Get("table")
		after := r.URL.Query().Get("after")
		switch table {
		case "users":
			if after == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"table": "users",
					"rows": []map[string]any{{
						"id": 1, "email": "admin@example.com", "nickname": "Admin",
						"password": "$2y$10$abcdefghijklmnopqrstuv", "permission": 2, "verified": true,
					}},
					"nextCursor": "1",
					"hasMore":    true,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table": "users",
				"rows": []map[string]any{{
					"id": 2, "email": "user@example.com", "nickname": "User",
					"password": "$2y$10$abcdefghijklmnopqrstuv", "permission": 0, "verified": true,
				}},
				"nextCursor": "",
				"hasMore":    false,
			})
		case "players":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table":   "players",
				"rows":    []map[string]any{{"id": 1, "userId": 2, "name": "Steve", "skin": 1, "cape": 0}},
				"hasMore": false,
			})
		case "textures":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table":   "textures",
				"rows":    []map[string]any{{"id": 1, "name": "Skin1", "type": "steve", "hash": texHash, "sizeKB": 1, "uploader": 2, "public": true}},
				"hasMore": false,
			})
		case "user_closet":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"table":   "user_closet",
				"rows":    []map[string]any{{"userId": 2, "textureId": 1, "name": "FavSkin"}},
				"hasMore": false,
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"table": table, "rows": []any{}, "hasMore": false})
		}
	})
	mux.HandleFunc("/plugin/skinloom-bridge/textures/"+texHash, func(w http.ResponseWriter, r *http.Request) {
		if !authOK(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Texture-Sha256", texHash)
		w.Header().Set("X-Texture-Width", "64")
		w.Header().Set("X-Texture-Height", "64")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(pngBytes)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewBridgeClient(BridgeConfig{
		URL:                srv.URL,
		Token:              wantToken,
		AllowInsecureLocal: true,
		HTTPClient:         srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewBridgeClient: %v", err)
	}
	m, err := client.Manifest(ctx)
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if m.Counts.Users != 2 || len(m.OtherTables) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	src, err := client.ResolveSource(m, "")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if src.Mail == nil || !src.Mail.HostSet || src.Mail.Host != "smtp.bridge.example.com" || src.Mail.Password != "smtp-secret" {
		t.Fatalf("expected mail resolved from bridge manifest, got %+v", src.Mail)
	}

	reader := &BridgeSourceReader{Client: client, Manifest: m}
	data, err := reader.Read(ctx)
	if err != nil {
		t.Fatalf("BridgeSourceReader.Read: %v", err)
	}
	if len(data.Users) != 2 || len(data.Players) != 1 || len(data.Textures) != 1 || len(data.Closet) != 1 {
		t.Fatalf("unexpected read counts: users=%d players=%d textures=%d closet=%d",
			len(data.Users), len(data.Players), len(data.Textures), len(data.Closet))
	}

	cacheDir := t.TempDir()
	fetcher := &BridgeFileFetcher{Client: client, CacheDir: cacheDir}
	infos := fetcher.Inspect(ctx, []string{texHash, strings.Repeat("f", 64)}, nil)
	if infos[texHash].Problem != "" || infos[texHash].Width != 64 || infos[texHash].Hash != texHash {
		t.Fatalf("unexpected inspected texture info: %+v", infos[texHash])
	}
	if infos[strings.Repeat("f", 64)].Problem == "" {
		t.Fatal("expected problem on 404 texture")
	}
	// Fetch should hit local cacheDir seamlessly without network error even if server closes.
	srv.Close()
	gotBytes, err := fetcher.Fetch(ctx, texHash)
	if err != nil || !bytes.Equal(gotBytes, pngBytes) {
		t.Fatalf("cached Fetch failed: err=%v len=%d", err, len(gotBytes))
	}
	_ = src
}

func TestBridgeRetriesIntegrityAndProtocolVersion(t *testing.T) {
	ctx := context.Background()
	pngBytes, texHash := makeTestPNG(t, 64, 32)
	const tok = "retry-secret"

	// 1. JSON connection bundle parsing.
	if u, parsedTok, ok := ParseBridgeCredentials(`{"url":"https://old.example.com","token":"abc"}`); !ok || u != "https://old.example.com" || parsedTok != "abc" {
		t.Fatalf("ParseBridgeCredentials failed: ok=%v u=%q tok=%q", ok, u, parsedTok)
	}

	// 2. Reject future unsupported protocolVersion cleanly.
	futureSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"protocolVersion": BridgeProtocolVersion + 1})
	}))
	defer futureSrv.Close()
	fc, _ := NewBridgeClient(BridgeConfig{URL: futureSrv.URL, Token: tok, AllowInsecureLocal: true, HTTPClient: futureSrv.Client()})
	if _, err := fc.Manifest(ctx); err == nil || !strings.Contains(err.Error(), "upgrade SkinLoom") {
		t.Fatalf("expected upgrade hint for newer protocolVersion, got %v", err)
	}

	// 3. Retry transient 503 on manifest & reject mismatched wire X-Texture-Sha256.
	var manifestCalls int
	retrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugin/skinloom-bridge/manifest":
			manifestCalls++
			if manifestCalls == 1 {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"protocolVersion": 1,
				"counts":          map[string]any{"users": 1},
			})
		case "/plugin/skinloom-bridge/textures/" + texHash:
			w.Header().Set("X-Texture-Sha256", strings.Repeat("0", 64))
			_, _ = w.Write(pngBytes)
		}
	}))
	defer retrySrv.Close()

	rc, _ := NewBridgeClient(BridgeConfig{URL: retrySrv.URL, Token: tok, AllowInsecureLocal: true, HTTPClient: retrySrv.Client()})
	m, err := rc.Manifest(ctx)
	if err != nil || manifestCalls != 2 || m.Counts.Users != 1 {
		t.Fatalf("expected retry success on 2nd attempt: calls=%d err=%v m=%+v", manifestCalls, err, m)
	}
	fetcher := &BridgeFileFetcher{Client: rc}
	infos := fetcher.Inspect(ctx, []string{texHash}, nil)
	if !strings.Contains(infos[texHash].Problem, "checksum mismatch") {
		t.Fatalf("expected checksum mismatch refusal, got %+v", infos[texHash])
	}
}
