package launcher

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as a fake frontend: SKINLOOM_TEST_HELPER selects a behaviour.
func TestMain(m *testing.M) {
	switch os.Getenv("SKINLOOM_TEST_HELPER") {
	case "":
		os.Exit(m.Run())
	case "serve":
		fmt.Println("helper ready")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "orphan":
		// Spawn a detached grandchild that inherits our stdout and outlives us, then exit.
		self, _ := os.Executable()
		gc := exec.Command(self)
		gc.Env = append(os.Environ(), "SKINLOOM_TEST_HELPER=grandchild")
		gc.Stdout, gc.Stderr = os.Stdout, os.Stderr
		detach(gc)
		if err := gc.Start(); err != nil {
			fmt.Println("grandchild start failed:", err)
			os.Exit(2)
		}
		_ = os.WriteFile(os.Getenv("SKINLOOM_TEST_PIDFILE"), []byte(strconv.Itoa(gc.Process.Pid)), 0o644)
		fmt.Println("parent exiting")
		os.Exit(3)
	case "grandchild":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(99)
}

// syncBuffer collects log output from concurrent goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func waitFor(t *testing.T, buf *syncBuffer, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), substr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; log:\n%s", substr, buf.String())
}

func helperSpec(t *testing.T, mode string, env ...string) Spec {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Mode: "custom", Port: 1, argv: []string{self}, env: append([]string{"SKINLOOM_TEST_HELPER=" + mode}, env...)}
}

func TestStartStop(t *testing.T) {
	var buf syncBuffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	w := NewWeb(log, log)
	w.Start(t.Context(), helperSpec(t, "serve"))
	waitFor(t, &buf, "helper ready", 10*time.Second)

	start := time.Now()
	w.Stop()
	if d := time.Since(start); d > 12*time.Second {
		t.Fatalf("Stop took %s", d)
	}
	if !strings.Contains(buf.String(), "frontend stopped") {
		t.Fatalf("missing stop log:\n%s", buf.String())
	}
}

// Regression: a grandchild that kept the inherited stdout open blocked the supervisor
// forever, so a crashed frontend was never restarted.
func TestCrashIsDetectedEvenIfGrandchildHoldsPipes(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "gc.pid")
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidfile); err == nil {
			if pid, err := strconv.Atoi(string(b)); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	var buf syncBuffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	w := NewWeb(log, log)
	defer w.Stop()
	w.Start(t.Context(), helperSpec(t, "orphan", "SKINLOOM_TEST_PIDFILE="+pidfile))

	waitFor(t, &buf, "parent exiting", 10*time.Second)
	waitFor(t, &buf, "frontend exited; restarting", 10*time.Second)
}

func TestLineWriter(t *testing.T) {
	var buf bytes.Buffer
	lw := newLineWriter(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: dropTime})), slog.LevelInfo)
	_, _ = io.WriteString(lw, "one\r\ntw")
	_, _ = io.WriteString(lw, "o\n\nthree")
	lw.Close()
	got := buf.String()
	for _, want := range []string{`msg=one`, `msg=two`, `msg=three`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
	if strings.Count(got, "\n") != 3 {
		t.Fatalf("empty lines must be skipped: %q", got)
	}
}

func dropTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return a
}

// Regression: if the configured port was taken, the gateway proxied public traffic to
// whatever else was listening there.
func TestResolvePortAvoidsBusyPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port

	got, changed, err := ResolvePort(busy)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || got == busy || got == 0 {
		t.Fatalf("ResolvePort(%d) = %d changed=%v; want a different free port", busy, got, changed)
	}
	if got, changed, _ := ResolvePort(0); got == 0 || !changed {
		t.Fatalf("ResolvePort(0) must pick a port, got %d", got)
	}
	free := got
	if got, changed, _ := ResolvePort(free); got != free || changed {
		t.Fatalf("a free port must be kept: %d → %d", free, got)
	}
}
