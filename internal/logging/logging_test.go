package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestDerivedLoggersFollowHotReconfigure(t *testing.T) {
	var buf bytes.Buffer
	ctl, log := New(&buf, "info", "text")
	comp := log.With("component", "web").WithGroup("g").With("k", "v")

	comp.Debug("hidden")
	comp.Info("first")
	if strings.Contains(buf.String(), "hidden") {
		t.Fatal("debug line emitted at info level")
	}
	if !strings.Contains(buf.String(), "component=web") || !strings.Contains(buf.String(), "g.k=v") {
		t.Fatalf("attrs/groups lost: %q", buf.String())
	}

	buf.Reset()
	ctl.Configure("debug", "json")
	comp.Debug("second")
	out := buf.String()
	if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"component":"web"`) || !strings.Contains(out, `"g":{"k":"v"`) {
		t.Fatalf("derived logger did not switch to JSON at debug: %q", out)
	}
}

func BenchmarkDerivedLogger(b *testing.B) {
	var buf bytes.Buffer
	_, log := New(&buf, "info", "json")
	comp := log.With("component", "http").With("x", 1)
	b.ReportAllocs()
	for b.Loop() {
		buf.Reset()
		comp.Info("request", "path", "/api")
	}
}
