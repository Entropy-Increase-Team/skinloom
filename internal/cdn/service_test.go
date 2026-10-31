package cdn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

func newSettings(t *testing.T) (*settings.Service, *hook.Bus) {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	database, err := db.Open(ctx, db.Options{Driver: "sqlite", DSN: "t.db", DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(ctx, database, dir, log); err != nil {
		t.Fatal(err)
	}
	bus := hook.NewBus(log, 1)
	t.Cleanup(func() { bus.Close(context.Background()) })
	reg := settings.NewRegistry()
	if err := reg.Register(SettingDefs()...); err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(reg, database.Ent, pubsub.New(), bus, log)
	if err := svc.Load(ctx); err != nil {
		t.Fatal(err)
	}
	return svc, bus
}

func set(t *testing.T, s *settings.Service, v map[string]any) {
	t.Helper()
	changes := map[string]json.RawMessage{}
	for k, x := range v {
		changes[k], _ = json.Marshal(x)
	}
	if _, err := s.Set(context.Background(), nil, changes, nil); err != nil {
		t.Fatal(err)
	}
}

// Player events are listened to (and so written to the outbox) only while a complete
// CDN setup is in effect: an incomplete one would fill the outbox with failing events.
func TestServiceFollowsSettings(t *testing.T) {
	s, bus := newSettings(t)
	var mu sync.Mutex
	var got []string
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URLs []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		got = append(got, in.URLs...)
		mu.Unlock()
	}))
	defer hookSrv.Close()

	c := NewService(bus, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if hook.OnPlayerUpdated.Listening(bus) {
		t.Fatal("listening without a CDN")
	}
	if err := c.Purge(context.Background(), []string{"Steve"}); !errors.Is(err, ErrOff) {
		t.Fatalf("purge without a CDN: %v", err)
	}

	set(t, s, map[string]any{"cdn.provider": Webhook, "cdn.webhook.url": hookSrv.URL})
	if err := c.Purge(context.Background(), []string{"Steve"}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("purge without a base URL: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // the settings change is delivered on the bus
	if hook.OnPlayerUpdated.Listening(bus) {
		t.Fatal("listening while the CDN settings are incomplete")
	}
	set(t, s, map[string]any{"cdn.base_url": "https://skin.example.com/"})
	eventually(t, func() bool { return hook.OnPlayerUpdated.Listening(bus) })
	if err := c.Purge(context.Background(), []string{"Steve", "Alex"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(got) != 12 || got[0] != "https://skin.example.com/Steve.json" {
		t.Fatalf("purged %v", got)
	}
	mu.Unlock()

	set(t, s, map[string]any{"cdn.provider": None})
	eventually(t, func() bool { return !hook.OnPlayerUpdated.Listening(bus) })
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
