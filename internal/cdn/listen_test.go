package cdn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
)

// sync registers the purge handlers only when the provider can be built. Registered
// while cdn.provider was set but its API token or base URL missing, every player change
// went to the outbox and every delivery failed with ErrIncomplete: retried 5 times over
// ~1h20m, re-running the event's other handlers each time.
func TestIncompleteProviderDoesNotListen(t *testing.T) {
	s, bus := newSettings(t)
	c := NewService(bus, s, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	set(t, s, map[string]any{"cdn.provider": Cloudflare, "cdn.base_url": "https://skin.example.com"}) // no zone, no token
	// settings.changed reaches sync asynchronously: give it time to (wrongly) register.
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && !hook.OnPlayerUpdated.Listening(bus); {
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := ConfigFrom(s.Current()).Purger(nil); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("setup: %v", err)
	}
	if hook.OnPlayerUpdated.Listening(bus) {
		raw, _ := json.Marshal(hook.PlayerEvent{Name: "Steve"})
		env, _ := json.Marshal(map[string]any{"hook": hook.OnPlayerUpdated.Def().ID(), "key": "k", "payload": json.RawMessage(raw)})
		err := bus.Deliver(context.Background(), env)
		t.Errorf("an incomplete CDN setup listens to player events; each delivery fails and is retried: %v", err)
	}
}
