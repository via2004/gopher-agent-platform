package tts

import "errors"

// Request errors describe input the caller can correct.
var (
	ErrInvalidText   = errors.New("tts text is invalid")
	ErrTextTooLong   = errors.New("tts text is too long")
	ErrInvalidTaskID = errors.New("tts task ID is invalid")
)

// Configuration errors describe an optional TTS feature that is unavailable.
var ErrNotConfigured = errors.New("tts is not configured")

// Processing errors describe malformed results returned by a provider.
var ErrInvalidProviderResult = errors.New("tts provider result is invalid")
