package jsrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fastschema/qjs"
)

// qjsEngine implements Engine on top of fastschema/qjs + tetratelabs/wazero.
// Every runtime instantiated through qjsEngine enables WithCloseOnContextDone
// and mounts a dedicated empty sandbox directory as WASI root so plugins have
// zero access to host files or environment variables.
type qjsEngine struct {
	cfg        engineConfig
	sandboxDir string
	nextID     atomic.Uint64
	mu         sync.Mutex
	closed     bool
}

// NewEngine creates a QuickJS-NG/Wazero runtime engine and warms up the shared
// precompiled WASM module with preemption enabled.
func NewEngine(opts ...EngineOption) (Engine, error) {
	cfg := engineConfig{log: slog.Default()}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	sandboxDir, err := os.MkdirTemp("", "skinloom-jsrt-root-*")
	if err != nil {
		return nil, fmt.Errorf("jsrt: create empty sandbox root: %w", err)
	}
	// Warm up global wazero module compilation once with CloseOnContextDone=true.
	rt, err := qjs.New(qjs.Option{
		CWD:                sandboxDir,
		CacheDir:           cfg.cacheDir,
		Context:            context.Background(),
		CloseOnContextDone: true,
		MemoryLimit:        DefaultMemoryLimit,
		MaxStackSize:       DefaultMaxStackSize,
	})
	if err != nil {
		_ = os.RemoveAll(sandboxDir)
		return nil, fmt.Errorf("jsrt: warmup quickjs module: %w", err)
	}
	rt.Close()
	return &qjsEngine{cfg: cfg, sandboxDir: sandboxDir}, nil
}

func (e *qjsEngine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	dir := e.sandboxDir
	e.mu.Unlock()
	if dir != "" {
		return os.RemoveAll(dir)
	}
	return nil
}

func (e *qjsEngine) Compile(ctx context.Context, filename string, code []byte) ([]byte, error) {
	if len(code) > MaxBundleBytes {
		return nil, ErrBundleTooLarge
	}
	if filename == "" {
		filename = "server.js"
	}
	rtCtx, rtCancel := context.WithCancel(context.Background())
	defer rtCancel()
	rt, err := qjs.New(qjs.Option{
		CWD:                e.sandboxDir,
		CacheDir:           e.cfg.cacheDir,
		Context:            rtCtx,
		CloseOnContextDone: true,
		MemoryLimit:        DefaultMemoryLimit,
		MaxStackSize:       DefaultMaxStackSize,
	})
	if err != nil {
		return nil, fmt.Errorf("jsrt: create compiler runtime: %w", err)
	}
	defer func() {
		_ = recover()
		rt.Close()
	}()
	stopWatch := watchContextCancel(ctx, rtCancel)
	defer stopWatch()

	bc, err := rt.Compile(filename, qjs.Code(string(code)), qjs.TypeModule())
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidModule, err)
	}
	return bc, nil
}

