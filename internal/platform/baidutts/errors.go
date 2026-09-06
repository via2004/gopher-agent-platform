package baidutts

import "errors"

// 配置错误在发起任何 Provider 请求之前返回。
var (
	ErrInvalidAPIKey    = errors.New("baidu tts API key is invalid")
	ErrInvalidSecretKey = errors.New("baidu tts secret key is invalid")
	ErrInvalidBaseURL   = errors.New("baidu tts base URL is invalid")
)

// Provider 错误描述百度 HTTP 调用边界发生的失败。
var (
	ErrTokenFailed      = errors.New("baidu tts token request failed")
	ErrProviderFailed   = errors.New("baidu tts provider request failed")
	ErrResponseTooLarge = errors.New("baidu tts provider response is too large")
	ErrResponseInvalid  = errors.New("baidu tts provider response is invalid")
)
