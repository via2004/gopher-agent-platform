package llm

import "errors"

var (
	ErrOnDeltaMissed        = errors.New("onDelta is necessary")
	ErrResponseFailed       = errors.New("response failed")
	ErrResponseNotCompleted = errors.New("response is not completed")
	ErrNotConfigured        = errors.New("llm is not configured")
)
