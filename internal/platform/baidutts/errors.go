package baidutts

import (
	"errors"
	"fmt"

	"gopherai/internal/tts"
)

// 配置错误在发起任何 Provider 请求之前返回。
var (
	ErrInvalidAPIKey    = errors.New("baidu tts API key is invalid")
	ErrInvalidSecretKey = errors.New("baidu tts secret key is invalid")
	ErrInvalidBaseURL   = errors.New("baidu tts base URL is invalid")
)

// Provider 错误描述百度 HTTP 调用边界发生的失败。
var (
	ErrTokenFailed      = fmt.Errorf("%w: baidu token request failed", tts.ErrProviderUnavailable)
	ErrProviderFailed   = fmt.Errorf("%w: baidu provider request failed", tts.ErrProviderUnavailable)
	ErrResponseTooLarge = fmt.Errorf("%w: baidu response is too large", tts.ErrInvalidProviderResult)
	ErrResponseInvalid  = fmt.Errorf("%w: baidu response is invalid", tts.ErrInvalidProviderResult)
)
