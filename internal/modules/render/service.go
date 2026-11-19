package render

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"
)

// Service runs renders on a bounded number of workers, renders each key once however
// many requests ask for it at the same time, and keeps recent results in memory.
// Renders are deterministic (the key holds the file hash and every parameter), so a
// cached result never goes stale.
type listElement = list.Element

type Service struct {
	sem        chan struct{}
	group      singleflight.Group
	provider   renderProviderSlot
	generation atomic.Uint64

	mu    sync.Mutex
	items map[string]*listElement
	order *list.List // most recently used first
	size  int
	limit int
}

type entry struct {
	key  string
	data []byte
}

type renderOutcome struct {
	data      []byte
	cacheable bool
}

// NewService creates a service with workers concurrent renders and a cache of at
// most cacheBytes encoded bytes.
func NewService(workers, cacheBytes int) *Service {
	s := &Service{sem: make(chan struct{}, max(1, workers)), items: map[string]*list.Element{}, order: list.New(), limit: cacheBytes}
	s.generation.Store(1)
	return s
}

// Get returns the result for key, calling render on a miss.
func (s *Service) Get(ctx context.Context, key string, render func() ([]byte, error)) ([]byte, error) {
	b, _, err := s.GetWithCacheable(ctx, key, func() ([]byte, bool, error) {
		out, rErr := render()
		return out, true, rErr
	})
	return b, err
}

// GetWithCacheable returns the result for key, calling render on a miss and only
// storing the result in the LRU cache when cacheable is true and the cache generation
// did not rotate while render was in flight.
func (s *Service) GetWithCacheable(ctx context.Context, key string, render func() ([]byte, bool, error)) ([]byte, bool, error) {
	if b, ok := s.lookup(key); ok {
		return b, true, nil
	}
	v, err, _ := s.group.Do(key, func() (any, error) {
		if b, ok := s.lookup(key); ok {
			return renderOutcome{data: b, cacheable: true}, nil
		}
		genBefore := s.generation.Load()
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer func() { <-s.sem }()
		b, cacheable, err := render()
		if err != nil {
			return nil, err
		}
		if cacheable {
			s.store(key, genBefore, b)
		}
		return renderOutcome{data: b, cacheable: cacheable}, nil
	})
	if err != nil {
		return nil, false, err
	}
	out := v.(renderOutcome)
	return out.data, out.cacheable, nil
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

func (s *Service) store(key string, expectedGen uint64, data []byte) {
	if len(data) > s.limit {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation.Load() != expectedGen {
		return
	}
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
