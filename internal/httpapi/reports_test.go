package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestReportsAndAdminTools(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none", "auth.registrations_per_ip": 0, "score.reporter_modification": -5, "score.reporter_reward": 10}`)
	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)
	alice := registerUser(t, h, "alice@example.com")
	bob := registerUser(t, h, "bob@example.com")
	carol := registerUser(t, h, "carol@example.com")
	var me UserView
	_ = json.Unmarshal(bob.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	bobID := me.ID

	var up UploadView
	_ = json.Unmarshal(upload(t, alice, "Alice's", "steve", true, testPNG(t, 64, 64, 1)).Body.Bytes(), &up)
	skin := up.Texture
	_ = json.Unmarshal(upload(t, carol, "Carol's", "steve", true, testPNG(t, 64, 64, 2)).Body.Bytes(), &up)
	carols := up.Texture

	// Reporting.
	path := fmt.Sprintf("/api/v1/textures/%d/reports", skin.ID)
	rec := bob.do("POST", path, `{"reason":"copied from my server"}`)
	var reported Reported
	_ = json.Unmarshal(rec.Body.Bytes(), &reported)
	if rec.Code != http.StatusOK || reported.Score != 1000-5 || reported.Report.Status != "pending" || reported.Report.Texture == nil {
		t.Fatalf("report: %d %s", rec.Code, rec.Body)
	}
	if rec := bob.do("POST", path, `{"reason":"again"}`); rec.Code != http.StatusConflict {
		t.Fatalf("twice: %d", rec.Code)
	}
	if rec := alice.do("POST", path, `{"reason":"mine"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("own texture: %d", rec.Code)
	}
	if rec := bob.do("POST", path, `{"reason":"   "}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty reason: %d", rec.Code)
	}
	if rec := (&browser{t: t, h: h}).do("POST", path, `{"reason":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", rec.Code)
	}
	var mine ReportPage
	_ = json.Unmarshal(bob.do("GET", "/api/v1/me/reports", "").Body.Bytes(), &mine)
	if len(mine.Reports) != 1 || mine.Reports[0].Reason != "copied from my server" {
		t.Fatalf("my reports: %+v", mine)
	}

	// Reviewing: administrators only.
	if rec := bob.do("GET", "/api/v1/admin/reports", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("queue as a user: %d", rec.Code)
	}
	var queue AdminReportPage
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/reports?status=pending", "").Body.Bytes(), &queue)
	if len(queue.Reports) != 1 || queue.Reports[0].Reporter == nil || queue.Reports[0].Reporter.Email != "bob@example.com" ||
		queue.Reports[0].Uploader == nil || queue.Reports[0].Uploader.Email != "alice@example.com" {
		t.Fatalf("queue: %+v", queue)
	}
	id := queue.Reports[0].ID
	rec = admin.do("POST", fmt.Sprintf("/api/v1/admin/reports/%d/review", id), `{"action":"ban"}`)
	var reviewed AdminReportView
	_ = json.Unmarshal(rec.Body.Bytes(), &reviewed)
	if rec.Code != http.StatusOK || reviewed.Status != "resolved" || reviewed.Action != "ban" || reviewed.Handler == nil {
		t.Fatalf("ban: %d %s", rec.Code, rec.Body)
	}
	if rec := alice.do("GET", "/api/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a banned uploader is signed out: %d", rec.Code)
	}
	_ = json.Unmarshal(bob.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	if me.Score != 1000+10 {
		t.Fatalf("reporter: charge back and reward: %d", me.Score)
	}
	if rec := admin.do("POST", fmt.Sprintf("/api/v1/admin/reports/%d/review", id), `{"action":"reject"}`); rec.Code != http.StatusConflict {
		t.Fatalf("reviewed twice: %d", rec.Code)
	}
	_ = json.Unmarshal(bob.do("POST", fmt.Sprintf("/api/v1/textures/%d/reports", carols.ID), `{"reason":"bad"}`).Body.Bytes(), &reported)
	if rec := admin.do("POST", fmt.Sprintf("/api/v1/admin/reports/%d/review", reported.Report.ID), `{"action":"delete"}`); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := get(t, h, nil, fmt.Sprintf("/api/v1/textures/%d", carols.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("the reported texture is gone: %d", rec.Code)
	}
	var audit AuditPage
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/audit?action=admin.report_reviewed", "").Body.Bytes(), &audit)
	if len(audit.Records) != 2 {
		t.Fatalf("reviews are audited: %d", len(audit.Records))
	}

	// Overview.
	rec = admin.do("GET", "/api/v1/admin/overview", "")
	var o AdminOverview
	_ = json.Unmarshal(rec.Body.Bytes(), &o)
	if rec.Code != http.StatusOK || o.Users != 4 || o.Textures != 1 || len(o.Days) != 31 || o.Days[30].Registrations != 4 || o.PendingReports != 0 {
		t.Fatalf("overview: %d %s", rec.Code, rec.Body)
	}
	if rec := bob.do("GET", "/api/v1/admin/overview", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("overview as a user: %d", rec.Code)
	}

	// Players.
	if rec := bob.do("POST", "/api/v1/me/players", `{"name":"Bobby"}`); rec.Code != http.StatusOK {
		t.Fatalf("create player: %d %s", rec.Code, rec.Body)
	}
	var players AdminPlayerPage
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/players?q=bob", "").Body.Bytes(), &players)
	if len(players.Players) != 1 || players.Players[0].Owner == nil || players.Players[0].Owner.ID != bobID {
		t.Fatalf("players: %+v", players)
	}
	pid := players.Players[0].ID
	rec = admin.do("PATCH", fmt.Sprintf("/api/v1/admin/players/%d", pid), `{"name":"Robert"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Robert"`) {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}
	if rec := admin.do("PATCH", fmt.Sprintf("/api/v1/admin/players/%d", pid), `{"userId":999999}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("give to nobody: %d %s", rec.Code, rec.Body)
	}
	if rec := admin.do("PUT", fmt.Sprintf("/api/v1/admin/players/%d/textures", pid), fmt.Sprintf(`{"skin":%d}`, skin.ID)); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a skin outside the owner's closet: %d %s", rec.Code, rec.Body)
	}

	// The closet API puts it there for free; then the player can wear it.
	closet := fmt.Sprintf("/api/v1/admin/users/%d/closet", bobID)
	if rec := admin.do("POST", closet, fmt.Sprintf(`{"textureId":%d}`, skin.ID)); rec.Code != http.StatusOK {
		t.Fatalf("give: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(bob.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	if me.Score != 1020-100 { // two upheld reports; the player cost 100, the closet item nothing
		t.Fatalf("a given closet item is free: %d", me.Score)
	}
	if rec := admin.do("PUT", fmt.Sprintf("/api/v1/admin/players/%d/textures", pid), fmt.Sprintf(`{"skin":%d}`, skin.ID)); rec.Code != http.StatusOK {
		t.Fatalf("wear: %d %s", rec.Code, rec.Body)
	}
	var items ClosetPage
	_ = json.Unmarshal(admin.do("GET", closet, "").Body.Bytes(), &items)
	if len(items.Items) != 1 {
		t.Fatalf("closet: %+v", items)
	}
	rec = admin.do("DELETE", fmt.Sprintf("%s/%d", closet, skin.ID), "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"refunded":0`) {
		t.Fatalf("take back: %d %s", rec.Code, rec.Body)
	}
	if rec := admin.do("DELETE", fmt.Sprintf("/api/v1/admin/players/%d", pid), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete player: %d %s", rec.Code, rec.Body)
	}
	_ = json.Unmarshal(admin.do("GET", "/api/v1/admin/audit?action=admin.", "").Body.Bytes(), &audit)
	var playerActions, closetActions int
	for _, r := range audit.Records {
		switch {
		case strings.HasPrefix(r.Action, "admin.player_"):
			playerActions++
		case strings.HasPrefix(r.Action, "admin.closet_"):
			closetActions++
		}
	}
	if playerActions != 3 || closetActions != 2 { // renamed, dressed, deleted; given, taken back
		t.Fatalf("player and closet changes are audited: %d, %d", playerActions, closetActions)
	}
}
