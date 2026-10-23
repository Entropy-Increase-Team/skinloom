package app

import (
	"net"
	"net/http"
	"testing"
)

func TestConnTrackerClosesOnlyUnusedConnections(t *testing.T) {
	tr := newConnTracker()
	fresh, freshPeer := net.Pipe()
	busy, busyPeer := net.Pipe()
	defer freshPeer.Close()
	defer busyPeer.Close()

	tr.ConnState(fresh, http.StateNew)
	tr.ConnState(busy, http.StateNew)
	tr.ConnState(busy, http.StateActive) // sent a request: must survive

	tr.CloseFresh()
	if _, err := fresh.Write([]byte("x")); err == nil {
		t.Fatal("unused connection must be closed on shutdown")
	}
	go func() { buf := make([]byte, 1); _, _ = busyPeer.Read(buf) }()
	if _, err := busy.Write([]byte("x")); err != nil {
		t.Fatalf("active connection was closed: %v", err)
	}

	late, latePeer := net.Pipe()
	defer latePeer.Close()
	tr.ConnState(late, http.StateNew)
	if _, err := late.Write([]byte("x")); err == nil {
		t.Fatal("connections accepted after shutdown began must be closed")
	}
}
