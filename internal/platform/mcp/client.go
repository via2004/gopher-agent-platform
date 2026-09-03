package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"gopherai/internal/llm"
)

const defaultCallTimeout = 10 * time.Second

type Config struct {
	Endpoint   string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client 通过 Streamable HTTP 发现并调用固定 allowlist 中的 MCP 工具。
type Client struct {
	session *mcpsdk.ClientSession
	tools   []llm.ToolDefinition
	timeout time.Duration
}

// NewClient 连接 MCP Server，并在启动时缓存 get_weather 的工具定义。
func NewClient(ctx context.Context, config Config) (*Client, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidEndpoint
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultCallTimeout
	}
	if timeout < 0 {
		return nil, ErrInvalidTimeout
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "gopherai-backend",
		Version: "0.1.0",
	}, nil)
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := client.Connect(connectCtx, &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: httpClient,
		MaxRetries: -1, // diable retry
		// 禁用的是另一种SSE: Server主动向Client推送消息所使用的独立长连接；
		// 他不会禁止当前POST请求本身使用SSE返回结果
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnectFailed, err)
	}

	tools, err := discoverWeatherTool(connectCtx, session)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	return &Client{session: session, tools: tools, timeout: timeout}, nil
}

func discoverWeatherTool(ctx context.Context, session *mcpsdk.ClientSession) ([]llm.ToolDefinition, error) {
	var found *mcpsdk.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrListToolsFailed, err)
		}
		if tool.Name == WeatherToolName {
			found = tool
			break
		}
	}
	if found == nil {
		return nil, ErrWeatherToolNotFound
	}

	schema, err := json.Marshal(found.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal input schema: %v", ErrToolDefinitionInvalid, err)
	}
	var schemaObject map[string]any
	if err := json.Unmarshal(schema, &schemaObject); err != nil || schemaObject == nil || schemaObject["type"] != "object" {
		return nil, ErrToolDefinitionInvalid
	}

	return []llm.ToolDefinition{{
		Name:        found.Name,
		Description: found.Description,
		InputSchema: schema,
	}}, nil
}

// Tools 返回缓存工具定义的副本，调用方不能修改 Client 内部状态。
func (c *Client) Tools() []llm.ToolDefinition {
	tools := make([]llm.ToolDefinition, len(c.tools))
	for i, tool := range c.tools {
		tools[i] = tool
		tools[i].InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
	}
	return tools
}

// Call 校验工具名和 JSON 参数后执行 MCP tools/call，并返回结构化结果 JSON。
func (c *Client) Call(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	if name != WeatherToolName {
		return nil, ErrToolNotAllowed
	}

	var argumentObject map[string]any
	if len(arguments) == 0 || json.Unmarshal(arguments, &argumentObject) != nil || argumentObject == nil {
		return nil, ErrArgumentsInvalid
	}

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result, err := c.session.CallTool(callCtx, &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: argumentObject,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCallToolFailed, err)
	}
	if result == nil {
		return nil, ErrToolResultInvalid
	}
	if result.IsError {
		return nil, fmt.Errorf("%w: %s", ErrToolFailed, toolErrorText(result.Content))
	}
	if result.StructuredContent == nil {
		return nil, ErrToolResultInvalid
	}

	output, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal structured content: %v", ErrToolResultInvalid, err)
	}
	return output, nil
}

func toolErrorText(contents []mcpsdk.Content) string {
	var messages []string
	for _, content := range contents {
		text, ok := content.(*mcpsdk.TextContent)
		if ok && strings.TrimSpace(text.Text) != "" {
			messages = append(messages, strings.TrimSpace(text.Text))
		}
	}
	if len(messages) == 0 {
		return "tool execution failed"
	}
	return strings.Join(messages, "; ")
}

// Close 释放 MCP Client Session 使用的传输资源。
func (c *Client) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	if err := c.session.Close(); err != nil && !errors.Is(err, mcpsdk.ErrConnectionClosed) {
		return err
	}
	return nil
}
