package v2

import (
	"context"
	"sync"
	"time"
)

// MemoryReplayStore remembers signed request nonces in process memory. It is the
// right store when the platform runs without Redis and the epay_request_nonces
// table is either absent or too busy to take a row per signed request: the very
// common single process deployment gets replay protection with no extra I/O.
//
// Semantics match DBReplayStore: the first Once for a key reports accepted=true,
// every repeat inside the TTL reports false.
//
// Limitations worth knowing before wiring it: the memory is per process, so two
// replicas do not share nonces (a replay sent to the other replica is accepted),
// and a restart forgets everything. The TTL bounds both holes to the timestamp
// tolerance window, which is why 10 minutes is enough.
type MemoryReplayStore struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]time.Time

	// nextSweep throttles expiry sweeps so Once stays O(1) on the hot path.
	nextSweep time.Time
}

// NewMemoryReplay builds an empty in-process replay store.
func NewMemoryReplay() *MemoryReplayStore {
	return &MemoryReplayStore{now: time.Now, entries: make(map[string]time.Time)}
}

// NewMemoryReplayWithClock is the deterministic variant used by tests.
func NewMemoryReplayWithClock(now func() time.Time) *MemoryReplayStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryReplayStore{now: now, entries: make(map[string]time.Time)}
}

// Once records the key and reports whether it was new.
func (store *MemoryReplayStore) Once(_ context.Context, key string, ttl time.Duration) (bool, error) {
	if store == nil {
		return false, nil
	}
	if ttl <= 0 {
		ttl = NonceTTL
	}
	now := store.now()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.entries == nil {
		store.entries = make(map[string]time.Time)
	}
	if expiresAt, seen := store.entries[key]; seen && expiresAt.After(now) {
		return false, nil
	}
	store.entries[key] = now.Add(ttl)
	sweepInterval := ttl / 2
	if sweepInterval <= 0 {
		sweepInterval = ttl
	}
	if !now.Before(store.nextSweep) {
		store.nextSweep = now.Add(sweepInterval)
		for stored, expiresAt := range store.entries {
			if !expiresAt.After(now) {
				delete(store.entries, stored)
			}
		}
	}
	return true, nil
}

// Len reports how many live entries the store holds, for tests and metrics.
func (store *MemoryReplayStore) Len() int {
	if store == nil {
		return 0
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now()
	live := 0
	for _, expiresAt := range store.entries {
		if expiresAt.After(now) {
			live++
		}
	}
	return live
}
