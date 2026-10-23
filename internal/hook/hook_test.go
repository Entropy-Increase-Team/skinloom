package hook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type regPayload struct{ Email string }

var (
	testGuard  = NewGuard[regPayload]("test.register", 1, FailClosed, "")
	testOpen   = NewGuard[regPayload]("test.open", 1, FailOpen, "")
	testFilter = NewFilter[string, regPayload]("test.name", 1, "")
	testEvent  = NewEvent[regPayload]("test.registered", 1, "")
)

func newBus(t *testing.T) *Bus {
	t.Helper()
	b := NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)), 2)
	t.Cleanup(func() { b.Close(context.Background()) })
	return b
}

func TestGuardRejectsWithOwner(t *testing.T) {
	b := newBus(t)
	err := b.Replace("plugin:invites", testGuard.Handle(0, func(_ context.Context, p regPayload) error {
		if !strings.HasSuffix(p.Email, "@ok.test") {
			return Reject("domain not allowed")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := testGuard.Check(context.Background(), b, regPayload{Email: "a@ok.test"}); err != nil {
		t.Fatalf("allowed email rejected: %v", err)
	}
	err = testGuard.Check(context.Background(), b, regPayload{Email: "a@bad.test"})
	r, ok := AsRejection(err)
	if !ok || r.Reason != "domain not allowed" || r.Owner != "plugin:invites" {
		t.Fatalf("want rejection from plugin:invites, got %#v", err)
	}
}

func TestGuardFailPolicies(t *testing.T) {
	b := newBus(t)
	boom := func(context.Context, regPayload) error { panic("boom") }
	if err := b.Replace("x", testGuard.Handle(0, boom), testOpen.Handle(0, boom)); err != nil {
		t.Fatal(err)
	}
	if err := testGuard.Check(context.Background(), b, regPayload{}); !errors.Is(err, ErrHookFailed) {
		t.Fatalf("fail-closed guard: want ErrHookFailed, got %v", err)
	}
	if err := testOpen.Check(context.Background(), b, regPayload{}); err != nil {
		t.Fatalf("fail-open guard: want nil, got %v", err)
	}
}

func TestFilterOrderAndFailureSkips(t *testing.T) {
	b := newBus(t)
	_ = b.Replace("a", testFilter.Handle(20, func(_ context.Context, v string, _ regPayload) (string, error) { return v + "-a", nil }))
	_ = b.Replace("b", testFilter.Handle(10, func(_ context.Context, v string, _ regPayload) (string, error) { return v + "-b", nil }),
		testFilter.Handle(15, func(_ context.Context, v string, _ regPayload) (string, error) { return "", errors.New("bad") }))
	if got := testFilter.Apply(context.Background(), b, "x", regPayload{}); got != "x-b-a" {
		t.Fatalf("got %q, want x-b-a", got)
	}
}

func TestReplaceSwapsOwnerAtomically(t *testing.T) {
	b := newBus(t)
	_ = b.Replace("p", testFilter.Handle(0, func(_ context.Context, v string, _ regPayload) (string, error) { return v + "1", nil }))
	_ = b.Replace("p", testFilter.Handle(0, func(_ context.Context, v string, _ regPayload) (string, error) { return v + "2", nil }))
	if got := testFilter.Apply(context.Background(), b, "v", regPayload{}); got != "v2" {
		t.Fatalf("got %q, want v2 (old version must be gone)", got)
	}
	b.Remove("p")
	if got := testFilter.Apply(context.Background(), b, "v", regPayload{}); got != "v" {
		t.Fatalf("got %q after Remove, want v", got)
	}
}

func TestEventDeliveredAsync(t *testing.T) {
	b := newBus(t)
	var wg sync.WaitGroup
	wg.Add(1)
	got := make(chan string, 1)
	_ = b.Replace("m", testEvent.Handle(0, func(_ context.Context, p regPayload) error {
		defer wg.Done()
		got <- p.Email
		return nil
	}))
	testEvent.Emit(b, regPayload{Email: "e@x.test"})
	select {
	case v := <-got:
		if v != "e@x.test" {
			t.Fatalf("got %q", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event not delivered")
	}
	wg.Wait()
}

func TestUnknownHookRejected(t *testing.T) {
	b := newBus(t)
	r := Registration{h: handler{hook: "no.such.hook"}}
	if err := b.Replace("x", r); err == nil {
		t.Fatal("want error for unknown hook")
	}
}
