// Package logging builds the process logger; level and format can change at runtime.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
)

// Options say where the log goes and how it looks.
type Options struct {
	Console io.Writer // usually os.Stderr
	File    io.Writer // log.file; nil for none
	Level   string    // debug | info | warn | error
	Format  string    // text | json
	// Pretty: the console is a terminal, so text is written for people (see
	// consoleHandler) instead of as key=value. Color: and it shows ANSI colors.
	// ConsoleStyle tells both for a file.
	Pretty, Color bool
}

// Logger is a slog handler whose level and format can be swapped while running.
type Logger struct {
	level  slog.LevelVar
	opts   Options
	format atomic.Value // string
	inner  atomic.Pointer[slog.Handler]
}

// New returns a logger writing to the console and, when set, the file.
func New(o Options) (*Logger, *slog.Logger) {
	l := &Logger{opts: o}
	l.Configure(o.Level, o.Format)
	return l, slog.New(l)
}

// Configure applies level and format; safe to call concurrently.
//
// With "text" the console of a terminal gets the readable form, and pipes (Docker,
// systemd) and the log file get key=value lines with full timestamps for grep and
// log tools. "json" is JSON everywhere.
func (l *Logger) Configure(level, format string) {
	l.level.Set(ParseLevel(level))
	format = strings.ToLower(format)
	if prev, _ := l.format.Load().(string); prev == format && l.inner.Load() != nil {
		return
	}
	plain := func(w io.Writer) slog.Handler {
		opts := &slog.HandlerOptions{Level: &l.level, ReplaceAttr: nameNotice}
		if format == "json" {
			return slog.NewJSONHandler(w, opts)
		}
		return slog.NewTextHandler(w, opts)
	}
	var h slog.Handler
	if format != "json" && l.opts.Pretty {
		h = newConsoleHandler(l.opts.Console, &l.level, l.opts.Color)
	} else {
		h = plain(l.opts.Console)
	}
	if l.opts.File != nil {
		// The file first: a background service's console may be no handle at all.
		h = slog.NewMultiHandler(plain(l.opts.File), h)
	}
	l.format.Store(format)
	l.inner.Store(&h)
}

// nameNotice writes LevelNotice as NOTICE, not slog's "WARN+2".
func nameNotice(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && a.Key == slog.LevelKey {
		if lv, ok := a.Value.Any().(slog.Level); ok && lv >= LevelNotice && lv < slog.LevelError {
			a.Value = slog.StringValue("NOTICE")
		}
	}
	return a
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

// Notice logs at LevelNotice: what the operator must not miss.
func Notice(log *slog.Logger, msg string, args ...any) {
	log.Log(context.Background(), LevelNotice, msg, args...)
}
