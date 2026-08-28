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
)
