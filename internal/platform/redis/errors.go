package redis

import "errors"

var (
	ErrClientInvalid      = errors.New("client is invalid")
	ErrLimitInvalid       = errors.New("limit is invalid")
	ErrWindowInvalid      = errors.New("window is invalid")
	ErrUnexpectedResult   = errors.New("unexpected script result")
	ErrUnexpectedTTL      = errors.New("TTL must be a positive")
	ErrEmptyChunk         = errors.New("chunk is empty")
	ErrEmptyChunkVector   = errors.New("chunk's vector is empty")
	ErrEmptyChunkContent  = errors.New("chunk's content is empty")
	ErrInvalidChunkVector = errors.New("chunk's vector is invalid")
	ErrInvalidVersion     = errors.New("version is invalid")
	ErrEmailInvalid       = errors.New("email is invalid")
	ErrCodeInvalid        = errors.New("verification code is invalid")
	ErrTTLInvalid         = errors.New("TTL is invalid")
	ErrCooldownInvalid    = errors.New("cooldown is invalid")
	ErrMaxAttemptsInvalid = errors.New("max attempts is invalid")
)