func (e *qjsEngine) NewInstance(ctx context.Context, spec InstanceSpec) (Instance, error) {
	lim := spec.Limits.withDefaults()
	if len(spec.Code) > lim.MaxBundleBytes {
		return nil, ErrBundleTooLarge
	}
	rtCtx, rtCancel := context.WithCancel(context.Background())
	rt, err := qjs.New(qjs.Option{
		CWD:                e.sandboxDir,
		CacheDir:           e.cfg.cacheDir,
		Context:            rtCtx,
		CloseOnContextDone: true,
		MemoryLimit:        lim.MemoryLimit,
		MaxStackSize:       lim.MaxStackSize,
	})
	if err != nil {
		rtCancel()
		return nil, fmt.Errorf("jsrt: create instance: %w", err)
	}

	log := spec.Log
	if log == nil {
		log = e.cfg.log
	}
	if spec.ID != "" {
		log = log.With("plugin", spec.ID)
	}

	inst := &qjsInstance{
		id:       e.nextID.Add(1),
		rt:       rt,
		rtCtx:    rtCtx,
		rtCancel: rtCancel,
		limits:   lim,
		log:      log,
	}

	if err := installSandboxGlobals(inst, log); err != nil {
		_ = inst.Close()
		return nil, fmt.Errorf("jsrt: install sandbox globals: %w", err)
	}

	if spec.Prepare != nil {
		if err := spec.Prepare(ctx, inst); err != nil {
			_ = inst.Close()
			return nil, err
		}
	}

	if len(spec.Bytecode) > 0 || len(spec.Code) > 0 {
		filename := spec.Filename
		if filename == "" {
			filename = "server.js"
		}
		var flags []qjs.EvalOptionFunc
		if len(spec.Bytecode) > 0 {
			flags = append(flags, qjs.Bytecode(spec.Bytecode), qjs.TypeModule())
		} else {
			flags = append(flags, qjs.Code(string(spec.Code)), qjs.TypeModule())
		}
		if _, _, err := inst.runGuarded(ctx, lim.Timeout, func() (*qjs.Value, error) {
			return inst.rt.Context().Eval(filename, flags...)
		}, true); err != nil {
			_ = inst.Close()
			return nil, fmt.Errorf("%w: %w", ErrInvalidModule, err)
		}
	}

	if spec.OnReady != nil {
		if err := spec.OnReady(ctx, inst); err != nil {
			_ = inst.Close()
			return nil, err
		}
	}

	return inst, nil
}

type qjsInstance struct {
	id       uint64
	rt       *qjs.Runtime
	rtCtx    context.Context
	rtCancel context.CancelFunc
	limits   Limits
	log      *slog.Logger

	callMu    sync.Mutex
	activeCtx context.Context
	tainted   atomic.Bool
	closed    atomic.Bool
}

func (i *qjsInstance) ID() uint64    { return i.id }
func (i *qjsInstance) Tainted() bool { return i.tainted.Load() }

func (i *qjsInstance) Close() error {
	if !i.closed.CompareAndSwap(false, true) {
		return nil
	}
	i.callMu.Lock()
	defer i.callMu.Unlock()
	defer func() {
		_ = recover()
	}()
	if !i.tainted.Load() && i.rt != nil {
		i.rt.Close()
	}
	i.rtCancel()
	if i.tainted.Load() && i.rt != nil {
		// Reclaim WASM module handles safely if not already closed on context done.
		func() {
			defer func() { _ = recover() }()
			i.rt.Close()
		}()
	}
	i.rt = nil
	return nil
}

func (i *qjsInstance) currentContext() context.Context {
	if i.activeCtx != nil {
		return i.activeCtx
	}
	return context.Background()
}

func (i *qjsInstance) RegisterHostFunc(name string, fn HostFunc) error {
	if i.closed.Load() || i.tainted.Load() {
		return errors.New("jsrt: instance unavailable")
	}
	rejectWith := func(this *qjs.This, err error) {
		jsErr := this.Context().NewError(err)
		defer jsErr.Free()
		_ = this.Promise().Reject(jsErr)
	}
	i.rt.Context().SetAsyncFunc(name, func(this *qjs.This) {
		ctx := i.currentContext()
		rawArgs := this.Args()
		args := make([]any, len(rawArgs))
		for idx, v := range rawArgs {
			goVal, err := jsValueToGo(v)
			if err != nil {
				rejectWith(this, err)
				return
			}
			args[idx] = goVal
		}
		res, err := fn(ctx, args)
		if err != nil {
			rejectWith(this, err)
			return
		}
		jsRes, err := goValueToJS(this.Context(), res)
		if err != nil {
			rejectWith(this, err)
			return
		}
		defer jsRes.Free()
		_ = this.Promise().Resolve(jsRes)
	})
	return nil
}

func (i *qjsInstance) SetGlobalJSON(name string, value any) error {
	if i.closed.Load() || i.tainted.Load() {
		return errors.New("jsrt: instance unavailable")
	}
	c := i.rt.Context()
	jsVal, err := goValueToJS(c, value)
	if err != nil {
		return err
	}
	// SetPropertyStr transfers ownership of jsVal to globalThis; do not Free jsVal here.
	c.Global().SetPropertyStr(name, jsVal)
	return nil
}

