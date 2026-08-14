package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// bucket is one key's token state. tokens is fractional so a rate that
// does not divide evenly into the period does not drift.
type bucket struct {
	tokens float64
	last   time.Time
	elem   *list.Element // position in the LRU
}

// Memory is an in-process token-bucket limiter, bounded by an LRU.
//
// The bound is not defensive: keys are tenant and subject identifiers,
// so an unbounded map grows with every distinct caller and is a memory
// leak reachable by anyone able to obtain tokens for many subjects. An
// evicted key simply starts with a full burst, which is the safe
// direction for a capacity control — it admits, it does not deny.
type Memory struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	order   *list.List // front = most recently used
	maxKeys int
}

func NewMemory(maxKeys int) *Memory {
	return &Memory{
		buckets: make(map[string]*bucket, maxKeys/4+1),
		order:   list.New(),
		maxKeys: maxKeys,
	}
}

func (m *Memory) Allow(key string, r Rule, now time.Time) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()

	perToken := r.Per / time.Duration(r.Rate) // e.g. 500ms at 120/min

	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(r.Burst), last: now}
		b.elem = m.order.PushFront(key)
		m.buckets[key] = b
		m.evictLocked()
	} else {
		m.order.MoveToFront(b.elem)
		if elapsed := now.Sub(b.last); elapsed > 0 {
			b.tokens += float64(elapsed) / float64(perToken)
			if b.tokens > float64(r.Burst) {
				b.tokens = float64(r.Burst)
			}
			b.last = now
		}
	}

	if b.tokens < 1 {
		// How long until one whole token exists.
		missing := 1 - b.tokens
		retry := time.Duration(missing * float64(perToken))
		return Decision{
			Allowed: false, RetryAfter: retry,
			Limit: r.Rate, Remaining: 0, Reset: retry,
		}
	}
	b.tokens--
	return Decision{
		Allowed: true, Limit: r.Rate,
		Remaining: int(b.tokens),
		Reset:     time.Duration((float64(r.Burst) - b.tokens) * float64(perToken)),
	}
}

// Len reports how many keys are held. Exported for the bound test, which
// cannot otherwise observe eviction.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}

// evictLocked drops the least recently used key while over the bound.
func (m *Memory) evictLocked() {
	for len(m.buckets) > m.maxKeys {
		oldest := m.order.Back()
		if oldest == nil {
			return
		}
		m.order.Remove(oldest)
		delete(m.buckets, oldest.Value.(string))
	}
}
