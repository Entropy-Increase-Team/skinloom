// Package logging builds the process logger; level and format can change at runtime.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
)

// Logger is a slog handler whose level and format can be swapped while running.
type Logger struct {
	level  slog.LevelVar
	out    io.Writer
	format atomic.Value // string
	inner  atomic.Pointer[slog.Handler]
}

// New returns a logger writing to out.
func New(out io.Writer, level, format string) (*Logger, *slog.Logger) {
	l := &Logger{out: out}
	l.Configure(level, format)
	return l, slog.New(l)
}

// Configure applies level and format; safe to call concurrently.
func (l *Logger) Configure(level, format string) {
	l.level.Set(ParseLevel(level))
	format = strings.ToLower(format)
	if prev, _ := l.format.Load().(string); prev == format && l.inner.Load() != nil {
		return
	}
	opts := &slog.HandlerOptions{Level: &l.level}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(l.out, opts)
	} else {
		h = slog.NewTextHandler(l.out, opts)
	}
	l.format.Store(format)
	l.inner.Store(&h)
}

// ParseLevel maps a config string to a slog level, defaulting to info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (l *Logger) handler() slog.Handler { return *l.inner.Load() }

func (l *Logger) Enabled(ctx context.Context, lv slog.Level) bool {
	return l.handler().Enabled(ctx, lv)
}

func (l *Logger) Handle(ctx context.Context, r slog.Record) error {
	return l.handler().Handle(ctx, r)
}

// WithAttrs and WithGroup return handlers that follow the root's live handler, so hot
// format changes also reach component loggers created at startup.
func (l *Logger) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &derived{root: l, attrs: attrs}
}

func (l *Logger) WithGroup(name string) slog.Handler {
	return &derived{root: l, group: name}
}

// derived keeps following the root's live handler. The handler built for the current
// root (root handler + this chain's groups/attrs) is cached and rebuilt only after a
// Configure, so a log call costs the same as with a plain slog handler.
type derived struct {
	root   *Logger
	parent *derived
	attrs  []slog.Attr
	group  string
	cache  atomic.Pointer[builtHandler]
}

type builtHandler struct {
	base *slog.Handler // root handler this was built from
	h    slog.Handler
}

func (d *derived) build() slog.Handler {
	base := d.root.inner.Load()
	if c := d.cache.Load(); c != nil && c.base == base {
		return c.h
	}
	var h slog.Handler
	if d.parent != nil {
		h = d.parent.build()
	} else {
		h = *base
	}
	if d.group != "" {
		h = h.WithGroup(d.group)
	}
	if len(d.attrs) > 0 {
		h = h.WithAttrs(d.attrs)
	}
	d.cache.Store(&builtHandler{base: base, h: h})
	return h
}

func (d *derived) Enabled(ctx context.Context, lv slog.Level) bool {
	return d.root.handler().Enabled(ctx, lv)
}

func (d *derived) Handle(ctx context.Context, r slog.Record) error { return d.build().Handle(ctx, r) }

func (d *derived) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &derived{root: d.root, parent: d, attrs: attrs}
}

func (d *derived) WithGroup(name string) slog.Handler {
	return &derived{root: d.root, parent: d, group: name}
}
