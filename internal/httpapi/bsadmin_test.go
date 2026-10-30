package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// patOf creates a personal access token for b with the given scopes.
func patOf(t *testing.T, b *browser, scopes ...string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "admin script", "scopes": scopes, "expiresInDays": 0})
	rec := b.do("POST", "/api/v1/me/tokens", string(body))
	var out NewPersonalToken
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusCreated {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	return "Bearer " + out.Token
}

func TestBSAdminAPI(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "auth.registrations_per_ip": 0, "score.per_closet_item": 0}`)
	h, _ := New(d)
	root := &browser{t: t, h: h}
	setupSite(t, d, root) // the super administrator
	alice := registerUser(t, h, "alice@example.com")
	bob := registerUser(t, h, "bob@example.com")
	aliceID, bobID := userID(t, alice), userID(t, bob)
	all := []string{"UsersManagement.ReadWrite", "PlayersManagement.ReadWrite", "ClosetManagement.ReadWrite", "ReportsManagement.ReadWrite", "Notification.ReadWrite"}
	super := patOf(t, root, all...)
	call := func(token, method, path, body string) (int, map[string]any) {
		t.Helper()
		rec := app(t, h, method, path, "application/json", body, "Authorization", token)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	ok := func(code int, out map[string]any, what string) {
		t.Helper()
		if code != http.StatusOK || out["code"] != float64(0) {
			t.Fatalf("%s: %d %v", what, code, out)
		}
	}

	// A user cannot use administration scopes.
	if rec := alice.do("POST", "/api/v1/me/tokens", `{"name":"x","scopes":["UsersManagement.Read"]}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("user with an admin scope: %d", rec.Code)
	}
	userToken := patOf(t, alice, "User.Read")
	if code, _ := call(userToken, "GET", "/api/admin/users", ""); code != http.StatusForbidden {
		t.Fatalf("user token on admin api: %d", code)
	}

	// Users.
	code, page := call(super, "GET", "/api/admin/users?q=email:alice", "")
	data, _ := page["data"].([]any)
	if code != http.StatusOK || page["total"] != float64(1) || page["per_page"] != float64(10) || len(data) != 1 || data[0].(map[string]any)["email"] != "alice@example.com" {
		t.Fatalf("list users: %d %v", code, page)
	}
	if _, page := call(super, "GET", "/api/admin/users?page=2", ""); page["current_page"] != float64(2) || len(page["data"].([]any)) != 0 {
		t.Fatalf("page 2: %v", page)
	}
	u := fmt.Sprintf("/api/admin/users/%d/", aliceID)
	code, out := call(super, "PUT", u+"nickname", `{"nickname":"Alice A."}`)
	ok(code, out, "nickname")
	code, out = call(super, "PUT", u+"score", `{"score":500}`)
	ok(code, out, "score")
	code, out = call(super, "PUT", u+"verification", ``)
	ok(code, out, "verification")
	var me UserView
	_ = json.Unmarshal(alice.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	if me.Nickname != "Alice A." || me.Score != 500 || !me.EmailVerified {
		t.Fatalf("after updates: %+v", me)
	}
	if code, out := call(super, "PUT", u+"score", `{"score":"lots"}`); code != http.StatusUnprocessableEntity || out["errors"] == nil {
		t.Fatalf("bad score: %d %v", code, out)
	}
	if code, _ := call(super, "PUT", "/api/admin/users/999999/nickname", `{"nickname":"x"}`); code != http.StatusNotFound {
		t.Fatalf("no such user: %d", code)
	}
	code, out = call(super, "PUT", u+"permission", `{"permission":-1}`)
	ok(code, out, "ban")
	if rec := app(t, h, "GET", "/api/user", "", "", "Authorization", userToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a ban revokes the user's tokens: %d", rec.Code)
	}
	code, out = call(super, "PUT", u+"permission", `{"permission":"0"}`)
	ok(code, out, "unban")
	if rec := alice.do("POST", "/api/v1/auth/login", `{"email":"alice@example.com","password":"user-pass-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("sign in again after the ban: %d %s", rec.Code, rec.Body)
	}
	code, out = call(super, "PUT", fmt.Sprintf("/api/admin/users/%d/permission", bobID), `{"permission":1}`)
	ok(code, out, "make bob an administrator")
	if code, _ := call(super, "PUT", u+"permission", `{"permission":2}`); code != http.StatusUnprocessableEntity {
		t.Fatalf("super administrator through the api: %d", code)
	}
	// An administrator manages ordinary users only, and not themself.
	bobToken := patOf(t, bob, "UsersManagement.ReadWrite")
	if code, out := call(bobToken, "PUT", fmt.Sprintf("/api/admin/users/%d/permission", bobID), `{"permission":0}`); code != http.StatusForbidden || out["code"] != float64(1) {
		t.Fatalf("own permission: %d %v", code, out)
	}
	if code, _ := call(bobToken, "PUT", "/api/admin/users/1/nickname", `{"nickname":"x"}`); code != http.StatusForbidden {
		t.Fatalf("an administrator changes the super administrator: %d", code)
	}
	code, out = call(bobToken, "PUT", u+"nickname", `{"nickname":"Alice"}`)
	ok(code, out, "an administrator changes a user")

	// Players.
	if rec := alice.do("POST", "/api/v1/me/players", `{"name":"Steve"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	pid := firstPlayerID(t, alice)
	code, page = call(super, "GET", fmt.Sprintf("/api/admin/players?q=uid:%d", aliceID), "")
	if code != http.StatusOK || page["total"] != float64(1) {
		t.Fatalf("players of alice: %d %v", code, page)
	}
	if _, page := call(super, "GET", "/api/admin/players?q=uid:0", ""); page["total"] != float64(0) {
		t.Fatalf("uid 0: %v", page)
	}
	p := fmt.Sprintf("/api/admin/players/%d", pid)
	code, out = call(super, "PUT", p+"/name", `{"player_name":"Steve2"}`)
	ok(code, out, "rename")
	code, out = call(super, "PUT", p+"/textures", `{"tid":999999,"type":"skin"}`)
	if out["code"] != float64(1) || !strings.Contains(fmt.Sprint(out["message"]), "999999") {
		t.Fatalf("no such texture: %d %v", code, out)
	}
	if code, out := call(super, "PUT", p+"/owner", `{"uid":999999}`); out["code"] != float64(1) {
		t.Fatalf("no such owner: %d %v", code, out)
	}
	code, out = call(super, "PUT", p+"/owner", fmt.Sprintf(`{"uid":%d}`, bobID))
	ok(code, out, "transfer")
	if firstPlayerID(t, bob) != pid {
		t.Fatal("bob owns the player")
	}
	// Bob (an administrator) now owns it: another administrator cannot manage it.
	other := patOf(t, root, "PlayersManagement.ReadWrite")
	code, out = call(other, "DELETE", p, "")
	ok(code, out, "the super administrator deletes an administrator's player")

	// Closets.
	up := upload(t, alice, "Mine", "steve", true, testPNG(t, 64, 64, 6))
	var uploaded UploadView
	_ = json.Unmarshal(up.Body.Bytes(), &uploaded)
	tid := uploaded.Texture.ID
	c := fmt.Sprintf("/api/admin/closet/%d", bobID)
	code, out = call(super, "POST", c, fmt.Sprintf(`{"tid":%d}`, tid))
	ok(code, out, "give")
	if tex := out["data"].(map[string]any)["texture"].(map[string]any); tex["tid"] != float64(tid) {
		t.Fatalf("given texture: %v", out)
	}
	if _, out := call(super, "POST", c, fmt.Sprintf(`{"tid":%d}`, tid)); out["code"] != float64(1) {
		t.Fatalf("twice: %v", out)
	}
	rec := app(t, h, "GET", c, "", "", "Authorization", super)
	var closet []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &closet)
	if len(closet) != 1 || closet[0]["pivot"].(map[string]any)["item_name"] != "Mine" {
		t.Fatalf("closet: %s", rec.Body)
	}
	code, out = call(super, "DELETE", c, fmt.Sprintf(`{"tid":%d}`, tid))
	ok(code, out, "take")
	if _, out := call(super, "DELETE", c, fmt.Sprintf(`{"tid":%d}`, tid)); out["code"] != float64(1) {
		t.Fatalf("take twice: %v", out)
	}

	// Reports.
	carol := registerUser(t, h, "carol@example.com")
	if rec := carol.do("POST", fmt.Sprintf("/api/v1/textures/%d/reports", tid), `{"reason":"not mine"}`); rec.Code != http.StatusOK {
		t.Fatalf("report: %d %s", rec.Code, rec.Body)
	}
	code, page = call(super, "GET", "/api/admin/reports?q=status:0", "")
	data, _ = page["data"].([]any)
	if code != http.StatusOK || page["total"] != float64(1) || page["per_page"] != float64(9) {
		t.Fatalf("reports: %d %v", code, page)
	}
	for _, q := range []string{"status:9", "reporter:abc", "tid:0"} {
		if _, page := call(super, "GET", "/api/admin/reports?q="+q, ""); page["total"] != float64(0) {
			t.Fatalf("impossible filter %s: %v", q, page)
		}
	}
	rep := data[0].(map[string]any)
	if rep["status"] != float64(0) || rep["informer"].(map[string]any)["email"] != "carol@example.com" || rep["texture"].(map[string]any)["tid"] != float64(tid) {
		t.Fatalf("report shape: %v", rep)
	}
	r := fmt.Sprintf("/api/admin/reports/%v", rep["id"])
	code, out = call(super, "PUT", r, `{"action":"reject"}`)
	ok(code, out, "reject")
	if out["data"].(map[string]any)["status"] != float64(2) {
		t.Fatalf("status: %v", out)
	}
	if _, out := call(super, "PUT", r, `{"action":"delete"}`); out["code"] != float64(1) {
		t.Fatalf("reviewed twice: %v", out)
	}

	// Notifications.
	code, out = call(super, "POST", "/api/admin/notifications", fmt.Sprintf(`{"receiver":"uid","uid":%d,"title":"Hi","content":"**x**"}`, aliceID))
	ok(code, out, "send")
	code, out = call(super, "POST", "/api/admin/notifications", `{"receiver":"email","email":"nobody@example.com","title":"Hi"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown email: %d %v", code, out)
	}
	code, out = call(super, "POST", "/api/admin/notifications", `{"receiver":"normal","title":"News"}`)
	ok(code, out, "announce")
	if body := alice.do("GET", "/api/v1/me/notifications", "").Body.String(); !strings.Contains(body, `"Hi"`) || !strings.Contains(body, `"News"`) {
		t.Fatalf("inbox: %s", body)
	}

	// The token stops working for administration once its user is no longer an
	// administrator.
	code, out = call(super, "PUT", fmt.Sprintf("/api/admin/users/%d/permission", bobID), `{"permission":0}`)
	ok(code, out, "demote bob")
	if code, _ := call(bobToken, "GET", "/api/admin/users", ""); code != http.StatusForbidden {
		t.Fatalf("demoted administrator's token: %d", code)
	}

	// Audited like the admin pages.
	if body := root.do("GET", "/api/v1/admin/audit?action=admin.", "").Body.String(); !strings.Contains(body, "admin.player_updated") ||
		!strings.Contains(body, "admin.closet_added") || !strings.Contains(body, "admin.report_reviewed") {
		t.Fatalf("audit: %s", body)
	}
}

// A page far past the end is empty: the offset must not overflow into a negative one
// (SQLite reads it as 0 and answers the first page again; PostgreSQL refuses it).
func TestBSPageFarPastTheEnd(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "auth.registrations_per_ip": 0, "score.per_closet_item": 0}`)
	h, _ := New(d)
	root := &browser{t: t, h: h}
	setupSite(t, d, root)
	alice := registerUser(t, h, "alice@example.com")
	upload(t, alice, "Mine", "steve", true, testPNG(t, 64, 64, 4))
	for _, c := range []struct{ token, path string }{
		{patOf(t, alice, "Closet.Read"), "/api/closet?perPage=100&page=100000000000000000"},
		{patOf(t, root, "UsersManagement.Read"), "/api/admin/users?page=1000000000000000000"},
		{patOf(t, root, "PlayersManagement.Read"), "/api/admin/players?page=1000000000000000000"},
		{patOf(t, root, "ReportsManagement.Read"), "/api/admin/reports?page=1100000000000000000"},
	} {
		rec := app(t, h, "GET", c.path, "", "", "Authorization", c.token)
		var page struct {
			Data []any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &page)
		if rec.Code != http.StatusOK || len(page.Data) != 0 {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body)
		}
	}
}