func (i *qjsInstance) Eval(ctx context.Context, filename, code string) (any, CallStats, error) {
	if filename == "" {
		filename = "<eval>"
	}
	return i.runGuarded(ctx, i.limits.Timeout, func() (*qjs.Value, error) {
		return i.rt.Context().Eval(filename, qjs.Code(code))
	}, false)
}

func (i *qjsInstance) InvokeExport(ctx context.Context, method string, args ...any) (any, CallStats, error) {
	return i.runGuarded(ctx, i.limits.Timeout, func() (*qjs.Value, error) {
		c := i.rt.Context()
		ns := i.moduleExports(c)
		defer ns.Free()
		if ns.IsUndefined() || ns.IsNull() {
			return nil, fmt.Errorf("%w: default export missing", ErrInvalidModule)
		}
		return invokeMethod(c, ns, method, args...)
	}, false)
}

func (i *qjsInstance) InvokeGlobal(ctx context.Context, path string, args ...any) (any, CallStats, error) {
	return i.runGuarded(ctx, i.limits.Timeout, func() (*qjs.Value, error) {
		c := i.rt.Context()
		parts := strings.Split(path, ".")
		cur := c.Global()
		var toFree []*qjs.Value
		defer func() {
			for _, v := range toFree {
				v.Free()
			}
		}()
		for idx := 0; idx < len(parts)-1; idx++ {
			next := cur.GetPropertyStr(parts[idx])
			if next == nil || next.IsUndefined() || next.IsNull() {
				if next != nil {
					next.Free()
				}
				return nil, fmt.Errorf("jsrt: global %q not found", path)
			}
			toFree = append(toFree, next)
			cur = next
		}
		return invokeMethod(c, cur, parts[len(parts)-1], args...)
	}, false)
}

func (i *qjsInstance) moduleExports(c *qjs.Context) *qjs.Value {
	mod := c.Global().GetPropertyStr("__jsrt_module_default")
	if !mod.IsUndefined() {
		return mod
	}
	mod.Free()
	return c.NewUndefined()
}

func invokeMethod(c *qjs.Context, target *qjs.Value, method string, args ...any) (*qjs.Value, error) {
	fn := target.GetPropertyStr(method)
	defer fn.Free()
	if !fn.IsFunction() {
		return nil, fmt.Errorf("jsrt: %q is not a function", method)
	}
	jsArgs := make([]*qjs.Value, len(args))
	for idx, arg := range args {
		v, err := goValueToJS(c, arg)
		if err != nil {
			for j := 0; j < idx; j++ {
				jsArgs[j].Free()
			}
			return nil, err
		}
		jsArgs[idx] = v
	}
	defer func() {
		for _, v := range jsArgs {
			if v != nil {
				v.Free()
			}
		}
	}()
	return c.Invoke(fn, target, jsArgs...)
}

