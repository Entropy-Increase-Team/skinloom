// Package hook is the extension bus every module — and later every plugin — uses.
//
// Three kinds of hooks:
//   - Guard:  synchronous, may reject an action with a reason (auth.register, texture.upload…)
//   - Filter: synchronous, rewrites a value (player.name, user.badges…)
//   - Event:  asynchronous notification after the fact (texture.uploaded…)
//
// Hooks are declared once with a typed payload (NewGuard / NewFilter / NewEvent); the
// declarations form the catalog that docs and the plugin SDK types are generated from.
// Handlers are registered per owner ("core", "module:yggdrasil", "plugin:invitation-codes")
// and an owner's handlers are swapped atomically, which is how hot reload works.
package hook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/registry"
)

// Kind of hook.
type Kind string

const (
	KindGuard  Kind = "guard"
	KindFilter Kind = "filter"
	KindEvent  Kind = "event"
)

// FailPolicy decides what a Guard does when a handler errors or times out
// (as opposed to rejecting on purpose).
type FailPolicy string

const (
	// FailOpen logs the failure and lets the action continue.
	FailOpen FailPolicy = "open"
	// FailClosed blocks the action with an internal error.
	FailClosed FailPolicy = "closed"
)

// Def describes one hook in the catalog.
type Def struct {
	Kind    Kind
	Name    string
	Version int
	Fail    FailPolicy    // guards only
	Timeout time.Duration // per handler call
	Payload reflect.Type
	Value   reflect.Type // filters only
	Doc     string
}

// ID is the versioned name, e.g. "texture.uploaded@1".
func (d Def) ID() string { return fmt.Sprintf("%s@%d", d.Name, d.Version) }

var catalog = struct {
	sync.Mutex
	defs map[string]Def
}{defs: map[string]Def{}}

func declare(d Def) Def {
	if d.Timeout == 0 {
		d.Timeout = 2 * time.Second
	}
	catalog.Lock()
	defer catalog.Unlock()
	if prev, ok := catalog.defs[d.Name]; ok && (prev.Kind != d.Kind || prev.Version != d.Version) {
		panic(fmt.Sprintf("hook %q declared twice with different kind/version", d.Name))
	}
	catalog.defs[d.Name] = d
	return d
}

// Catalog returns every declared hook sorted by name.
func Catalog() []Def {
	catalog.Lock()
	defer catalog.Unlock()
	out := make([]Def, 0, len(catalog.defs))
	for _, d := range catalog.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func lookup(name string) (Def, bool) {
	catalog.Lock()
	defer catalog.Unlock()
	d, ok := catalog.defs[name]
	return d, ok
}

// Rejection is returned by a Guard handler to refuse an action on purpose.
type Rejection struct {
	Reason string
	Owner  string
}

func (r *Rejection) Error() string { return r.Reason }

// Reject builds a Rejection.
func Reject(reason string) error { return &Rejection{Reason: reason} }

// AsRejection reports whether err is (or wraps) a Rejection.
func AsRejection(err error) (*Rejection, bool) {
	var r *Rejection
	ok := errors.As(err, &r)
	return r, ok
}

// ErrHookFailed wraps handler failures surfaced by fail-closed guards.
var ErrHookFailed = errors.New("hook handler failed")

type handlerFunc func(ctx context.Context, in any) (any, error)

type handler struct {
	hook  string
	owner string
	order int
	fn    handlerFunc
}

// Registration is one handler, built by Guard.Handle / Filter.Handle / Event.Handle and
// installed with Bus.Replace.
type Registration struct {
	h handler
}

// Bus dispatches hooks to registered handlers.
type Bus struct {
	log    *slog.Logger
	table  *registry.Owned[handler]
	index  atomic.Pointer[map[string][]handler]
	events chan func()
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

// NewBus starts a bus with workers goroutines for asynchronous events.
func NewBus(log *slog.Logger, workers int) *Bus {
	if workers <= 0 {
		workers = 4
	}
	b := &Bus{log: log, table: registry.NewOwned[handler](), events: make(chan func(), 1024)}
	empty := map[string][]handler{}
	b.index.Store(&empty)
	for range workers {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			for job := range b.events {
				job()
			}
		}()
	}
	return b
}

// Replace atomically swaps every handler owner has registered.
func (b *Bus) Replace(owner string, regs ...Registration) error {
	hs := make([]handler, 0, len(regs))
	for _, r := range regs {
		if _, ok := lookup(r.h.hook); !ok {
			return fmt.Errorf("unknown hook %q", r.h.hook)
		}
		h := r.h
		h.owner = owner
		hs = append(hs, h)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.table.Replace(owner, hs)
	b.rebuild()
	return nil
}

// Remove drops every handler owner registered.
func (b *Bus) Remove(owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.table.Remove(owner)
	b.rebuild()
}

func (b *Bus) rebuild() {
	idx := map[string][]handler{}
	for _, h := range b.table.All() {
		idx[h.hook] = append(idx[h.hook], h)
	}
	for name := range idx {
		hs := idx[name]
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].order < hs[j].order })
	}
	b.index.Store(&idx)
}

