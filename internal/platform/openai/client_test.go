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
		_, _ = writer.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"hello from OpenAI"}]}]}`))
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
	if got != "hello from OpenAI" {
		t.Fatalf("Generate() = %q, want %q", got, "hello from OpenAI")
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
		_, _ = writer.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
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
	if got != "hello from OpenAI" {
		t.Fatalf("GenerateStream() = %q, want %q", got, "hello from OpenAI")
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