func (i *qjsInstance) runGuarded(ctx context.Context, timeout time.Duration, fn func() (*qjs.Value, error), saveDefaultExport bool) (resVal any, stats CallStats, resErr error) {
	if i.closed.Load() || i.tainted.Load() {
		return nil, CallStats{}, errors.New("jsrt: instance tainted or closed")
	}
	i.callMu.Lock()
	defer i.callMu.Unlock()

	if i.closed.Load() || i.tainted.Load() {
		return nil, CallStats{}, errors.New("jsrt: instance tainted or closed")
	}

	start := time.Now()
	callCtx := ctx
	if callCtx == nil {
		callCtx = context.Background()
	}
	var cancel context.CancelFunc
	if _, hasDeadline := callCtx.Deadline(); !hasDeadline && timeout > 0 {
		callCtx, cancel = context.WithTimeout(callCtx, timeout)
	} else {
		callCtx, cancel = context.WithCancel(callCtx)
	}
	i.activeCtx = callCtx

	done := make(chan struct{})
	var timedOut atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-callCtx.Done():
			timedOut.Store(true)
			i.rtCancel()
		case <-done:
		}
	}()

	defer func() {
		// Stop the preemption watcher first; read callCtx.Err() before calling
		// cancel() so healthy or non-timeout panics are not masked as context.Canceled.
		close(done)
		wg.Wait()
		preCancelErr := callCtx.Err()
		cancel()
		i.activeCtx = nil
		stats.Duration = time.Since(start)

		if r := recover(); r != nil {
			i.tainted.Store(true)
			stats.TimedOut = timedOut.Load() || errors.Is(preCancelErr, context.DeadlineExceeded)
			msg := fmt.Sprint(r)
			switch {
			case stats.TimedOut:
				resErr = ErrTimeout
			case preCancelErr != nil:
				resErr = preCancelErr
			case isOOMString(msg):
				stats.OOM = true
				resErr = fmt.Errorf("%w: %s", ErrOutOfMemory, msg)
			default:
				resErr = fmt.Errorf("jsrt: wasm trap: %s", msg)
			}
			resVal = nil
			return
		}

		if resErr != nil {
			if timedOut.Load() || errors.Is(preCancelErr, context.DeadlineExceeded) {
				i.tainted.Store(true)
				stats.TimedOut = true
				resErr = ErrTimeout
				resVal = nil
				return
			}
			if isOOMString(resErr.Error()) {
				i.tainted.Store(true)
				stats.OOM = true
				resErr = fmt.Errorf("%w: %w", ErrOutOfMemory, resErr)
				resVal = nil
				return
			}
		}

		// Clear any dangling timers scheduled during this call.
		if !i.tainted.Load() && i.rt != nil {
			func() {
				defer func() { _ = recover() }()
				clearVal, _ := i.rt.Context().Eval("<clear-timers>", qjs.Code("globalThis.__jsrt_clearTimers && globalThis.__jsrt_clearTimers()"))
				if clearVal != nil {
					clearVal.Free()
				}
			}()
		}
	}()

	val, err := fn()
	if err != nil {
		return nil, stats, normalizeJSError(err)
	}
	if val != nil && val.IsPromise() {
		// js_std_await consumes the Promise JSValue passed into it, so do not Free(val).
		awaited, awaitErr := val.Await()
		if awaitErr != nil {
			return nil, stats, normalizeJSError(awaitErr)
		}
		val = awaited
	}
	if val == nil {
		return nil, stats, nil
	}
	defer val.Free()
	if saveDefaultExport {
		i.rt.Context().Global().SetPropertyStr("__jsrt_module_default", val.Clone())
		return nil, stats, nil
	}
	out, convErr := jsValueToGo(val)
	if convErr != nil {
		return nil, stats, convErr
	}
	return out, stats, nil
}

func watchContextCancel(ctx context.Context, onCancel func()) func() {
	if ctx == nil || ctx.Done() == nil {
		return func() {}
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			onCancel()
		case <-done:
		}
	}()
	return func() {
		close(done)
		wg.Wait()
	}
}

func isOOMString(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "out of memory") || strings.Contains(low, "out_of_memory") ||
		strings.Contains(low, "memory limit")
}

func normalizeJSError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if isOOMString(msg) {
		return fmt.Errorf("%w: %w", ErrOutOfMemory, err)
	}
	lines := strings.SplitN(msg, "\n", 2)
	je := &JSError{Message: lines[0]}
	if len(lines) > 1 {
		je.Stack = lines[1]
	}
	return je
}

func hostWrap[A, B any](inst *qjsInstance, fn func(A, B)) qjs.Function {
	return func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		var a A
		var b B
		if len(args) > 0 {
			a, _ = jsArgTo[A](args[0])
		}
		if len(args) > 1 {
			b, _ = jsArgTo[B](args[1])
		}
		fn(a, b)
		return this.Context().NewUndefined(), nil
	}
}

