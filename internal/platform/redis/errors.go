package redis

import "errors"

var (
	ErrClientInvalid    = errors.New("client is invalid")
	ErrLimitInvalid     = errors.New("limit is invalid")
	ErrWindowInvalid    = errors.New("window is invalid")
	ErrUnexpectedResult = errors.New("unexpected script result")
	ErrUnexpectedTTL    = errors.New("TTL must be a positive")
)
