package store

import (
	"testing"
	"time"
)

func TestBucketCounterPrunesExpiredKeys(t *testing.T) {
	c := newBucketCounter()
	c.buckets["stale"] = &windowBucket{start: time.Now().UTC().Add(-3 * time.Minute), count: 1}
	c.operations = 255

	c.bump("scope", nil, "ip", "198.51.100.10", 60, true)

	if _, ok := c.buckets["stale"]; ok {
		t.Fatal("expected stale counter bucket to be pruned")
	}
}

func TestBucketCounterDoesNotIncrementWhenRecordingDisabled(t *testing.T) {
	c := newBucketCounter()
	if got := c.bump("scope", nil, "ip", "198.51.100.10", 60, false); got != 0 {
		t.Fatalf("expected non-recording bump to remain zero, got %d", got)
	}
	if got := c.bump("scope", nil, "ip", "198.51.100.10", 60, true); got != 1 {
		t.Fatalf("expected next recording bump to be one, got %d", got)
	}
}

func TestBucketCounterUsesSlidingWindow(t *testing.T) {
	c := newBucketCounter()
	key := "198.51.100.11"
	if got := c.bump("scope", nil, "ip", key, 2, true); got != 1 {
		t.Fatalf("first event count = %d, want 1", got)
	}
	if got := c.bump("scope", nil, "ip", key, 2, true); got != 2 {
		t.Fatalf("second event count = %d, want 2", got)
	}
	// Advancing the logical slot is enough to verify stale samples are removed;
	// use the real clock for the boundary behavior and keep the assertion broad.
	time.Sleep(2100 * time.Millisecond)
	if got := c.bump("scope", nil, "ip", key, 2, false); got != 0 {
		t.Fatalf("expired events count = %d, want 0", got)
	}
}
