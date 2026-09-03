package llm

import "encoding/json"

// ToolDefinition 是模型可见的工具描述，不依赖具体模型或 MCP SDK。
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}
