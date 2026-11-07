package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/contract"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/*.golden")

// TestContractGolden (design 02 §6 item 1) records every game-facing endpoint for a
// site with each kind of player (classic, slim, with a cape, without textures, wearing a
// private skin, owned by a banned user, missing) and compares the normalized responses
// with testdata/contract.golden. The golden file was checked line by line against
// Blessing Skin 6 and its usm-api, legacy-api and yggdrasil-api plugins; ADR 0037 lists
// where SkinLoom answers differently on purpose. Any change shows up as a diff to
// review (go test -run TestContractGolden -update to accept it).
func TestContractGolden(t *testing.T) {
	h, tx := contractSite(t)
	classic, slim, cape := tx["classic"], tx["slim"], tx["cape"]

	var opts contract.Options
	var out strings.Builder
	call := func(method, path, body string) *httptest.ResponseRecorder {
		var rec *httptest.ResponseRecorder
		if method == "GET" {
			rec = get(t, h, nil, path)
		} else {
			rec = launcher(t, h, method, path, body)
		}
		line := method + " " + path
		if body != "" {
			line += " " + body
		}
		fmt.Fprintf(&out, "### %s\n%s\n", line, contract.Normalize(contract.Response{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes()}, opts))
		return rec
	}
	const ygg = YggdrasilPrefix

	// The key signs profiles; read it first so the signatures below are checked.
	var meta struct {
		Key string `json:"signaturePublickey"`
	}
	_ = json.Unmarshal(call("GET", ygg+"/", "").Body.Bytes(), &meta)
	key, err := contract.ParsePublicKey(meta.Key)
	if err != nil {
		t.Fatal(err)
	}
	opts.PublicKey = key

	fmt.Fprintf(&out, "## CustomSkinLoader (02 §2.1)\n\n")
	for _, p := range []string{"/Steve.json", "/csl/steve.json", "/Alex.json", "/Naked.json", "/Hidden.json", "/Banned.json", "/Nobody.json"} {
		call("GET", p, "")
	}
	fmt.Fprintf(&out, "## UniSkinMod (02 §2.2)\n\n")
	for _, p := range []string{"/usm/Steve.json", "/usm/alex.json", "/usm/Naked.json", "/usm/Banned.json", "/usm/Nobody.json"} {
		call("GET", p, "")
	}
	fmt.Fprintf(&out, "## Legacy (02 §2.3)\n\n")
	for _, p := range []string{"/skin/Steve.png", "/cape/Steve.png", "/cape/Alex.png", "/skin/Naked.png", "/skin/Banned.png", "/skin/Nobody.png"} {
		call("GET", p, "")
	}
	fmt.Fprintf(&out, "## Texture files (02 §2.1)\n\n")
	missing := strings.Repeat("0", 64)
	for _, p := range []string{"/textures/" + classic.Hash, "/csl/textures/" + slim.Hash, "/usm/textures/" + cape.Hash, "/textures/" + missing,
		fmt.Sprintf("/raw/%d", classic.ID), "/raw/999"} {
		call("GET", p, "")
	}

	fmt.Fprintf(&out, "## Yggdrasil (02 §2.4)\n\n")
	var uuids []struct{ ID, Name string }
	_ = json.Unmarshal(call("POST", ygg+"/api/profiles/minecraft", `["Steve","alex","Nobody","Banned"]`).Body.Bytes(), &uuids)
	call("GET", ygg+"/api/users/profiles/minecraft/steve", "")
	call("GET", ygg+"/api/users/profiles/minecraft/Nobody", "")
	steveUUID := ""
	for _, u := range uuids {
		if u.Name == "Steve" {
			steveUUID = u.ID
		}
	}
	if steveUUID == "" {
		t.Fatalf("no UUID for Steve: %+v", uuids)
	}
	call("GET", ygg+"/sessionserver/session/minecraft/profile/"+steveUUID, "")
	call("GET", ygg+"/sessionserver/session/minecraft/profile/"+steveUUID+"?unsigned=false", "")
	call("GET", ygg+"/sessionserver/session/minecraft/profile/"+strings.Repeat("f", 32), "")
	call("GET", ygg+"/minecraftservices/publickeys", "")

	call("POST", ygg+"/authserver/authenticate", `{"username":"alice@example.com","password":"wrong-pass"}`)
	call("POST", ygg+"/authserver/authenticate", `{"username":"alice@example.com","password":"user-pass-1","clientToken":"golden","requestUser":true}`)
	var auth struct {
		AccessToken string `json:"accessToken"`
	}
	_ = json.Unmarshal(call("POST", ygg+"/authserver/authenticate",
		`{"username":"steve","password":"user-pass-1","clientToken":"golden","agent":{"name":"Minecraft","version":1}}`).Body.Bytes(), &auth)
	tok := auth.AccessToken
	// Bodies with the token are written with <token>, so the golden file stays stable.
	withToken := func(method, path, body string) *httptest.ResponseRecorder {
		rec := launcher(t, h, method, path, strings.ReplaceAll(body, "<token>", tok))
		fmt.Fprintf(&out, "### %s %s %s\n%s\n", method, path, body, contract.Normalize(contract.Response{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes()}, opts))
		return rec
	}
	withToken("POST", ygg+"/authserver/validate", `{"accessToken":"<token>","clientToken":"golden"}`)
	withToken("POST", ygg+"/sessionserver/session/minecraft/join", fmt.Sprintf(`{"accessToken":"<token>","selectedProfile":%q,"serverId":"golden-server"}`, steveUUID))
	call("GET", ygg+"/sessionserver/session/minecraft/hasJoined?username=Steve&serverId=golden-server", "")
	call("GET", ygg+"/sessionserver/session/minecraft/hasJoined?username=Steve&serverId=another-server", "")
	call("GET", ygg+"/sessionserver/session/minecraft/hasJoined?username=Alex&serverId=golden-server", "")
	withToken("POST", ygg+"/authserver/invalidate", `{"accessToken":"<token>","clientToken":"golden"}`)
	withToken("POST", ygg+"/authserver/validate", `{"accessToken":"<token>"}`)
	withToken("POST", ygg+"/authserver/refresh", `{"accessToken":"<token>","clientToken":"golden"}`)
	call("POST", ygg+"/authserver/signout", `{"username":"STEVE","password":"user-pass-1"}`)
	call("POST", ygg+"/authserver/authenticate", `{"username":"bob@example.com","password":"user-pass-1"}`)

	golden := filepath.Join("testdata", "contract.golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test -run TestContractGolden -update to create it)", err)
	}
	if got := out.String(); strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("the game API answers differently; review the diff and run go test -run TestContractGolden -update to accept it\n--- got ---\n%s", got)
	}
}

