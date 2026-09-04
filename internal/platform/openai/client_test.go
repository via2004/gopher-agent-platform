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

func TestNewClientRejectsMissingConfiguration(t *testing.T) {
	if _, err := NewClient("", "gpt-5"); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("NewClient() error = %v, want %v", err, ErrMissingAPIKey)
	}
	if _, err := NewClient("test-key", ""); !errors.Is(err, ErrMissingModel) {
		t.Fatalf("NewClient() error = %v, want %v", err, ErrMissingModel)
	}
	if _, err := NewClientWithConfig(Config{APIKey: "test-key", Model: "gpt-test", WireAPI: "chat_completions", RequiresAuth: true}); !errors.Is(err, ErrUnsupportedWireAPI) {
		t.Fatalf("NewClientWithConfig() error = %v, want %v", err, ErrUnsupportedWireAPI)
	}
}

func TestClientGenerateUsesResponsesAPI(t *testing.T) {
	var captured struct {
		Model     string `json:"model"`
		Store     bool   `json:"store"`
		Reasoning struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Input []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/responses" {
			t.Errorf("request = %s %s, want POST /responses", req.Method, req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decode request: %v; body = %s", err, body)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"gpt-test-actual","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30},"output":[{"type":"message","content":[{"type":"output_text","text":"hello from OpenAI"}]}]}`))
	}))
	defer server.Close()

	client, err := NewClientWithConfig(Config{
		APIKey:                 "test-key",
		Model:                  "gpt-test",
		BaseURL:                server.URL + "/",
		WireAPI:                "responses",
		RequiresAuth:           true,
		ReasoningEffort:        "medium",
		DisableResponseStorage: false,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	got, err := client.Generate(context.Background(), []llm.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "previous answer"},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got.Content != "hello from OpenAI" || got.Model != "gpt-test-actual" ||
		got.InputTokens != 20 || got.OutputTokens != 10 || got.TotalTokens != 30 {
		t.Fatalf("Generate() = %#v", got)
	}
	if captured.Model != "gpt-test" || !captured.Store || captured.Reasoning.Effort != "medium" || len(captured.Input) != 2 {
		t.Fatalf("request = %#v, want model and 2 input messages", captured)
	}
	if captured.Input[0].Type != "message" || captured.Input[0].Role != "user" || captured.Input[0].Content != "hello" ||
		captured.Input[1].Role != "assistant" || captured.Input[1].Content != "previous answer" {
		t.Fatalf("request input = %#v", captured.Input)
	}
}

func TestClientGenerateReturnsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"invalid request"}}`))
	}))
	defer server.Close()

	client, err := NewClient("test-key", "gpt-test", option.WithBaseURL(server.URL+"/"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.Generate(context.Background(), []llm.Message{{Role: "user", Content: strings.Repeat("x", 3)}})
	if err == nil || !strings.Contains(err.Error(), "generate OpenAI response") {
		t.Fatalf("Generate() error = %v, want wrapped provider error", err)
	}
}

func TestClientGenerateStreamUsesResponsesAPI(t *testing.T) {
	var captured struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
		Input  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("decode request: %v; body = %s", err, body)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello \"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"from OpenAI\"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-test-actual\",\"usage\":{\"input_tokens\":20,\"output_tokens\":10,\"total_tokens\":30}}}\n\n"))
	}))
	defer server.Close()

	client, err := NewClient("test-key", "gpt-test", option.WithBaseURL(server.URL+"/"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	var deltas []string
	got, err := client.GenerateStream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if got.Content != "hello from OpenAI" || got.Model != "gpt-test-actual" ||
		got.InputTokens != 20 || got.OutputTokens != 10 || got.TotalTokens != 30 {
		t.Fatalf("GenerateStream() = %#v", got)
	}
	if len(deltas) != 2 || deltas[0] != "hello " || deltas[1] != "from OpenAI" {
		t.Fatalf("deltas = %#v", deltas)
	}
	if captured.Model != "gpt-test" || !captured.Stream || len(captured.Input) != 1 || captured.Input[0].Role != "user" || captured.Input[0].Content != "hello" {
		t.Fatalf("request = %#v", captured)
	}
}

func TestClientGenerateStreamRejectsMissingCallback(t *testing.T) {
	_, err := (&Client{}).GenerateStream(context.Background(), nil, nil)
	if !errors.Is(err, llm.ErrOnDeltaMissed) {
		t.Fatalf("GenerateStream() error = %v, want %v", err, llm.ErrOnDeltaMissed)
	}
}

func TestClientGenerateStreamReturnsFailedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"provider failed\",\"code\":\"server_error\"}}}\n\n"))
	}))
	defer server.Close()

	client, err := NewClient("test-key", "gpt-test", option.WithBaseURL(server.URL+"/"))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.GenerateStream(context.Background(), []llm.Message{{Role: "user", Content: "hello"}}, func(string) error { return nil })
	if !errors.Is(err, llm.ErrResponseFailed) || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("GenerateStream() error = %v, want wrapped provider failure", err)
	}
}

