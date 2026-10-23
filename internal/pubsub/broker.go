// Package pubsub is the in-process fan-out used for change notifications (settings,
// themes, plugins) and the browser SSE stream. A Redis bridge for multi-node setups
// plugs in behind the same Broker.
package pubsub

import (
	"sync"
)

// Message is one published event.
type Message struct {
	Topic string
	Data  any
}

// Broker delivers messages to every subscriber of a topic. Slow subscribers drop
// messages instead of blocking publishers; consumers treat messages as "something
// changed, re-read state", so a dropped message is harmless.
type Broker struct {
	mu     sync.RWMutex
	subs   map[string]map[*subscription]struct{}
	closed bool
}

type subscription struct {
	ch   chan Message
	once sync.Once
}

func (s *subscription) close() { s.once.Do(func() { close(s.ch) }) }

// New returns an empty broker.
func New() *Broker {
	return &Broker{subs: map[string]map[*subscription]struct{}{}}
}

// Subscribe returns a channel for topic and a cancel function that closes it. After
// Close the returned channel is already closed.
func (b *Broker) Subscribe(topic string, buffer int) (<-chan Message, func()) {
	if buffer <= 0 {
		buffer = 8
	}
	s := &subscription{ch: make(chan Message, buffer)}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		s.close()
		return s.ch, func() {}
	}
	if b.subs[topic] == nil {
		b.subs[topic] = map[*subscription]struct{}{}
	}
	b.subs[topic][s] = struct{}{}
	b.mu.Unlock()
	return s.ch, func() {
		b.mu.Lock()
		delete(b.subs[topic], s)
		b.mu.Unlock()
		s.close()
	}
}

// Publish sends data to all current subscribers of topic without blocking.
func (b *Broker) Publish(topic string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for s := range b.subs[topic] {
		select {
		case s.ch <- Message{Topic: topic, Data: data}:
		default:
		}
	}
}

// Close ends every subscription (long-lived consumers such as SSE streams see their
// channel close and return) and turns later Publish/Subscribe calls into no-ops.
// Called when the server starts shutting down.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, set := range b.subs {
		for s := range set {
			s.close()
		}
	}
	b.subs = map[string]map[*subscription]struct{}{}
}

// Topics used across the app.
const (
	// TopicConfigChanged carries an int64 config version.
	TopicConfigChanged = "config.changed"
)