type contractTex struct {
	ID   int64
	Hash string
}

// contractSite is the site of TestContractGolden: players Steve (classic skin and a
// cape), Alex (slim), Naked (nothing), Hidden (a private skin) and Banned (owner
// banned), at https://skins.example.com.
func contractSite(t *testing.T) (http.Handler, map[string]contractTex) {
	t.Helper()
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "site.url": "https://skins.example.com"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")
	bob := registerUser(t, h, "bob@example.com")

	up := func(b *browser, name, typ string, public bool, seed byte) contractTex {
		w, hgt := 64, 64
		if typ == "cape" {
			hgt = 32
		}
		var v UploadView
		rec := upload(t, b, name, typ, public, testPNG(t, w, hgt, seed))
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("upload %s: %d %s", name, rec.Code, rec.Body)
		}
		return contractTex{v.Texture.ID, v.Texture.Hash}
	}
	classic := up(alice, "Classic", "steve", true, 1)
	slim := up(alice, "Slim", "alex", true, 2)
	cape := up(alice, "Cape", "cape", true, 3)
	private := up(alice, "Private", "steve", false, 4)
	player := func(b *browser, name string, skin, capeID int64) {
		rec := b.do("POST", "/api/v1/me/players", fmt.Sprintf(`{"name":%q}`, name))
		var c PlayerCreated
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("player %s: %d %s", name, rec.Code, rec.Body)
		}
		if skin != 0 || capeID != 0 {
			body := map[string]int64{}
			if skin != 0 {
				body["skin"] = skin
			}
			if capeID != 0 {
				body["cape"] = capeID
			}
			raw, _ := json.Marshal(body)
			if rec := b.do("PUT", fmt.Sprintf("/api/v1/me/players/%d/textures", c.Player.ID), string(raw)); rec.Code != http.StatusOK {
				t.Fatalf("wear %s: %d %s", name, rec.Code, rec.Body)
			}
		}
	}
	player(alice, "Steve", classic.ID, cape.ID)
	player(alice, "Alex", slim.ID, 0)
	player(alice, "Naked", 0, 0)
	player(alice, "Hidden", private.ID, 0)
	if rec := bob.do("POST", "/api/v1/me/closet", fmt.Sprintf(`{"textureId":%d}`, classic.ID)); rec.Code != http.StatusOK {
		t.Fatalf("closet: %d %s", rec.Code, rec.Body)
	}
	player(bob, "Banned", classic.ID, 0)
	d.DB.Ent.User.Update().Where(user.Email("bob@example.com")).SetStatus(user.StatusBanned).ExecX(context.Background())

	return h, map[string]contractTex{"classic": classic, "slim": slim, "cape": cape, "private": private}
}

