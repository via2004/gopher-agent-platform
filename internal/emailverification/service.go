package emailverification

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"
)

const (
	codeLength            = 6
	defaultCodeTTL        = 10 * time.Minute
	defaultResendInterval = time.Minute
	defaultMaxAttempts    = 5
	cleanupTimeout        = 5 * time.Second
	codeRange             = 1_000_000
	maxEmailBytes         = 254
)

type Config struct {
	CodeTTL        time.Duration
	ResendInterval time.Duration
	MaxAttempts    int
}

func DefaultConfig() Config {
	return Config{
		CodeTTL:        defaultCodeTTL,
		ResendInterval: defaultResendInterval,
		MaxAttempts:    defaultMaxAttempts,
	}
}

type codeGenerator func() (string, error)

type Service struct {
	codes          CodeStore
	sender         Sender
	codeTTL        time.Duration
	resendInterval time.Duration // 发送邮件的最短间隔
	maxAttempts    int
	generateCode   codeGenerator
}

// NewService 创建邮箱验证码服务。codes 和 sender 同时为 nil 表示功能未启用；
// 只缺少其中一个依赖属于无效配置。
func NewService(codes CodeStore, sender Sender, config Config) (*Service, error) {
	if codes == nil && sender == nil {
		return &Service{generateCode: generateNumericCode}, nil
	}
	if codes == nil {
		return nil, ErrInvalidCodeStore
	}
	if sender == nil {
		return nil, ErrInvalidSender
	}
	if config.CodeTTL <= 0 || config.ResendInterval <= 0 ||
		config.ResendInterval > config.CodeTTL || config.MaxAttempts <= 0 {
		return nil, ErrInvalidConfig
	}

	return &Service{
		codes:          codes,
		sender:         sender,
		codeTTL:        config.CodeTTL,
		resendInterval: config.ResendInterval,
		maxAttempts:    config.MaxAttempts,
		generateCode:   generateNumericCode,
	}, nil
}

// Send 生成并保存一次性验证码，然后通过邮件发送给规范化后的邮箱。
func (s *Service) Send(ctx context.Context, email string) error {
	if s == nil || s.codes == nil || s.sender == nil {
		return ErrNotConfigured
	}

	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	code, err := s.generateCode()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGenerateCodeFailed, err)
	}
	if !validCode(code) {
		return ErrGenerateCodeFailed
	}

	if err := s.codes.Save(ctx, email, code, s.codeTTL, s.resendInterval); err != nil {
		if errors.Is(err, ErrSendTooFrequent) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrStoreFailed, err)
	}

	if err := s.sender.SendVerificationCode(ctx, email, code); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		cleanupErr := s.codes.Delete(cleanupCtx, email, code)
		if cleanupErr != nil {
			cleanupErr = fmt.Errorf("%w: compensate failed send: %w", ErrStoreFailed, cleanupErr)
		}
		return errors.Join(fmt.Errorf("%w: %w", ErrSendFailed, err), cleanupErr)
	}

	return nil
}

// VerifyAndConsume 原子校验验证码；验证成功后验证码不能再次使用。
func (s *Service) VerifyAndConsume(ctx context.Context, email, code string) error {
	if s == nil || s.codes == nil || s.sender == nil {
		return ErrNotConfigured
	}

	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return ErrInvalidCode
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.codes.VerifyAndConsume(ctx, email, code, s.maxAttempts); err != nil {
		if errors.Is(err, ErrCodeInvalidOrExpired) || errors.Is(err, ErrTooManyAttempts) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrStoreFailed, err)
	}
	return nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > maxEmailBytes {
		return "", ErrInvalidEmail
	}
	return value, nil
}

func validCode(code string) bool {
	if len(code) != codeLength {
		return false
	}
	for i := range code {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

func generateNumericCode() (string, error) {
	// 操作系统提供的密码学安全随机源。它比 math/rand 更难预测，适合验证码、Token 等安全场景。
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(codeRange)) // [0, codeRange)
	if err != nil {
		return "", err
	}
	/*
		%0*d
		0: 不足宽度时用0填充
		*: 宽度由后面的参数提供
		d: 按十进制整数输出
	*/
	return fmt.Sprintf("%0*d", codeLength, value.Int64()), nil
}
