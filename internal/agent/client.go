package agent

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"gopherai/internal/llm"
)

const maxPlanningOutputRunes = 20_000

// Model 是 Agent 编排普通与流式 Tool Calling 需要的模型能力。
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

// 流式的带至多一轮工具调用的模型回答
func (c *Client) GenerateStream(ctx context.Context, messages []llm.Message, onDelta func(string) error) (*llm.Result, error) {
	if onDelta == nil {
		return nil, llm.ErrOnDeltaMissed
	}
	streamingModel, ok := c.model.(llm.StreamingToolModel)
	toolDefinitions := c.tools.Tools()
	// 模型客户端不支持流式工具调用，或没有可用工具时，使用普通流式生成。
	if !ok || len(toolDefinitions) == 0 {
		return c.model.GenerateStream(ctx, messages, onDelta)
	}
	// 先缓存规划阶段的正文；是否调用工具，要等模型返回完整规划结果后判断。
	planningDeltas := make([]string, 0)
	planningRuneCount := 0
	// 无工具调用时转发缓存正文；有工具调用时丢弃它，只转发最终回答。这不是 encrypted_content。
	bufferPlanningDelta := func(delta string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if delta == "" {
			return nil
		}
		deltaRunes := utf8.RuneCountInString(delta)
		if deltaRunes > maxPlanningOutputRunes-planningRuneCount {
			return ErrPlanningOutputTooLarge
		}
		planningRuneCount += deltaRunes
		planningDeltas = append(planningDeltas, delta)
		return nil
	}

	planning, err := streamingModel.GenerateStreamWithTools(ctx, messages, toolDefinitions, bufferPlanningDelta)
	if err != nil {
		return nil, err
	}
	if planning == nil || planning.Result == nil {
		return nil, ErrInvalidPlanning
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch len(planning.ToolCalls) {
	case 0:
		for _, delta := range planningDeltas {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := onDelta(delta); err != nil {
				return nil, err
			}
		}
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
