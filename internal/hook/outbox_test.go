package hook

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/job"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
)

// Publish stores an event in the caller's transaction (it commits or rolls back with
// the change), only while a handler listens; Deliver runs the handlers with a key that
// stays the same on retries, and fails (for the queue to retry) when one fails.
func TestPublishAndDeliver(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		b := newBus(t)
		outbox := func() []*ent.Job { return d.Ent.Job.Query().Where(job.Kind(OutboxJobKind)).AllX(ctx) }

		if err := testEvent.Publish(ctx, b, d.Ent, regPayload{Email: "nobody listens"}); err != nil || len(outbox()) != 0 {
			t.Fatalf("no handler, nothing stored: %v, %d", err, len(outbox()))
		}
		if err := testEvent.Publish(ctx, nil, d.Ent, regPayload{}); err != nil {
			t.Fatal(err)
		}

		var got []string
		var keys []string
		fail := true
		if err := b.Replace("test", testEvent.Handle(0, func(ctx context.Context, p regPayload) error {
			got = append(got, p.Email)
			keys = append(keys, DeliveryKey(ctx))
			if fail {
				return errors.New("down")
			}
			return nil
		})); err != nil {
			t.Fatal(err)
		}

		tx, err := d.Ent.Tx(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := testEvent.Publish(ctx, b, tx.Client(), regPayload{Email: "rolled back"}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n := len(outbox()); n != 0 {
			t.Fatalf("a rolled back change leaves no event: %d", n)
		}

		if err := testEvent.Publish(ctx, b, d.Ent, regPayload{Email: "a@example.com"}); err != nil {
			t.Fatal(err)
		}
		list := outbox()
		if len(list) != 1 {
			t.Fatalf("%d events stored", len(list))
		}
		raw := json.RawMessage(list[0].Payload)
		if err := b.Deliver(ctx, raw); err == nil {
			t.Fatal("a failing handler fails the delivery, so the queue retries it")
		}
		fail = false
		if err := b.Deliver(ctx, raw); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[1] != "a@example.com" || keys[0] == "" || keys[0] != keys[1] {
			t.Fatalf("deliveries %v, keys %v", got, keys)
		}

		if err := b.Deliver(ctx, json.RawMessage(`{"hook":"no.such@1","payload":{}}`)); err == nil || !strings.Contains(err.Error(), "unknown event") {
			t.Fatalf("an unknown event is given up: %v", err)
		}
	})
}
