package app

import (
	"net"
	"net/http"
	"sync"
)

// connTracker remembers connections that were accepted but have not sent a request yet.
//
// http.Server.Shutdown treats such connections as active for up to 5 seconds. Browsers
// pre-connect sockets they may never use (and Go's own client dials speculatively), so
// every shutdown — and therefore every restart or rolling update — could stall for 5s.
// Closing them is safe: no request is in flight on a connection in StateNew.
type connTracker struct {
	mu       sync.Mutex
	fresh    map[net.Conn]struct{}
	shutdown bool
}

func newConnTracker() *connTracker { return &connTracker{fresh: map[net.Conn]struct{}{}} }

// ConnState is installed as http.Server.ConnState.
func (t *connTracker) ConnState(c net.Conn, state http.ConnState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch state {
	case http.StateNew:
		if t.shutdown {
			c.Close()
			return
		}
		t.fresh[c] = struct{}{}
	default:
		delete(t.fresh, c)
	}
}

// CloseFresh closes every connection still waiting for its first request and closes
// new ones from now on. Registered with http.Server.RegisterOnShutdown.
func (t *connTracker) CloseFresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.shutdown = true
	for c := range t.fresh {
		c.Close()
		delete(t.fresh, c)
	}
}
