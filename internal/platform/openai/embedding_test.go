package openai

import (
	"context"
	"encoding/json"
	"errors"
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
