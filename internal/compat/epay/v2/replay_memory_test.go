package v2

import (
	"context"
	"testing"
	"time"
)

func TestMemoryReplayAcceptsOneSignature(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	store := NewMemoryReplayWithClock(func() time.Time { return now })
	key := ReplayKey("merchant-1", "pay/refund", "signature-value")

	accepted, err := store.Once(context.Background(), key, NonceTTL)
	if err != nil || !accepted {
		t.Fatalf("first once: accepted=%v err=%v", accepted, err)
	}
	accepted, err = store.Once(context.Background(), key, NonceTTL)
	if err != nil || accepted {
		t.Fatalf("second once: accepted=%v err=%v", accepted, err)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("len = %d, want 1", got)
	}
}

func TestMemoryReplayForgetsKeysAfterTTL(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	store := NewMemoryReplayWithClock(func() time.Time { return now })
	key := ReplayKey("merchant-1", "pay/close", "signature-value")

	if accepted, err := store.Once(context.Background(), key, time.Minute); err != nil || !accepted {
		t.Fatalf("first once: accepted=%v err=%v", accepted, err)
	}
	now = now.Add(2 * time.Minute)
	if accepted, err := store.Once(context.Background(), key, time.Minute); err != nil || !accepted {
		t.Fatalf("once after ttl: accepted=%v err=%v", accepted, err)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("len = %d, want the expired entry to be swept", got)
	}
}

func TestMemoryReplaySweepsExpiredEntries(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	store := NewMemoryReplayWithClock(func() time.Time { return now })
	for _, key := range []string{"a", "b", "c"} {
		if _, err := store.Once(context.Background(), key, NonceTTL); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.Len(); got != 3 {
		t.Fatalf("len = %d, want 3", got)
	}
	now = now.Add(NonceTTL + time.Second)
	if _, err := store.Once(context.Background(), "d", NonceTTL); err != nil {
		t.Fatal(err)
	}
	if got := store.Len(); got != 1 {
		t.Fatalf("len = %d, want only the fresh entry", got)
	}
}

func TestMemoryReplayNilStoreIsSafe(t *testing.T) {
	var store *MemoryReplayStore
	if accepted, err := store.Once(context.Background(), "key", NonceTTL); accepted || err != nil {
		t.Fatalf("nil store: accepted=%v err=%v", accepted, err)
	}
	if got := store.Len(); got != 0 {
		t.Fatalf("len = %d", got)
	}
}

// TestServiceUsesProvidedReplayStore makes sure an injected store is actually used,
// which is how the platform swaps Redis in for the database fallback.
func TestServiceUsesProvidedReplayStore(t *testing.T) {
	store := NewMemoryReplay()
	service := New(Deps{Replay: store})
	if service.replayStore() != store {
		t.Fatal("replayStore did not return the injected store")
	}
	// Without an injected store the database fallback is built once and reused.
	fallback := New(Deps{})
	first := fallback.replayStore()
	if first == nil {
		t.Fatal("fallback store is nil")
	}
	if second := fallback.replayStore(); second != first {
		t.Fatal("fallback store was rebuilt")
	}
	if _, ok := first.(*DBReplayStore); !ok {
		t.Fatalf("fallback store is %T, want *DBReplayStore", first)
	}
}
