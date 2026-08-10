package llm

import (
	"context"
	"errors"
)

type Message struct {
	Role    string
	Content string
}

// final result
type Result struct {
	Content      string
	Model        string
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

var (
	ErrNotConfigured = errors.New("llm is not configured")
)

type ModelClient interface {
	Client
	StreamingClient
}

type Client interface {
	Generate(ctx context.Context, messages []Message) (*Result, error)
}

type UnavailableClient struct{}

func (UnavailableClient) Generate(
	ctx context.Context,
	messages []Message,
) (*Result, error) {
	return nil, ErrNotConfigured
}

func (UnavailableClient) GenerateStream(ctx context.Context,
	messages []Message, onDelta func(string) error) (responseContent *Result, err error) {
	return nil, ErrNotConfigured
}
