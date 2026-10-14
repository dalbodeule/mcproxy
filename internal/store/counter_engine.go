package store

import (
	"fmt"
	"sync"
	"time"
)

type bucketCounter struct {
	mu            sync.Mutex
	buckets       map[string]*windowBucket
	operations    uint64
	lastCleanupAt time.Time
}

type windowBucket struct {
	start  time.Time
	count  int // retained for compatibility with stale-bucket tests/debugging
	window int
	slots  []int
	slotAt int64
	total  int
}

func newBucketCounter() *bucketCounter {
	return &bucketCounter{buckets: make(map[string]*windowBucket)}
}

func (c *bucketCounter) bump(scope string, serverID *int, kind, key string, windowSec int, record bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	composite := fmt.Sprintf("%s:%v:%s:%s:%d", scope, serverIDValue(serverID), kind, key, windowSec)
	now := time.Now().UTC()
	c.operations++
	if c.operations%256 == 0 || c.lastCleanupAt.IsZero() || now.Sub(c.lastCleanupAt) >= 30*time.Second {
		for name, bucket := range c.buckets {
			if now.Sub(bucket.start) >= 2*time.Minute || (bucket.window > 0 && now.Sub(bucket.start) >= time.Duration(bucket.window+60)*time.Second) {
				delete(c.buckets, name)
			}
		}
		c.lastCleanupAt = now
	}
	b, ok := c.buckets[composite]
	if windowSec < 1 {
		windowSec = 1
	}
	if !ok || b.window != windowSec {
		b = &windowBucket{start: now, window: windowSec, slots: make([]int, windowSec), slotAt: now.Unix()}
		c.buckets[composite] = b
	}
	// Keep one counter per second. This avoids fixed-window boundary bursts
	// while keeping the hot path allocation-free after a key is initialized.
	currentSlot := now.Unix()
	if currentSlot > b.slotAt {
		advance := currentSlot - b.slotAt
		if advance >= int64(len(b.slots)) {
			for i := range b.slots {
				b.slots[i] = 0
			}
			b.total = 0
		} else {
			for i := int64(1); i <= advance; i++ {
				idx := int((b.slotAt + i) % int64(len(b.slots)))
				b.total -= b.slots[idx]
				b.slots[idx] = 0
			}
		}
		b.slotAt = currentSlot
	}
	if record {
		idx := int(currentSlot % int64(len(b.slots)))
		b.slots[idx]++
		b.total++
	}
	b.count = b.total
	return b.total
}

func (c *bucketCounter) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.buckets)
}

func serverIDValue(id *int) int {
	if id == nil {
		return 0
	}
	return *id
}
