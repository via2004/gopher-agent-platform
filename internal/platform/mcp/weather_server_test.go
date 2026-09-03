package mcp_test

import (
	"context"
	"errors"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpplatform "gopherai/internal/platform/mcp"
	"gopherai/internal/weather"
)

type fakeWeatherClient struct {
	result *weather.Result
	err    error
	city   string
}

func (f *fakeWeatherClient) Get(_ context.Context, city string) (*weather.Result, error) {
	f.city = city
	return f.result, f.err
}

func TestWeatherServerRegistersAndCallsTool(t *testing.T) {
	fake := &fakeWeatherClient{result: &weather.Result{Location: "上海", TemperatureC: 23}}
	server, err := mcpplatform.NewWeatherServer(fake)
	if err != nil {
		t.Fatal(err)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "get_weather" {
		t.Fatalf("tools = %#v, error = %v", tools, err)
	}
	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "get_weather",
		Arguments: map[string]any{"city": "上海"},
	})
	if err != nil || result.IsError {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if fake.city != "上海" {
		t.Fatalf("city = %q", fake.city)
	}
}

func TestWeatherServerReturnsProviderErrorAsToolError(t *testing.T) {
	fake := &fakeWeatherClient{err: errors.New("provider unavailable")}
	server, err := mcpplatform.NewWeatherServer(fake)
	if err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()
	if _, err := server.Connect(t.Context(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name:      "get_weather",
		Arguments: map[string]any{"city": "上海"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result = %#v, want tool error", result)
	}
}
