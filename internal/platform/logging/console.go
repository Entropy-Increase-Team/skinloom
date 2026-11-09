package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// LevelNotice is for what the operator must see: the site's address, the one-time
// setup link, shutting down. It sits between WARN and ERROR so that log.level = warn
// still shows it; only "error" hides it.
const LevelNotice = slog.LevelWarn + 2

// ANSI styles of the console format.
const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	cyan   = "\x1b[36m"
	bold   = "\x1b[1m"
)

// levelLabel names a level in five columns, and gives its color.
func levelLabel(lv slog.Level) (string, string) {
	switch {
	case lv >= slog.LevelError:
		return "ERROR", "\x1b[1;31m"
	case lv >= LevelNotice:
		return "NOTE ", "\x1b[1;32m"
	case lv >= slog.LevelWarn:
		return "WARN ", yellow
	case lv >= slog.LevelInfo:
		return "INFO ", cyan
	default:
		return "DEBUG", dim
	}
}

// consoleHandler writes one line per record for people at a terminal:
//
//	15:04:05 WARN  [web] frontend exited  code=1
//
// Short time, a colored level, the component as a tag, the message, then the
// attributes. The log file and pipes keep slog's key=value form (see Logger).
type consoleHandler struct {
	out   io.Writer
	mu    *sync.Mutex
	level slog.Leveler
	color bool
	tag   string // the component attribute
	attrs []byte // attributes added by WithAttrs, rendered
	group string // key prefix from WithGroup, "a.b."
}

func newConsoleHandler(out io.Writer, level slog.Leveler, color bool) *consoleHandler {
	return &consoleHandler{out: out, mu: &sync.Mutex{}, level: level, color: color}
}

func (h *consoleHandler) Enabled(_ context.Context, lv slog.Level) bool {
	return lv >= h.level.Level()
}

func (h *consoleHandler) Handle(_ context.Context, r slog.Record) error {
	label, style := levelLabel(r.Level)
	b := make([]byte, 0, 160)
	b = h.paint(b, dim, r.Time.Format(time.TimeOnly))
	b = append(b, ' ')
	b = h.paint(b, style, label)
	b = append(b, ' ')
	if h.tag != "" {
		b = h.paint(b, dim, "["+h.tag+"]")
		b = append(b, ' ')
	}
	msg := oneLine(r.Message)
	if r.Level >= LevelNotice {
		b = h.paint(b, bold, msg)
	} else {
		b = append(b, msg...)
	}
	attrs := h.attrs
	r.Attrs(func(a slog.Attr) bool {
		attrs = h.appendAttr(attrs, h.group, a)
		return true
	})
	if len(attrs) > 0 {
		b = append(b, ' ') // two spaces between the message and its attributes
		b = append(b, attrs...)
	}
	b = append(b, '\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.out.Write(b)
	return err
}

func (h *consoleHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append([]byte(nil), h.attrs...)
	for _, a := range as {
		if a.Key == "component" && h.group == "" && c.tag == "" {
			c.tag = a.Value.String()
			continue
		}
		c.attrs = c.appendAttr(c.attrs, h.group, a)
	}
	return &c
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	c := *h
	c.group = h.group + name + "."
	return &c
}

// appendAttr renders " key=value" (groups as prefix.key=value).
func (h *consoleHandler) appendAttr(b []byte, prefix string, a slog.Attr) []byte {
	v := a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return b
	}
	if v.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, g := range v.Group() {
			b = h.appendAttr(b, prefix, g)
		}
		return b
	}
	b = append(b, ' ')
	b = h.paint(b, dim, prefix+a.Key+"=")
	text := quote(valueText(v))
	if a.Key == "err" || a.Key == "error" {
		return h.paint(b, red, text)
	}
	return append(b, text...)
}

func (h *consoleHandler) paint(b []byte, style, s string) []byte {
	if !h.color {
		return append(b, s...)
	}
	b = append(b, style...)
	b = append(b, s...)
	return append(b, reset...)
}

func valueText(v slog.Value) string {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339)
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return err.Error()
		}
		return fmt.Sprint(v.Any())
	default:
		return v.String()
	}
}

// quote quotes a value that would otherwise be ambiguous: empty, with spaces, quotes or
// anything unprintable. An equals sign stays bare: links with query strings read better.
func quote(s string) string {
	if s == "" || strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || !unicode.IsPrint(r)
	}) {
		return strconv.Quote(s)
	}
	return s
}

// oneLine keeps a message on its line: a record is one line of the console.
func oneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	return strings.Join(strings.Fields(s), " ")
}
