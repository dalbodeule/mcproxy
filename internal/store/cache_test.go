package store

import (
	"testing"
	"time"
)

func TestSnapshotCacheInvalidationAdvancesGeneration(t *testing.T) {
	cache := newSnapshotCache()
	cache.mu.Lock()
	cache.loadedAt = time.Now()
	cache.mu.Unlock()
	if cache.stale() {
		t.Fatal("fresh cache unexpectedly reported stale")
	}

	before := cache.generation.Load()
	cache.invalidate()
	if got := cache.generation.Load(); got != before+1 {
		t.Fatalf("generation = %d, want %d", got, before+1)
	}
	if !cache.stale() {
		t.Fatal("invalidated cache did not become stale")
	}
}
