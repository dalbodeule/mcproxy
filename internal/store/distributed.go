package store

import (
	"context"
	"crypto/tls"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisCounterPrefix = "mcproxy:counter:"
const redisLoginPrefix = "mcproxy:login:"

type distributedCoordinator struct {
	redis   *redis.Client
	channel string
	cancel  context.CancelFunc
}

var slidingWindowScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local cutoff = tonumber(ARGV[2])
local member = ARGV[3]
local ttl = tonumber(ARGV[4])
local record = ARGV[5]
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
if record == '1' then
  redis.call('ZADD', key, now, member)
end
local count = redis.call('ZCARD', key)
redis.call('PEXPIRE', key, ttl)
return count
`)

var loginReservationScript = redis.NewScript(`
local key = KEYS[1]
local member = ARGV[1]
local now = tonumber(ARGV[2])
local expires = tonumber(ARGV[3])
local limit = tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', key, '-inf', now)
if redis.call('ZSCORE', key, member) then
  redis.call('PEXPIRE', key, expires - now + 1000)
  return 1
end
if redis.call('ZCARD', key) >= limit then
  redis.call('PEXPIRE', key, expires - now + 1000)
  return 0
end
redis.call('ZADD', key, expires, member)
redis.call('PEXPIRE', key, expires - now + 1000)
return 1
`)

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

func (d *distributedCoordinator) Ping(ctx context.Context) error {
	if d == nil || d.redis == nil {
		return fmt.Errorf("redis coordinator unavailable")
	}
	return d.redis.Ping(ctx).Err()
}

func (d *distributedCoordinator) PublishInvalidate(ctx context.Context) error {
	if d == nil || d.redis == nil {
		return fmt.Errorf("redis coordinator unavailable")
	}
	return d.redis.Publish(ctx, d.channel, "invalidate").Err()
}

func (d *distributedCoordinator) IncrWindow(ctx context.Context, scope string, serverID *int, kind, key string, windowSec int, record bool) (int, error) {
	if d == nil || d.redis == nil {
		return 0, fmt.Errorf("redis coordinator unavailable")
	}
	if windowSec < 1 {
		windowSec = 1
	}
	redisKey := fmt.Sprintf("%s%s:%d:%s:%s:%d", redisCounterPrefix, scope, serverIDValue(serverID), kind, key, windowSec)
	now := time.Now().UnixMilli()
	member := strconv.FormatInt(now, 10) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	flag := "0"
	if record {
		flag = "1"
	}
	count, err := slidingWindowScript.Run(ctx, d.redis, []string{redisKey}, now, now-int64(windowSec)*1000, member, (windowSec+1)*1000, flag).Int()
	if err != nil {
		return 0, err
	}
	return count, nil
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

func (d *distributedCoordinator) ReserveLogin(ctx context.Context, ip, member string, limit int, ttl time.Duration) (bool, error) {
	if d == nil || d.redis == nil {
		return false, fmt.Errorf("redis coordinator unavailable")
	}
	if limit < 1 {
		return true, nil
	}
	now := time.Now().UnixMilli()
	expires := now + ttl.Milliseconds()
	if expires <= now {
		expires = now + 90_000
	}
	key := redisLoginPrefix + ip
	allowed, err := loginReservationScript.Run(ctx, d.redis, []string{key}, member, now, expires, limit).Int()
	return allowed == 1, err
}

func (d *distributedCoordinator) ReleaseLogin(ctx context.Context, ip, member string) error {
	if d == nil || d.redis == nil {
		return fmt.Errorf("redis coordinator unavailable")
	}
	return d.redis.ZRem(ctx, redisLoginPrefix+ip, member).Err()
}
