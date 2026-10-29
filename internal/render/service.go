package render

import (
	"container/list"
	"context"
	"sync"

	"golang.org/x/sync/singleflight"
)

// Service runs renders on a bounded number of workers, renders each key once however
// many requests ask for it at the same time, and keeps recent results in memory.
// Renders are deterministic (the key holds the file hash and every parameter), so a
// cached result never goes stale.
type Service struct {
	sem   chan struct{}
	group singleflight.Group

	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List // most recently used first
	size  int
	limit int
}

type entry struct {
	key  string
	data []byte
}

// NewService creates a service with workers concurrent renders and a cache of at
// most cacheBytes encoded bytes.
func NewService(workers, cacheBytes int) *Service {
	return &Service{sem: make(chan struct{}, max(1, workers)), items: map[string]*list.Element{}, order: list.New(), limit: cacheBytes}
}

// Get returns the result for key, calling render on a miss.
func (s *Service) Get(ctx context.Context, key string, render func() ([]byte, error)) ([]byte, error) {
	if b, ok := s.lookup(key); ok {
		return b, nil
	}
	v, err, _ := s.group.Do(key, func() (any, error) {
		if b, ok := s.lookup(key); ok {
			return b, nil
		}
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { <-s.sem }()
		b, err := render()
		if err != nil {
			return nil, err
		}
		s.store(key, b)
		return b, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

func (s *Service) lookup(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		s.order.MoveToFront(el)
		return el.Value.(*entry).data, true
	}
	return nil, false
}

func (s *Service) store(key string, data []byte) {
	if len(data) > s.limit {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[key]; ok {
		return
	}
	s.items[key] = s.order.PushFront(&entry{key, data})
	s.size += len(data)
	for s.size > s.limit {
		last := s.order.Back()
		e := last.Value.(*entry)
		s.order.Remove(last)
		delete(s.items, e.key)
		s.size -= len(e.data)
	}
}
