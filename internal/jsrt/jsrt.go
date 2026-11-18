// Package jsrt isolates the embedded JavaScript runtime behind an engine-agnostic
// sandbox abstraction so plugins run in pooled, memory-capped, preemptively
// time-bounded ES2023 isolates without CGo.
package jsrt

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	// MaxBundleBytes caps a plugin's compiled server.js bundle at 5 MiB.
	MaxBundleBytes = 5 * 1024 * 1024
	// DefaultPoolSize is the default number of warm JS isolates maintained per plugin.
	DefaultPoolSize = 2
	// DefaultMemoryLimit is the default per-isolate heap limit (64 MiB).
	DefaultMemoryLimit = 64 * 1024 * 1024
	// DefaultMaxStackSize caps the QuickJS call stack per isolate (1 MiB).
	DefaultMaxStackSize = 1 * 1024 * 1024
	// DefaultTimeout is the default execution timeout for a single JS call.
	DefaultTimeout = 2 * time.Second
	// DefaultMaxTimerDelay bounds setTimeout delays inside a single invocation.
	DefaultMaxTimerDelay = 30 * time.Second
)

var (
	// ErrTimeout indicates JS execution exceeded the per-call deadline and the isolate was terminated.
	ErrTimeout = errors.New("jsrt: execution timed out")
	// ErrOutOfMemory indicates JS execution hit the isolate's 64 MiB memory cap.
	ErrOutOfMemory = errors.New("jsrt: memory limit exceeded")
	// ErrPoolClosed indicates Borrow or Do was called after DrainAndClose started.
	ErrPoolClosed = errors.New("jsrt: runtime pool closed")
	// ErrBundleTooLarge indicates the JS bundle exceeds MaxBundleBytes.
	ErrBundleTooLarge = errors.New("jsrt: bundle exceeds maximum allowed size")
	// ErrInvalidModule indicates the bundle could not be compiled or lacks expected exports.
	ErrInvalidModule = errors.New("jsrt: invalid module")
)

// JSError represents an uncaught JavaScript exception or rejected Promise.
type JSError struct {
	Message string
	Stack   string
}

func (e *JSError) Error() string {
	if e.Message == "" {
		return "javascript error"
	}
	return e.Message
}

// Limits configures isolate resource bounds for a plugin pool. Zero fields
// fall back to package defaults.
type Limits struct {
	PoolSize       int
	MemoryLimit    int
	MaxStackSize   int
	Timeout        time.Duration
	MaxBundleBytes int
}

func (l Limits) withDefaults() Limits {
	if l.PoolSize <= 0 {
		l.PoolSize = DefaultPoolSize
	}
	if l.MemoryLimit <= 0 {
		l.MemoryLimit = DefaultMemoryLimit
	}
	if l.MaxStackSize <= 0 {
		l.MaxStackSize = DefaultMaxStackSize
	}
	if l.Timeout <= 0 {
		l.Timeout = DefaultTimeout
	}
	if l.MaxBundleBytes <= 0 {
		l.MaxBundleBytes = MaxBundleBytes
	}
	return l
}

// CallStats reports resource usage and failure mode for a single JS invocation.
type CallStats struct {
	Duration  time.Duration
	TimedOut  bool
	OOM       bool
	Recreated bool
}

// HostFunc is a Go capability callable (and awaitable via Promise) from JS.
type HostFunc func(ctx context.Context, args []any) (any, error)

// Engine compiles plugin bundles and instantiates isolated JS runtimes.
type Engine interface {
	Compile(ctx context.Context, filename string, code []byte) ([]byte, error)
	NewInstance(ctx context.Context, spec InstanceSpec) (Instance, error)
	Close() error
}

// InstanceSpec configures a single JS isolate created by Engine.
type InstanceSpec struct {
	ID       string
	Filename string
	Code     []byte
	Bytecode []byte
	Limits   Limits
	Log      *slog.Logger
	// Prepare runs inside a fresh isolate before evaluating the module so host
	// bridges and globals can be registered cleanly.
	Prepare func(ctx context.Context, inst Instance) error
	// OnReady runs right after the module evaluates inside the isolate (e.g. to
	// run plugin setup(ctx) and populate per-isolate handler closures).
	OnReady func(ctx context.Context, inst Instance) error
}

// Instance is one stateful, single-threaded JS isolate checked out from a Pool.
// Callers must never share one Instance concurrently across goroutines without
// returning it to the Pool.
type Instance interface {
	ID() uint64
	Eval(ctx context.Context, filename, code string) (any, CallStats, error)
	InvokeExport(ctx context.Context, method string, args ...any) (any, CallStats, error)
	InvokeGlobal(ctx context.Context, path string, args ...any) (any, CallStats, error)
	RegisterHostFunc(name string, fn HostFunc) error
	SetGlobalJSON(name string, value any) error
	Tainted() bool
	Close() error
}

// EngineOption customizes Engine construction.
type EngineOption func(*engineConfig)

type engineConfig struct {
	log      *slog.Logger
	cacheDir string
}

// WithLogger sets the fallback logger for the engine.
func WithLogger(log *slog.Logger) EngineOption {
	return func(c *engineConfig) { c.log = log }
}

// WithCacheDir stores compiled wazero machine code caches on disk across restarts.
func WithCacheDir(dir string) EngineOption {
	return func(c *engineConfig) { c.cacheDir = dir }
}
