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

	mcpplatform "gopherai/internal/platform/mcp"
	"gopherai/internal/weather"
)

type countingWeatherClient struct {
	result  *weather.Result
	err     error
	wait    bool
	calls   atomic.Int32
	started chan struct{}
}

func (f *countingWeatherClient) Get(ctx context.Context, city string) (*weather.Result, error) {
	f.calls.Add(1)
	if f.started != nil {
		close(f.started)
	}
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	result := *f.result
	result.Location = city
	return &result, nil
}

func TestClientDiscoversAndCallsWeatherTool(t *testing.T) {
	weatherClient := &countingWeatherClient{result: &weather.Result{
		TemperatureC:    23,
		Condition:       "晴",
		HumidityPercent: 68,
		WindSpeedKMPH:   12,
	}}
	server, err := mcpplatform.NewWeatherServer(weatherClient)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := newMCPHTTPServer(t, server)

	client, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{
		Endpoint:   httpServer.URL + "/mcp",
		HTTPClient: httpServer.Client(),
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	tools := client.Tools()
	if len(tools) != 1 || tools[0].Name != mcpplatform.WeatherToolName || tools[0].Description == "" {
		t.Fatalf("Tools() = %#v", tools)
	}
	var schema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil || schema["type"] != "object" {
		t.Fatalf("input schema = %s, error = %v", tools[0].InputSchema, err)
	}

	// Tools 必须返回深拷贝，避免上层修改缓存中的 Schema。
	tools[0].Name = "changed"
	tools[0].InputSchema[0] = 'x'
	if cached := client.Tools(); cached[0].Name != mcpplatform.WeatherToolName || !json.Valid(cached[0].InputSchema) {
		t.Fatalf("cached tools were modified: %#v", cached)
	}

	output, err := client.Call(t.Context(), mcpplatform.WeatherToolName, json.RawMessage(`{"city":"上海"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result weather.Result
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.Location != "上海" || result.TemperatureC != 23 || weatherClient.calls.Load() != 1 {
		t.Fatalf("result = %#v, calls = %d", result, weatherClient.calls.Load())
	}
}

func TestClientRejectsUnknownToolAndInvalidArguments(t *testing.T) {
	weatherClient := &countingWeatherClient{result: &weather.Result{}}
	client := newWeatherMCPClient(t, weatherClient, time.Second)

	if _, err := client.Call(t.Context(), "unknown", json.RawMessage(`{}`)); !errors.Is(err, mcpplatform.ErrToolNotAllowed) {
		t.Fatalf("unknown tool error = %v", err)
	}
	for _, arguments := range []json.RawMessage{nil, json.RawMessage(`[]`), json.RawMessage(`{"city":`)} {
		if _, err := client.Call(t.Context(), mcpplatform.WeatherToolName, arguments); !errors.Is(err, mcpplatform.ErrArgumentsInvalid) {
			t.Fatalf("arguments %q error = %v", arguments, err)
		}
	}
	if weatherClient.calls.Load() != 0 {
		t.Fatalf("weather calls = %d, want 0", weatherClient.calls.Load())
	}
}

func TestClientMapsToolError(t *testing.T) {
	weatherClient := &countingWeatherClient{err: errors.New("provider unavailable")}
	client := newWeatherMCPClient(t, weatherClient, time.Second)

	_, err := client.Call(t.Context(), mcpplatform.WeatherToolName, json.RawMessage(`{"city":"上海"}`))
	if !errors.Is(err, mcpplatform.ErrToolFailed) || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("Call() error = %v", err)
	}
}

func TestClientRejectsUnstructuredToolResult(t *testing.T) {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "unstructured-server", Version: "0.1.0"}, nil)
	server.AddTool(&mcpsdk.Tool{
		Name:        mcpplatform.WeatherToolName,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "unstructured"}},
		}, nil
	})
	httpServer := newMCPHTTPServer(t, server)
	client, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{
		Endpoint:   httpServer.URL + "/mcp",
		HTTPClient: httpServer.Client(),
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Call(t.Context(), mcpplatform.WeatherToolName, json.RawMessage(`{}`))
	if !errors.Is(err, mcpplatform.ErrToolResultInvalid) {
		t.Fatalf("Call() error = %v", err)
	}
}

func TestClientCallTimeout(t *testing.T) {
	started := make(chan struct{})
	weatherClient := &countingWeatherClient{
		result:  &weather.Result{},
		wait:    true,
		started: started,
	}
	client := newWeatherMCPClient(t, weatherClient, 30*time.Millisecond)

	_, err := client.Call(t.Context(), mcpplatform.WeatherToolName, json.RawMessage(`{"city":"上海"}`))
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, mcpplatform.ErrCallToolFailed) {
		t.Fatalf("Call() error = %v", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("weather handler did not start")
	}
}

func TestNewClientRejectsInvalidConfigAndUnavailableServer(t *testing.T) {
	if _, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{}); !errors.Is(err, mcpplatform.ErrInvalidEndpoint) {
		t.Fatalf("empty endpoint error = %v", err)
	}
	if _, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{Endpoint: "http://example.com/mcp", Timeout: -1}); !errors.Is(err, mcpplatform.ErrInvalidTimeout) {
		t.Fatalf("negative timeout error = %v", err)
	}

	closedServer := httptest.NewServer(http.NotFoundHandler())
	endpoint := closedServer.URL + "/mcp"
	closedServer.Close()
	if _, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{Endpoint: endpoint, Timeout: time.Second}); !errors.Is(err, mcpplatform.ErrConnectFailed) {
		t.Fatalf("unavailable server error = %v", err)
	}
}

func TestNewClientRequiresWeatherTool(t *testing.T) {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "empty-server", Version: "0.1.0"}, nil)
	httpServer := newMCPHTTPServer(t, server)

	_, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{
		Endpoint:   httpServer.URL + "/mcp",
		HTTPClient: httpServer.Client(),
		Timeout:    time.Second,
	})
	if !errors.Is(err, mcpplatform.ErrWeatherToolNotFound) {
		t.Fatalf("NewClient() error = %v", err)
	}
}

func newWeatherMCPClient(t *testing.T, weatherClient mcpplatform.WeatherClient, timeout time.Duration) *mcpplatform.Client {
	t.Helper()
	server, err := mcpplatform.NewWeatherServer(weatherClient)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := newMCPHTTPServer(t, server)
	client, err := mcpplatform.NewClient(t.Context(), mcpplatform.Config{
		Endpoint:   httpServer.URL + "/mcp",
		HTTPClient: httpServer.Client(),
		Timeout:    timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return client
}

func newMCPHTTPServer(t *testing.T, server *mcpsdk.Server) *httptest.Server {
	t.Helper()
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{
			Stateless:                    true,
			JSONResponse:                 true,
			PropagateRequestCancellation: true,
		},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	return httpServer
}
