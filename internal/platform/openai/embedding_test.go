package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"
)

type embeddingRoundTripper func(*http.Request) (*http.Response, error)

func (f embeddingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func embeddingHTTPClient(body string, inspect func(*http.Request)) *http.Client {
	return &http.Client{Transport: embeddingRoundTripper(func(req *http.Request) (*http.Response, error) {
		if inspect != nil {
			inspect(req)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
}

func TestNewEmbedderValidatesConfiguration(t *testing.T) {
	if _, err := NewEmbedder("", "model"); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("missing key error = %v", err)
	}
	if _, err := NewEmbedder("key", ""); !errors.Is(err, ErrMissingModel) {
		t.Fatalf("missing model error = %v", err)
	}
	if _, err := NewEmbedderWithConfig(EmbeddingConfig{RequiresAuth: true, Model: "model"}); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("config missing key error = %v", err)
	}
	if _, err := NewEmbedderWithConfig(EmbeddingConfig{APIKey: "key"}); !errors.Is(err, ErrMissingModel) {
		t.Fatalf("config missing model error = %v", err)
	}
}

func TestEmbedderEmbedsBatchAndRestoresIndexOrder(t *testing.T) {
	var captured struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}
	httpClient := embeddingHTTPClient(`{
		"data":[
			{"index":1,"embedding":[3,4]},
			{"index":0,"embedding":[1,2]}
		],
		"model":"embedding-test",
		"object":"list",
		"usage":{"prompt_tokens":2,"total_tokens":2}
	}`, func(req *http.Request) {
		if got := req.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if err := json.NewDecoder(req.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
	})
	embedder, err := NewEmbedder("test-key", "embedding-test",
		option.WithBaseURL("http://embedding.test/"), option.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{" first ", "second"}
	vectors, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 3 {
		t.Fatalf("vectors = %#v", vectors)
	}
	if captured.Model != "embedding-test" || strings.Join(captured.Input, "|") != "first|second" {
		t.Fatalf("request = %#v", captured)
	}
	if texts[0] != " first " {
		t.Fatalf("Embed() mutated input: %#v", texts)
	}
}

func TestEmbedderRejectsInvalidInputAndResponse(t *testing.T) {
	embedder, err := NewEmbedder("key", "model", option.WithBaseURL("http://embedding.test/"),
		option.WithHTTPClient(embeddingHTTPClient(`{"data":[]}`, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := embedder.Embed(context.Background(), nil); !errors.Is(err, ErrEmptyEmbeddingInput) {
		t.Fatalf("empty input error = %v", err)
	}
	if _, err := embedder.Embed(context.Background(), []string{" "}); !errors.Is(err, ErrEmptyEmbeddingInput) {
		t.Fatalf("blank input error = %v", err)
	}
	if _, err := embedder.Embed(context.Background(), []string{"text"}); !errors.Is(err, ErrEmbeddingResponseInvalid) {
		t.Fatalf("invalid response error = %v", err)
	}
}

func TestEmbedderRejectsDuplicateAndEmptyVectors(t *testing.T) {
	tests := []string{
		`{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`,
		`{"data":[{"index":0,"embedding":[]}]}`,
	}
	inputs := [][]string{{"a", "b"}, {"a"}}
	for i, body := range tests {
		embedder, err := NewEmbedder("key", "model", option.WithBaseURL("http://embedding.test/"),
			option.WithHTTPClient(embeddingHTTPClient(body, nil)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := embedder.Embed(context.Background(), inputs[i]); !errors.Is(err, ErrEmbeddingResponseInvalid) {
			t.Fatalf("case %d error = %v", i, err)
		}
	}
}

func TestEmbedderSplitsLargeInputAndPreservesGlobalOrder(t *testing.T) {
	requestSizes := make([]int, 0, 3)
	globalOffset := 0
	httpClient := &http.Client{Transport: embeddingRoundTripper(func(req *http.Request) (*http.Response, error) {
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			return nil, err
		}

		requestSizes = append(requestSizes, len(request.Input))
		data := make([]map[string]any, 0, len(request.Input))
		for index := len(request.Input) - 1; index >= 0; index-- {
			value := float64(globalOffset + index)
			data = append(data, map[string]any{
				"index":     index,
				"embedding": []float64{value, value + 0.5},
			})
		}
		globalOffset += len(request.Input)
		body, err := json.Marshal(map[string]any{"data": data})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    req,
		}, nil
	})}

	embedder, err := NewEmbedder("key", "model",
		option.WithBaseURL("http://embedding.test/"), option.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 130)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	vectors, err := embedder.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if got := fmt.Sprint(requestSizes); got != "[64 64 2]" {
		t.Fatalf("request sizes = %s, want [64 64 2]", got)
	}
	if len(vectors) != len(texts) {
		t.Fatalf("vectors = %d, want %d", len(vectors), len(texts))
	}
	for i, vector := range vectors {
		if len(vector) != 2 || vector[0] != float32(i) {
			t.Fatalf("vector %d = %#v", i, vector)
		}
	}
}

func TestEmbedderReturnsNoPartialResultWhenLaterBatchFails(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: embeddingRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 2 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"embedding failed"}}`)),
				Request:    req,
			}, nil
		}

		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			return nil, err
		}
		data := make([]map[string]any, len(request.Input))
		for i := range request.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{1, 2}}
		}
		body, err := json.Marshal(map[string]any{"data": data})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    req,
		}, nil
	})}

	embedder, err := NewEmbedder("key", "model",
		option.WithBaseURL("http://embedding.test/"),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, maxEmbeddingBatchSize+1)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	vectors, err := embedder.Embed(context.Background(), texts)
	if !errors.Is(err, ErrEmbeddingFailed) {
		t.Fatalf("Embed() error = %v, want %v", err, ErrEmbeddingFailed)
	}
	if vectors != nil {
		t.Fatalf("vectors = %#v, want nil", vectors)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestEmbedderRejectsDimensionMismatchAcrossBatches(t *testing.T) {
	requests := 0
	httpClient := &http.Client{Transport: embeddingRoundTripper(func(req *http.Request) (*http.Response, error) {
		requests++
		var request struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			return nil, err
		}
		dimension := 2
		if requests == 2 {
			dimension = 3
		}
		data := make([]map[string]any, len(request.Input))
		for i := range request.Input {
			data[i] = map[string]any{"index": i, "embedding": make([]float64, dimension)}
		}
		body, err := json.Marshal(map[string]any{"data": data})
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    req,
		}, nil
	})}
	embedder, err := NewEmbedder("key", "model",
		option.WithBaseURL("http://embedding.test/"), option.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, maxEmbeddingBatchSize+1)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	if _, err := embedder.Embed(context.Background(), texts); !errors.Is(err, ErrEmbeddingResponseInvalid) {
		t.Fatalf("Embed() error = %v, want %v", err, ErrEmbeddingResponseInvalid)
	}
}
