package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const negotiatedProtocolVersion = "2026-07-28"

type echoInput struct {
	Text string `json:"text" jsonschema:"text returned unchanged"`
}

type echoOutput struct {
	Text string `json:"text"`
}

// TestStreamableHTTPToolRoundTrip 验证工具发现、正常调用和结构化结果解析的完整 HTTP 闭环。
func TestStreamableHTTPToolRoundTrip(t *testing.T) {
	session := newStreamableHTTPSession(t, func(server *mcpsdk.Server) {
		// 泛型 AddTool 会生成 Schema、校验输入，并把 JSON 参数解码为 echoInput。
		mcpsdk.AddTool(server, &mcpsdk.Tool{
			Name:        "echo",
			Description: "return the provided text unchanged",
		}, func(_ context.Context, _ *mcpsdk.CallToolRequest, input echoInput) (*mcpsdk.CallToolResult, echoOutput, error) {
			return nil, echoOutput{Text: input.Text}, nil
		})
	})

	// Connect 会先执行协议发现和协商；这里防止 SDK 静默降级到旧协议。
	initialized := session.InitializeResult()
	if initialized == nil || initialized.ProtocolVersion != negotiatedProtocolVersion {
		t.Fatalf("negotiated protocol = %#v, want %s", initialized, negotiatedProtocolVersion)
	}

	// tools/list 让 Client 在不知道 Server 实现的情况下发现工具及其参数 Schema。
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" || tools.Tools[0].InputSchema == nil {
		t.Fatalf("tools = %#v, want one echo tool with an input schema", tools.Tools)
	}

	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned tool error: %#v", result.Content)
	}

	// StructuredContent 是给程序读取的结构化结果；经 HTTP 后具体类型会变成 map，
	// 所以重新按 JSON 解码回 echoOutput，而不是依赖 map[string]any。
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var output echoOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.Text != "hello" {
		t.Fatalf("structured output = %#v, want hello", output)
	}

	// SDK 还会生成一份 JSON TextContent，供不支持 StructuredContent 的旧 Client 使用。
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v, want one text content", result.Content)
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok || text.Text != `{"text":"hello"}` {
		t.Fatalf("content = %#v, want JSON text fallback", result.Content[0])
	}
}

// TestStreamableHTTPRejectsInvalidArguments 验证 SDK 会按工具 Schema 拦截错误参数且不执行 Handler。
func TestStreamableHTTPRejectsInvalidArguments(t *testing.T) {
	var handlerCalls atomic.Int32
	session := newStreamableHTTPSession(t, func(server *mcpsdk.Server) {
		mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "echo"},
			func(_ context.Context, _ *mcpsdk.CallToolRequest, input echoInput) (*mcpsdk.CallToolResult, echoOutput, error) {
				handlerCalls.Add(1)
				return nil, echoOutput{Text: input.Text}, nil
			})
	})

	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"text": 42},
	})
	// 参数校验失败属于“工具执行结果错误”，不是 MCP 通信错误，所以 err 仍为 nil，
	// Client 需要通过 result.IsError 判断。
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("CallTool() IsError = false, want schema validation error")
	}
	if handlerCalls.Load() != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls.Load())
	}
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v, want one validation error", result.Content)
	}
	text, ok := result.Content[0].(*mcpsdk.TextContent)
	if !ok || !strings.Contains(text.Text, "validating") {
		t.Fatalf("content = %#v, want validation error text", result.Content[0])
	}
}

// TestStreamableHTTPToolCallCancellation 验证 Client 取消请求后，取消信号会传播到 Server Handler。
func TestStreamableHTTPToolCallCancellation(t *testing.T) {
	started := make(chan struct{})
	serverResult := make(chan error, 1)
	session := newStreamableHTTPSession(t, func(server *mcpsdk.Server) {
		mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "slow_echo"},
			func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ echoInput) (*mcpsdk.CallToolResult, echoOutput, error) {
				close(started)
				// 工具模拟一个长任务，直到 HTTP Client 取消请求。
				<-ctx.Done()
				serverResult <- ctx.Err()
				return nil, echoOutput{}, ctx.Err()
			})
	})

	ctx, cancel := context.WithCancel(context.Background())
	clientResult := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
			Name:      "slow_echo",
			Arguments: map[string]any{"text": "hello"},
		})
		clientResult <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool handler did not start")
	}
	cancel()

	select {
	case err := <-clientResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("client call did not observe cancellation")
	}

	select {
	case err := <-serverResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("server error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server handler did not observe cancellation")
	}
}

func newStreamableHTTPSession(t *testing.T, configure func(*mcpsdk.Server)) *mcpsdk.ClientSession {
	t.Helper()

	// mcp.Server 只描述协议能力和工具；下面再用 Handler 把它接到 HTTP。
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "gopherai-test-server",
		Version: "0.1.0",
	}, nil)
	configure(server)

	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{
			// 单工具请求不需要协议 Session 或 Server 主动向 Client 发消息。
			Stateless: true,
			// 阶段 1 使用单次 JSON 响应，暂不引入 MCP 传输层 SSE。
			JSONResponse: true,
			// HTTP 请求取消时，同步取消正在运行的工具 Handler。
			PropagateRequestCancellation: true,
		},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "gopherai-test-client",
		Version: "0.1.0",
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint:   httpServer.URL + "/mcp",
		HTTPClient: httpServer.Client(),
		// 测试要直接暴露连接错误，不能由 SDK 重试掩盖。
		MaxRetries: -1,
		// Stateless 模式不需要额外维持 Server -> Client 的 SSE 连接。
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close MCP session: %v", err)
		}
	})
	return session
}
