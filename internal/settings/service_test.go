package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

func newService(t *testing.T, database *db.DB) (*settings.Service, *pubsub.Broker) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	broker := pubsub.New()
	bus := hook.NewBus(log, 1)
	t.Cleanup(func() { bus.Close(context.Background()) })
	svc := settings.NewService(settings.NewRegistry(), database.Ent, broker, bus, log)
	if err := svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc, broker
}

// forEachDB runs fn against SQLite and, when CI provides them, MySQL and PostgreSQL
// (SKINLOOM_TEST_MYSQL / SKINLOOM_TEST_POSTGRES hold DSNs of disposable databases).
// This also proves the per-dialect migrations apply and behave the same.
func forEachDB(t *testing.T, fn func(t *testing.T, database *db.DB)) {
	targets := []struct{ driver, dsn string }{{"sqlite", "test.db"}}
	if dsn := os.Getenv("SKINLOOM_TEST_MYSQL"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"mysql", dsn})
	}
	if dsn := os.Getenv("SKINLOOM_TEST_POSTGRES"); dsn != "" {
		targets = append(targets, struct{ driver, dsn string }{"postgres", dsn})
	}
	for _, tg := range targets {
		t.Run(tg.driver, func(t *testing.T) {
			fn(t, openDB(t, tg.driver, tg.dsn))
		})
	}
}

func openDB(t *testing.T, driver, dsn string) *db.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), db.Options{Driver: driver, DSN: dsn, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := migrate.Up(context.Background(), database, dir, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if driver != "sqlite" { // shared server database: start every test from empty tables
		for _, table := range []string{"settings", "system_state"} {
			if _, err := database.SQL.Exec("DELETE FROM " + table); err != nil {
				t.Fatal(err)
			}
		}
	}
	return database
}

func TestDefaultsAndSet(t *testing.T) { forEachDB(t, testDefaultsAndSet) }

func testDefaultsAndSet(t *testing.T, database *db.DB) {
	ctx := context.Background()
	svc, broker := newService(t, database)
	changes, cancel := broker.Subscribe(pubsub.TopicConfigChanged, 4)
	defer cancel()

	snap := svc.Current()
	if got := snap.Localized("site.name", "en"); got != "SkinLoom" {
		t.Fatalf("default en name = %q", got)
	}
	if snap.IsSet("site.name") {
		t.Fatal("default must not count as set")
	}

	snap, err := svc.Set(ctx, nil, map[string]json.RawMessage{
		"site.name": json.RawMessage(`{"zh-CN":"测试","en":"Test"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != 1 || snap.Localized("site.name", "zh-CN") != "测试" {
		t.Fatalf("after set: version %d, name %q", snap.Version, snap.Localized("site.name", "zh-CN"))
	}
	select {
	case msg := <-changes:
		if msg.Data.(int64) != 1 {
			t.Fatalf("broadcast version %v", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("no change broadcast")
	}

	snap, err = svc.Set(ctx, nil, map[string]json.RawMessage{"site.name": json.RawMessage(`null`)})
	if err != nil {
		t.Fatal(err)
	}
	if snap.IsSet("site.name") || snap.Version != 2 {
		t.Fatalf("reset failed: set=%v version=%d", snap.IsSet("site.name"), snap.Version)
	}
}

func TestValidationRejectsWholeUpdate(t *testing.T) { forEachDB(t, testValidationRejectsWholeUpdate) }

func testValidationRejectsWholeUpdate(t *testing.T, database *db.DB) {
	svc, _ := newService(t, database)
	_, err := svc.Set(context.Background(), nil, map[string]json.RawMessage{
		"site.description": json.RawMessage(`{"en":"ok"}`),
		"site.url":         json.RawMessage(`"ftp://nope"`),
		"no.such":          json.RawMessage(`1`),
	})
	var verr *settings.ValidationError
	if !errors.As(err, &verr) || len(verr.Fields) != 2 {
		t.Fatalf("want 2 field errors, got %v", err)
	}
	if svc.Current().IsSet("site.description") || svc.Current().Version != 0 {
		t.Fatal("a rejected update must not write anything")
	}
}

func TestWatchPicksUpOtherWriters(t *testing.T) { forEachDB(t, testWatchPicksUpOtherWriters) }

func testWatchPicksUpOtherWriters(t *testing.T, database *db.DB) {
	writer, _ := newService(t, database)
	reader, broker := newService(t, database)
	changes, cancel := broker.Subscribe(pubsub.TopicConfigChanged, 4)
	defer cancel()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go reader.Watch(ctx, 20*time.Millisecond)

	if _, err := writer.Set(context.Background(), nil, map[string]json.RawMessage{
		"site.url": json.RawMessage(`"https://skin.example.com"`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not notice the change")
	}
	if got := reader.Current().String("site.url"); got != "https://skin.example.com" {
		t.Fatalf("reader sees %q", got)
	}
}

func TestPickLocalized(t *testing.T) {
	m := map[string]string{"zh-CN": "中文", "en": "English"}
	cases := map[string]string{"zh-CN": "中文", "zh-TW": "中文", "en-US": "English", "ja": "English"}
	for locale, want := range cases {
		if got := settings.PickLocalized(m, locale); got != want {
			t.Errorf("PickLocalized(%s) = %q, want %q", locale, got, want)
		}
	}
}