func (b *Bus) handlers(name string) []handler { return (*b.index.Load())[name] }

// Close stops accepting events and waits for queued ones until ctx expires.
func (b *Bus) Close(ctx context.Context) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	close(b.events)
	b.mu.Unlock()
	done := make(chan struct{})
	go func() { b.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// call runs one handler with its timeout and turns panics into errors.
func (b *Bus) call(ctx context.Context, d Def, h handler, in any) (out any, err error) {
	ctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h.fn(ctx, in)
}

// Guard is a typed guard hook.
type Guard[P any] struct{ def Def }

// NewGuard declares a guard hook.
func NewGuard[P any](name string, version int, fail FailPolicy, doc string) Guard[P] {
	return Guard[P]{def: declare(Def{Kind: KindGuard, Name: name, Version: version, Fail: fail, Doc: doc,
		Payload: reflect.TypeFor[P]()})}
}

// Def returns the catalog entry.
func (g Guard[P]) Def() Def { return g.def }

// Handle builds a registration; lower order runs first.
func (g Guard[P]) Handle(order int, fn func(ctx context.Context, p P) error) Registration {
	return Registration{h: handler{hook: g.def.Name, order: order, fn: func(ctx context.Context, in any) (any, error) {
		return nil, fn(ctx, in.(P))
	}}}
}

// Check runs handlers in order. The first Rejection is returned as-is; other errors
// follow the guard's fail policy.
func (g Guard[P]) Check(ctx context.Context, b *Bus, p P) error {
	for _, h := range b.handlers(g.def.Name) {
		_, err := b.call(ctx, g.def, h, p)
		if err == nil {
			continue
		}
		if r, ok := AsRejection(err); ok {
			r.Owner = h.owner
			return r
		}
		b.log.Error("guard handler failed", "hook", g.def.Name, "owner", h.owner, "err", err)
		if g.def.Fail == FailClosed {
			return fmt.Errorf("%w: %s by %s", ErrHookFailed, g.def.Name, h.owner)
		}
	}
	return nil
}

// Filter is a typed filter hook rewriting a V given payload P.
type Filter[V, P any] struct{ def Def }

type filterIn[V, P any] struct {
	v V
	p P
}

// NewFilter declares a filter hook.
func NewFilter[V, P any](name string, version int, doc string) Filter[V, P] {
	return Filter[V, P]{def: declare(Def{Kind: KindFilter, Name: name, Version: version, Doc: doc,
		Payload: reflect.TypeFor[P](), Value: reflect.TypeFor[V]()})}
}

// Def returns the catalog entry.
func (f Filter[V, P]) Def() Def { return f.def }

// Handle builds a registration; lower order runs first.
func (f Filter[V, P]) Handle(order int, fn func(ctx context.Context, v V, p P) (V, error)) Registration {
	return Registration{h: handler{hook: f.def.Name, order: order, fn: func(ctx context.Context, in any) (any, error) {
		x := in.(filterIn[V, P])
		return fn(ctx, x.v, x.p)
	}}}
}

// Apply threads v through every handler; a failing handler is skipped (value unchanged).
func (f Filter[V, P]) Apply(ctx context.Context, b *Bus, v V, p P) V {
	for _, h := range b.handlers(f.def.Name) {
		out, err := b.call(ctx, f.def, h, filterIn[V, P]{v: v, p: p})
		if err != nil {
			b.log.Error("filter handler failed; value left unchanged", "hook", f.def.Name, "owner", h.owner, "err", err)
			continue
		}
		v = out.(V)
	}
	return v
}

// Event is a typed asynchronous hook.
type Event[P any] struct{ def Def }

// NewEvent declares an event hook.
func NewEvent[P any](name string, version int, doc string) Event[P] {
	return Event[P]{def: declare(Def{Kind: KindEvent, Name: name, Version: version, Doc: doc,
		Timeout: 30 * time.Second, Payload: reflect.TypeFor[P]()})}
}

// Def returns the catalog entry.
func (e Event[P]) Def() Def { return e.def }

// Handle builds a registration; lower order runs first.
func (e Event[P]) Handle(order int, fn func(ctx context.Context, p P) error) Registration {
	return Registration{h: handler{hook: e.def.Name, order: order, fn: func(ctx context.Context, in any) (any, error) {
		return nil, fn(ctx, in.(P))
	}}}
}

// Emit queues p for every handler registered at the time of the call.
//
// M0 delivers in-process and best-effort; durable at-least-once delivery through the
// outbox table arrives with the job queue in M1, behind this same method.
func (e Event[P]) Emit(b *Bus, p P) {
	hs := b.handlers(e.def.Name)
	if len(hs) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	job := func() {
		for _, h := range hs {
			if _, err := b.call(context.Background(), e.def, h, p); err != nil {
				b.log.Error("event handler failed", "hook", e.def.Name, "owner", h.owner, "err", err)
			}
		}
	}
	select {
	case b.events <- job:
	default:
		b.log.Warn("event queue full; event dropped", "hook", e.def.Name)
	}
}
