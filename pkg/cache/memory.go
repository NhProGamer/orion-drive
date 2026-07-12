package cache

import (
	"sync"
	"time"
)

type item struct {
	value   []byte
	expires time.Time // zero means no expiry
}

// Memory is an in-process Store with lazy expiration plus a background sweeper.
type Memory struct {
	mu    sync.RWMutex
	items map[string]item
}

// NewMemory returns a ready in-memory cache and starts its janitor.
func NewMemory() *Memory {
	m := &Memory{items: make(map[string]item)}
	go m.sweep()
	return m
}

// Set stores value under key for ttl (0 = no expiry).
func (m *Memory) Set(key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it := item{value: value}
	if ttl > 0 {
		it.expires = time.Now().Add(ttl)
	}
	m.items[key] = it
	return nil
}

// Get returns the value and whether it was present and unexpired.
func (m *Memory) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	it, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !it.expires.IsZero() && time.Now().After(it.expires) {
		m.Delete(key)
		return nil, false
	}
	return it.value, true
}

// Delete removes a key.
func (m *Memory) Delete(key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

func (m *Memory) sweep() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for k, it := range m.items {
			if !it.expires.IsZero() && now.After(it.expires) {
				delete(m.items, k)
			}
		}
		m.mu.Unlock()
	}
}
