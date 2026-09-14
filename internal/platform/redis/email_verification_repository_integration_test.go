package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"gopherai/internal/emailverification"
)

func newIntegrationEmailVerificationCodeStore(t *testing.T) (*EmailVerificationCodeStore, *goredis.Client) {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := client.Ping(ctx).Err()
	cancel()
	if err != nil {
		_ = client.Close()
		t.Skipf("Redis/Valkey is unavailable: %v", err)
	}
	store, err := NewEmailVerificationCodeStore(client)
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	store.prefix = fmt.Sprintf("gopherai:test:email_verification:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		var keys []string
		for _, email := range []string{"one@example.com", "two@example.com", "user@example.com"} {
			keys = append(keys, store.codeKey(email), store.cooldownKey(email), store.attemptsKey(email))
		}
		_ = client.Del(context.Background(), keys...).Err()
		_ = client.Close()
	})
	return store, client
}

func TestNewEmailVerificationCodeStoreRejectsNilClient(t *testing.T) {
	store, err := NewEmailVerificationCodeStore(nil)
	if !errors.Is(err, ErrClientInvalid) || store != nil {
		t.Fatalf("NewEmailVerificationCodeStore() = %#v, %v", store, err)
	}
}

func TestEmailVerificationKeysAreHashedAndSeparated(t *testing.T) {
	store := &EmailVerificationCodeStore{prefix: "test"}
	email := "user@example.com"
	keys := []string{store.codeKey(email), store.cooldownKey(email), store.attemptsKey(email)}
	for _, key := range keys {
		if strings.Contains(key, email) {
			t.Fatalf("key exposes email: %q", key)
		}
	}
	if keys[0] == keys[1] || keys[0] == keys[2] || keys[1] == keys[2] {
		t.Fatalf("keys are not separated: %#v", keys)
	}
	if store.codeKey(email) != keys[0] || store.codeKey("other@example.com") == keys[0] {
		t.Fatalf("key hashing is not deterministic or email-specific")
	}
}

func TestEmailVerificationCodeStoreSaveAndCooldown(t *testing.T) {
	store, client := newIntegrationEmailVerificationCodeStore(t)
	ctx := context.Background()
	email := "user@example.com"
	if err := client.Set(ctx, store.attemptsKey(email), 3, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(ctx, email, "123456", 3*time.Second, 300*time.Millisecond); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if value, err := client.Get(ctx, store.codeKey(email)).Result(); err != nil || value != "123456" {
		t.Fatalf("stored code = %q, %v", value, err)
	}
	if exists, err := client.Exists(ctx, store.attemptsKey(email)).Result(); err != nil || exists != 0 {
		t.Fatalf("attempts exists = %d, %v; want cleared", exists, err)
	}
	for _, key := range []string{store.codeKey(email), store.cooldownKey(email)} {
		ttl, err := client.PTTL(ctx, key).Result()
		if err != nil || ttl <= 0 {
			t.Fatalf("PTTL(%q) = %s, %v", key, ttl, err)
		}
	}

	if err := store.Save(ctx, email, "654321", 3*time.Second, 300*time.Millisecond); !errors.Is(err, emailverification.ErrSendTooFrequent) {
		t.Fatalf("second Save() error = %v, want %v", err, emailverification.ErrSendTooFrequent)
	}
	if value, err := client.Get(ctx, store.codeKey(email)).Result(); err != nil || value != "123456" {
		t.Fatalf("code during cooldown = %q, %v; want original", value, err)
	}

	time.Sleep(400 * time.Millisecond)
	if err := store.Save(ctx, email, "654321", 3*time.Second, 300*time.Millisecond); err != nil {
		t.Fatalf("Save() after cooldown error = %v", err)
	}
	if value, err := client.Get(ctx, store.codeKey(email)).Result(); err != nil || value != "654321" {
		t.Fatalf("new code = %q, %v", value, err)
	}
}

func TestEmailVerificationCodeStoreConcurrentSaveAllowsOne(t *testing.T) {
	store, _ := newIntegrationEmailVerificationCodeStore(t)
	const callers = 12
	var succeeded atomic.Int64
	var tooFrequent atomic.Int64
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			code := fmt.Sprintf("%06d", index)
			err := store.Save(context.Background(), "user@example.com", code, time.Minute, time.Minute)
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, emailverification.ErrSendTooFrequent):
				tooFrequent.Add(1)
			default:
				t.Errorf("Save() error = %v", err)
			}
		}(i)
	}
	wait.Wait()
	if succeeded.Load() != 1 || tooFrequent.Load() != callers-1 {
		t.Fatalf("Save() results = success %d, too frequent %d", succeeded.Load(), tooFrequent.Load())
	}
}

