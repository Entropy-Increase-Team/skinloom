package settings

import (
	"encoding/json"
	"testing"
)

// Regression: Set and the config-version watcher both reload. A slow reload that read
// an older version must never replace a newer snapshot (the site would show stale
// settings until the next poll, and browsers would be told an older version).
func TestPublishNeverRegresses(t *testing.T) {
	s := &Service{reg: NewRegistry()}
	s.cur.Store(&Snapshot{values: map[string]json.RawMessage{}, reg: s.reg})

	newer := &Snapshot{Version: 7, values: map[string]json.RawMessage{}, reg: s.reg}
	older := &Snapshot{Version: 6, values: map[string]json.RawMessage{}, reg: s.reg}

	if !s.publish(newer) {
		t.Fatal("publishing a newer snapshot must succeed")
	}
	if s.publish(older) {
		t.Fatal("publishing an older snapshot must be refused")
	}
	if got := s.Current().Version; got != 7 {
		t.Fatalf("current version = %d, want 7", got)
	}
	same := &Snapshot{Version: 7, values: map[string]json.RawMessage{"site.url": json.RawMessage(`"https://a.test"`)}, reg: s.reg}
	if !s.publish(same) {
		t.Fatal("a reload at the same version (e.g. after Touch) must still be published")
	}
}
