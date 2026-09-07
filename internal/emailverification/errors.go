package emailverification

import "errors"

// 请求错误表示调用方可以修正的输入问题。
var (
	ErrInvalidEmail = errors.New("email verification email is invalid")
	ErrInvalidCode  = errors.New("email verification code is invalid")
)

// 业务错误表示验证码当前不能继续发送或使用。
var (
	ErrCodeInvalidOrExpired = errors.New("email verification code is invalid or expired")
	ErrTooManyAttempts      = errors.New("email verification attempts exceeded")
	ErrSendTooFrequent      = errors.New("email verification code was sent too recently")
	ErrNotConfigured        = errors.New("email verification is not configured")
)

// 配置错误表示 Service 依赖或参数不完整。
var (
	ErrInvalidCodeStore = errors.New("email verification code store is invalid")
	ErrInvalidSender    = errors.New("email verification sender is invalid")
	ErrInvalidConfig    = errors.New("email verification config is invalid")
)

// 处理错误将随机数、Redis 和 SMTP 的底层失败收敛成稳定语义。
var (
	ErrGenerateCodeFailed = errors.New("generate email verification code failed")
	ErrStoreFailed        = errors.New("email verification store failed")
	ErrSendFailed         = errors.New("send email verification code failed")
)
