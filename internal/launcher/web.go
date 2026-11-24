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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Spec describes how to run the frontend.
type Spec struct {
	Mode      string // managed | dev
	ServerDir string // managed: directory containing server.js
	Node      string // managed: node executable
	DevDir    string // dev: frontend workspace root (web/)
	// PackageManager for dev mode: auto | bun | pnpm | npm.
	PackageManager string
	Port           int
	APIInternal    string // URL the frontend uses for server-side calls to the backend

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

// WebStatus describes the current lifecycle state of the supervised frontend.
type WebStatus struct {
	Mode       string        // managed | dev | custom | ""
	State      string        // idle | starting | restarting | stopped
	Attempts   int           // number of launches in the current Start session
	Failures   int           // consecutive failures since Start or last healthy run
	StartedAt  time.Time     // when Start was called
	LastErr    string        // last unexpected exit or launch error, if restarting
	LastExitAt time.Time     // when the most recent failure occurred
	NextRetry  time.Time     // scheduled next restart time while waiting in backoff
	Backoff    time.Duration // backoff duration for the current retry wait
	Output     []string      // recent stdout/stderr lines from the last failed run
}

// Web supervises one frontend process at a time; Start replaces the running one.
type Web struct {
	log    *slog.Logger // supervisor messages
	out    *slog.Logger // the frontend's own stdout/stderr
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	status WebStatus
}

// NewWeb returns an idle supervisor. log receives supervisor messages, out receives the
// frontend's own output line by line.
func NewWeb(log, out *slog.Logger) *Web {
	return &Web{log: log, out: out, status: WebStatus{State: "idle"}}
}

// Status returns a snapshot of the supervisor's current state.
func (w *Web) Status() WebStatus {
	w.mu.Lock()
	st := w.status
	if len(st.Output) > 0 {
		st.Output = append([]string(nil), st.Output...)
	}
	w.mu.Unlock()
	return st
}

// Start (re)starts the frontend with spec, stopping a previous instance first.
func (w *Web) Start(parent context.Context, spec Spec) {
	w.Stop()
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	w.mu.Lock()
	w.cancel, w.done = cancel, done
	w.status = WebStatus{
		Mode:      spec.Mode,
		State:     "starting",
		Attempts:  1,
		StartedAt: time.Now(),
	}
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
	if cancel != nil {
		w.status.State = "stopped"
	}
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
	failures := 0
	for {
		if failures > 0 {
			w.mu.Lock()
			w.status.NextRetry = time.Time{}
			w.mu.Unlock()
		}
		started := time.Now()
		output, err := w.runOnce(ctx, spec)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
			failures = 0
		}
		failures++
		errMsg := "process exited unexpectedly (exit status 0)"
		if err != nil {
			errMsg = err.Error()
		}
		now := time.Now()
		w.mu.Lock()
		w.status.State = "restarting"
		w.status.Attempts++
		w.status.Failures = failures
		w.status.LastErr = errMsg
		w.status.LastExitAt = now
		w.status.NextRetry = now.Add(backoff)
		w.status.Backoff = backoff
		w.status.Output = output
		w.mu.Unlock()
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

func (w *Web) runOnce(ctx context.Context, spec Spec) ([]string, error) {
	cmd, err := command(spec)
	if err != nil {
		return nil, err
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
	tail := newTailBuffer(maxTailLines, maxTailBytes)
	stdout, stderr := newLineWriter(w.out, false), newLineWriter(w.out, true)
	stdout.tail, stderr.tail = tail, tail
	closePipes := func() {
		_ = stdout.Close()
		_ = stderr.Close()
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = pipeGrace
	prepare(cmd)
	if err := cmd.Start(); err != nil {
		closePipes()
		return nil, fmt.Errorf("start frontend: %w", err)
	}
	adopt(cmd, w.log)
	w.log.Info("frontend started", "mode", spec.Mode, "program", filepath.Base(cmd.Path), "pid", cmd.Process.Pid, "port", spec.Port)

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		closePipes()
		if errors.Is(err, exec.ErrWaitDelay) {
			return tail.Lines(), errors.New("exited, but a child process kept its output open")
		}
		return tail.Lines(), err
	case <-ctx.Done():
		terminate(cmd)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited // returns within pipeGrace after the kill
		}
		closePipes()
		w.log.Info("frontend stopped")
		return nil, ctx.Err()
	}
}

func command(spec Spec) (*exec.Cmd, error) {
	switch spec.Mode {
	case "managed":
		cmd := exec.Command(spec.Node, "server.js")
		cmd.Dir = spec.ServerDir
		return cmd, nil
	case "dev":
		return devCommand(spec)
	case "custom":
		if len(spec.argv) == 0 {
			return nil, errors.New("custom mode needs a command")
		}
		return exec.Command(spec.argv[0], spec.argv[1:]...), nil
	default:
		return nil, fmt.Errorf("launcher cannot run web mode %q", spec.Mode)
	}
}

// lineWriter turns the frontend's output into one log record per line.
type lineWriter struct {
	log    *slog.Logger
	stderr bool
	tail   *tailBuffer
	mu     sync.Mutex
	buf    []byte
}

const (
	maxLine      = 64 * 1024
	maxTailLines = 20
	maxTailBytes = 4096
)

func newLineWriter(log *slog.Logger, stderr bool) *lineWriter {
	return &lineWriter{log: log, stderr: stderr}
}

type tailBuffer struct {
	mu       sync.Mutex
	maxLines int
	maxBytes int
	lines    []string
	bytes    int
}

func newTailBuffer(maxLines, maxBytes int) *tailBuffer {
	return &tailBuffer{maxLines: maxLines, maxBytes: maxBytes}
}

func (t *tailBuffer) Add(line string) {
	if len(line) > 512 {
		line = line[:512] + "…"
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	t.bytes += len(line)
	for len(t.lines) > t.maxLines || (t.bytes > t.maxBytes && len(t.lines) > 1) {
		t.bytes -= len(t.lines[0])
		t.lines = t.lines[1:]
	}
}

func (t *tailBuffer) Lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.lines) == 0 {
		return nil
	}
	out := make([]string, len(t.lines))
	copy(out, t.lines)
	return out
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
	if level, msg, keep := frontendLine(string(b), l.stderr); keep {
		l.log.Log(context.Background(), level, msg)
		if l.tail != nil {
			l.tail.Add(msg)
		}
	}
}

// ansiCode matches a terminal escape sequence: Next.js colors its output when FORCE_COLOR
// reaches it, and the log adds its own colors (or none, in files).
var ansiCode = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// requestLine matches the request log of the Next.js server: "GET /skinlib 200 in 12ms".
var requestLine = regexp.MustCompile(`^(GET|HEAD|POST|PUT|PATCH|DELETE|OPTIONS) \S+ \d{3} in `)

// frontendLine gives a line of the frontend's output its level, so it reads like the
// rest of the log. It drops empty lines and the "- Local:" and "- Network:" banner,
// which names the internal port: people must use the public one.
func frontendLine(line string, stderr bool) (slog.Level, string, bool) {
	line = strings.TrimSpace(ansiCode.ReplaceAllString(line, ""))
	switch {
	case line == "", strings.HasPrefix(line, "- Local:"), strings.HasPrefix(line, "- Network:"):
		return 0, "", false
	case requestLine.MatchString(line):
		return slog.LevelDebug, line, true // like the backend's request log
	case strings.HasPrefix(line, "⨯"), strings.HasPrefix(line, "Error:"), strings.HasPrefix(line, "Error "):
		return slog.LevelError, line, true
	case stderr, strings.HasPrefix(line, "⚠"):
		return slog.LevelWarn, line, true
	default:
		return slog.LevelInfo, line, true
	}
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
		return "", errors.New("node not found: use the release bundle (ships runtime/node) or install Node.js 24")
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
