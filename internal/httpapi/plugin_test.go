package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

func TestPluginEndToEndHTTPAPI(t *testing.T) {
	ctx := context.Background()
	d, dir := newTestDepsDir(t)
	var logBuf bytes.Buffer
	d.Log = slog.New(slog.NewTextHandler(&logBuf, nil))
	d.Bus = hook.NewBus(d.Log, 4)
	t.Cleanup(func() {
		d.Bus.Close(context.Background())
		if t.Failed() {
			t.Logf("logs:\n%s", logBuf.String())
		}
	})
	d.Accounts.SetHooks(d.Bus)
	d.Textures.SetHooks(d.Bus)
	d.Players.SetHooks(d.Bus)
	d.Reports.SetHooks(d.Bus)
	d.Yggdrasil.SetHooks(d.Bus)
	setSettings(t, d, `{"captcha.provider": "none"}`)

	q := jobs.New(d.DB.Ent, d.Log)
	q.Register(hook.OutboxJobKind, d.Bus.Deliver)
	d.Accounts.SetJobNotifier(q.Kick)

	mgr, err := plugin.NewManager(plugin.ManagerDeps{
		DB:       d.DB.Ent,
		Bus:      d.Bus,
		Settings: d.Settings,
		Notify:   d.Notify,
		DataDir:  dir,
		Log:      d.Log,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Shutdown(context.Background()) })
	d.Plugins = mgr

	manifest := []byte(`{
		"id": "invite-code",
		"kind": "plugin",
		"version": "1.0.0",
		"name": "Invite Code",
		"apiVersion": 1,
		"server": "server.js",
		"permissions": ["storage.docs", "storage.kv"],
		"hooks": {
			"guard": ["auth.register"],
			"filter": ["user.badges"],
			"event": ["user.registered"]
		},
		"storage": {
			"collections": {
				"codes": {
					"indexes": {
						"code": "string:unique",
						"usedBy": "number"
					}
				}
			}
		}
	}`)
	serverJS := []byte(`
		export default {
			setup(ctx) {
				const codes = ctx.db.collection("codes");

				ctx.hooks.guard("auth.register", async (p) => {
					const code = String((p.form && p.form.inviteCode) || "").trim();
					if (!code) return reject("invite_required");
					const row = await codes.findOne({ code });
					if (!row) return reject("invite_invalid");
					if (row.usedBy != null) return reject("invite_used");
				});

				ctx.events.on("user.registered", async (p) => {
					if (p.method === "setup") return;
					const code = String((p.form && p.form.inviteCode) || "").trim();
					if (!code) return;
					const hasSecret = Boolean(p.form && p.form.passwordSecret);
					await codes.updateOne({ code }, { usedBy: p.userId, hasSecret });
					await ctx.kv.set("lastRegistered:" + p.userId, { method: p.method, hasSecret });
				});

				ctx.hooks.filter("user.badges", (badges, u) => {
					const out = Array.isArray(badges) ? [...badges] : [];
					if (u.userId > 1) {
						out.push({ text: "Invited", color: "#2563eb" });
					}
					return out;
				});

				ctx.http.post("/codes", { auth: "admin" }, async (req) => {
					const code = String(req.body?.code || "");
					const doc = await codes.insertOne({ code, usedBy: null });
					return { status: 201, body: doc };
				});

				ctx.http.get("/codes/:code", { auth: "public" }, async (req) => {
					const doc = await codes.findOne({ code: req.params.code });
					if (!doc) return { status: 404, body: { error: "not found" } };
					return doc;
				});
			}
		};
	`)
	staticFiles := map[string][]byte{
		"web/widget.js": []byte(`export const widget = "invite-code";`),
	}
	if _, err := mgr.LoadFromBytes(ctx, manifest, serverJS, staticFiles, nil, true); err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}

	h, _ := New(d)
	admin := &browser{t: t, h: h}
	setupSite(t, d, admin)

	// 1. CSRF check on /api/plugins/* when using browser session cookie
	noCSRFBrowser := &browser{t: t, h: h, cookie: admin.cookie, headers: map[string]string{CSRFHeader: ""}}
	noCSRFRec := noCSRFBrowser.do(http.MethodPost, "/api/plugins/invite-code/codes", `{"code":"JOIN-2026"}`)
	if noCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 missing CSRF header on /api/plugins/*, got %d (%s)", noCSRFRec.Code, noCSRFRec.Body.String())
	}

	// 2. Admin creates an invite code via /api/plugins/invite-code/codes
	recCreate := admin.do("POST", "/api/plugins/invite-code/codes", `{"code":"JOIN-2026"}`)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("POST /api/plugins/invite-code/codes = %d: %s", recCreate.Code, recCreate.Body.String())
	}

	// 3. Registration without inviteCode is rejected cleanly with RFC 9457 422
	guest := &browser{t: t, h: h}
	recNoCode := guest.do("POST", "/api/v1/auth/register", `{"email":"alice@example.com","nickname":"Alice","password":"alice-pass-1"}`)
	if recNoCode.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on missing inviteCode, got %d: %s", recNoCode.Code, recNoCode.Body.String())
	}
	if !strings.Contains(recNoCode.Body.String(), "invite_required") {
		t.Fatalf("expected invite_required in 422 Problem Details, got %s", recNoCode.Body.String())
	}

	// 4. Registration with valid inviteCode succeeds, passing redacted form into user.registered
	recOK := guest.do("POST", "/api/v1/auth/register", `{
		"email": "alice@example.com",
		"nickname": "Alice",
		"password": "alice-pass-1",
		"form": { "inviteCode": "JOIN-2026", "passwordSecret": "do-not-leak" }
	}`)
	if recOK.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid invite registration, got %d: %s", recOK.Code, recOK.Body.String())
	}
	var regUser UserView
	if err := json.Unmarshal(recOK.Body.Bytes(), &regUser); err != nil {
		t.Fatalf("decode registered user: %v", err)
	}

	// Deliver outbox event user.registered synchronously in test
	if _, err := q.RunDue(ctx); err != nil {
		t.Fatalf("q.RunDue: %v", err)
	}

	// 5. Verify code is marked usedBy == regUser.ID and sensitive form field was redacted
	recCode := guest.do("GET", "/api/plugins/invite-code/codes/JOIN-2026", "")
	if recCode.Code != http.StatusOK {
		t.Fatalf("GET /api/plugins/invite-code/codes/JOIN-2026 = %d: %s", recCode.Code, recCode.Body.String())
	}
	var codeDoc map[string]any
	if err := json.Unmarshal(recCode.Body.Bytes(), &codeDoc); err != nil {
		t.Fatal(err)
	}
	if codeDoc["usedBy"] != float64(regUser.ID) {
		t.Fatalf("expected usedBy=%d, got %#v", regUser.ID, codeDoc["usedBy"])
	}
	if codeDoc["hasSecret"] != false {
		t.Fatalf("expected passwordSecret redacted from user.registered form, got %#v", codeDoc["hasSecret"])
	}

	// 6. Reusing the same invite code fails with 422 invite_used
	guest2 := &browser{t: t, h: h}
	recDup := guest2.do("POST", "/api/v1/auth/register", `{
		"email": "bob@example.com",
		"nickname": "Bob",
		"password": "bob-pass-123",
		"form": { "inviteCode": "JOIN-2026" }
	}`)
	if recDup.Code != http.StatusUnprocessableEntity || !strings.Contains(recDup.Body.String(), "invite_used") {
		t.Fatalf("expected 422 invite_used on second registration, got %d: %s", recDup.Code, recDup.Body.String())
	}

	// 7. GET /api/v1/auth/me reflects custom badge added by user.badges filter
	recMe := guest.do("GET", "/api/v1/auth/me", "")
	if recMe.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/me = %d: %s", recMe.Code, recMe.Body.String())
	}
	var me UserView
	if err := json.Unmarshal(recMe.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if len(me.Badges) != 1 || me.Badges[0].Text != "Invited" || me.Badges[0].Color != "#2563eb" {
		t.Fatalf("expected badge [{Invited #2563eb}], got %#v", me.Badges)
	}

	// 8. Static plugin assets served at /plugins-assets/{id}/* with nosniff
	recAsset := guest.do("GET", "/plugins-assets/invite-code/web/widget.js", "")
	if recAsset.Code != http.StatusOK || recAsset.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET /plugins-assets/... failed: code=%d nosniff=%q body=%s",
			recAsset.Code, recAsset.Header().Get("X-Content-Type-Options"), recAsset.Body.String())
	}
}
