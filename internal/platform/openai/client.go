package openai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"gopherai/internal/llm"
)

var (
	ErrMissingAPIKey      = errors.New("OpenAI API key is empty")
	ErrMissingModel       = errors.New("OpenAI model is empty")
	ErrUnsupportedWireAPI = errors.New("unsupported OpenAI wire API")
)

type Config struct {
	APIKey                 string
	Model                  string
	BaseURL                string
	WireAPI                string
	RequiresAuth           bool
	ReasoningEffort        string
	DisableResponseStorage bool
}

type Client struct {
	client                 openaisdk.Client
	model                  string
	reasoningEffort        string
	disableResponseStorage bool
}

func NewClient(apiKey, model string, options ...option.RequestOption) (*Client, error) {
	return NewClientWithConfig(Config{
		APIKey:                 apiKey,
		Model:                  model,
		WireAPI:                "responses",
		RequiresAuth:           true,
		ReasoningEffort:        "low",
		DisableResponseStorage: true,
	}, options...)
}

func NewClientWithConfig(config Config, options ...option.RequestOption) (*Client, error) {
	if strings.TrimSpace(config.Model) == "" {
		return nil, ErrMissingModel
	}
	if config.WireAPI != "responses" {
		return nil, ErrUnsupportedWireAPI
	}
	if config.RequiresAuth && strings.TrimSpace(config.APIKey) == "" {
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
	return &Client{
		client:                 openaisdk.NewClient(requestOptions...),
		model:                  config.Model,
		reasoningEffort:        config.ReasoningEffort,
		disableResponseStorage: config.DisableResponseStorage,
	}, nil
}

/*
ResponseNewParams.Input
    ↓
ResponseNewParamsInputUnion
    ↓
OfInputItemList
    ↓
ResponseInputParam
    ↓
ResponseInputItemUnionParam
    ↓
OfMessage
    ↓
EasyInputMessageParam
*/

func (c *Client) Generate(ctx context.Context, messages []llm.Message) (string, error) {
	input := make(responses.ResponseInputParam, 0, len(messages))
	for _, item := range messages {
		input = append(input, responses.ResponseInputItemUnionParam{
			OfMessage: &responses.EasyInputMessageParam{
				Type: responses.EasyInputMessageTypeMessage,
				Role: responses.EasyInputMessageRole(item.Role),
				Content: responses.EasyInputMessageContentUnionParam{
					OfString: openaisdk.String(item.Content),
				},
			},
		})
	}

	response, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openaisdk.Bool(!c.disableResponseStorage),
		Reasoning: responses.ReasoningParam{
			Effort: responses.ReasoningEffort(c.reasoningEffort),
		},
	})
	if err != nil {
		return "", fmt.Errorf("generate OpenAI response: %w", err)
	}

	return response.OutputText(), nil
}
