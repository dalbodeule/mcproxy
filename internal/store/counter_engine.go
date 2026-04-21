package store

import (
	"fmt"
	"sync"
	"time"
)

type bucketCounter struct {
	mu      sync.Mutex
	buckets map[string]*windowBucket
}

type windowBucket struct {
	start time.Time
	count int
}

func newBucketCounter() *bucketCounter {
	return &bucketCounter{buckets: make(map[string]*windowBucket)}
}

func (c *bucketCounter) bump(scope string, serverID *int, kind, key string, windowSec int, record bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	composite := fmt.Sprintf("%s:%v:%s:%s:%d", scope, serverIDValue(serverID), kind, key, windowSec)
	now := time.Now().UTC()
	b, ok := c.buckets[composite]
	if !ok || now.Sub(b.start) >= time.Duration(windowSec)*time.Second {
		b = &windowBucket{start: now, count: 0}
		c.buckets[composite] = b
	}
	if record {
		b.count++
	}
	return b.count
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
