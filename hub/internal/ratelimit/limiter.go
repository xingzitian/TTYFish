// Package ratelimit provides bounded, concurrent fixed-window request counters.
package ratelimit

import (
	"sort"
	"sync"
	"time"
)

type Limit struct {
	Key    string
	Max    int
	Window time.Duration
}
type counter struct {
	count int
	until time.Time
}
type Limiter struct {
	mu        sync.Mutex
	counters  map[string]counter
	capacity  int
	now       func() time.Time
	nextSweep time.Time
}

func New(capacity int, clocks ...func() time.Time) *Limiter {
	if capacity < 1 {
		panic("rate limiter capacity must be positive")
	}
	clock := time.Now
	if len(clocks) > 0 {
		clock = clocks[0]
		if clock == nil {
			panic("rate limiter clock must not be nil")
		}
	}
	return &Limiter{counters: make(map[string]counter), capacity: capacity, now: clock}
}

// Allow reserves all requested budgets atomically. A rejection consumes none of them.
// Zero means allowed; otherwise the returned duration is the earliest safe retry time.
func (l *Limiter) Allow(limits ...Limit) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !now.Before(l.nextSweep) || len(l.counters)+len(limits) > l.capacity {
		for key, c := range l.counters {
			if !now.Before(c.until) {
				delete(l.counters, key)
			}
		}
		l.nextSweep = now.Add(30 * time.Second)
	}
	var retry time.Duration
	missing := 0
	seen := map[string]bool{}
	for _, limit := range limits {
		if limit.Max <= 0 || limit.Window <= 0 {
			panic("invalid rate limit")
		}
		if seen[limit.Key] {
			panic("duplicate rate limit key")
		}
		seen[limit.Key] = true
		c, exists := l.counters[limit.Key]
		if !exists {
			missing++
		}
		if exists && now.Before(c.until) && c.count >= limit.Max && c.until.Sub(now) > retry {
			retry = c.until.Sub(now)
		}
	}
	if retry > 0 {
		return retry
	}
	if len(l.counters)+missing > l.capacity {
		// Never evict active counters: doing so would let rotating identities reset budgets.
		waits := make([]time.Duration, 0, len(l.counters))
		for _, c := range l.counters {
			waits = append(waits, c.until.Sub(now))
		}
		sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
		needed := len(l.counters) + missing - l.capacity
		if needed > len(waits) {
			return time.Minute
		}
		return waits[needed-1]
	}
	for _, limit := range limits {
		c := l.counters[limit.Key]
		if !now.Before(c.until) {
			c = counter{until: now.Add(limit.Window)}
		}
		c.count++
		l.counters[limit.Key] = c
	}
	return 0
}