func hostWrapErr(inst *qjsInstance, fn any) qjs.Function {
	return func(this *qjs.This) (*qjs.Value, error) {
		c := this.Context()
		args := this.Args()
		switch f := fn.(type) {
		case func(int) ([]byte, error):
			var n int
			if len(args) > 0 {
				n = int(args[0].Int64())
			}
			b, err := f(n)
			if err != nil {
				return nil, err
			}
			return goValueToJS(c, b)
		case func() (string, error):
			s, err := f()
			if err != nil {
				return nil, err
			}
			return c.NewString(s), nil
		case func(string, string) (map[string]any, error):
			var raw, base string
			if len(args) > 0 {
				raw = args[0].String()
			}
			if len(args) > 1 {
				base = args[1].String()
			}
			m, err := f(raw, base)
			if err != nil {
				return nil, err
			}
			return goValueToJS(c, m)
		default:
			return nil, errors.New("unsupported host wrapper")
		}
	}
}

func jsArgTo[T any](v *qjs.Value) (T, error) {
	var zero T
	if v == nil || v.IsUndefined() || v.IsNull() {
		return zero, nil
	}
	switch any(zero).(type) {
	case string:
		return any(v.String()).(T), nil
	default:
		return qjs.ToGoValue[T](v)
	}
}

func jsValueToGo(v *qjs.Value) (any, error) {
	if v == nil || v.IsUndefined() || v.IsNull() {
		return nil, nil
	}
	switch {
	case v.IsBool():
		return v.Bool(), nil
	case v.IsString():
		return v.String(), nil
	case v.IsNumber():
		return v.Float64(), nil
	case v.IsByteArray():
		return qjs.JsArrayBufferToGo(v)
	case qjs.IsTypedArray(v):
		return qjs.JsTypedArrayToGo(v)
	case v.IsDate():
		if t := v.DateTime(); t != nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
		return nil, nil
	case v.IsError():
		return nil, normalizeJSError(v.Exception())
	}
	c := v.Context()
	if c == nil {
		return nil, errors.New("jsrt: missing context for value conversion")
	}
	jsonObj := c.Global().GetPropertyStr("JSON")
	defer jsonObj.Free()
	stringifyFn := jsonObj.GetPropertyStr("stringify")
	defer stringifyFn.Free()
	strVal, err := c.Invoke(stringifyFn, jsonObj, v)
	if err != nil {
		return nil, err
	}
	defer strVal.Free()
	if strVal.IsUndefined() || strVal.IsNull() {
		return nil, nil
	}
	raw := strVal.String()
	if raw == "" || raw == "undefined" {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func goValueToJS(c *qjs.Context, val any) (*qjs.Value, error) {
	if val == nil {
		return c.NewNull(), nil
	}
	switch v := val.(type) {
	case bool:
		return c.NewBool(v), nil
	case string:
		return c.NewString(v), nil
	case int:
		return c.NewInt64(int64(v)), nil
	case int32:
		return c.NewInt32(v), nil
	case int64:
		return c.NewInt64(v), nil
	case uint:
		return c.NewFloat64(float64(v)), nil
	case uint32:
		return c.NewUint32(v), nil
	case uint64:
		return c.NewFloat64(float64(v)), nil
	case float32:
		return c.NewFloat64(float64(v)), nil
	case float64:
		return c.NewFloat64(v), nil
	case []byte:
		ab := c.NewArrayBuffer(v)
		defer ab.Free()
		ctor := c.Global().GetPropertyStr("Uint8Array")
		defer ctor.Free()
		return ctor.CallConstructor(ab), nil
	case time.Time:
		t := v
		return c.NewDate(&t), nil
	case *time.Time:
		if v == nil {
			return c.NewNull(), nil
		}
		return c.NewDate(v), nil
	case json.RawMessage:
		if len(v) == 0 {
			return c.NewNull(), nil
		}
		return c.ParseJSON(string(v)), nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return nil, err
	}
	return c.ParseJSON(string(raw)), nil
}
