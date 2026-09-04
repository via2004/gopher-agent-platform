package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

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
	response, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: messagesToInput(messages)},
		Store: openaisdk.Bool(!c.disableResponseStorage),
		Reasoning: responses.ReasoningParam{
			Effort: responses.ReasoningEffort(c.reasoningEffort),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate OpenAI response: %w", err)
	}

	return resultFromResponse(response), nil
}

// GenerateWithTools 发送工具定义，让模型返回普通文本或结构化 function call。
func (c *Client) GenerateWithTools(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.ToolModelResult, error) {
	params, err := functionTools(tools)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: messagesToInput(messages)},
		Tools: params,
		Include: []responses.ResponseIncludable{
			// 返回模型推理过程对应的加密内容，放在reasoning output item的encrypted_content字段中
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		Store: openaisdk.Bool(!c.disableResponseStorage),
		Reasoning: responses.ReasoningParam{
			Effort: responses.ReasoningEffort(c.reasoningEffort),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate OpenAI response with tools: %w", err)
	}

	toolCalls, err := parseToolCalls(response.Output)
	if err != nil {
		return nil, err
	}
	continuation, err := continuationFromOutput(response.Output)
	if err != nil {
		return nil, err
	}
	return &llm.ToolModelResult{
		Result:       resultFromResponse(response),
		ToolCalls:    toolCalls,
		Continuation: continuation,
	}, nil
}

// GenerateWithToolResult 将模型之前的 function call 和工具结果一起回填给模型。
func (c *Client) GenerateWithToolResult(ctx context.Context, messages []llm.Message, continuation []json.RawMessage, call llm.ToolCall, output json.RawMessage) (*llm.Result, error) {
	if call.ID == "" || call.CallID == "" || call.Name == "" || !json.Valid(call.Arguments) || len(continuation) == 0 || len(output) == 0 || !json.Valid(output) {
		return nil, ErrInvalidToolCall
	}
	input := messagesToInput(messages)
	for _, item := range continuation {
		if !json.Valid(item) {
			return nil, ErrInvalidToolCall
		}
		input = append(input, param.Override[responses.ResponseInputItemUnionParam](json.RawMessage(bytes.Clone(item))))
	}
	input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(call.CallID, string(output)))
	response, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Store: openaisdk.Bool(!c.disableResponseStorage),
		Reasoning: responses.ReasoningParam{
			Effort: responses.ReasoningEffort(c.reasoningEffort),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate OpenAI response with tool result: %w", err)
	}
	return resultFromResponse(response), nil
}

func messagesToInput(messages []llm.Message) responses.ResponseInputParam {
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
	return input
}

func resultFromResponse(response *responses.Response) *llm.Result {
	return &llm.Result{
		Content:      response.OutputText(),
		Model:        response.Model,
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
		TotalTokens:  response.Usage.TotalTokens,
	}
}

func functionTools(tools []llm.ToolDefinition) ([]responses.ToolUnionParam, error) {
	params := make([]responses.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || len(tool.InputSchema) == 0 {
			return nil, ErrInvalidToolDefinition
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema == nil || schema["type"] != "object" {
			return nil, ErrInvalidToolDefinition
		}
		param := responses.ToolParamOfFunction(tool.Name, schema, true)
		param.OfFunction.Description = paramOptString(tool.Description)
		params = append(params, param)
	}
	return params, nil
}

func paramOptString(value string) param.Opt[string] {
	if strings.TrimSpace(value) == "" {
		return param.Opt[string]{}
	}
	return param.NewOpt(value)
}

func parseToolCalls(items []responses.ResponseOutputItemUnion) ([]llm.ToolCall, error) {
	calls := make([]llm.ToolCall, 0)
	for _, item := range items {
		if item.Type != "function_call" {
			continue
		}
		call := item.AsFunctionCall()
		if call.ID == "" || call.CallID == "" || call.Name == "" || !json.Valid([]byte(call.Arguments)) {
			return nil, ErrInvalidToolCall
		}
		calls = append(calls, llm.ToolCall{
			ID:        call.ID,
			CallID:    call.CallID,
			Name:      call.Name,
			Arguments: json.RawMessage(call.Arguments),
		})
	}
	return calls, nil
}

func continuationFromOutput(items []responses.ResponseOutputItemUnion) ([]json.RawMessage, error) {
	continuation := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		raw := json.RawMessage(item.RawJSON())
		if !json.Valid(raw) {
			return nil, ErrInvalidToolCall
		}
		continuation = append(continuation, bytes.Clone(raw))
	}
	return continuation, nil
}

func (c *Client) GenerateStream(ctx context.Context, messages []llm.Message, onDelta func(string) error) (result *llm.Result, err error) {
	if onDelta == nil {
		return nil, llm.ErrOnDeltaMissed
	}
	resultMessage := make([]rune, 0)

	stream := c.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: messagesToInput(messages)},
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
