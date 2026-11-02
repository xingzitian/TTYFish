package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowsAndAtomicBudgets(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(10)
	l.now = func() time.Time { return now }
	account := Limit{Key: "account", Max: 1, Window: time.Minute}
	source := Limit{Key: "source", Max: 1, Window: time.Minute}
	if l.Allow(account) != 0 {
		t.Fatal("first request rejected")
	}
	now = now.Add(35*time.Second + 200*time.Millisecond)
	if wait := l.Allow(source, account); wait != 24800*time.Millisecond {
		t.Fatalf("incorrect retry time: %v", wait)
	}
	if l.Allow(source) != 0 {
		t.Fatal("rejected combination consumed another budget")
	}
	now = now.Add(24800 * time.Millisecond)
	if l.Allow(account) != 0 {
		t.Fatal("exact window boundary did not reset")
	}
}

func TestCapacityIsBoundedWithoutEviction(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(2)
	l.now = func() time.Time { return now }
	one := Limit{Key: "one", Max: 1, Window: time.Minute}
	two := Limit{Key: "two", Max: 1, Window: time.Minute}
	three := Limit{Key: "three", Max: 1, Window: time.Minute}
	if l.Allow(one, two) != 0 || l.Allow(three) <= 0 {
		t.Fatal("capacity not enforced")
	}
	if l.Allow(one) <= 0 || len(l.counters) != 2 {
		t.Fatal("active counter evicted")
	}
	now = now.Add(time.Minute)
	if l.Allow(three) != 0 || len(l.counters) != 1 {
		t.Fatal("expired entries not reclaimed")
	}
}

func TestConcurrentRequestsDoNotExceedBudget(t *testing.T) {
	l := New(10)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow(Limit{Key: "same", Max: 10, Window: time.Minute}) == 0 {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 10 {
		t.Fatalf("allowed %d requests", successes.Load())
	}
}

func TestCapacityReclaimsExpiredEntriesBeforePeriodicSweep(t *testing.T) {
	now := time.Unix(1000, 0)
	l := New(3, func() time.Time { return now })
	if l.Allow(Limit{Key: "short", Max: 1, Window: 5 * time.Second}, Limit{Key: "long", Max: 1, Window: time.Minute}) != 0 {
		t.Fatal("initial reservation rejected")
	}
	now = now.Add(6 * time.Second)
	newLimits := []Limit{{Key: "new-one", Max: 1, Window: time.Minute}, {Key: "new-two", Max: 1, Window: time.Minute}}
	if l.Allow(newLimits...) != 0 || len(l.counters) != 3 {
		t.Fatal("expired capacity not reclaimed")
	}
	if l.Allow(newLimits...) <= 0 {
		t.Fatal("request was allowed without reserving counters")
	}
}
