package health

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheckerReportsReadyWhenDependenciesAreHealthy(t *testing.T) {
	databaseCalls := 0
	redisCalls := 0
	checker := NewChecker(
		func(context.Context) error {
			databaseCalls++
			return nil
		},
		func(context.Context) error {
			redisCalls++
			return nil
		},
	)

	if err := checker.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if databaseCalls != 1 || redisCalls != 1 {
		t.Fatalf("dependency calls = database %d, redis %d; want both 1", databaseCalls, redisCalls)
	}
}

func TestCheckerReportsPostgreSQLFailureAndSkipsRedis(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	redisCalls := 0
	checker := NewChecker(
		func(context.Context) error { return databaseErr },
		func(context.Context) error {
			redisCalls++
			return nil
		},
	)

	err := checker.Check(context.Background())
	if !errors.Is(err, databaseErr) {
		t.Fatalf("Check() error = %v, want wrapped database error", err)
	}
	if !strings.Contains(err.Error(), "postgresql") {
		t.Fatalf("Check() error = %q, want postgresql component", err)
	}
	if redisCalls != 0 {
		t.Fatalf("redis calls = %d, want 0 after database failure", redisCalls)
	}
}

func TestCheckerReportsRedisFailure(t *testing.T) {
	redisErr := errors.New("redis unavailable")
	checker := NewChecker(
		func(context.Context) error { return nil },
		func(context.Context) error { return redisErr },
	)

	err := checker.Check(context.Background())
	if !errors.Is(err, redisErr) {
		t.Fatalf("Check() error = %v, want wrapped redis error", err)
	}
	if !strings.Contains(err.Error(), "redis") {
		t.Fatalf("Check() error = %q, want redis component", err)
	}
}

func TestCheckerPassesContextToDependencies(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-value")
	checker := NewChecker(
		func(got context.Context) error {
			if got.Value(contextKey{}) != "request-value" {
				t.Fatalf("database context value = %v, want request-value", got.Value(contextKey{}))
			}
			return nil
		},
		func(got context.Context) error {
			if got.Value(contextKey{}) != "request-value" {
				t.Fatalf("redis context value = %v, want request-value", got.Value(contextKey{}))
			}
			return nil
		},
	)

	if err := checker.Check(ctx); err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
}
