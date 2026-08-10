package llm

import (
	"context"
	"errors"
)

type Message struct {
	Role    string
	Content string
}

var (
	ErrNotConfigured = errors.New("llm is not configured")
)

type ModelClient interface {
	Client
	StreamingClient
}

type Client interface {
	Generate(ctx context.Context, messages []Message) (string, error)
}

type UnavailableClient struct{}

func (UnavailableClient) Generate(
	ctx context.Context,
	messages []Message,
) (string, error) {
	return "", ErrNotConfigured
}

func (UnavailableClient) GenerateStream(ctx context.Context,
	messages []Message, onDelta func(string) error) (responseContent string, err error) {
	return "", ErrNotConfigured
}
