package jsrt

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// PoolSpec configures a warm isolate pool for a single plugin.
type PoolSpec struct {
	ID       string
	Filename string
	Code     []byte
	Limits   Limits
	Log      *slog.Logger
	Prepare  func(ctx context.Context, inst Instance) error
	// OnSetup runs once on the first warm instance so the host can collect declared
	// hooks/routes before adding the instance into the serving pool.
	OnSetup func(ctx context.Context, inst Instance) error
	// OnWarm runs on each additional or replacement instance to replay setup
	// handler bindings cleanly into that isolate's heap.
	OnWarm func(ctx context.Context, inst Instance) error
}

// PoolStats summarizes live runtime metrics for an isolate pool.
type PoolStats struct {
	Size      int    `json:"size"`
	Idle      int    `json:"idle"`
	InUse     int64  `json:"inUse"`
	Recreated uint64 `json:"recreated"`
	Timeouts  uint64 `json:"timeouts"`
	OOMs      uint64 `json:"ooms"`
}

// Pool manages warm JS isolates (default 2) with bounded checkout, automatic
// replacement of tainted isolates, and graceful in-flight draining on shutdown.
type Pool struct {
	engine   Engine
	spec     PoolSpec
	bytecode []byte
	limits   Limits

	idle    chan Instance
	closing chan struct{}

	mu          sync.Mutex
	broadcastMu sync.Mutex
	closed      bool
	inFlight    sync.WaitGroup
	bgRepl      sync.WaitGroup

	inUse     atomic.Int64
	recreated atomic.Uint64
	timeouts  atomic.Uint64
	ooms      atomic.Uint64
}

// NewPool compiles spec.Code once, warms PoolSize isolates (running OnSetup on the
// first and OnWarm on the rest), and returns a ready pool.
func NewPool(engine Engine, spec PoolSpec) (*Pool, error) {
	lim := spec.Limits.withDefaults()
	if len(spec.Code) > lim.MaxBundleBytes {
		return nil, ErrBundleTooLarge
	}

	var bytecode []byte
	if len(spec.Code) > 0 {
		compileCtx, cancel := context.WithTimeout(context.Background(), max(lim.Timeout, 5*time.Second))
		defer cancel()
		var err error
		bytecode, err = engine.Compile(compileCtx, spec.Filename, spec.Code)
		if err != nil {
			return nil, err
		}
	}
	spec.Limits = lim

	p := &Pool{
		engine:   engine,
		spec:     spec,
		bytecode: bytecode,
		limits:   lim,
		idle:     make(chan Instance, lim.PoolSize),
		closing:  make(chan struct{}),
	}

	created := make([]Instance, 0, lim.PoolSize)
	cleanupAll := func() {
		for _, inst := range created {
			_ = inst.Close()
		}
	}

	for i := 0; i < lim.PoolSize; i++ {
		hookFn := spec.OnWarm
		if i == 0 && spec.OnSetup != nil {
			hookFn = spec.OnSetup
		}
		inst, err := p.spawnInstance(hookFn)
		if err != nil {
			cleanupAll()
			return nil, err
		}
		created = append(created, inst)
	}
	for _, inst := range created {
		p.idle <- inst
	}
	return p, nil
}

func (p *Pool) spawnInstance(onReady func(context.Context, Instance) error) (Instance, error) {
	initCtx, cancel := context.WithTimeout(context.Background(), max(p.limits.Timeout, 5*time.Second))
	defer cancel()
	return p.engine.NewInstance(initCtx, InstanceSpec{
		ID:       p.spec.ID,
		Filename: p.spec.Filename,
		Code:     p.spec.Code,
		Bytecode: p.bytecode,
		Limits:   p.limits,
		Log:      p.spec.Log,
		Prepare:  p.spec.Prepare,
		OnReady:  onReady,
	})
}

// Borrow checks out one warm isolate from the pool, waiting until one is available,
// ctx expires, or DrainAndClose is called.
func (p *Pool) Borrow(ctx context.Context) (Instance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPoolClosed
	}
	p.inFlight.Add(1)
	p.mu.Unlock()

	select {
	case <-p.closing:
		p.inFlight.Done()
		return nil, ErrPoolClosed
	case <-ctx.Done():
		p.inFlight.Done()
		return nil, ctx.Err()
	case inst := <-p.idle:
		if inst == nil {
			p.inFlight.Done()
			return nil, ErrPoolClosed
		}
		p.inUse.Add(1)
		if inst.Tainted() {
			p.ReplaceOnBroken(inst)
			return p.Borrow(ctx)
		}
		return inst, nil
	}
}

