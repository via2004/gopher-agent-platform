package tts

import "errors"

// 请求错误表示调用方可以修正的输入问题。
var (
	ErrInvalidText   = errors.New("tts text is invalid")
	ErrTextTooLong   = errors.New("tts text is too long")
	ErrInvalidTaskID = errors.New("tts task ID is invalid")
)

// 配置错误表示可选的 TTS 功能当前不可用。
var ErrNotConfigured = errors.New("tts is not configured")

// 处理错误表示 Provider 返回了不符合领域约束的结果。
var (
	ErrProviderUnavailable   = errors.New("tts provider is unavailable")
	ErrInvalidProviderResult = errors.New("tts provider result is invalid")
)
