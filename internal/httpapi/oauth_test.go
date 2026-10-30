package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// app talks to the OAuth endpoints and the resource API as a third-party server does:
// no cookies, no CSRF header.
func app(t *testing.T, h http.Handler, method, path, ctype, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOAuthFlowAndResourceAPI(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "auth.registrations_per_ip": 0, "site.url": "https://skins.example.com", "score.per_closet_item": 0}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	dev := registerUser(t, h, "dev@example.com")
	alice := registerUser(t, h, "alice@example.com")
	form := "application/x-www-form-urlencoded"

	// The developer registers an application.
	rec := dev.do("POST", "/api/v1/me/oauth/clients", `{"name":"Forum","redirectUris":["https://forum.example/cb"],"confidential":true}`)
	var created OAuthClientSecret
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusCreated || created.Secret == "" || !created.Client.Confidential {
		t.Fatalf("create client: %d %s", rec.Code, rec.Body)
	}
	clientID := fmt.Sprint(created.Client.ID)

	// /oauth/authorize sends the browser to the consent page with the same query.
	query := "response_type=code&client_id=" + clientID + "&redirect_uri=" + url.QueryEscape("https://forum.example/cb") +
		"&scope=" + url.QueryEscape("User.Read Player.ReadWrite Closet.ReadWrtie Notification.Read") + "&state=abc"
	rec = app(t, h, "GET", "/oauth/authorize?"+query, "", "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/user/oauth/authorize?"+query {
		t.Fatalf("authorize redirect: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec = alice.do("GET", "/api/v1/oauth/authorize?"+query, "")
	var info AuthorizeInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &info)
	if rec.Code != http.StatusOK || info.Client == nil || info.Client.Name != "Forum" || len(info.Scopes) != 4 || info.Granted {
		t.Fatalf("authorize info: %d %s", rec.Code, rec.Body)
	}
	if rec := alice.do("GET", "/api/v1/oauth/authorize?response_type=code&client_id=999", ""); !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("unknown client: %s", rec.Body)
	}
	// Approving needs the CSRF header like any session request.
	if rec := app(t, h, "POST", "/api/v1/oauth/authorize", "application/json", `{"client_id":"`+clientID+`","response_type":"code","allow":true}`,
		"Cookie", SessionCookie+"="+alice.cookie); rec.Code != http.StatusForbidden {
		t.Fatalf("approve without the CSRF header: %d", rec.Code)
	}
	values, _ := url.ParseQuery(query)
	approve, _ := json.Marshal(map[string]any{"response_type": "code", "client_id": clientID, "redirect_uri": values.Get("redirect_uri"),
		"scope": values.Get("scope"), "state": "abc", "allow": true})
	rec = alice.do("POST", "/api/v1/oauth/authorize", string(approve))
	var res AuthorizeResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	back, _ := url.Parse(res.Redirect)
	if rec.Code != http.StatusOK || back == nil || back.Host != "forum.example" || back.Query().Get("state") != "abc" || back.Query().Get("code") == "" {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}

	// The application's server exchanges the code (form, HTTP Basic), with a stray
	// cookie: /oauth reads none, so no CSRF header is needed.
	exchange := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "redirect_uri": {"https://forum.example/cb"}}
	rec = app(t, h, "POST", "/oauth/token", form, exchange.Encode(), "Authorization", "Basic "+basic(clientID, "wrong"))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"error":"invalid_client"`) || rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("wrong secret: %d %s", rec.Code, rec.Body)
	}
	rec = app(t, h, "POST", "/oauth/token", form, exchange.Encode(), "Authorization", "Basic "+basic(clientID, created.Secret),
		"Cookie", SessionCookie+"="+alice.cookie)
	var tok struct {
		TokenType    string `json:"token_type"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	if rec.Code != http.StatusOK || tok.TokenType != "Bearer" || tok.AccessToken == "" || tok.ExpiresIn != 86400 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	bearer := "Bearer " + tok.AccessToken

	// The resource API.
	if rec := app(t, h, "GET", "/api/user", "", ""); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Unauthenticated.") {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body)
	}
	rec = app(t, h, "GET", "/api/user", "", "", "Authorization", bearer)
	var u map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec.Code != http.StatusOK || u["email"] != "alice@example.com" || u["permission"] != float64(0) || u["password"] != nil || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("user: %d %s", rec.Code, rec.Body)
	}
	rec = app(t, h, "POST", "/api/players", "application/json", `{"name":"Steve"}`, "Authorization", bearer)
	var added struct {
		Code int
		Data struct {
			PID  int64 `json:"pid"`
			Name string
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &added)
	if rec.Code != http.StatusOK || added.Code != 0 || added.Data.Name != "Steve" {
		t.Fatalf("add player: %d %s", rec.Code, rec.Body)
	}
	if rec := app(t, h, "POST", "/api/players", form, "name=steve", "Authorization", bearer); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"errors"`) {
		t.Fatalf("name taken (form body): %d %s", rec.Code, rec.Body)
	}
	pid := fmt.Sprint(added.Data.PID)
	if rec := app(t, h, "PUT", "/api/players/"+pid+"/name", "application/json", `{"name":"Alex"}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"name":"Alex"`) {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}

	// A texture alice uploads lands in her closet; wear it through the API.
	up := upload(t, alice, "Mine", "steve", true, testPNG(t, 64, 64, 4))
	var uploaded UploadView
	_ = json.Unmarshal(up.Body.Bytes(), &uploaded)
	tid := fmt.Sprint(uploaded.Texture.ID)
	rec = app(t, h, "GET", "/api/closet?perPage=500", "", "", "Authorization", bearer)
	var page struct {
		Total   int `json:"total"`
		PerPage int `json:"per_page"`
		Data    []struct {
			TID   int64 `json:"tid"`
			Pivot struct {
				ItemName string `json:"item_name"`
			}
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if rec.Code != http.StatusOK || page.Total != 1 || page.PerPage != 100 || page.Data[0].Pivot.ItemName != "Mine" {
		t.Fatalf("closet: %d %s", rec.Code, rec.Body)
	}
	if rec := app(t, h, "PUT", "/api/players/"+pid+"/textures", "application/json", `{"skin":`+tid+`}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"tid_skin":`+tid) {
		t.Fatalf("wear: %s", rec.Body)
	}
	if rec := app(t, h, "PUT", "/api/players/"+pid+"/textures", "application/json", `{"cape":`+tid+`}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"code":1`) {
		t.Fatalf("a skin as a cape: %s", rec.Body)
	}
	if rec := app(t, h, "DELETE", "/api/players/"+pid+"/textures", "application/json", `{"type":["skin"]}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"tid_skin":0`) {
		t.Fatalf("clear: %s", rec.Body)
	}
	if rec := app(t, h, "PUT", "/api/closet/"+tid, "application/json", `{"name":"Renamed"}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"code":0`) {
		t.Fatalf("rename closet item: %s", rec.Body)
	}
	if rec := app(t, h, "DELETE", "/api/closet/"+tid, "", "", "Authorization", bearer); !strings.Contains(rec.Body.String(), `"code":0`) {
		t.Fatalf("remove: %s", rec.Body)
	}
	if rec := app(t, h, "POST", "/api/closet", "application/json", `{"tid":`+tid+`,"name":"Back"}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"code":0`) {
		t.Fatalf("add back: %s", rec.Body)
	}
	if rec := app(t, h, "POST", "/api/closet", "application/json", `{"tid":`+tid+`,"name":"Again"}`, "Authorization", bearer); !strings.Contains(rec.Body.String(), `"code":1`) {
		t.Fatalf("twice: %s", rec.Body)
	}

	// Someone else's player.
	bob := registerUser(t, h, "bob@example.com")
	if rec := bob.do("POST", "/api/v1/me/players", `{"name":"Bobby"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if rec := app(t, h, "DELETE", "/api/players/"+fmt.Sprint(firstPlayerID(t, bob)), "", "", "Authorization", bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("someone else's player: %d %s", rec.Code, rec.Body)
	}
	if rec := app(t, h, "DELETE", "/api/players/999999", "", "", "Authorization", bearer); rec.Code != http.StatusNotFound {
		t.Fatalf("no such player: %d", rec.Code)
	}

	// Notifications.
	if _, err := d.Notify.Send(t.Context(), userID(t, alice), "Hello", "**hi**", nil); err != nil {
		t.Fatal(err)
	}
	rec = app(t, h, "GET", "/api/user/notifications", "", "", "Authorization", bearer)
	var unread []struct{ ID, Title string }
	_ = json.Unmarshal(rec.Body.Bytes(), &unread)
	if len(unread) != 1 || unread[0].Title != "Hello" {
		t.Fatalf("notifications: %s", rec.Body)
	}
	if rec := app(t, h, "POST", "/api/user/notifications/"+unread[0].ID, "", "", "Authorization", bearer); !strings.Contains(rec.Body.String(), "<strong>hi</strong>") {
		t.Fatalf("read: %s", rec.Body)
	}
	if rec := app(t, h, "POST", "/api/user/notifications/123456", "", "", "Authorization", bearer); rec.Code != http.StatusNotFound {
		t.Fatalf("someone else's or no notification: %d", rec.Code)
	}

	// A personal access token with only User.Read.
	rec = alice.do("POST", "/api/v1/me/tokens", `{"name":"script","scopes":["User.Read"],"expiresInDays":30}`)
	var pat NewPersonalToken
	_ = json.Unmarshal(rec.Body.Bytes(), &pat)
	if rec.Code != http.StatusCreated || !strings.HasPrefix(pat.Token, "slp_") || pat.Info.ExpiresAt == nil {
		t.Fatalf("pat: %d %s", rec.Code, rec.Body)
	}
	if rec := app(t, h, "GET", "/api/user", "", "", "Authorization", "Bearer "+pat.Token); rec.Code != http.StatusOK {
		t.Fatalf("pat user: %d", rec.Code)
	}
	if rec := app(t, h, "GET", "/api/players", "", "", "Authorization", "Bearer "+pat.Token); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Invalid scope") {
		t.Fatalf("pat without Player.Read: %d %s", rec.Code, rec.Body)
	}

	// Grants: listed; revoking signs the application out.
	rec = alice.do("GET", "/api/v1/me/oauth/grants", "")
	if !strings.Contains(rec.Body.String(), `"Closet.ReadWrite"`) {
		t.Fatalf("grants: %s", rec.Body)
	}
	if rec := alice.do("DELETE", "/api/v1/me/oauth/grants/"+clientID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", rec.Code)
	}
	if rec := app(t, h, "GET", "/api/user", "", "", "Authorization", bearer); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after revoke: %d", rec.Code)
	}

	// Refresh with a form body and client_secret_post, and metadata, and the root.
	values = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {clientID}, "client_secret": {created.Secret}}
	if rec := app(t, h, "POST", "/oauth/token", form, values.Encode()); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Fatalf("refresh after revoke: %d %s", rec.Code, rec.Body)
	}
	if rec := app(t, h, "GET", "/.well-known/oauth-authorization-server", "", ""); !strings.Contains(rec.Body.String(), `"token_endpoint":"https://skins.example.com/oauth/token"`) {
		t.Fatalf("metadata: %s", rec.Body)
	}
	if rec := app(t, h, "GET", "/api", "", ""); !strings.Contains(rec.Body.String(), `"blessing_skin":"`+BSCompatVersion+`"`) {
		t.Fatalf("api root: %s", rec.Body)
	}
	if rec := app(t, h, "OPTIONS", "/oauth/token", "", "", "Origin", "https://spa.example", "Access-Control-Request-Method", "POST"); rec.Code != http.StatusNoContent ||
		rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d", rec.Code)
	}
}