// Return returns an isolate to the pool, or replaces it if tainted or unhealthy.
func (p *Pool) Return(inst Instance) {
	if inst == nil {
		return
	}
	if inst.Tainted() {
		p.ReplaceOnBroken(inst)
		return
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()

	if closed {
		_ = inst.Close()
		p.inUse.Add(-1)
		p.inFlight.Done()
		return
	}

	select {
	case p.idle <- inst:
		p.inUse.Add(-1)
		p.inFlight.Done()
	default:
		_ = inst.Close()
		p.inUse.Add(-1)
		p.inFlight.Done()
	}
}

// ReplaceOnBroken closes a broken/tainted isolate and asynchronously replenishes
// the pool with a freshly warmed isolate unless shutting down.
func (p *Pool) ReplaceOnBroken(inst Instance) {
	if inst == nil {
		return
	}
	p.mu.Lock()
	closed := p.closed
	if !closed {
		p.bgRepl.Add(1)
	}
	p.mu.Unlock()

	p.recreated.Add(1)
	p.inUse.Add(-1)
	p.inFlight.Done()

	go func() {
		_ = inst.Close()
	}()

	if closed {
		return
	}

	go func() {
		defer p.bgRepl.Done()
		select {
		case <-p.closing:
			return
		default:
		}
		warmed, err := p.spawnInstance(p.spec.OnWarm)
		if err != nil {
			select {
			case <-p.closing:
				return
			case <-time.After(50 * time.Millisecond):
			}
			warmed, err = p.spawnInstance(p.spec.OnWarm)
		}
		if err != nil {
			if p.spec.Log != nil {
				p.spec.Log.Error("failed to replace broken plugin isolate", "plugin", p.spec.ID, "err", err)
			}
			return
		}
		select {
		case <-p.closing:
			_ = warmed.Close()
		case p.idle <- warmed:
		default:
			_ = warmed.Close()
		}
	}()
}

// RecordStats updates pool failure counters from CallStats after an invocation.
func (p *Pool) RecordStats(stats CallStats) {
	if stats.TimedOut {
		p.timeouts.Add(1)
	}
	if stats.OOM {
		p.ooms.Add(1)
	}
}

// Do borrows an isolate, invokes fn, records stats, and returns or replaces it cleanly.
func (p *Pool) Do(ctx context.Context, fn func(Instance) (CallStats, error)) (CallStats, error) {
	inst, err := p.Borrow(ctx)
	if err != nil {
		return CallStats{}, err
	}
	stats, callErr := fn(inst)
	p.RecordStats(stats)
	if inst.Tainted() {
		stats.Recreated = true
		p.ReplaceOnBroken(inst)
	} else {
		p.Return(inst)
	}
	return stats, callErr
}

// Broadcast borrows every warm isolate in the pool (up to PoolSize) and invokes fn
// on each one, ensuring state notifications (such as settings updates) reach all
// isolates rather than only a single checked-out instance.
func (p *Pool) Broadcast(ctx context.Context, fn func(Instance) (CallStats, error)) error {
	p.broadcastMu.Lock()
	defer p.broadcastMu.Unlock()

	size := p.limits.PoolSize
	if size <= 0 {
		size = 1
	}
	borrowed := make([]Instance, 0, size)
	defer func() {
		for _, inst := range borrowed {
			if inst.Tainted() {
				p.ReplaceOnBroken(inst)
			} else {
				p.Return(inst)
			}
		}
	}()
	for i := 0; i < size; i++ {
		inst, err := p.Borrow(ctx)
		if err != nil {
			if len(borrowed) == 0 {
				return err
			}
			break
		}
		borrowed = append(borrowed, inst)
	}
	var firstErr error
	for _, inst := range borrowed {
		stats, err := fn(inst)
		p.RecordStats(stats)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Stats returns a snapshot of current pool utilization and fault counters.
func (p *Pool) Stats() PoolStats {
	return PoolStats{
		Size:      p.limits.PoolSize,
		Idle:      len(p.idle),
		InUse:     p.inUse.Load(),
		Recreated: p.recreated.Load(),
		Timeouts:  p.timeouts.Load(),
		OOMs:      p.ooms.Load(),
	}
}

// DrainAndClose stops new checkouts, waits up to timeout for in-flight calls to
// complete, and closes all pooled isolates.
func (p *Pool) DrainAndClose(timeout time.Duration) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closing)
	p.mu.Unlock()

	drained := make(chan struct{})
	go func() {
		p.inFlight.Wait()
		p.bgRepl.Wait()
		close(drained)
	}()

	var waitErr error
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-drained:
		case <-timer.C:
			waitErr = ErrTimeout
		}
	} else {
		<-drained
	}

	for {
		select {
		case inst := <-p.idle:
			if inst != nil {
				_ = inst.Close()
			}
		default:
			return waitErr
		}
	}
}
