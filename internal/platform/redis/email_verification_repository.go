package redis

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"gopherai/internal/emailverification"
)

const (
	defaultEmailVerificationPrefix = "gopherai:email_verification"
	maxEmailBytes                  = 254
	verificationCodeLen            = 6
)

/*
	resendTTL > codeTTL 也能正常工作，只是会产生没有有效验证码且不能重发的时间段。
	当前项目主动禁止这种配置，是为了避免较差的用户体验。如下的Lua脚本中的第一个语句,
	先检查resendTTL是否过期,所以如果resendTTL>codeTTL,那这个code都过期了依然还不能resend
	所以从设置上要有resendTTL <= codeTTL,resendTTL过期了也不存在这个空窗期
*/

/*
key:    s.codeKey(email), s.cooldownKey(email), s.attemptsKey(email)},
value:  code, ttl.Milliseconds(), cooldown.Milliseconds(),
*/
var saveEmailVerificationCodeScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[2]) == 1 then
    return 0
end
redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
redis.call("SET", KEYS[2], "1", "PX", ARGV[3])
redis.call("DEL", KEYS[3])
return 1
`)

/*
key:   s.codeKey(email), s.attemptsKey(email)
value: code, maxAttempts,
*/
var verifyAndConsumeEmailVerificationCodeScript = redis.NewScript(`
local stored = redis.call("GET", KEYS[1])
if not stored then
    redis.call("DEL", KEYS[2])
    return 0
end
local ttl = redis.call("PTTL", KEYS[1])
if ttl <= 0 then
    redis.call("DEL", KEYS[1], KEYS[2])
    return -2
end
if stored == ARGV[1] then
    redis.call("DEL", KEYS[1], KEYS[2])
    return 1
end
local attempts = redis.call("INCR", KEYS[2])
redis.call("PEXPIRE", KEYS[2], ttl)
if attempts >= tonumber(ARGV[2]) then
    redis.call("DEL", KEYS[1], KEYS[2])
    return -1
end
return 0
`)

/*
key:   s.codeKey(email), s.cooldownKey(email), s.attemptsKey(email)},
value: code,
*/
var deleteEmailVerificationCodeScript = redis.NewScript(`
local stored = redis.call("GET", KEYS[1])
if stored and stored == ARGV[1] then
    redis.call("DEL", KEYS[1], KEYS[2], KEYS[3])
    return 1
end
return 0
`)

// EmailVerificationCodeStore 使用 Redis 保存短期邮箱验证码、发送冷却和失败次数。
type EmailVerificationCodeStore struct {
	client *redis.Client
	prefix string
}

func NewEmailVerificationCodeStore(client *redis.Client) (*EmailVerificationCodeStore, error) {
	if client == nil {
		return nil, ErrClientInvalid
	}
	return &EmailVerificationCodeStore{
		client: client,
		prefix: defaultEmailVerificationPrefix,
	}, nil
}

// Save 原子保存验证码和发送冷却；冷却尚未结束时拒绝覆盖现有验证码。
func (s *EmailVerificationCodeStore) Save(
	ctx context.Context,
	email, code string,
	ttl, cooldown time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validEmailKeyInput(email) {
		return ErrEmailInvalid
	}
	if !validVerificationCode(code) {
		return ErrCodeInvalid
	}
	if ttl < time.Millisecond {
		return ErrTTLInvalid
	}
	if cooldown < time.Millisecond {
		return ErrCooldownInvalid
	}
	if cooldown > ttl {
		return ErrCooldownInvalid
	}

	result, err := saveEmailVerificationCodeScript.Run(
		ctx,
		s.client,
		[]string{s.codeKey(email), s.cooldownKey(email), s.attemptsKey(email)},
		code,
		ttl.Milliseconds(),
		cooldown.Milliseconds(),
	).Int64()
	if err != nil {
		return fmt.Errorf("save email verification code: %w", err)
	}
	switch result {
	case 1:
		return nil
	case 0:
		return emailverification.ErrSendTooFrequent
	default:
		return ErrUnexpectedResult
	}
}

// VerifyAndConsume 原子校验验证码、累计错误次数，并在正确时消费验证码。
func (s *EmailVerificationCodeStore) VerifyAndConsume(
	ctx context.Context,
	email, code string,
	maxAttempts int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validEmailKeyInput(email) {
		return ErrEmailInvalid
	}
	if !validVerificationCode(code) {
		return ErrCodeInvalid
	}
	if maxAttempts <= 0 {
		return ErrMaxAttemptsInvalid
	}

	result, err := verifyAndConsumeEmailVerificationCodeScript.Run(
		ctx,
		s.client,
		[]string{s.codeKey(email), s.attemptsKey(email)},
		code,
		maxAttempts,
	).Int64()
	if err != nil {
		return fmt.Errorf("verify email verification code: %w", err)
	}
	switch result {
	case 1:
		return nil
	case 0:
		return emailverification.ErrCodeInvalidOrExpired
	case -1:
		return emailverification.ErrTooManyAttempts
	case -2:
		return ErrUnexpectedTTL
	default:
		return ErrUnexpectedResult
	}
}

// Delete 仅在 Redis 中仍保存指定验证码时删除本次发送产生的全部临时状态。
func (s *EmailVerificationCodeStore) Delete(ctx context.Context, email, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validEmailKeyInput(email) {
		return ErrEmailInvalid
	}
	if !validVerificationCode(code) {
		return ErrCodeInvalid
	}

	result, err := deleteEmailVerificationCodeScript.Run(
		ctx,
		s.client,
		[]string{s.codeKey(email), s.cooldownKey(email), s.attemptsKey(email)},
		code,
	).Int64()
	if err != nil {
		return fmt.Errorf("delete email verification code: %w", err)
	}
	if result != 0 && result != 1 {
		return ErrUnexpectedResult
	}
	return nil
}

func (s *EmailVerificationCodeStore) codeKey(email string) string {
	return s.key("code", email)
}

func (s *EmailVerificationCodeStore) cooldownKey(email string) string {
	return s.key("cooldown", email)
}

func (s *EmailVerificationCodeStore) attemptsKey(email string) string {
	return s.key("attempts", email)
}

func (s *EmailVerificationCodeStore) key(kind, email string) string {
	digest := sha256.Sum256([]byte(email))
	return fmt.Sprintf("%s:%s:%x", s.prefix, kind, digest)
}

func validEmailKeyInput(email string) bool {
	if email == "" || email != strings.TrimSpace(email) || email != strings.ToLower(email) || len(email) > maxEmailBytes {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func validVerificationCode(code string) bool {
	if len(code) != verificationCodeLen {
		return false
	}
	for i := range code {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}
