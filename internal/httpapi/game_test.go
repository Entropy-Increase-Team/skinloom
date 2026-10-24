package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

func TestPlayersAndGameAPIs(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")

	var up UploadView
	_ = json.Unmarshal(upload(t, alice, "Slim", "alex", true, testPNG(t, 64, 64, 1)).Body.Bytes(), &up)
	slim := up.Texture
	_ = json.Unmarshal(upload(t, alice, "Cape", "cape", true, testPNG(t, 64, 32, 2)).Body.Bytes(), &up)
	cape := up.Texture

	var rules PlayerRules
	_ = json.Unmarshal(get(t, h, nil, "/api/v1/players/rules").Body.Bytes(), &rules)
	if rules.NameRule != "official" || rules.MinLength != 3 || rules.MaxLength != 16 || rules.Cost != 100 || !rules.Refund {
		t.Fatalf("rules %+v", rules)
	}

	// Management.
	if rec := (&browser{t: t, h: h}).do("POST", "/api/v1/me/players", `{"name":"Steve"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", rec.Code)
	}
	rec := alice.do("POST", "/api/v1/me/players", `{"name":"Steve"}`)
	var created PlayerCreated
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusOK || created.Player.Name != "Steve" || len(created.Player.UUID) != 32 || created.Score != up.Score-100 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	steve := created.Player
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"STEVE"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("taken name: %d %s", rec.Code, rec.Body)
	}
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"no spaces"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad name: %d", rec.Code)
	}
	if rec := alice.do("PUT", fmt.Sprintf("/api/v1/me/players/%d/textures", steve.ID), fmt.Sprintf(`{"skin":%d}`, cape.ID)); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a cape as the skin (C07): %d %s", rec.Code, rec.Body)
	}
	rec = alice.do("PUT", fmt.Sprintf("/api/v1/me/players/%d/textures", steve.ID), fmt.Sprintf(`{"skin":%d,"cape":%d}`, slim.ID, cape.ID))
	var worn PlayerView
	_ = json.Unmarshal(rec.Body.Bytes(), &worn)
	if rec.Code != http.StatusOK || worn.Skin == nil || worn.Skin.Type != "alex" || worn.Cape == nil {
		t.Fatalf("wear: %d %s", rec.Code, rec.Body)
	}
	other := registerUser(t, h, "bob@example.com")
	if rec := other.do("PATCH", fmt.Sprintf("/api/v1/me/players/%d", steve.ID), `{"name":"Mine"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("someone else's player: %d", rec.Code)
	}

	// CustomSkinLoader: exact JSON, any case, the stored name back.
	for _, path := range []string{"/Steve.json", "/STEVE.json", "/csl/steve.json"} {
		rec := get(t, h, nil, path)
		want := fmt.Sprintf(`{"username":"Steve","skins":{"slim":"%s"},"cape":"%s"}`, slim.Hash, cape.Hash)
		if rec.Code != http.StatusOK || rec.Body.String() != want || rec.Header().Get("Cache-Control") != "public, max-age=60" ||
			rec.Header().Get("Last-Modified") == "" || rec.Header().Get("Set-Cookie") != "" {
			t.Fatalf("%s: %d %s %v", path, rec.Code, rec.Body, rec.Header())
		}
	}
	lm := get(t, h, nil, "/Steve.json").Header().Get("Last-Modified")
	if rec := get(t, h, nil, "/Steve.json", "If-Modified-Since", lm); rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", rec.Code)
	}

	// UniSkinMod.
	var usm map[string]any
	rec = get(t, h, nil, "/usm/steve.json")
	_ = json.Unmarshal(rec.Body.Bytes(), &usm)
	if rec.Code != http.StatusOK || usm["player_name"] != "Steve" || usm["model_preference"].([]any)[0] != "slim" ||
		usm["skins"].(map[string]any)["slim"] != slim.Hash || usm["cape"] != cape.Hash || usm["last_update"].(float64) <= 0 {
		t.Fatalf("usm: %d %s", rec.Code, rec.Body)
	}

	// Legacy: the image itself.
	for path, hash := range map[string]string{"/skin/Steve.png": slim.Hash, "/cape/steve.png": cape.Hash} {
		rec := get(t, h, nil, path)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		if direct := get(t, h, nil, "/textures/"+hash); !bytes.Equal(direct.Body.Bytes(), rec.Body.Bytes()) {
			t.Fatalf("%s is not the texture", path)
		}
		if _, err := png.Decode(rec.Body); err != nil {
			t.Fatal(err)
		}
	}

	// A player wearing nothing: nulls, the default model.
	rec = alice.do("POST", "/api/v1/me/players", `{"name":"Naked"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("second player: %d %s", rec.Code, rec.Body)
	}
	if rec := get(t, h, nil, "/Naked.json"); rec.Body.String() != `{"username":"Naked","skins":{"default":null},"cape":null}` {
		t.Fatalf("nothing worn: %s", rec.Body)
	}
	if rec := get(t, h, nil, "/skin/Naked.png"); rec.Code != http.StatusNotFound {
		t.Fatalf("legacy without a skin: %d", rec.Code)
	}

	// Unknown names: 404 (USM with its JSON error), or 204 for CSL when configured.
	if rec := get(t, h, nil, "/Nobody.json"); rec.Code != http.StatusNotFound {
		t.Fatalf("csl unknown: %d", rec.Code)
	}
	if rec := get(t, h, nil, "/usm/Nobody.json"); rec.Code != http.StatusNotFound || rec.Body.String() != `{"errno":1,"msg":"Player not found."}` {
		t.Fatalf("usm unknown: %d %s", rec.Code, rec.Body)
	}
	setSettings(t, d, `{"delivery.missing_player_status": 204}`)
	if rec := get(t, h, nil, "/csl/Nobody.json"); rec.Code != http.StatusNoContent {
		t.Fatalf("csl unknown with 204: %d", rec.Code)
	}
	if rec := get(t, h, nil, "/Steve.txt"); rec.Code != http.StatusNotFound {
		t.Fatalf("not a profile path: %d", rec.Code)
	}

	// Names that are not ASCII arrive percent-encoded.
	setSettings(t, d, `{"player.name_rule": "cjk", "player.name_length_min": 1}`)
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"史蒂夫"}`); rec.Code != http.StatusOK {
		t.Fatalf("CJK name: %d %s", rec.Code, rec.Body)
	}
	// Uppercase escapes are Go's own form (the router sees the decoded path); lowercase
	// ones keep the raw path, and the router hands the parameter over still encoded.
	for _, path := range []string{"/%E5%8F%B2%E8%92%82%E5%A4%AB.json", "/csl/%e5%8f%b2%e8%92%82%e5%a4%ab.json"} {
		if rec := get(t, h, nil, path); rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"username":"史蒂夫"`)) {
			t.Fatalf("encoded CJK name %s: %d %s", path, rec.Code, rec.Body)
		}
	}

	// A banned owner: 403 everywhere.
	d.DB.Ent.User.Update().Where(user.EmailNormalized("alice@example.com")).SetStatus(user.StatusBanned).ExecX(context.Background())
	for _, path := range []string{"/Steve.json", "/usm/Steve.json", "/skin/Steve.png"} {
		if rec := get(t, h, nil, path); rec.Code != http.StatusForbidden {
			t.Fatalf("%s for a banned owner: %d", path, rec.Code)
		}
	}
	d.DB.Ent.User.Update().Where(user.EmailNormalized("alice@example.com")).SetStatus(user.StatusActive).ExecX(context.Background())

	// Delete: the price paid comes back, and the name is free again.
	var list []PlayerView
	_ = json.Unmarshal(alice.do("GET", "/api/v1/me/players", "").Body.Bytes(), &list)
	if len(list) != 3 {
		t.Fatalf("list %+v", list)
	}
	rec = alice.do("DELETE", fmt.Sprintf("/api/v1/me/players/%d", steve.ID), "")
	var deleted PlayerDeleted
	_ = json.Unmarshal(rec.Body.Bytes(), &deleted)
	if rec.Code != http.StatusOK || deleted.Refunded != 100 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := get(t, h, nil, "/Steve.json"); rec.Code != http.StatusNoContent {
		t.Fatalf("deleted player (204 configured): %d", rec.Code)
	}
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest("GET", "/Naked.json", nil))
	if head.Code != http.StatusOK {
		t.Fatalf("other players stay: %d", head.Code)
	}
}
