package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestClosetAndTextureManagement(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "score.per_closet_item": 5, "score.award_per_like": 2}`)
	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)
	alice := registerUser(t, h, "alice@example.com")
	bob := registerUser(t, h, "bob@example.com")

	var up UploadView
	_ = json.Unmarshal(upload(t, alice, "Alice's", "steve", true, testPNG(t, 64, 64, 1)).Body.Bytes(), &up)
	skin := up.Texture

	// Favorites.
	rec := bob.do("POST", "/api/v1/me/closet", fmt.Sprintf(`{"textureId":%d}`, skin.ID))
	var fav Favorited
	_ = json.Unmarshal(rec.Body.Bytes(), &fav)
	if rec.Code != http.StatusOK || fav.Item.Name != "Alice's" || fav.Score != 1000-5 || fav.Item.Texture.Likes != 2 {
		t.Fatalf("favorite: %d %s", rec.Code, rec.Body)
	}
	if rec := bob.do("POST", "/api/v1/me/closet", fmt.Sprintf(`{"textureId":%d}`, skin.ID)); rec.Code != http.StatusConflict {
		t.Fatalf("twice: %d", rec.Code)
	}
	if rec := bob.do("PATCH", fmt.Sprintf("/api/v1/me/closet/%d", skin.ID), `{"name":"Borrowed"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Borrowed") {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	rec = bob.do("DELETE", fmt.Sprintf("/api/v1/me/closet/%d", skin.ID), "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"refunded":5`) {
		t.Fatalf("unfavorite: %d %s", rec.Code, rec.Body)
	}
	if rec := bob.do("DELETE", fmt.Sprintf("/api/v1/me/closet/%d", skin.ID), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("not in the closet: %d", rec.Code)
	}

	// Edits: the uploader and administrators only.
	if rec := bob.do("PATCH", fmt.Sprintf("/api/v1/textures/%d", skin.ID), `{"name":"Mine"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("someone else's texture: %d", rec.Code)
	}
	if rec := alice.do("PATCH", fmt.Sprintf("/api/v1/textures/%d", skin.ID), `{"type":"cape"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a square image as a cape: %d %s", rec.Code, rec.Body)
	}
	rec = alice.do("PATCH", fmt.Sprintf("/api/v1/textures/%d", skin.ID), `{"name":"Renamed","type":"alex","public":false}`)
	var edited TextureView
	_ = json.Unmarshal(rec.Body.Bytes(), &edited)
	if rec.Code != http.StatusOK || edited.Name != "Renamed" || edited.Type != "alex" || edited.Public {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	// An administrator's change to someone else's texture is audited and free.
	if rec := admin.do("PATCH", fmt.Sprintf("/api/v1/textures/%d", skin.ID), `{"public":true}`); rec.Code != http.StatusOK {
		t.Fatalf("admin edit: %d %s", rec.Code, rec.Body)
	}
	if rec := admin.do("DELETE", fmt.Sprintf("/api/v1/textures/%d", skin.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("admin delete: %d %s", rec.Code, rec.Body)
	}
	var audit AuditPage
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/audit?action=admin.texture_deleted", "").Body.Bytes(), &audit)
	if len(audit.Records) != 1 {
		t.Fatalf("audit: %+v", audit)
	}
	if rec := get(t, h, nil, fmt.Sprintf("/api/v1/textures/%d", skin.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted texture: %d", rec.Code)
	}

	// Check-in and the dashboard.
	rec = alice.do("POST", "/api/v1/me/sign", "")
	var sign SignResult
	_ = json.Unmarshal(rec.Body.Bytes(), &sign)
	if rec.Code != http.StatusOK || sign.Gained < 10 || sign.Gained > 100 || sign.NextAt.IsZero() {
		t.Fatalf("check-in: %d %s", rec.Code, rec.Body)
	}
	if rec := alice.do("POST", "/api/v1/me/sign", ""); rec.Code != http.StatusConflict || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second check-in: %d %v", rec.Code, rec.Header())
	}
	rec = alice.do("GET", "/api/v1/me/dashboard", "")
	var dash Dashboard
	_ = json.Unmarshal(rec.Body.Bytes(), &dash)
	if rec.Code != http.StatusOK || dash.Score != sign.Score || dash.Sign.NextAt == nil || dash.Sign.Min != 10 || dash.Uploads != 0 {
		t.Fatalf("dashboard: %d %s", rec.Code, rec.Body)
	}
	if rec := (&browser{t: t, h: h}).do("GET", "/api/v1/me/dashboard", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard signed out: %d", rec.Code)
	}
}
