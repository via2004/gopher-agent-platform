package llm

import (
	"context"
)

type StreamingClient interface {
	GenerateStream(ctx context.Context,
		messages []Message,
		onDelta func(string) error) (responseContent *Result, err error)
}
