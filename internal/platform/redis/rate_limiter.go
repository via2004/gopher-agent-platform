package redis

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRateLimiterPrefix             = "gopherai:rate_limit:chat:user"
	defaultRAGUploadRatePrefix           = "gopherai:rate_limit:rag_upload:user"
	defaultTTSRatePrefix                 = "gopherai:rate_limit:tts:user"
	defaultAuthRegisterRatePrefix        = "gopherai:rate_limit:auth_register:ip"
	defaultAuthLoginRatePrefix           = "gopherai:rate_limit:auth_login:ip"
	defaultEmailVerificationIPRatePrefix = "gopherai:rate_limit:email_verification:ip"
)

/*
key:   []string{key},
value: l.window.Milliseconds(),
*/
var allowScript = redis.NewScript(`
	local current = redis.call("INCR", KEYS[1])

	if current == 1 then
		redis.call("PEXPIRE", KEYS[1], ARGV[1])
	end

	local ttl = redis.call("PTTL", KEYS[1])
	return {current, ttl}
`)

type RateLimiter struct {
	client       *redis.Client
	limit        int64
	window       time.Duration
	prefix       string
	hashIdentity bool
}

func NewRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	return newRateLimiter(client, limit, window, defaultRateLimiterPrefix)
}

// NewRAGUploadRateLimiter 创建使用独立 key prefix 的 RAG 上传限流器。
func NewRAGUploadRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	return newRateLimiter(client, limit, window, defaultRAGUploadRatePrefix)
}

// NewTTSRateLimiter 创建使用独立 key prefix 的 TTS 任务创建限流器。
func NewTTSRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	return newRateLimiter(client, limit, window, defaultTTSRatePrefix)
}

func NewAuthRegisterRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	return newHashedRateLimiter(client, limit, window, defaultAuthRegisterRatePrefix)
}

func NewAuthLoginRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	return newHashedRateLimiter(client, limit, window, defaultAuthLoginRatePrefix)
}

func NewEmailVerificationIPRateLimiter(client *redis.Client, limit int64, window time.Duration) (*RateLimiter, error) {
	return newHashedRateLimiter(client, limit, window, defaultEmailVerificationIPRatePrefix)
}

func newHashedRateLimiter(client *redis.Client, limit int64, window time.Duration, prefix string) (*RateLimiter, error) {
	limiter, err := newRateLimiter(client, limit, window, prefix)
	if err != nil {
		return nil, err
	}
	limiter.hashIdentity = true
	return limiter, nil
}

func newRateLimiter(client *redis.Client, limit int64, window time.Duration, prefix string) (*RateLimiter, error) {
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
		prefix: prefix,
	}, nil
}

func (l *RateLimiter) Allow(ctx context.Context, identity string) (bool, time.Duration, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return false, 0, ErrIdentityInvalid
	}
	if l.hashIdentity {
		digest := sha256.Sum256([]byte(identity))
		identity = fmt.Sprintf("%x", digest)
	}
	return l.allowKeySuffix(ctx, identity)
}

func (l *RateLimiter) allowKeySuffix(ctx context.Context, keySuffix string) (bool, time.Duration, error) {
	key := fmt.Sprintf("%s:%s", l.prefix, keySuffix)

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
