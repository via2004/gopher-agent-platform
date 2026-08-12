package redis

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newIntegrationRateLimiter(t *testing.T, limit int64, window time.Duration) (*RateLimiter, *redis.Client, string) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := client.Ping(ctx).Err()
	cancel()
	if err != nil {
		_ = client.Close()
		t.Skipf("Redis/Valkey is unavailable: %v", err)
	}
	limiter, err := NewRateLimiter(client, limit, window)
	if err != nil {
		_ = client.Close()
		t.Fatalf("NewRateLimiter() error = %v", err)
	}
	keyPrefix := fmt.Sprintf("gopherai:test:rate-limit:%d", time.Now().UnixNano())
	limiter.prefix = keyPrefix
	t.Cleanup(func() {
		_ = client.Del(context.Background(), keyPrefix+":1", keyPrefix+":2").Err()
		_ = client.Close()
	})
	return limiter, client, keyPrefix
}

func TestRateLimiterAllowTracksCountAndTTL(t *testing.T) {
	limiter, _, _ := newIntegrationRateLimiter(t, 2, 5*time.Second)
	ctx := context.Background()

	allowed, retryAfter, err := limiter.Allow(ctx, 1)
	if err != nil || !allowed {
		t.Fatalf("first Allow() = %v, %v, want allowed", allowed, err)
	}
	if retryAfter <= 0 || retryAfter > 5*time.Second {
		t.Fatalf("first retryAfter = %s, want (0, 5s]", retryAfter)
	}

	allowed, _, err = limiter.Allow(ctx, 1)
	if err != nil || !allowed {
		t.Fatalf("second Allow() = %v, %v, want allowed", allowed, err)
	}
	allowed, retryAfter, err = limiter.Allow(ctx, 1)
	if err != nil || allowed {
		t.Fatalf("third Allow() = %v, %v, want denied", allowed, err)
	}
	if retryAfter <= 0 {
		t.Fatalf("denied retryAfter = %s, want positive", retryAfter)
	}
}

func TestRateLimiterSeparatesUsers(t *testing.T) {
	limiter, _, _ := newIntegrationRateLimiter(t, 1, time.Second)
	ctx := context.Background()

	allowed, _, err := limiter.Allow(ctx, 1)
	if err != nil || !allowed {
		t.Fatalf("user 1 Allow() = %v, %v, want allowed", allowed, err)
	}
	allowed, _, err = limiter.Allow(ctx, 2)
	if err != nil || !allowed {
		t.Fatalf("user 2 Allow() = %v, %v, want allowed", allowed, err)
	}
}

func TestRateLimiterWindowExpires(t *testing.T) {
	limiter, _, _ := newIntegrationRateLimiter(t, 1, 100*time.Millisecond)
	ctx := context.Background()

	if allowed, _, err := limiter.Allow(ctx, 1); err != nil || !allowed {
		t.Fatalf("first Allow() = %v, %v, want allowed", allowed, err)
	}
	if allowed, _, err := limiter.Allow(ctx, 1); err != nil || allowed {
		t.Fatalf("second Allow() = %v, %v, want denied", allowed, err)
	}
	time.Sleep(150 * time.Millisecond)
	if allowed, _, err := limiter.Allow(ctx, 1); err != nil || !allowed {
		t.Fatalf("Allow() after expiry = %v, %v, want allowed", allowed, err)
	}
}

func TestRateLimiterConcurrentRequestsRespectLimit(t *testing.T) {
	const limit = int64(10)
	limiter, _, _ := newIntegrationRateLimiter(t, limit, 5*time.Second)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _, err := limiter.Allow(context.Background(), 1)
			if err != nil {
				t.Errorf("Allow() error = %v", err)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != limit {
		t.Fatalf("allowed concurrent requests = %d, want %d", got, limit)
	}
}
