package plugintest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	plugin "github.com/Entropy-Increase-Team/skinloom/sdk/go"
	"github.com/Entropy-Increase-Team/skinloom/sdk/go/plugintest"
)

type samplePlugin struct {
	ctx            *plugin.Context
	settingsEvents int
	cronRuns       int
	lifecycleLog   []string
}

func (p *samplePlugin) Setup(ctx *plugin.Context) error {
	p.ctx = ctx

	if db := ctx.DB(); db != nil {
		mig := fstest.MapFS{
			"migrations/001_init.sql": &fstest.MapFile{
				Data: []byte(`-- +goose Up
CREATE TABLE {{prefix}}checkins (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	note TEXT NOT NULL
);
`),
			},
		}
		if err := db.Migrate(context.Background(), mig, "migrations"); err != nil {
			return err
		}
	}

	ctx.Hooks.Guard("auth.register", func(_ context.Context, raw json.RawMessage) error {
		var payload struct {
			Email string `json:"email"`
		}
		_ = json.Unmarshal(raw, &payload)
		if strings.HasSuffix(payload.Email, "@blocked.test") {
			return plugin.Reject("domain blocked")
		}
		return nil
	})

	ctx.Hooks.Filter("user.badges", func(_ context.Context, val, _ json.RawMessage) (any, error) {
		var badges []map[string]any
		_ = json.Unmarshal(val, &badges)
		badges = append(badges, map[string]any{"text": ctx.Settings.GetString("badgeLabel")})
		return badges, nil
	})

	ctx.Events.On("user.registered", func(evCtx context.Context, raw json.RawMessage) error {
		var payload struct {
			UserID int64 `json:"userId"`
		}
		_ = json.Unmarshal(raw, &payload)
		_, err := ctx.Score.Apply(evCtx, payload.UserID, 50, "welcome", "plugin", ctx.PluginID())
		if err != nil {
			return err
		}
		_, err = ctx.Notify.Send(evCtx, []int64{payload.UserID}, "Welcome", "You got 50 bonus score!")
		return err
	})

	ctx.HTTP.Post("/checkin", func(reqCtx context.Context, req *plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
		if req.User == nil {
			return nil, plugin.Reject("login required")
		}
		var body struct {
			Note string `json:"note"`
		}
		_ = req.DecodeJSON(&body)
		if _, err := ctx.DB().Exec(reqCtx, `INSERT INTO {{prefix}}checkins (user_id, note) VALUES (?, ?)`, req.User.UserID, body.Note); err != nil {
			return nil, err
		}
		bal, err := ctx.Score.Apply(reqCtx, req.User.UserID, 10, "checkin", "plugin", ctx.PluginID())
		if err != nil {
			return nil, err
		}
		_ = ctx.KV.Set(reqCtx, "last_note", body.Note, 60)
		return plugin.JSON(http.StatusOK, map[string]any{"ok": true, "balance": bal}), nil
	}, plugin.WithAuth(plugin.AuthUser))

	ctx.HTTP.StreamFunc(http.MethodGet, "/stream/ping/:id", func(w http.ResponseWriter, r *http.Request) {
		viewer := plugin.ProxyViewer(r)
		params := plugin.StreamParams(r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: id=" + params["id"] + " role=" + viewer.Role + "\n\n"))
	}, plugin.WithAuth(plugin.AuthUser))

	ctx.Cron.Schedule("daily-reset", "0 0 * * *", func(cronCtx context.Context) error {
		p.cronRuns++
		return ctx.Audit.Record(cronCtx, "cron.ran", "system", "daily", map[string]any{"runs": p.cronRuns})
	})

	ctx.Services.Expose("ping", func(_ context.Context, caller string, args []json.RawMessage) (any, error) {
		return map[string]any{"caller": caller, "argCount": len(args)}, nil
	})

	ctx.Settings.OnChange(func(_ context.Context, _ map[string]any) error {
		p.settingsEvents++
		return nil
	})

	return nil
}

func (p *samplePlugin) OnInstall(_ context.Context) error {
	p.lifecycleLog = append(p.lifecycleLog, "install")
	return nil
}

func (p *samplePlugin) OnUpgrade(_ context.Context, from string) error {
	p.lifecycleLog = append(p.lifecycleLog, "upgrade:"+from)
	return nil
}

func TestHarnessFullPluginWorkflow(t *testing.T) {
	ctx := context.Background()
	p := &samplePlugin{}
	h := plugintest.New(t, p,
		plugintest.WithPluginID("checkin-go"),
		plugintest.WithVersion("1.2.0"),
		plugintest.WithSQL(),
		plugintest.WithSettings(map[string]any{"badgeLabel": "Early Bird"}),
		plugintest.WithHost(func(mh *plugintest.MockHost) {
			mh.Users[7] = &plugin.User{ID: 7, Email: "alice@example.com", Nickname: "Alice", Role: "user", Score: 100}
		}),
	)

	// Guard hook
	if err := h.TriggerGuard(ctx, "auth.register", map[string]any{"email": "ok@example.com"}); err != nil {
		t.Fatalf("guard unexpectedly rejected valid email: %v", err)
	}
	err := h.TriggerGuard(ctx, "auth.register", map[string]any{"email": "spam@blocked.test"})
	if rej, ok := plugin.IsRejection(err); !ok || rej.Reason != "domain blocked" {
		t.Fatalf("expected RejectionError 'domain blocked', got %v", err)
	}

	// Filter hook + settings update
	outRaw, err := h.TriggerFilter(ctx, "user.badges", []map[string]any{}, nil)
	if err != nil {
		t.Fatalf("TriggerFilter: %v", err)
	}
	if !strings.Contains(string(outRaw), "Early Bird") {
		t.Fatalf("expected badge from settings, got %s", string(outRaw))
	}
	if err := h.UpdateSettings(ctx, map[string]any{"badgeLabel": "Veteran"}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if p.settingsEvents != 1 {
		t.Fatalf("expected 1 settings change event, got %d", p.settingsEvents)
	}

	// Event hook -> Score + Notify
	if err := h.TriggerEvent(ctx, "user.registered", map[string]any{"userId": 7}); err != nil {
		t.Fatalf("TriggerEvent: %v", err)
	}
	if h.Host().Users[7].Score != 150 {
		t.Fatalf("expected score 150, got %d", h.Host().Users[7].Score)
	}
	if len(h.Host().Notifications) != 1 || h.Host().Notifications[0].Title != "Welcome" {
		t.Fatalf("unexpected notifications: %+v", h.Host().Notifications)
	}

	// HTTP JSON route -> SQLite table + Score + KV
	resp, err := h.DoHTTP(ctx, &plugin.HTTPRequest{
		Method: http.MethodPost,
		Path:   "/checkin",
		Body:   []byte(`{"note":"day-1"}`),
		User:   &plugin.Viewer{Authenticated: true, UserID: 7, Role: "user"},
	})
	if err != nil {
		t.Fatalf("DoHTTP: %v", err)
	}
	if resp.Status != http.StatusOK || !strings.Contains(string(resp.RawBody), `"balance":160`) {
		t.Fatalf("unexpected HTTP response: status=%d body=%s", resp.Status, string(resp.RawBody))
	}
	var cnt int
	if err := h.DB().QueryRow(ctx, `SELECT COUNT(*) FROM {{prefix}}checkins WHERE user_id = ?`, 7).Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("expected 1 checkin row in isolated DB, got cnt=%d err=%v", cnt, err)
	}

	// Streaming HTTP route with token verification + params + ProxyViewer
	streamReq := httptest.NewRequest(http.MethodGet, "/stream/ping/42", nil)
	streamReq.Header.Set("X-SkinLoom-User-ID", "7")
	streamReq.Header.Set("X-SkinLoom-User-Role", "admin")
	rec := h.DoStreamHTTP(streamReq)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "id=42 role=admin") {
		t.Fatalf("unexpected stream response: code=%d body=%q", rec.Code, rec.Body.String())
	}

	// Missing/wrong proxy token must be rejected directly by StreamHTTPHandler
	badReq := httptest.NewRequest(http.MethodGet, "/stream/ping/42", nil)
	badReq.Header.Set(plugin.HeaderProxyToken, "wrong")
	badRec := httptest.NewRecorder()
	h.Server().StreamHTTPHandler().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bad proxy token, got %d", badRec.Code)
	}

	// Cron + Audit + Service + Lifecycle
	if err := h.TriggerCron(ctx, "daily-reset"); err != nil {
		t.Fatalf("TriggerCron: %v", err)
	}
	if len(h.Host().Audits) != 1 || h.Host().Audits[0].Action != "cron.ran" {
		t.Fatalf("unexpected audits: %+v", h.Host().Audits)
	}
	svcOut, err := h.CallService(ctx, "ping", "other-plugin", "a", 2)
	if err != nil || !strings.Contains(string(svcOut), `"caller":"other-plugin"`) {
		t.Fatalf("CallService failed: out=%s err=%v", string(svcOut), err)
	}
	if err := h.Lifecycle(ctx, "onInstall", nil); err != nil {
		t.Fatalf("Lifecycle onInstall: %v", err)
	}
	if err := h.Lifecycle(ctx, "onUpgrade", map[string]any{"from": "1.1.0"}); err != nil {
		t.Fatalf("Lifecycle onUpgrade: %v", err)
	}
	if len(p.lifecycleLog) != 2 || p.lifecycleLog[1] != "upgrade:1.1.0" {
		t.Fatalf("unexpected lifecycleLog: %v", p.lifecycleLog)
	}
}

func TestHarnessStrictPermissions(t *testing.T) {
	ctx := context.Background()
	p := &samplePlugin{}
	_ = plugintest.New(t, p,
		plugintest.WithPluginID("strict-demo"),
		plugintest.WithPermissions("storage.kv"), // intentionally omitting score.write
		plugintest.WithHost(func(mh *plugintest.MockHost) {
			mh.Users[1] = &plugin.User{ID: 1, Email: "u@example.com", Score: 10}
		}),
	)

	// KV succeeds
	if err := p.ctx.KV.Set(ctx, "k", "v", 0); err != nil {
		t.Fatalf("expected KV.Set to succeed: %v", err)
	}
	// Score fails with PermissionDenied
	_, err := p.ctx.Score.Apply(ctx, 1, 5, "test", "plugin", "strict-demo")
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for Score.Apply, got %v", err)
	}
}
