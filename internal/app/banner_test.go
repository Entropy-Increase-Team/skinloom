package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestBannerInATerminal(t *testing.T) {
	var buf bytes.Buffer
	writeBanner(&buf, true, false, "1.2.3")
	out := buf.String()
	for _, row := range strings.Split(bannerArt, "\n") {
		if row = strings.TrimRight(row, " "); row != "" && !strings.Contains(out, row) {
			t.Errorf("missing row %q in\n%s", row, out)
		}
	}
	if !strings.Contains(out, "1.2.3") {
		t.Errorf("no version in\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("escape codes without color:\n%q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("trailing spaces: %q", line)
		}
	}
}

func TestBannerColored(t *testing.T) {
	var buf bytes.Buffer
	writeBanner(&buf, true, true, "1.2.3")
	if !strings.Contains(buf.String(), "\x1b[") || !strings.HasSuffix(strings.TrimRight(buf.String(), "\n"), "\x1b[0m") {
		t.Errorf("colors missing or left on:\n%q", buf.String())
	}
}

// Log files, Docker and systemd get no banner: it would repeat on every restart.
func TestNoBannerOutsideATerminal(t *testing.T) {
	var buf bytes.Buffer
	writeBanner(&buf, false, false, "1.2.3")
	if buf.Len() != 0 {
		t.Errorf("banner outside a terminal:\n%s", buf.String())
	}
}

// The art is plain ASCII, so every console shows it, and all rows line up.
func TestBannerArtIsASCII(t *testing.T) {
	for i, r := range bannerArt {
		if r > 127 {
			t.Fatalf("non-ASCII %q at %d", r, i)
		}
	}
	rows := strings.Split(strings.Trim(bannerArt, "\n"), "\n")
	if len(rows) < 3 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, row := range rows {
		if w := len(strings.TrimRight(row, " ")); w > 72 {
			t.Errorf("row of %d columns does not fit an 80-column terminal: %q", w, row)
		}
	}
}
