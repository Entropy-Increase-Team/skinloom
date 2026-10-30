package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// launcher talks to the Yggdrasil API the way authlib-injector and game servers do:
// JSON bodies, no cookies, no CSRF header.
func launcher(t *testing.T, h http.Handler, method, path, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestYggdrasilAPI(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "site.url": "https://skins.example.com", "yggdrasil.skin_domains": "cdn.example.com"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"Steve"}`); rec.Code != http.StatusOK {
		t.Fatalf("player: %d %s", rec.Code, rec.Body)
	}

	// Metadata.
	rec := launcher(t, h, "GET", YggdrasilPrefix+"/", "")
	var meta struct {
		Meta struct {
			ServerName     string            `json:"serverName"`
			Implementation string            `json:"implementationName"`
			Links          map[string]string `json:"links"`
			NonEmail       bool              `json:"feature.non_email_login"`
		} `json:"meta"`
		SkinDomains []string `json:"skinDomains"`
		PublicKey   string   `json:"signaturePublickey"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &meta)
	if rec.Code != http.StatusOK || meta.Meta.Implementation != "SkinLoom" || !meta.Meta.NonEmail ||
		meta.Meta.Links["homepage"] != "https://skins.example.com/" || !strings.HasPrefix(meta.PublicKey, "-----BEGIN PUBLIC KEY-----") ||
		!strings.Contains(strings.Join(meta.SkinDomains, ","), "skins.example.com") || !strings.Contains(strings.Join(meta.SkinDomains, ","), "cdn.example.com") {
		t.Fatalf("metadata: %d %s", rec.Code, rec.Body)
	}

	// Errors in Yggdrasil's format.
	rec = launcher(t, h, "POST", YggdrasilPrefix+"/authserver/authenticate", `{"username":"alice@example.com","password":"wrong-pass"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"error":"ForbiddenOperationException"`) {
		t.Fatalf("wrong password: %d %s", rec.Code, rec.Body)
	}
	if rec := launcher(t, h, "POST", YggdrasilPrefix+"/authserver/authenticate", `{not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", rec.Code)
	}

	// Login by player name, with a session cookie the browser happens to send: no CSRF
	// header needed, since the API reads no cookies.
	rec = launcher(t, h, "POST", YggdrasilPrefix+"/authserver/authenticate",
		`{"username":"steve","password":"user-pass-1","clientToken":"launcher-1","requestUser":true,"agent":{"name":"Minecraft","version":1}}`,
		"Cookie", SessionCookie+"="+alice.cookie)
	var auth struct {
		AccessToken     string `json:"accessToken"`
		ClientToken     string `json:"clientToken"`
		SelectedProfile struct{ ID, Name string }
		User            struct{ ID string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &auth)
	if rec.Code != http.StatusOK || auth.ClientToken != "launcher-1" || auth.SelectedProfile.Name != "Steve" || auth.User.ID == "" {
		t.Fatalf("authenticate: %d %s", rec.Code, rec.Body)
	}
	if rec := launcher(t, h, "POST", YggdrasilPrefix+"/authserver/validate", fmt.Sprintf(`{"accessToken":%q}`, auth.AccessToken)); rec.Code != http.StatusNoContent {
		t.Fatalf("validate: %d %s", rec.Code, rec.Body)
	}

	// Upload a skin through the texture API (multipart, Bearer), then join and check.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "my-skin.png")
	_, _ = fw.Write(testPNG(t, 64, 64, 9))
	_ = mw.WriteField("model", "slim")
	_ = mw.Close()
	uuid := auth.SelectedProfile.ID
	rec = launcher(t, h, "PUT", YggdrasilPrefix+"/api/user/profile/"+uuid+"/skin", body.String(),
		"Content-Type", mw.FormDataContentType(), "Authorization", "Bearer "+auth.AccessToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	if rec := launcher(t, h, "PUT", YggdrasilPrefix+"/api/user/profile/"+uuid+"/skin", body.String(),
		"Content-Type", mw.FormDataContentType(), "Authorization", "Bearer nope"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload with a bad token: %d", rec.Code)
	}

	rec = launcher(t, h, "POST", YggdrasilPrefix+"/sessionserver/session/minecraft/join",
		fmt.Sprintf(`{"accessToken":%q,"selectedProfile":%q,"serverId":"-6a1b2c"}`, auth.AccessToken, uuid))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("join: %d %s", rec.Code, rec.Body)
	}
	rec = launcher(t, h, "GET", YggdrasilPrefix+"/sessionserver/session/minecraft/hasJoined?username=Steve&serverId=-6a1b2c", "")
	var profile struct {
		ID         string
		Name       string
		Properties []struct{ Name, Value, Signature string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &profile)
	if rec.Code != http.StatusOK || profile.ID != uuid || len(profile.Properties) != 2 || profile.Properties[0].Signature == "" {
		t.Fatalf("hasJoined: %d %s", rec.Code, rec.Body)
	}
	if rec := launcher(t, h, "GET", YggdrasilPrefix+"/sessionserver/session/minecraft/hasJoined?username=Steve&serverId=other", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unknown server id: %d", rec.Code)
	}

	// Profile by UUID: unsigned unless asked; lookups.
	rec = launcher(t, h, "GET", YggdrasilPrefix+"/sessionserver/session/minecraft/profile/"+uuid, "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "signature") {
		t.Fatalf("profile: %d %s", rec.Code, rec.Body)
	}
	if rec := launcher(t, h, "GET", YggdrasilPrefix+"/sessionserver/session/minecraft/profile/"+uuid+"?unsigned=false", ""); !strings.Contains(rec.Body.String(), "signature") {
		t.Fatalf("signed profile: %s", rec.Body)
	}
	if rec := launcher(t, h, "GET", YggdrasilPrefix+"/sessionserver/session/minecraft/profile/"+strings.Repeat("0", 32), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unknown profile: %d", rec.Code)
	}
	if rec := launcher(t, h, "POST", YggdrasilPrefix+"/api/profiles/minecraft", `["STEVE","nobody"]`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Steve"`) {
		t.Fatalf("bulk lookup: %d %s", rec.Code, rec.Body)
	}
	if rec := launcher(t, h, "GET", YggdrasilPrefix+"/api/users/profiles/minecraft/nobody", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("single lookup of nobody: %d", rec.Code)
	}
	if rec := launcher(t, h, "GET", YggdrasilPrefix+"/minecraftservices/publickeys", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "profilePropertyKeys") {
		t.Fatalf("publickeys: %d %s", rec.Code, rec.Body)
	}

	// The account page counts launchers and signs them out.
	if body := alice.do("GET", "/api/v1/me/yggdrasil", "").Body.String(); !strings.Contains(body, `"tokens":1`) ||
		!strings.Contains(body, `"apiUrl":"https://skins.example.com/api/yggdrasil"`) {
		t.Fatalf("me/yggdrasil: %s", body)
	}
	if rec := alice.do("POST", "/api/v1/me/yggdrasil/signout", ""); rec.Code != http.StatusOK {
		t.Fatalf("sign launchers out: %d", rec.Code)
	}
	if rec := launcher(t, h, "POST", YggdrasilPrefix+"/authserver/validate", fmt.Sprintf(`{"accessToken":%q}`, auth.AccessToken)); rec.Code != http.StatusForbidden {
		t.Fatalf("validate after signout: %d", rec.Code)
	}
}
