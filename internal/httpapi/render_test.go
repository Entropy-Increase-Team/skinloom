package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"net/http"
	"strings"
	"testing"

	_ "golang.org/x/image/webp"
)

// decoded checks an image answer and returns its size and format.
func decoded(t *testing.T, rec *http.Response, body []byte) (image.Point, string) {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("not an image (%s): %v", rec.Header.Get("Content-Type"), err)
	}
	if ct := rec.Header.Get("Content-Type"); ct != "image/"+format {
		t.Fatalf("Content-Type %s for a %s", ct, format)
	}
	return img.Bounds().Size(), format
}

func TestAvatarsAndPreviews(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)
	alice := registerUser(t, h, "alice@example.com")
	bob := registerUser(t, h, "bob@example.com")
	var me UserView
	_ = json.Unmarshal(alice.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)

	var up UploadView
	_ = json.Unmarshal(upload(t, alice, "Skin", "alex", true, testPNG(t, 64, 64, 1)).Body.Bytes(), &up)
	skin := up.Texture
	_ = json.Unmarshal(upload(t, alice, "Cape", "cape", true, testPNG(t, 64, 32, 2)).Body.Bytes(), &up)
	cape := up.Texture
	_ = json.Unmarshal(upload(t, alice, "Secret", "steve", false, testPNG(t, 64, 64, 3)).Body.Bytes(), &up)
	secret := up.Texture

	img := func(b *browser, path string, header ...string) (*http.Response, []byte) {
		t.Helper()
		rec := get(t, h, b, path, header...)
		return rec.Result(), rec.Body.Bytes()
	}

	// By hash: WebP by default, sizes snap to the list, cached for good.
	res, body := img(nil, "/avatar/hash/"+skin.Hash+"?size=20")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("avatar by hash: %d %s", res.StatusCode, body)
	}
	if size, format := decoded(t, res, body); size != (image.Point{24, 24}) || format != "webp" {
		t.Fatalf("size %v, %s", size, format)
	}
	if !strings.Contains(res.Header.Get("Cache-Control"), "immutable") || res.Header.Get("Vary") != "Accept" {
		t.Fatalf("headers: %v", res.Header)
	}
	if res2, _ := img(nil, "/avatar/hash/"+skin.Hash+"?size=20", "If-None-Match", res.Header.Get("ETag")); res2.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: %d", res2.StatusCode)
	}
	if res, body := img(nil, "/avatar/hash/"+skin.Hash+"?png&3d"); res.StatusCode != http.StatusOK {
		t.Fatalf("3d png: %d", res.StatusCode)
	} else if size, format := decoded(t, res, body); size != (image.Point{100, 100}) || format != "png" {
		t.Fatalf("3d: %v %s", size, format)
	}
	if res, body := img(nil, "/avatar/hash/"+skin.Hash, "Accept", "image/png,image/jpeg"); res.StatusCode == http.StatusOK {
		if _, format := decoded(t, res, body); format != "png" {
			t.Fatalf("a client without WebP gets PNG: %s", format)
		}
	}
	if res, _ := img(nil, "/avatar/hash/"+cape.Hash); res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("a cape has no head: %d", res.StatusCode)
	}
	if res, body := img(nil, "/avatar/hash/"+strings.Repeat("0", 64)+"?png"); res.StatusCode != http.StatusOK {
		t.Fatalf("unknown hash: the placeholder, %d", res.StatusCode)
	} else {
		decoded(t, res, body)
	}

	// By texture id: a private texture of someone else is the placeholder.
	resA, bodyA := img(bob, fmt.Sprintf("/avatar/%d?png", secret.ID))
	resB, bodyB := img(alice, fmt.Sprintf("/avatar/%d?png", secret.ID))
	if resA.StatusCode != http.StatusOK || resB.StatusCode != http.StatusOK || bytes.Equal(bodyA, bodyB) {
		t.Fatalf("private avatar: %d %d, same image: %v", resA.StatusCode, resB.StatusCode, bytes.Equal(bodyA, bodyB))
	}
	if resA.Header.Get("Cache-Control") != "private, no-cache" || resB.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatal("an answer that depends on who asks must not be stored by shared caches")
	}
	if res, _ := img(nil, fmt.Sprintf("/avatar/%d", cape.ID)); res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("cape by id: %d", res.StatusCode)
	}

	// By player: unknown is a 404 (as Blessing Skin), a player without a skin the placeholder.
	if res, _ := img(nil, "/avatar/player/Nobody"); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown player: %d", res.StatusCode)
	}
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"Alice"}`); rec.Code != http.StatusOK {
		t.Fatalf("player: %d %s", rec.Code, rec.Body)
	}
	if res, _ := img(nil, "/avatar/player/ALICE"); res.StatusCode != http.StatusOK || strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("player avatar: %d %v", res.StatusCode, res.Header)
	}

	// By user: the chosen skin; capes and invisible skins are refused.
	if rec := alice.do("PUT", "/api/v1/me/avatar", fmt.Sprintf(`{"textureId":%d}`, cape.ID)); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cape as avatar: %d", rec.Code)
	}
	if rec := bob.do("PUT", "/api/v1/me/avatar", fmt.Sprintf(`{"textureId":%d}`, secret.ID)); rec.Code != http.StatusForbidden {
		t.Fatalf("someone else's private skin: %d", rec.Code)
	}
	placeholder, _ := img(nil, fmt.Sprintf("/avatar/user/%d?png", me.ID))
	_, before := img(nil, fmt.Sprintf("/avatar/user/%d?png", me.ID))
	if rec := alice.do("PUT", "/api/v1/me/avatar", fmt.Sprintf(`{"textureId":%d}`, skin.ID)); rec.Code != http.StatusNoContent {
		t.Fatalf("set avatar: %d %s", rec.Code, rec.Body)
	}
	_, after := img(nil, fmt.Sprintf("/avatar/user/%d?png", me.ID))
	if placeholder.StatusCode != http.StatusOK || bytes.Equal(before, after) {
		t.Fatal("the avatar follows the chosen skin")
	}
	if !strings.Contains(alice.do("GET", "/api/v1/auth/me", "").Body.String(), fmt.Sprintf(`"avatarTextureId":%d`, skin.ID)) {
		t.Fatal("me shows the avatar")
	}

	// Previews: front and back of a skin, a cape, visibility.
	if res, body := img(nil, fmt.Sprintf("/preview/%d?height=64&png", skin.ID)); res.StatusCode != http.StatusOK {
		t.Fatalf("preview: %d", res.StatusCode)
	} else if size, _ := decoded(t, res, body); size != (image.Point{72, 64}) {
		t.Fatalf("skin preview %v", size)
	}
	if res, body := img(nil, "/preview/hash/"+cape.Hash+"?height=64"); res.StatusCode != http.StatusOK {
		t.Fatalf("cape preview: %d", res.StatusCode)
	} else if size, _ := decoded(t, res, body); size != (image.Point{40, 64}) {
		t.Fatalf("cape preview %v", size)
	}
	if res, _ := img(bob, fmt.Sprintf("/preview/%d", secret.ID)); res.StatusCode != http.StatusForbidden {
		t.Fatalf("someone else's private preview: %d", res.StatusCode)
	}
	if res, _ := img(nil, "/preview/hash/"+strings.Repeat("0", 64)); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown preview hash: %d", res.StatusCode)
	}
}

func TestNotificationsAPI(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "site.announcement": {"en": "**Hello** <script>x</script>"}}`)
	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)
	alice := registerUser(t, h, "alice@example.com")
	bob := registerUser(t, h, "bob@example.com")

	if rec := alice.do("POST", "/api/v1/admin/notifications", `{"to":"all","title":"x","content":""}`); rec.Code != http.StatusForbidden {
		t.Fatalf("users cannot send: %d", rec.Code)
	}
	rec := admin.do("POST", "/api/v1/admin/notifications", `{"to":"all","title":"Maintenance","content":"**Tonight** at 23:00"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kind":"announcement"`) {
		t.Fatalf("announce: %d %s", rec.Code, rec.Body)
	}
	rec = admin.do("POST", "/api/v1/admin/notifications", `{"to":"user","email":"ALICE@example.com","title":"Just you","content":"hi"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"kind":"notification"`) {
		t.Fatalf("to one user: %d %s", rec.Code, rec.Body)
	}
	if rec := admin.do("POST", "/api/v1/admin/notifications", `{"to":"user","userId":999999,"title":"x","content":""}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("to nobody: %d", rec.Code)
	}

	if body := alice.do("GET", "/api/v1/me/notifications/unread", "").Body.String(); !strings.Contains(body, `"unread":2`) {
		t.Fatalf("alice unread: %s", body)
	}
	var page NotificationPage
	_ = json.Unmarshal(alice.do("GET", "/api/v1/me/notifications", "").Body.Bytes(), &page)
	if len(page.Notifications) != 2 || page.Unread != 2 || page.Notifications[0].Title != "Just you" ||
		!strings.Contains(page.Notifications[1].ContentHTML, "<strong>Tonight</strong>") || !page.Notifications[1].Announcement {
		t.Fatalf("page: %+v", page)
	}
	id := page.Notifications[0].ID
	if rec := bob.do("POST", fmt.Sprintf("/api/v1/me/notifications/%d/read", id), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("someone else's: %d", rec.Code)
	}
	if rec := alice.do("POST", fmt.Sprintf("/api/v1/me/notifications/%d/read", id), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("read: %d", rec.Code)
	}
	if body := alice.do("POST", "/api/v1/me/notifications/read-all", "").Body.String(); !strings.Contains(body, `"count":1`) {
		t.Fatalf("read all: %s", body)
	}
	if body := bob.do("GET", "/api/v1/me/notifications/unread", "").Body.String(); !strings.Contains(body, `"unread":1`) {
		t.Fatalf("bob gets the announcement only: %s", body)
	}

	var boot Bootstrap
	_ = json.Unmarshal(get(t, h, nil, "/api/v1/site/bootstrap", "Accept-Language", "en").Body.Bytes(), &boot)
	if !strings.Contains(boot.Site.AnnouncementHTML, "<strong>Hello</strong>") || strings.Contains(boot.Site.AnnouncementHTML, "<script") {
		t.Fatalf("announcement HTML: %q", boot.Site.AnnouncementHTML)
	}
	var audit AuditPage
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/audit?action=admin.notification_sent", "").Body.Bytes(), &audit)
	if len(audit.Records) != 2 {
		t.Fatalf("sends are audited: %d", len(audit.Records))
	}
}