func TestClientGenerateStreamWithToolsReturnsToolCall(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_1\",\"summary\":[],\"encrypted_content\":\"opaque\"}}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"上海\\\"}\"}}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-tool\",\"usage\":{\"input_tokens\":11,\"output_tokens\":4,\"total_tokens\":15},\"output\":[{\"type\":\"reasoning\",\"id\":\"rs_1\",\"summary\":[],\"encrypted_content\":\"opaque\"},{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"上海\\\"}\"}]}}\n\n"))
	}))
	defer server.Close()

	client, err := NewClient("test-key", "gpt-test", option.WithBaseURL(server.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	got, err := client.GenerateStreamWithTools(context.Background(), []llm.Message{{Role: "user", Content: "上海天气"}}, []llm.ToolDefinition{{
		Name: "get_weather", InputSchema: json.RawMessage(weatherToolSchema),
	}}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deltas) != 0 || len(got.ToolCalls) != 1 {
		t.Fatalf("deltas = %#v, tool calls = %#v", deltas, got.ToolCalls)
	}
	call := got.ToolCalls[0]
	if call.Name != "get_weather" || string(call.Arguments) != `{"city":"上海"}` || len(got.Continuation) != 2 {
		t.Fatalf("tool result = %#v continuation = %#v", call, got.Continuation)
	}
	if got.Result.InputTokens != 11 || got.Result.OutputTokens != 4 || got.Result.TotalTokens != 15 {
		t.Fatalf("result = %#v", got.Result)
	}
	if request["tools"] == nil || request["include"] == nil {
		t.Fatalf("request missing tools/include: %#v", request)
	}
}

func TestClientGenerateStreamWithToolResultStreamsFinalAnswer(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"上海现在\"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"晴，23°C\"}\n\n"))
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-final\",\"usage\":{\"input_tokens\":20,\"output_tokens\":5,\"total_tokens\":25},\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"上海现在晴，23°C\"}]}]}}\n\n"))
	}))
	defer server.Close()

	client, err := NewClient("test-key", "gpt-test", option.WithBaseURL(server.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{ID: "fc_1", CallID: "call_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"上海"}`)}
	continuation := []json.RawMessage{json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"上海\"}"}`)}
	var deltas []string
	result, err := client.GenerateStreamWithToolResult(context.Background(), []llm.Message{{Role: "user", Content: "上海天气"}}, continuation, call, json.RawMessage(`{"location":"上海","temperature_c":23}`), func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "上海现在晴，23°C" || result.TotalTokens != 25 || strings.Join(deltas, "") != result.Content {
		t.Fatalf("result = %#v, deltas = %#v", result, deltas)
	}
	input, ok := request["input"].([]any)
	if !ok || len(input) != 3 {
		t.Fatalf("request input = %#v", request["input"])
	}
	if item, ok := input[2].(map[string]any); !ok || item["type"] != "function_call_output" {
		t.Fatalf("function output = %#v", input[2])
	}
}
