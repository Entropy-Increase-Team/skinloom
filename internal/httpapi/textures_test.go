package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int, seed byte) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x) + seed, G: byte(y), B: seed, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// upload posts the multipart upload form as browser b.
func upload(t *testing.T, b *browser, name, typ string, public bool, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return uploadAs(t, b, "image/png", name, typ, public, data)
}

// uploadAs is upload with the file part declared as partType.
func uploadAs(t *testing.T, b *browser, partType, name, typ string, public bool, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="file"; filename="skin.png"`},
		"Content-Type":        {partType},
	})
	part.Write(data)
	_ = mw.WriteField("name", name)
	_ = mw.WriteField("type", typ)
	_ = mw.WriteField("public", fmt.Sprint(public))
	mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/textures", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(CSRFHeader, "1")
	if b.cookie != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: b.cookie})
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, b *browser, path string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	if b != nil && b.cookie != "" {
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: b.cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTextureEndpoints(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	guest := &browser{t: t, h: h}
	alice := registerUser(t, h, "alice@example.com")

	var rules TextureRules
	_ = json.Unmarshal(get(t, h, nil, "/api/v1/textures/rules").Body.Bytes(), &rules)
	if rules.MaxSizeKB != 1024 || rules.Rates.PrivatePerStorage != 10 || !rules.AllowDownload {
		t.Fatalf("rules %+v", rules)
	}

	img := testPNG(t, 64, 64, 1)
	if rec := upload(t, guest, "x", "steve", true, img); rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", rec.Code)
	}
	if rec := upload(t, alice, "x", "steve", true, []byte("not a png")); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "body.file") {
		t.Fatalf("not a PNG: %d %s", rec.Code, rec.Body)
	}
	rec := upload(t, alice, "My skin", "steve", true, img)
	var up UploadView
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	if rec.Code != http.StatusOK || up.Texture.ID == 0 || up.Duplicate || up.Cost < 1 || up.Score != 1000-up.Cost || up.Texture.Uploader == nil {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	pub := up.Texture

	setSettings(t, d, `{"score.private_per_storage": 100000}`)
	if rec := upload(t, alice, "Too dear", "steve", false, testPNG(t, 64, 64, 2)); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("not enough score: %d %s", rec.Code, rec.Body)
	}
	setSettings(t, d, `{"score.private_per_storage": 1}`)
	rec = upload(t, alice, "Secret", "alex", false, testPNG(t, 64, 64, 3))
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	priv := up.Texture
	if rec.Code != http.StatusOK || priv.Public {
		t.Fatalf("private upload: %d %s", rec.Code, rec.Body)
	}

	// Library and detail pages.
	var page TexturePage
	_ = json.Unmarshal(get(t, h, guest, "/api/v1/textures").Body.Bytes(), &page)
	if len(page.Textures) != 1 || page.Textures[0].ID != pub.ID {
		t.Fatalf("guests see public textures: %+v", page)
	}
	_ = json.Unmarshal(get(t, h, alice, "/api/v1/textures?category=skin&sort=likes").Body.Bytes(), &page)
	if len(page.Textures) != 1 {
		t.Fatalf("the library is public textures, also for their uploader: %+v", page)
	}
	mine := fmt.Sprintf("/api/v1/textures?uploader=%d", pub.Uploader.ID)
	_ = json.Unmarshal(get(t, h, alice, mine).Body.Bytes(), &page)
	if len(page.Textures) != 2 {
		t.Fatalf("your own uploads include the private ones: %+v", page)
	}
	_ = json.Unmarshal(get(t, h, guest, mine).Body.Bytes(), &page)
	if len(page.Textures) != 1 {
		t.Fatalf("someone else's uploads are only the public ones: %+v", page)
	}
	if rec := get(t, h, nil, "/api/v1/textures?sort=email"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("sort outside the whitelist (S07): %d", rec.Code)
	}
	var detail TextureDetail
	_ = json.Unmarshal(get(t, h, alice, fmt.Sprintf("/api/v1/textures/%d", pub.ID)).Body.Bytes(), &detail)
	if !detail.Mine || !detail.InCloset || detail.Name != "My skin" {
		t.Fatalf("detail for the uploader: %+v", detail)
	}
	if rec := get(t, h, guest, fmt.Sprintf("/api/v1/textures/%d", priv.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("private texture, default status: %d", rec.Code)
	}
	setSettings(t, d, `{"texture.private_status": 404}`)
	if rec := get(t, h, guest, fmt.Sprintf("/api/v1/textures/%d", priv.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("private texture, hidden: %d", rec.Code)
	}
	if rec := get(t, h, guest, "/api/v1/textures/999999"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing texture: %d", rec.Code)
	}

	var closet ClosetPage
	_ = json.Unmarshal(get(t, h, alice, "/api/v1/me/closet?category=skin").Body.Bytes(), &closet)
	if len(closet.Items) != 2 || closet.Items[0].Texture.ID != priv.ID {
		t.Fatalf("closet: %+v", closet)
	}
	if rec := get(t, h, guest, "/api/v1/me/closet"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("closet signed out: %d", rec.Code)
	}

	// Files by hash: immutable, conditional requests.
	for _, prefix := range []string{"/textures/", "/csl/textures/", "/usm/textures/"} {
		rec := get(t, h, nil, prefix+pub.Hash)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" ||
			!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") || rec.Header().Get("ETag") != `"`+pub.Hash+`"` {
			t.Fatalf("%s: %d %v", prefix, rec.Code, rec.Header())
		}
		if _, err := png.Decode(rec.Body); err != nil {
			t.Fatalf("%s: %v", prefix, err)
		}
	}
	if rec := get(t, h, nil, "/textures/"+pub.Hash, "If-None-Match", `"`+pub.Hash+`"`); rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", rec.Code)
	}
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest("HEAD", "/textures/"+pub.Hash, nil))
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD: %d, %d bytes, %v", head.Code, head.Body.Len(), head.Header())
	}
	for _, bad := range []string{strings.Repeat("0", 64), "..%2Fconfig.toml", "abc"} {
		if rec := get(t, h, nil, "/textures/"+bad); rec.Code != http.StatusNotFound {
			t.Fatalf("/textures/%s: %d", bad, rec.Code)
		}
	}

	// Originals by id: visibility and texture.allow_download.
	rec = get(t, h, nil, fmt.Sprintf("/raw/%d.png", pub.ID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), "My skin.png") {
		t.Fatalf("raw: %d %v", rec.Code, rec.Header())
	}
	if rec := get(t, h, nil, fmt.Sprintf("/raw/%d", priv.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("raw private for a guest: %d", rec.Code)
	}
	if rec := get(t, h, alice, fmt.Sprintf("/raw/%d", priv.ID)); rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "private") {
		t.Fatalf("raw private for the uploader: %d %v", rec.Code, rec.Header())
	}
	setSettings(t, d, `{"texture.allow_download": false}`)
	if rec := get(t, h, nil, fmt.Sprintf("/raw/%d", pub.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("downloads off: %d", rec.Code)
	}
}

func TestUploadClientsAndLimits(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")
	img := testPNG(t, 64, 64, 5)

	// Browsers declare files of an unknown type as application/octet-stream; the
	// content decides, not the declaration.
	if rec := uploadAs(t, alice, "application/octet-stream", "Generic", "steve", true, img); rec.Code != http.StatusOK {
		t.Fatalf("octet-stream PNG: %d %s", rec.Code, rec.Body)
	}
	if rec := uploadAs(t, alice, "application/octet-stream", "Fake", "steve", true, []byte("<svg/>")); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("octet-stream non-PNG: %d", rec.Code)
	}

	// Each attempt decodes an image, duplicates included: attempts are limited per user.
	limited := false
	for range uploadsPerUser + 5 {
		if rec := upload(t, alice, "Again", "steve", true, img); rec.Code == http.StatusTooManyRequests {
			limited = rec.Header().Get("Retry-After") != ""
			break
		}
	}
	if !limited {
		t.Fatal("uploads must be limited per user")
	}
	if rec := upload(t, registerUser(t, h, "bob@example.com"), "Bob's", "steve", true, img); rec.Code != http.StatusOK {
		t.Fatalf("another user is not affected: %d", rec.Code)
	}
}
