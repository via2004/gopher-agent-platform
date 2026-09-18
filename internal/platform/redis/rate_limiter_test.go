package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"
)

func TestRateLimiterRejectsEmptyIdentity(t *testing.T) {
	limiter := &RateLimiter{}

	allowed, retryAfter, err := limiter.Allow(context.Background(), "  ")

	if allowed || retryAfter != 0 || !errors.Is(err, ErrIdentityInvalid) {
		t.Fatalf("Allow() = %v, %v, %v; want false, 0, %v", allowed, retryAfter, err, ErrIdentityInvalid)
	}
}

func TestIPRateLimiterConstructorsValidateSharedConfiguration(t *testing.T) {
	client := redisclient.NewClient(&redisclient.Options{Addr: "127.0.0.1:6379"})
	t.Cleanup(func() { _ = client.Close() })

	tests := []struct {
		name    string
		client  *redisclient.Client
		limit   int64
		window  time.Duration
		wantErr error
	}{
		{name: "missing client", limit: 1, window: time.Second, wantErr: ErrClientInvalid},
		{name: "invalid limit", client: client, window: time.Second, wantErr: ErrLimitInvalid},
		{name: "invalid window", client: client, limit: 1, wantErr: ErrWindowInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewAuthRegisterRateLimiter(test.client, test.limit, test.window)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewAuthRegisterRateLimiter() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
