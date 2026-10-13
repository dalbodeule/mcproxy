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
