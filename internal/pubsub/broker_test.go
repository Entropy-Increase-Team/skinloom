package pubsub

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("t", 1)
	defer cancel()
	b.Publish("t", 1)
	b.Publish("t", 2) // buffer full: dropped, never blocks
	b.Publish("other", 3)
	if m := <-ch; m.Data != 1 {
		t.Fatalf("got %v", m.Data)
	}
	select {
	case m := <-ch:
		t.Fatalf("unexpected message %v", m)
	default:
	}
}

func TestCancelIsIdempotent(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("t", 1)
	cancel()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel must be closed after cancel")
	}
	b.Publish("t", 1) // no panic after cancel
}

func TestCloseEndsSubscriptions(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("t", 1)
	defer cancel() // cancel after Close must not panic
	b.Close()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("want closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not close subscriptions")
	}
	late, lateCancel := b.Subscribe("t", 1)
	defer lateCancel()
	if _, ok := <-late; ok {
		t.Fatal("subscribing after Close must return a closed channel")
	}
	b.Publish("t", 1)
	b.Close()
}
