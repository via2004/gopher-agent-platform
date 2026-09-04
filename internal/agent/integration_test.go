package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3/option"

	"gopherai/internal/agent"
	"gopherai/internal/llm"
	mcpplatform "gopherai/internal/platform/mcp"
	openaiplatform "gopherai/internal/platform/openai"
	"gopherai/internal/weather"
)

type integrationWeatherClient struct {
	calls atomic.Int32
	city  string
}

func (c *integrationWeatherClient) Get(_ context.Context, city string) (*weather.Result, error) {
	c.calls.Add(1)
	c.city = city
	return &weather.Result{Location: city, TemperatureC: 23, Condition: "晴"}, nil
}

// TestClientToolLoopIntegration 验证 Agent 能串联 OpenAI Tool Calling、MCP Client 和 MCP Server。
func TestClientToolLoopIntegration(t *testing.T) {
	weatherClient := &integrationWeatherClient{}
	mcpServer, err := mcpplatform.NewWeatherServer(weatherClient)
	if err != nil {
		t.Fatal(err)
	}
	mcpHandler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return mcpServer },
		&mcpsdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	mcpHTTPServer := httptest.NewServer(mcpHandler)
	defer mcpHTTPServer.Close()
	mcpClient, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{
		Endpoint:   mcpHTTPServer.URL,
		HTTPClient: mcpHTTPServer.Client(),
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mcpClient.Close()

	var modelCalls atomic.Int32
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if modelCalls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"model":"gpt-tool","usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13},"output":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque"},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"上海\"}"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"gpt-final","usage":{"input_tokens":20,"output_tokens":5,"total_tokens":25},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"上海现在晴，23°C"}]}]}`))
	}))
	defer modelServer.Close()
	model, err := openaiplatform.NewClient("test-key", "gpt-test", option.WithBaseURL(modelServer.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := agent.NewClient(model, mcpClient)
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.Generate(t.Context(), []llm.Message{{Role: "user", Content: "上海天气怎么样？"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "上海现在晴，23°C" || result.InputTokens != 30 || result.OutputTokens != 8 || result.TotalTokens != 38 {
		t.Fatalf("Generate() = %#v", result)
	}
	if modelCalls.Load() != 2 || weatherClient.calls.Load() != 1 || weatherClient.city != "上海" {
		t.Fatalf("model calls = %d, weather calls = %d, city = %q", modelCalls.Load(), weatherClient.calls.Load(), weatherClient.city)
	}
}
