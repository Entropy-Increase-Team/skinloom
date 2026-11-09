// Package registry provides copy-on-write tables that can be swapped atomically while
// requests are being served. Everything a module or plugin contributes (hooks, routes,
// menus, cron jobs) is grouped by owner so the owner's whole contribution is replaced
// or removed in one step — the core of hot reload.
package registry

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Owned holds items grouped by owner. Reads are lock-free snapshots.
type Owned[T any] struct {
	mu  sync.Mutex
	cur atomic.Pointer[map[string][]T]
}

// NewOwned returns an empty table.
func NewOwned[T any]() *Owned[T] {
	o := &Owned[T]{}
	empty := map[string][]T{}
	o.cur.Store(&empty)
	return o
}

// Replace swaps owner's items for items (nil or empty removes the owner).
func (o *Owned[T]) Replace(owner string, items []T) {
	o.mu.Lock()
	defer o.mu.Unlock()
	old := *o.cur.Load()
	next := make(map[string][]T, len(old)+1)
	for k, v := range old {
		if k != owner {
			next[k] = v
		}
	}
	if len(items) > 0 {
		next[owner] = append([]T(nil), items...)
	}
	o.cur.Store(&next)
}

// Remove drops everything owner contributed.
func (o *Owned[T]) Remove(owner string) { o.Replace(owner, nil) }

// Snapshot returns the current table. Callers must not modify it.
func (o *Owned[T]) Snapshot() map[string][]T { return *o.cur.Load() }

// Owners lists owners in sorted order.
func (o *Owned[T]) Owners() []string {
	snap := o.Snapshot()
	out := make([]string, 0, len(snap))
	for k := range snap {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// All flattens the table, owners in sorted order, items in insertion order.
func (o *Owned[T]) All() []T {
	snap := o.Snapshot()
	var out []T
	for _, owner := range o.Owners() {
		out = append(out, snap[owner]...)
	}
	return out
}
