package account

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := NewLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := range 3 {
		if _, blocked := l.Blocked("k"); blocked {
			t.Fatalf("blocked after %d failures", i)
		}
		l.Fail("k")
	}
	wait, blocked := l.Blocked("k")
	if !blocked || wait != time.Minute {
		t.Fatalf("Blocked = %v, %v; want blocked for the rest of the window", wait, blocked)
	}
	if _, blocked := l.Blocked("other"); blocked {
		t.Fatal("keys are independent")
	}
	now = now.Add(time.Minute)
	if _, blocked := l.Blocked("k"); blocked {
		t.Fatal("the window ended")
	}
	l.Fail("k")
	l.Reset("k")
	if len(l.hits) != 0 {
		t.Fatal("Reset must forget the key")
	}
}

func TestLimiterBoundsMemory(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := NewLimiter(5, time.Minute)
	l.now = func() time.Time { return now }
	for i := range 5000 {
		l.Fail(fmt.Sprint("old", i))
	}
	now = now.Add(2 * time.Minute) // all of those windows are over
	// The map may grow to twice its last live size (8192 here) before a sweep, so
	// crossing that line must leave only the 4000 live keys.
	for i := range 4000 {
		l.Fail(fmt.Sprint("new", i))
	}
	if len(l.hits) != 4000 {
		t.Fatalf("expired keys must be swept, %d left", len(l.hits))
	}
}

func TestIPKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":          "203.0.113.9",
		"::ffff:203.0.113.9":   "203.0.113.9",
		"2001:db8:1:2:3:4:5:6": "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff::1": "2001:db8:1:2::/64",
		"not-an-ip":            "not-an-ip",
	} {
		if got := ipKey(in); got != want {
			t.Errorf("ipKey(%q) = %q, want %q", in, got, want)
		}
	}
}
