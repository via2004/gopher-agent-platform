package openai

import (
	"context"
	"errors"
	"fmt"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"strings"

	"gopherai/internal/llm"
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

func (c *Client) Generate(ctx context.Context, messages []llm.Message) (*llm.Result, error) {
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

	result := &llm.Result{}

	response, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openaisdk.Bool(!c.disableResponseStorage),
		Reasoning: responses.ReasoningParam{
			Effort: responses.ReasoningEffort(c.reasoningEffort),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate OpenAI response: %w", err)
	}

	result.Content = response.OutputText()
	result.Model = response.Model
	result.InputTokens = response.Usage.InputTokens
	result.OutputTokens = response.Usage.OutputTokens
	result.TotalTokens = response.Usage.TotalTokens

	return result, nil
}

func (c *Client) GenerateStream(ctx context.Context, messages []llm.Message, onDelta func(string) error) (result *llm.Result, err error) {
	if onDelta == nil {
		return nil, llm.ErrOnDeltaMissed
	}
	resultMessage := make([]rune, 0)
	input := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
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

	stream := c.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openaisdk.Bool(!c.disableResponseStorage),
		Reasoning: responses.ReasoningParam{
			Effort: responses.ReasoningEffort(c.reasoningEffort),
		},
	})

	defer func() {
		if errs := stream.Close(); errs != nil {
			err = errors.Join(err, fmt.Errorf("close stream error: %w", errs))
		}
	}()

	completed := false
	for stream.Next() {
		data := stream.Current()
		switch data.Type {
		case "response.output_text.delta":
			// 输出文本片段
			if err := onDelta(data.Delta); err != nil {
				return nil, err
			}
			resultMessage = append(resultMessage, []rune(data.Delta)...)
		case "response.completed":
			// 完成
			completed = true
			result = &llm.Result{
				Content:      string(resultMessage),
				Model:        data.Response.Model,
				InputTokens:  data.Response.Usage.InputTokens,
				OutputTokens: data.Response.Usage.OutputTokens,
				TotalTokens:  data.Response.Usage.TotalTokens,
			}
		case "response.failed":
			return nil, fmt.Errorf("%w: msg: %s; code: %s", llm.ErrResponseFailed,
				data.Response.Error.Message, data.Response.Error.Code)
		}
		if completed {
			break
		}
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}
	if !completed {
		return nil, llm.ErrResponseNotCompleted
	}

	return result, nil
}

func (c *Client) Info() llm.ModelInfo {
	return llm.ModelInfo{
		Provider: "openai",
		Model:    c.model,
	}
}