func TestEmailVerificationCodeStoreCountsFailuresAndExpiresCode(t *testing.T) {
	store, client := newIntegrationEmailVerificationCodeStore(t)
	ctx := context.Background()
	email := "user@example.com"
	if err := store.Save(ctx, email, "123456", 3*time.Second, time.Second); err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt < 3; attempt++ {
		err := store.VerifyAndConsume(ctx, email, "000000", 3)
		if !errors.Is(err, emailverification.ErrCodeInvalidOrExpired) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
		value, getErr := client.Get(ctx, store.attemptsKey(email)).Int()
		if getErr != nil || value != attempt {
			t.Fatalf("attempt counter = %d, %v; want %d", value, getErr, attempt)
		}
		attemptTTL, _ := client.PTTL(ctx, store.attemptsKey(email)).Result()
		codeTTL, _ := client.PTTL(ctx, store.codeKey(email)).Result()
		if attemptTTL <= 0 || attemptTTL > codeTTL+100*time.Millisecond {
			t.Fatalf("attempt TTL = %s, code TTL = %s", attemptTTL, codeTTL)
		}
	}

	if err := store.VerifyAndConsume(ctx, email, "000000", 3); !errors.Is(err, emailverification.ErrTooManyAttempts) {
		t.Fatalf("last attempt error = %v, want %v", err, emailverification.ErrTooManyAttempts)
	}
	if exists, err := client.Exists(ctx, store.codeKey(email), store.attemptsKey(email)).Result(); err != nil || exists != 0 {
		t.Fatalf("verification state exists = %d, %v; want deleted", exists, err)
	}
	if exists, err := client.Exists(ctx, store.cooldownKey(email)).Result(); err != nil || exists != 1 {
		t.Fatalf("cooldown exists = %d, %v; want retained", exists, err)
	}
}

func TestEmailVerificationCodeStoreConsumesCodeOnceConcurrently(t *testing.T) {
	store, client := newIntegrationEmailVerificationCodeStore(t)
	ctx := context.Background()
	email := "user@example.com"
	if err := store.Save(ctx, email, "123456", time.Minute, time.Minute); err != nil {
		t.Fatal(err)
	}

	const callers = 12
	var succeeded atomic.Int64
	var rejected atomic.Int64
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := store.VerifyAndConsume(context.Background(), email, "123456", 5)
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, emailverification.ErrCodeInvalidOrExpired):
				rejected.Add(1)
			default:
				t.Errorf("VerifyAndConsume() error = %v", err)
			}
		}()
	}
	wait.Wait()
	if succeeded.Load() != 1 || rejected.Load() != callers-1 {
		t.Fatalf("VerifyAndConsume() results = success %d, rejected %d", succeeded.Load(), rejected.Load())
	}
	if exists, err := client.Exists(ctx, store.codeKey(email), store.attemptsKey(email)).Result(); err != nil || exists != 0 {
		t.Fatalf("verification state exists = %d, %v", exists, err)
	}
}

