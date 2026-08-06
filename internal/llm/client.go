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
