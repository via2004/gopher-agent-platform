package tts

import "context"

type Provider interface {
	Create(ctx context.Context, text string) (*Task, error)
	Get(ctx context.Context, taskID string) (*Task, error)
}