func TestEmailVerificationCodeStoreDeleteIsConditional(t *testing.T) {
	store, client := newIntegrationEmailVerificationCodeStore(t)
	ctx := context.Background()
	email := "user@example.com"
	if err := store.Save(ctx, email, "123456", time.Minute, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, store.attemptsKey(email), 1, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(ctx, email, "654321"); err != nil {
		t.Fatalf("Delete(wrong code) error = %v", err)
	}
	if exists, err := client.Exists(ctx, store.codeKey(email), store.cooldownKey(email), store.attemptsKey(email)).Result(); err != nil || exists != 3 {
		t.Fatalf("state after mismatched Delete = %d keys, %v; want 3", exists, err)
	}

	if err := store.Delete(ctx, email, "123456"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if exists, err := client.Exists(ctx, store.codeKey(email), store.cooldownKey(email), store.attemptsKey(email)).Result(); err != nil || exists != 0 {
		t.Fatalf("state after Delete = %d keys, %v; want 0", exists, err)
	}
	if err := store.Delete(ctx, email, "123456"); err != nil {
		t.Fatalf("idempotent Delete() error = %v", err)
	}
}

func TestEmailVerificationCodeStoreRejectsExpiredAndCorruptTTL(t *testing.T) {
	store, client := newIntegrationEmailVerificationCodeStore(t)
	ctx := context.Background()
	email := "user@example.com"
	if err := store.Save(ctx, email, "123456", 50*time.Millisecond, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := store.VerifyAndConsume(ctx, email, "123456", 5); !errors.Is(err, emailverification.ErrCodeInvalidOrExpired) {
		t.Fatalf("expired VerifyAndConsume() error = %v", err)
	}

	for _, code := range []string{"000000", "123456"} {
		if err := client.Set(ctx, store.codeKey(email), "123456", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if err := store.VerifyAndConsume(ctx, email, code, 5); !errors.Is(err, ErrUnexpectedTTL) {
			t.Fatalf("corrupt TTL with code %q error = %v, want %v", code, err, ErrUnexpectedTTL)
		}
	}
}

func TestEmailVerificationCodeStoreSeparatesEmails(t *testing.T) {
	store, _ := newIntegrationEmailVerificationCodeStore(t)
	ctx := context.Background()
	if err := store.Save(ctx, "one@example.com", "111111", time.Minute, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "two@example.com", "222222", time.Minute, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyAndConsume(ctx, "one@example.com", "111111", 5); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyAndConsume(ctx, "two@example.com", "222222", 5); err != nil {
		t.Fatal(err)
	}
}

func TestEmailVerificationCodeStoreRejectsInvalidInputBeforeRedis(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	store, err := NewEmailVerificationCodeStore(client)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Save(context.Background(), " user@example.com ", "123456", time.Minute, time.Minute); !errors.Is(err, ErrEmailInvalid) {
		t.Fatalf("invalid email error = %v", err)
	}
	if err := store.Save(context.Background(), "user@example.com", "12345x", time.Minute, time.Minute); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("invalid code error = %v", err)
	}
	if err := store.Save(context.Background(), "user@example.com", "123456", 0, time.Minute); !errors.Is(err, ErrTTLInvalid) {
		t.Fatalf("invalid TTL error = %v", err)
	}
	if err := store.Save(context.Background(), "user@example.com", "123456", time.Minute, 0); !errors.Is(err, ErrCooldownInvalid) {
		t.Fatalf("invalid cooldown error = %v", err)
	}
	if err := store.Save(context.Background(), "user@example.com", "123456", time.Minute, 2*time.Minute); !errors.Is(err, ErrCooldownInvalid) {
		t.Fatalf("cooldown over TTL error = %v", err)
	}
	if err := store.VerifyAndConsume(context.Background(), "user@example.com", "123456", 0); !errors.Is(err, ErrMaxAttemptsInvalid) {
		t.Fatalf("invalid attempts error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Delete(ctx, "user@example.com", "123456"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Delete() error = %v", err)
	}
}
