package agent

import (
	"context"
	"encoding/json"

	"gopherai/internal/llm"
)

// Model 是 Agent 编排需要的模型能力；流式工具循环会在后续阶段补充。
type Model interface {
	llm.ToolModel
	llm.StreamingClient
	llm.ModelDescriptor
}

// ToolExecutor 是 Agent 发现和执行外部工具的边界。
type ToolExecutor interface {
	Tools() []llm.ToolDefinition
	Call(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error)
}

// Client 将模型 Tool Calling 和外部 Tool Executor 编排成 llm.ModelClient。
type Client struct {
	model Model
	tools ToolExecutor
}

var _ llm.ModelClient = (*Client)(nil)

func NewClient(model Model, tools ToolExecutor) (*Client, error) {
	if model == nil {
		return nil, ErrInvalidModel
	}
	if tools == nil {
		return nil, ErrInvalidToolExecutor
	}
	return &Client{model: model, tools: tools}, nil
}

// Generate 最多执行一轮“模型规划 -> 工具调用 -> 模型最终回答”。
func (c *Client) Generate(ctx context.Context, messages []llm.Message) (*llm.Result, error) {
	planning, err := c.model.GenerateWithTools(ctx, messages, c.tools.Tools())
	if err != nil {
		return nil, err
	}
	if planning == nil || planning.Result == nil {
		return nil, ErrInvalidPlanning
	}

	switch len(planning.ToolCalls) {
	case 0:
		return planning.Result, nil
	case 1:
		// 当前边界只允许一次工具调用。
	default:
		return nil, ErrMultipleToolCalls
	}

	call := planning.ToolCalls[0]
	output, err := c.tools.Call(ctx, call.Name, call.Arguments)
	if err != nil {
		return nil, err
	}
	final, err := c.model.GenerateWithToolResult(ctx, messages, planning.Continuation, call, output)
	if err != nil {
		return nil, err
	}
	if final == nil {
		return nil, ErrInvalidFinalResult
	}

	// 一次业务请求可能包含 planning 和 final 两次模型调用，统一汇总 usage。
	final.InputTokens += planning.Result.InputTokens
	final.OutputTokens += planning.Result.OutputTokens
	final.TotalTokens += planning.Result.TotalTokens
	return final, nil
}

// GenerateStream 在阶段 6 前保持原有流式模型行为，不执行工具调用。
func (c *Client) GenerateStream(ctx context.Context, messages []llm.Message, onDelta func(string) error) (*llm.Result, error) {
	streamingModel, ok := c.model.(llm.StreamingToolModel)
	toolDefinitions := c.tools.Tools()
	if !ok || len(toolDefinitions) == 0 {
		return c.model.GenerateStream(ctx, messages, onDelta)
	}

	planning, err := streamingModel.GenerateStreamWithTools(ctx, messages, toolDefinitions, onDelta)
	if err != nil {
		return nil, err
	}
	if planning == nil || planning.Result == nil {
		return nil, ErrInvalidPlanning
	}
	switch len(planning.ToolCalls) {
	case 0:
		return planning.Result, nil
	case 1:
	default:
		return nil, ErrMultipleToolCalls
	}

	call := planning.ToolCalls[0]
	output, err := c.tools.Call(ctx, call.Name, call.Arguments)
	if err != nil {
		return nil, err
	}
	final, err := streamingModel.GenerateStreamWithToolResult(ctx, messages, planning.Continuation, call, output, onDelta)
	if err != nil {
		return nil, err
	}
	if final == nil {
		return nil, ErrInvalidFinalResult
	}
	final.InputTokens += planning.Result.InputTokens
	final.OutputTokens += planning.Result.OutputTokens
	final.TotalTokens += planning.Result.TotalTokens
	return final, nil
}

func (c *Client) Info() llm.ModelInfo {
	return c.model.Info()
}
