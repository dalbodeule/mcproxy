package store

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisCounterPrefix = "mcproxy:counter:"

type distributedCoordinator struct {
	redis   *redis.Client
	channel string
	cancel  context.CancelFunc
}

func newDistributedCoordinator(ctx context.Context, addr, password string, useTLS bool, channel string, onInvalidate func()) (*distributedCoordinator, error) {
	if addr == "" {
		return nil, nil
	}
	options := &redis.Options{Addr: addr, Password: password}
	if useTLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	rdb := redis.NewClient(options)
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	subCtx, cancel := context.WithCancel(context.Background())
	d := &distributedCoordinator{redis: rdb, channel: channel, cancel: cancel}
	pubsub := rdb.Subscribe(subCtx, channel)
	go func() {
		defer pubsub.Close()
		for msg := range pubsub.Channel() {
			if msg != nil {
				onInvalidate()
			}
		}
	}()
	return d, nil
}

func (d *distributedCoordinator) Close() error {
	if d == nil {
		return nil
	}
	if d.cancel != nil {
		d.cancel()
	}
	if d.redis != nil {
		return d.redis.Close()
	}
	return nil
}

func (d *distributedCoordinator) PublishInvalidate(ctx context.Context) {
	if d == nil || d.redis == nil {
		return
	}
	_ = d.redis.Publish(ctx, d.channel, "invalidate").Err()
}

func (d *distributedCoordinator) IncrWindow(ctx context.Context, scope string, serverID *int, kind, key string, windowSec int, record bool) (int, error) {
	if d == nil || d.redis == nil {
		return 0, fmt.Errorf("redis coordinator unavailable")
	}
	redisKey := fmt.Sprintf("%s%s:%d:%s:%s:%d", redisCounterPrefix, scope, serverIDValue(serverID), kind, key, windowSec)
	if record {
		pipe := d.redis.TxPipeline()
		incr := pipe.Incr(ctx, redisKey)
		pipe.Expire(ctx, redisKey, time.Duration(windowSec)*time.Second)
		if _, err := pipe.Exec(ctx); err != nil {
			return 0, err
		}
		return int(incr.Val()), nil
	}
	val, err := d.redis.Get(ctx, redisKey).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

func (d *distributedCoordinator) CountCounters(ctx context.Context) (int, error) {
	if d == nil || d.redis == nil {
		return 0, nil
	}
	var (
		cursor uint64
		total  int
	)
	for {
		keys, next, err := d.redis.Scan(ctx, cursor, redisCounterPrefix+"*", 100).Result()
		if err != nil {
			return 0, err
		}
		total += len(keys)
		cursor = next
		if cursor == 0 {
			return total, nil
		}
	}
}
