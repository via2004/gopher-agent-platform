package openai

import "errors"

var (
	ErrMissingAPIKey      = errors.New("OpenAI API key is empty")
	ErrMissingModel       = errors.New("OpenAI model is empty")
	ErrUnsupportedWireAPI = errors.New("unsupported OpenAI wire API")
)
