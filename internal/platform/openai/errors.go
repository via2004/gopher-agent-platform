package openai

import "errors"

var (
	ErrMissingAPIKey            = errors.New("OpenAI API key is empty")
	ErrMissingModel             = errors.New("OpenAI model is empty")
	ErrUnsupportedWireAPI       = errors.New("unsupported OpenAI wire API")
	ErrInvalidToolDefinition    = errors.New("OpenAI tool definition is invalid")
	ErrInvalidToolCall          = errors.New("OpenAI tool call is invalid")
	ErrEmbeddingFailed          = errors.New("embedding error")
	ErrEmptyEmbeddingInput      = errors.New("embedding input is empty")
	ErrEmbeddingResponseInvalid = errors.New("embedding output is invalid")
)
