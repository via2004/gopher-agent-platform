package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// that's a lua script
var allowScript = redis.NewScript(`
	local current = redis.call("INCR", KEYS[1])

	if current == 1 then
		redis.call("PEXPIRE", KEYS[1], ARGV[1])
	end

	local ttl = redis.call("PTTL", KEYS[1])
	return {current, ttl}
`)

type RateLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
	prefix string
}

func NewRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	if client == nil {
		return nil, ErrClientInvalid
	}
	if limit <= 0 {
		return nil, ErrLimitInvalid
	}
	if window <= 0 {
		return nil, ErrWindowInvalid
	}
	return &RateLimiter{
		client: client,
		limit:  limit,
		window: window,
		prefix: "gopherai:rate_limit:chat:user",
	}, nil
}

func (l *RateLimiter) Allow(ctx context.Context, userID uint64) (bool, time.Duration, error) {
	key := fmt.Sprintf("%s:%d", l.prefix, userID)

	result, err := allowScript.Run(
		ctx,
		l.client,
		[]string{key},
		l.window.Milliseconds(),
	).Int64Slice()

	if err != nil {
		return false, 0, err
	}
	if len(result) != 2 {
		return false, 0, ErrUnexpectedResult
	}

	current := result[0]
	ttlMilliseconds := result[1]

	if ttlMilliseconds < 0 {
		return false, 0, ErrUnexpectedTTL
	}

	allowed := current <= l.limit
	retryAfter := time.Duration(ttlMilliseconds) * time.Millisecond

	return allowed, retryAfter, nil
}
