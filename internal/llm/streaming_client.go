package llm

import (
	"context"
	"errors"
)

var (
	ErrOnDeltaMissed        = errors.New("onDelta is necessary")
	ErrResponseFailed       = errors.New("response failed")
	ErrResponseNotCompleted = errors.New("response is not completed")
)

type StreamingClient interface {
	GenerateStream(ctx context.Context,
		messages []Message,
		onDelta func(string) error) (responseContent *Result, err error)
}
