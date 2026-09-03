package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"

	"gopherai/internal/llm"
)

const weatherToolSchema = `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`

func TestClientGenerateWithToolsReturnsToolCall(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-tool","usage":{"input_tokens":11,"output_tokens":4,"total_tokens":15},"output":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque-reasoning"},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"上海\"}"}]}`))
	}))
	defer server.Close()

	client, err := NewClientWithConfig(Config{
		APIKey:       "test-key",
		Model:        "gpt-test",
		BaseURL:      server.URL + "/",
		WireAPI:      "responses",
		RequiresAuth: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.GenerateWithTools(context.Background(), []llm.Message{{Role: "user", Content: "上海天气"}}, []llm.ToolDefinition{{
		Name:        "get_weather",
		Description: "query weather",
		InputSchema: json.RawMessage(weatherToolSchema),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", got.ToolCalls)
	}
	call := got.ToolCalls[0]
	if call.ID != "fc_1" || call.CallID != "call_1" || call.Name != "get_weather" || string(call.Arguments) != `{"city":"上海"}` {
		t.Fatalf("tool call = %#v", call)
	}
	if got.Result.InputTokens != 11 || got.Result.OutputTokens != 4 || got.Result.TotalTokens != 15 {
		t.Fatalf("result = %#v", got.Result)
	}
	if len(got.Continuation) != 2 || !strings.Contains(string(got.Continuation[0]), "opaque-reasoning") {
		t.Fatalf("continuation = %#v", got.Continuation)
	}

	tools, ok := request["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("request tools = %#v", request["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "function" || tool["name"] != "get_weather" || tool["description"] != "query weather" {
		t.Fatalf("request tool = %#v", tools[0])
	}
	if request["input"] == nil {
		t.Fatalf("request input missing: %#v", request)
	}
	include, ok := request["include"].([]any)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("request include = %#v", request["include"])
	}
}

func TestClientGenerateWithToolsRejectsInvalidDefinition(t *testing.T) {
	client, err := NewClientWithConfig(Config{
		APIKey: "test-key", Model: "gpt-test", WireAPI: "responses", RequiresAuth: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []llm.ToolDefinition{
		{},
		{Name: "get_weather", InputSchema: json.RawMessage(`[]`)},
		{Name: "get_weather", InputSchema: json.RawMessage(`{"type":"string"}`)},
	} {
		if _, err := client.GenerateWithTools(context.Background(), nil, []llm.ToolDefinition{tool}); !errors.Is(err, ErrInvalidToolDefinition) {
			t.Fatalf("tool %#v error = %v", tool, err)
		}
	}
}

func TestClientGenerateWithToolResultBuildsFunctionCallOutput(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-final","usage":{"input_tokens":30,"output_tokens":8,"total_tokens":38},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"上海现在晴，23°C"}]}]}`))
	}))
	defer server.Close()

	client, err := NewClient("test-key", "gpt-test", option.WithBaseURL(server.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{
		ID:        "fc_1",
		CallID:    "call_1",
		Name:      "get_weather",
		Arguments: json.RawMessage(`{"city":"上海"}`),
	}
	continuation := []json.RawMessage{
		json.RawMessage(`{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque-reasoning"}`),
		json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"上海\"}"}`),
	}
	result, err := client.GenerateWithToolResult(context.Background(), []llm.Message{{Role: "user", Content: "上海天气"}}, continuation, call, json.RawMessage(`{"location":"上海","temperature_c":23}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "上海现在晴，23°C" || result.Model != "gpt-final" || result.TotalTokens != 38 {
		t.Fatalf("result = %#v", result)
	}

	input, ok := request["input"].([]any)
	if !ok || len(input) != 4 {
		t.Fatalf("request input = %#v", request["input"])
	}
	reasoning, ok := input[1].(map[string]any)
	if !ok || reasoning["type"] != "reasoning" || reasoning["encrypted_content"] != "opaque-reasoning" {
		t.Fatalf("reasoning input = %#v", input[1])
	}
	functionCall, ok := input[2].(map[string]any)
	if !ok || functionCall["type"] != "function_call" || functionCall["call_id"] != "call_1" || functionCall["name"] != "get_weather" || functionCall["arguments"] != `{"city":"上海"}` {
		t.Fatalf("function call input = %#v", input[2])
	}
	functionOutput, ok := input[3].(map[string]any)
	if !ok || functionOutput["type"] != "function_call_output" || functionOutput["call_id"] != "call_1" || functionOutput["output"] != `{"location":"上海","temperature_c":23}` {
		t.Fatalf("function output input = %#v", input[3])
	}
}

func TestClientGenerateWithToolResultRejectsInvalidCall(t *testing.T) {
	client, err := NewClientWithConfig(Config{APIKey: "test-key", Model: "gpt-test", WireAPI: "responses", RequiresAuth: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GenerateWithToolResult(context.Background(), nil, nil, llm.ToolCall{}, json.RawMessage(`{}`))
	if !errors.Is(err, ErrInvalidToolCall) {
		t.Fatalf("error = %v", err)
	}
}
