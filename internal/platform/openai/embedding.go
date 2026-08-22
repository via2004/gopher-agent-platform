package openai

import (
	"context"
	"fmt"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"strings"
)

type Embedder struct {
	client openaisdk.EmbeddingService
	model  string
}

type EmbeddingConfig struct {
	APIKey       string
	Model        string
	BaseURL      string
	RequiresAuth bool
}

func NewEmbedder(apiKey string, model string,
	options ...option.RequestOption) (*Embedder, error) {
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)
	if apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if model == "" {
		return nil, ErrMissingModel
	}

	return NewEmbedderWithConfig(EmbeddingConfig{
		APIKey:       apiKey,
		Model:        model,
		RequiresAuth: true,
	}, options...)
}

func NewEmbedderWithConfig(config EmbeddingConfig, options ...option.RequestOption) (*Embedder, error) {
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.Model = strings.TrimSpace(config.Model)
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	if config.Model == "" {
		return nil, ErrMissingModel
	}
	if config.RequiresAuth && config.APIKey == "" {
		return nil, ErrMissingAPIKey
	}

	requestOptions := make([]option.RequestOption, 0, len(options)+2)

	if config.APIKey != "" {
		requestOptions = append(requestOptions, option.WithAPIKey(config.APIKey))
	}

	if config.BaseURL != "" {
		requestOptions = append(requestOptions, option.WithBaseURL(config.BaseURL))
	}

	requestOptions = append(requestOptions, options...)

	return &Embedder{
		client: openaisdk.NewEmbeddingService(requestOptions...),
		model:  config.Model,
	}, nil
}

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	if len(texts) == 0 {
		return nil, ErrEmptyEmbeddingInput
	}
	input := make([]string, len(texts))
	for i := range texts {
		input[i] = strings.TrimSpace(texts[i])
		if input[i] == "" {
			return nil, ErrEmptyEmbeddingInput
		}
	}

	response, err := e.client.New(ctx, openaisdk.EmbeddingNewParams{
		Input: openaisdk.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: input,
		},
		Model: e.model,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmbeddingFailed, err)
	}

	if len(input) != len(response.Data) {
		return nil, ErrEmbeddingResponseInvalid
	}
	seen := make([]bool, len(input))

	for _, data := range response.Data {
		if data.Index < 0 || data.Index >= int64(len(input)) || seen[data.Index] {
			return nil, ErrEmbeddingResponseInvalid
		}
		// 所有向量都是空向量也会通过，这里再检查一下是不是存在空向量
		if len(data.Embedding) == 0 {
			return nil, ErrEmbeddingResponseInvalid
		}

		result[data.Index] = make([]float32, len(data.Embedding))
		for i, e := range data.Embedding {
			result[data.Index][i] = float32(e)
		}
		seen[data.Index] = true
	}

	// 不满足代表着Embedding 返回的向量维度不一致
	for i := 1; i < len(result); i++ {
		if len(result[i]) != len(result[i-1]) {
			return nil, ErrEmbeddingResponseInvalid
		}
	}

	return result, nil
}