func basic(id, secret string) string {
	return base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(id) + ":" + url.QueryEscape(secret)))
}

func firstPlayerID(t *testing.T, b *browser) int64 {
	t.Helper()
	var list []PlayerView
	_ = json.Unmarshal(b.do("GET", "/api/v1/me/players", "").Body.Bytes(), &list)
	if len(list) == 0 {
		t.Fatal("no players")
	}
	return list[0].ID
}

func userID(t *testing.T, b *browser) int64 {
	t.Helper()
	var me UserView
	_ = json.Unmarshal(b.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	return me.ID
}

// The resource API and /oauth allow any origin; /api/v1 must not: its CSRF defense
// is that no other site gets a preflight for CSRFHeader approved.
func TestCORSStaysOffTheRESTAPI(t *testing.T) {
	d := newTestDeps(t)
	h, _ := New(d)
	preflight := func(path string) http.Header {
		t.Helper()
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", CSRFHeader)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Header()
	}
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/me/password", "/api/v1/admin/settings", "/api/v1/auth/social/x/start", "/api/yggdrasil/authserver/authenticate"} {
		if got := preflight(path).Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s allows origin %q", path, got)
		}
	}
	for _, path := range []string{"/api/user", "/api/admin/users", "/oauth/token"} {
		hdr := preflight(path)
		if hdr.Get("Access-Control-Allow-Origin") != "*" || strings.Contains(hdr.Get("Access-Control-Allow-Headers"), CSRFHeader) {
			t.Errorf("%s: %v", path, hdr)
		}
	}
}
