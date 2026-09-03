package llm

import (
	"context"
	"encoding/json"
)

// ToolDefinition 是模型可见的工具描述，不依赖具体模型或 MCP SDK。
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolCall 是模型要求应用执行一次工具调用的结构化请求。
type ToolCall struct {
	ID        string
	CallID    string
	Name      string
	Arguments json.RawMessage
}

// ToolModelResult 表示模型的规划结果：普通文本或一个/多个 ToolCall。
type ToolModelResult struct {
	Result       *Result
	ToolCalls    []ToolCall
	Continuation []json.RawMessage
}

// ToolModel 是支持 OpenAI 风格工具调用的模型边界。
type ToolModel interface {
	GenerateWithTools(ctx context.Context, messages []Message, tools []ToolDefinition) (*ToolModelResult, error)
	GenerateWithToolResult(ctx context.Context, messages []Message, continuation []json.RawMessage, call ToolCall, output json.RawMessage) (*Result, error)
}
