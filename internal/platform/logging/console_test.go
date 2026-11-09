package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 11, 9, 15, 4, 5, 0, time.Local)

func consoleLine(t *testing.T, color bool, level slog.Level, h func(slog.Handler) slog.Handler, msg string, args ...any) string {
	t.Helper()
	var buf bytes.Buffer
	var lv slog.LevelVar
	lv.Set(slog.LevelDebug)
	var handler slog.Handler = newConsoleHandler(&buf, &lv, color)
	if h != nil {
		handler = h(handler)
	}
	r := slog.NewRecord(at, level, msg, 0)
	r.Add(args...)
	if err := handler.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestConsoleLine(t *testing.T) {
	web := func(h slog.Handler) slog.Handler {
		return h.WithAttrs([]slog.Attr{slog.String("component", "web")})
	}
	for _, c := range []struct {
		name  string
		level slog.Level
		h     func(slog.Handler) slog.Handler
		msg   string
		args  []any
		want  string
	}{
		{"short time, padded level, message, attributes", slog.LevelInfo, nil, "settings updated",
			[]any{"version", 3}, "15:04:05 INFO  settings updated  version=3\n"},
		{"component becomes a tag", slog.LevelWarn, web, "frontend exited",
			[]any{"code", 1}, "15:04:05 WARN  [web] frontend exited  code=1\n"},
		{"values with spaces are quoted", slog.LevelError, nil, "send mail",
			[]any{"err", errors.New("dial tcp: refused")}, "15:04:05 ERROR send mail  err=\"dial tcp: refused\"\n"},
		{"notice has its own name", LevelNotice, nil, "SkinLoom is running",
			[]any{"url", "http://localhost:17546/"}, "15:04:05 NOTE  SkinLoom is running  url=http://localhost:17546/\n"},
		{"no attributes, no trailing spaces", slog.LevelDebug, nil, "tick", nil, "15:04:05 DEBUG tick\n"},
		{"groups prefix keys", slog.LevelInfo, func(h slog.Handler) slog.Handler {
			return h.WithGroup("req").WithAttrs([]slog.Attr{slog.String("path", "/api")})
		}, "done", []any{"status", 200}, "15:04:05 INFO  done  req.path=/api req.status=200\n"},
		{"empty strings stay visible", slog.LevelInfo, nil, "x", []any{"name", ""}, "15:04:05 INFO  x  name=\"\"\n"},
		{"durations read as durations", slog.LevelInfo, nil, "migrated", []any{"took", 1500 * time.Millisecond},
			"15:04:05 INFO  migrated  took=1.5s\n"},
		{"messages of one line", slog.LevelWarn, nil, "line one\nline two", nil, "15:04:05 WARN  line one line two\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := consoleLine(t, false, c.level, c.h, c.msg, c.args...); got != c.want {
				t.Errorf("\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestConsoleColor(t *testing.T) {
	plain := consoleLine(t, false, slog.LevelError, nil, "boom", "err", "x")
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("escape codes without color: %q", plain)
	}
	for level, code := range map[slog.Level]string{
		slog.LevelError: "\x1b[1;31m", slog.LevelWarn: "\x1b[33m", LevelNotice: "\x1b[1;32m", slog.LevelDebug: "\x1b[2m",
	} {
		if got := consoleLine(t, true, level, nil, "m"); !strings.Contains(got, code) {
			t.Errorf("level %v: %q has no %q", level, got, code)
		}
	}
	if got := consoleLine(t, true, slog.LevelInfo, nil, "m", "err", "bad"); !strings.Contains(got, "\x1b[31mbad") {
		t.Errorf("err value not red: %q", got)
	}
}

// What the operator must see (the site address, the setup link) is a notice: it stays
// visible with log.level = warn, and only "error" hides it.
func TestNoticeSurvivesWarnLevel(t *testing.T) {
	var lv slog.LevelVar
	lv.Set(slog.LevelWarn)
	h := newConsoleHandler(&bytes.Buffer{}, &lv, false)
	ctx := context.Background()
	if h.Enabled(ctx, slog.LevelInfo) {
		t.Error("info shown at warn level")
	}
	if !h.Enabled(ctx, LevelNotice) {
		t.Error("notice hidden at warn level")
	}
	lv.Set(slog.LevelError)
	if h.Enabled(ctx, LevelNotice) {
		t.Error("notice shown at error level")
	}
}

// The console is for people, the file for grep and log tools: one logger writes the
// readable form to a terminal and key=value with full timestamps to the file.
func TestConsoleAndFileFormats(t *testing.T) {
	var console, file bytes.Buffer
	_, log := New(Options{Console: &console, File: &file, Level: "info", Format: "text", Pretty: true})
	log.With("component", "migrate").Log(context.Background(), LevelNotice, "first run: open this link", "url", "http://x/setup?token=t")

	if c := console.String(); !strings.Contains(c, "NOTE  [migrate] first run: open this link  url=http://x/setup?token=t") || strings.Contains(c, "time=") {
		t.Errorf("console: %q", c)
	}
	f := file.String()
	for _, want := range []string{"time=", "level=NOTICE", "component=migrate", `url="http://x/setup?token=t"`} {
		if !strings.Contains(f, want) {
			t.Errorf("file has no %q: %q", want, f)
		}
	}
	if strings.Contains(f, "\x1b[") {
		t.Errorf("escape codes in the file: %q", f)
	}
}

// Without a terminal (Docker, systemd, a pipe) the console gets key=value lines too,
// and JSON stays JSON everywhere; NOTICE is named in both.
func TestPlainAndJSONNameNotice(t *testing.T) {
	var buf bytes.Buffer
	ctl, log := New(Options{Console: &buf, Level: "info", Format: "text"})
	log.Log(context.Background(), LevelNotice, "running")
	if !strings.Contains(buf.String(), "level=NOTICE") {
		t.Errorf("text: %q", buf.String())
	}
	buf.Reset()
	ctl.Configure("info", "json")
	log.Log(context.Background(), LevelNotice, "running")
	if !strings.Contains(buf.String(), `"level":"NOTICE"`) {
		t.Errorf("json: %q", buf.String())
	}
}
