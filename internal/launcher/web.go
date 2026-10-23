// Package launcher runs and supervises the Next.js frontend as a child process, so one
// `skinloom up` starts the whole site on Windows, macOS and Linux.
package launcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spec describes how to run the frontend.
type Spec struct {
	Mode        string // managed | dev
	ServerDir   string // managed: directory containing server.js
	Node        string // managed: node executable
	DevDir      string // dev: pnpm workspace root (web/)
	Port        int
	APIInternal string // URL the frontend uses for server-side calls to the backend

	// argv/env replace the command for Mode "custom" (tests).
	argv []string
	env  []string
}

// Upstream is the address the gateway proxies to.
func (s Spec) Upstream() *url.URL {
	return &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port))}
}

// ResolvePort returns port if it is free on 127.0.0.1, otherwise (or for 0) a free
// port chosen by the OS; changed reports whether a different port was picked. Never
// proxying to a busy port matters: the gateway would forward public traffic to
// whatever other program listens there.
func ResolvePort(port int) (got int, changed bool, err error) {
	if port > 0 {
		if ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
			ln.Close()
			return port, false, nil
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, false, fmt.Errorf("find a free port for the frontend: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, true, nil
}

// Web supervises one frontend process at a time; Start replaces the running one.
type Web struct {
	log    *slog.Logger // supervisor messages
	out    *slog.Logger // the frontend's own stdout/stderr
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewWeb returns an idle supervisor. log receives supervisor messages, out receives the
// frontend's own output line by line.
func NewWeb(log, out *slog.Logger) *Web { return &Web{log: log, out: out} }

// Start (re)starts the frontend with spec, stopping a previous instance first.
func (w *Web) Start(parent context.Context, spec Spec) {
	w.Stop()
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.mu.Lock()
	w.cancel, w.done = cancel, done
	w.mu.Unlock()
	go func() {
		defer close(done)
		w.supervise(ctx, spec)
	}()
}

// Stop terminates the frontend and waits for it to exit.
func (w *Web) Stop() {
	w.mu.Lock()
	cancel, done := w.cancel, w.done
	w.cancel, w.done = nil, nil
	w.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		w.log.Warn("frontend did not exit in time")
	}
}

func (w *Web) supervise(ctx context.Context, spec Spec) {
	backoff := time.Second
	for {
		started := time.Now()
		err := w.runOnce(ctx, spec)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		w.log.Error("frontend exited; restarting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// pipeGrace bounds how long Wait keeps reading output after the process exited. A
// grandchild that inherited stdout and kept running used to block the supervisor
// forever, so a crashed frontend was never restarted.
const pipeGrace = 3 * time.Second

func (w *Web) runOnce(ctx context.Context, spec Spec) error {
	cmd, err := command(spec)
	if err != nil {
		return err
	}
	cmd.Env = append(os.Environ(),
		"PORT="+strconv.Itoa(spec.Port),
		"HOSTNAME=127.0.0.1",
		"SKINLOOM_API_INTERNAL="+spec.APIInternal,
		"NEXT_TELEMETRY_DISABLED=1",
	)
	if spec.Mode == "managed" {
		cmd.Env = append(cmd.Env, "NODE_ENV=production")
	}
	cmd.Env = append(cmd.Env, spec.env...)
	stdout, stderr := newLineWriter(w.out, slog.LevelInfo), newLineWriter(w.out, slog.LevelWarn)
	defer stdout.Close()
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = pipeGrace
	prepare(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start frontend: %w", err)
	}
	adopt(cmd, w.log)
	w.log.Info("frontend started", "mode", spec.Mode, "pid", cmd.Process.Pid, "port", spec.Port)

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if errors.Is(err, exec.ErrWaitDelay) {
			return errors.New("exited, but a child process kept its output open")
		}
		return err
	case <-ctx.Done():
		terminate(cmd)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited // returns within pipeGrace after the kill
		}
		w.log.Info("frontend stopped")
		return ctx.Err()
	}
}

func command(spec Spec) (*exec.Cmd, error) {
	switch spec.Mode {
	case "managed":
		cmd := exec.Command(spec.Node, "server.js")
		cmd.Dir = spec.ServerDir
		return cmd, nil
	case "dev":
		pnpm, err := exec.LookPath("pnpm")
		if err != nil {
			return nil, errors.New("dev mode needs pnpm on PATH (https://pnpm.io/installation)")
		}
		cmd := exec.Command(pnpm, "--filter", "@skinloom/web", "dev", "--port", strconv.Itoa(spec.Port), "--hostname", "127.0.0.1")
		cmd.Dir = spec.DevDir
		return cmd, nil
	case "custom":
		if len(spec.argv) == 0 {
			return nil, errors.New("custom mode needs a command")
		}
		return exec.Command(spec.argv[0], spec.argv[1:]...), nil
	default:
		return nil, fmt.Errorf("launcher cannot run web mode %q", spec.Mode)
	}
}

// lineWriter turns a byte stream into one log record per line.
type lineWriter struct {
	log   *slog.Logger
	level slog.Level
	mu    sync.Mutex
	buf   []byte
}

const maxLine = 64 * 1024

func newLineWriter(log *slog.Logger, level slog.Level) *lineWriter {
	return &lineWriter{log: log, level: level}
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.emit(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > maxLine { // a runaway line without newline: flush in chunks
		l.emit(l.buf)
		l.buf = l.buf[:0]
	}
	return len(p), nil
}

// Close flushes a final unterminated line.
func (l *lineWriter) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) > 0 {
		l.emit(l.buf)
		l.buf = nil
	}
	return nil
}

func (l *lineWriter) emit(b []byte) {
	line := strings.TrimRight(string(b), "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	l.log.Log(context.Background(), l.level, line)
}

// ResolveServerDir finds the directory with the Next.js standalone server.js.
// dir may point at the standalone root (monorepo layout puts server.js under apps/web).
func ResolveServerDir(dir string) (string, error) {
	for _, cand := range []string{dir, filepath.Join(dir, "apps", "web")} {
		if _, err := os.Stat(filepath.Join(cand, "server.js")); err == nil {
			return cand, nil
		}
	}
	return "", fmt.Errorf("no Next.js server.js under %s", dir)
}

// FindNode returns the node executable: explicit path, then the bundled runtime next to
// the skinloom binary, then PATH.
func FindNode(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("web.node %q: %w", explicit, err)
		}
		return explicit, nil
	}
	name := "node"
	if runtime.GOOS == "windows" {
		name = "node.exe"
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		for _, cand := range []string{
			filepath.Join(base, "runtime", "node", name),
			filepath.Join(base, "runtime", "node", "bin", name),
		} {
			if _, err := os.Stat(cand); err == nil {
				return cand, nil
			}
		}
	}
	p, err := exec.LookPath("node")
	if err != nil {
		return "", errors.New("node not found: use the release bundle (ships runtime/node) or install Node.js 20.9+")
	}
	return p, nil
}

// DefaultWebDir is <exe dir>/web, where release bundles put the frontend.
func DefaultWebDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "web"
	}
	return filepath.Join(filepath.Dir(exe), "web")
}
