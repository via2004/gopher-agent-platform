package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopherai/internal/llm"
	openaiplatform "gopherai/internal/platform/openai"
)

var llmEnvironmentNames = []string{
	"OPENAI_API_KEY",
	"OPENAI_MODEL",
	"OPENAI_BASE_URL",
	"OPENAI_WIRE_API",
	"OPENAI_REQUIRES_AUTH",
	"OPENAI_REASONING_EFFORT",
	"OPENAI_DISABLE_RESPONSE_STORAGE",
}

func TestLoadEnvironmentAllowsMissingFile(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := loadEnvironment(); err != nil {
		t.Fatalf("loadEnvironment() error = %v, want nil", err)
	}
}

func TestLoadEnvironmentRejectsInvalidFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("INVALID LINE\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Chdir(directory)

	err := loadEnvironment()
	if err == nil || !strings.Contains(err.Error(), "load .env") {
		t.Fatalf("loadEnvironment() error = %v, want wrapped parse error", err)
	}
}

func TestBuildLLMClientReturnsUnavailableClientWithoutConfiguration(t *testing.T) {
	clearLLMEnvironment(t)

	client, err := buildLLMClient()
	if err != nil {
		t.Fatalf("buildLLMClient() error = %v", err)
	}
	if _, ok := client.(llm.UnavailableClient); !ok {
		t.Fatalf("buildLLMClient() client = %T, want llm.UnavailableClient", client)
	}
}

func TestBuildLLMClientBuildsConfiguredOpenAIClient(t *testing.T) {
	clearLLMEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_MODEL", "gpt-test")
	t.Setenv("OPENAI_BASE_URL", "https://example.com/")

	client, err := buildLLMClient()
	if err != nil {
		t.Fatalf("buildLLMClient() error = %v", err)
	}
	if _, ok := client.(*openaiplatform.Client); !ok {
		t.Fatalf("buildLLMClient() client = %T, want *openai.Client", client)
	}
}

func TestBuildLLMClientRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		setEnv  func(t *testing.T)
		wantErr error
	}{
		{
			name: "missing model",
			setEnv: func(t *testing.T) {
				t.Setenv("OPENAI_API_KEY", "test-key")
			},
			wantErr: openaiplatform.ErrMissingModel,
		},
		{
			name: "missing API key",
			setEnv: func(t *testing.T) {
				t.Setenv("OPENAI_MODEL", "gpt-test")
			},
			wantErr: openaiplatform.ErrMissingAPIKey,
		},
		{
			name: "unsupported wire API",
			setEnv: func(t *testing.T) {
				t.Setenv("OPENAI_API_KEY", "test-key")
				t.Setenv("OPENAI_MODEL", "gpt-test")
				t.Setenv("OPENAI_WIRE_API", "chat_completions")
			},
			wantErr: openaiplatform.ErrUnsupportedWireAPI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearLLMEnvironment(t)
			tt.setEnv(t)

			_, err := buildLLMClient()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("buildLLMClient() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildLLMClientRejectsInvalidBooleanValues(t *testing.T) {
	tests := []string{
		"OPENAI_REQUIRES_AUTH",
		"OPENAI_DISABLE_RESPONSE_STORAGE",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			clearLLMEnvironment(t)
			t.Setenv(name, "not-a-bool")

			_, err := buildLLMClient()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("buildLLMClient() error = %v, want error naming %s", err, name)
			}
		})
	}
}

func clearLLMEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range llmEnvironmentNames {
		t.Setenv(name, "")
	}
}