// TestCompareSites runs the BS comparison (skinloom import compare) against two
// servers: the same site twice agrees on everything, and a changed answer is found.
func TestCompareSites(t *testing.T) {
	h, _ := contractSite(t)
	same := httptest.NewServer(h)
	defer same.Close()
	names := []string{"Steve", "Alex", "Naked", "Hidden", "Banned", "Nobody"}
	res, err := contract.Compare(context.Background(), same.Client(), &contract.Site{Base: same.URL}, &contract.Site{Base: same.URL + "/"}, names)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) < 40 {
		t.Errorf("only %d requests", len(res))
	}
	for _, r := range res {
		if !r.Same {
			t.Errorf("%s differs from itself:\n%s\n%s", r.Request, r.Old, r.New)
		}
	}

	// The "old" site gives Alex no skin.
	changed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Alex.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"username":"Alex","skins":{"default":null},"cape":null}`))
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer changed.Close()
	res, err = contract.Compare(context.Background(), changed.Client(), &contract.Site{Base: changed.URL}, &contract.Site{Base: same.URL}, names)
	if err != nil {
		t.Fatal(err)
	}
	var diffs []string
	for _, r := range res {
		if !r.Same {
			diffs = append(diffs, r.Request)
		}
	}
	if len(diffs) != 1 || diffs[0] != "GET /Alex.json" {
		t.Errorf("differences: %v", diffs)
	}

	// A SkinLoom whose import made a new signing key: each site's signatures are valid
	// with its own key, but game servers that cached the old one must restart.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	otherKey := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != contract.YggdrasilPrefix+"/" {
			h.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Error(err)
		}
		m["signaturePublickey"] = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m)
	}))
	defer otherKey.Close()
	res, err = contract.Compare(context.Background(), otherKey.Client(), &contract.Site{Base: same.URL}, &contract.Site{Base: otherKey.URL}, names)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 || res[0].Request != contract.KeyRequest || res[0].Same {
		t.Errorf("the different signing key is not reported first: %+v", res[:min(len(res), 1)])
	}
}

// Renders larger than render.WebPMaxPixels are PNG even for clients that take WebP:
// encoding them as WebP would miss the 50 ms target (ADR 0038).
func TestLargeRendersArePNG(t *testing.T) {
	h, tx := contractSite(t)
	for _, tc := range []struct{ path, want string }{
		{"/avatar/player/Steve?size=100", "image/webp"},
		{"/avatar/player/Steve?size=256&3d", "image/webp"},
		{"/avatar/player/Steve?size=512", "image/png"},
		{fmt.Sprintf("/preview/%d?height=256", tx["classic"].ID), "image/webp"},
		{fmt.Sprintf("/preview/%d?height=512", tx["classic"].ID), "image/png"},
	} {
		rec := get(t, h, nil, tc.path, "Accept", "image/webp,image/*,*/*")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != tc.want {
			t.Errorf("%s: %d %s, want %s", tc.path, rec.Code, rec.Header().Get("Content-Type"), tc.want)
		}
	}
}
