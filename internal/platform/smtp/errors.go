package smtp

import "errors"

// 配置错误在连接 SMTP Server 之前返回。
var (
	ErrHostInvalid     = errors.New("SMTP host is invalid")
	ErrPortInvalid     = errors.New("SMTP port is invalid")
	ErrUsernameInvalid = errors.New("SMTP username is invalid")
	ErrPasswordInvalid = errors.New("SMTP password is invalid")
	ErrFromInvalid     = errors.New("SMTP from address is invalid")
	ErrFromNameInvalid = errors.New("SMTP from name is invalid")
	ErrTimeoutInvalid  = errors.New("SMTP timeout is invalid")
)

// 请求错误表示调用方传入了无效的收件人或验证码。
var (
	ErrRecipientInvalid = errors.New("SMTP recipient is invalid")
	ErrCodeInvalid      = errors.New("SMTP verification code is invalid")
)

// ErrDeliveryFailed 收敛连接、STARTTLS、鉴权和邮件发送失败。
var ErrDeliveryFailed = errors.New("SMTP delivery failed")
