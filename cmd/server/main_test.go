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

var embeddingEnvironmentNames = []string{
	"OPENAI_EMBEDDING_API_KEY",
	"OPENAI_EMBEDDING_BASE_URL",
	"OPENAI_EMBEDDING_MODEL",
	"OPENAI_EMBEDDING_REQUIRES_AUTH",
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

func TestPositiveEnvInt64(t *testing.T) {
	t.Setenv("TEST_POSITIVE_INT", " 42 ")
	value, err := positiveEnvInt64("TEST_POSITIVE_INT")
	if err != nil || value != 42 {
		t.Fatalf("positiveEnvInt64() = %d, %v, want 42", value, err)
	}

	for _, value := range []string{"", "invalid", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TEST_POSITIVE_INT", value)
			if _, err := positiveEnvInt64("TEST_POSITIVE_INT"); err == nil || !strings.Contains(err.Error(), "TEST_POSITIVE_INT") {
				t.Fatalf("positiveEnvInt64(%q) error = %v", value, err)
			}
		})
	}
}

func TestConnectorsRejectMissingURLs(t *testing.T) {
	if _, err := connectPostgreSQL(""); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("connectPostgreSQL() error = %v", err)
	}
	if _, err := connectRedis(""); err == nil || !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("connectRedis() error = %v", err)
	}
}

func TestEmbeddingConfigFallsBackToGeneralOpenAIConnection(t *testing.T) {
	clearLLMEnvironment(t)
	clearEmbeddingEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "general-key")
	t.Setenv("OPENAI_BASE_URL", "https://general.example/v1")
	t.Setenv("OPENAI_REQUIRES_AUTH", "false")
	t.Setenv("OPENAI_EMBEDDING_MODEL", "embedding-model")

	config, err := embeddingConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.APIKey != "general-key" || config.BaseURL != "https://general.example/v1" ||
		config.Model != "embedding-model" || config.RequiresAuth {
		t.Fatalf("embedding config = %#v", config)
	}
}

func TestEmbeddingConfigUsesDedicatedConnection(t *testing.T) {
	clearLLMEnvironment(t)
	clearEmbeddingEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "general-key")
	t.Setenv("OPENAI_BASE_URL", "https://general.example/v1")
	t.Setenv("OPENAI_REQUIRES_AUTH", "false")
	t.Setenv("OPENAI_EMBEDDING_API_KEY", "embedding-key")
	t.Setenv("OPENAI_EMBEDDING_BASE_URL", "https://embedding.example/v1")
	t.Setenv("OPENAI_EMBEDDING_MODEL", "embedding-model")
	t.Setenv("OPENAI_EMBEDDING_REQUIRES_AUTH", "true")

	config, err := embeddingConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.APIKey != "embedding-key" || config.BaseURL != "https://embedding.example/v1" ||
		config.Model != "embedding-model" || !config.RequiresAuth {
		t.Fatalf("embedding config = %#v", config)
	}
}

func TestEmbeddingConfigRejectsInvalidAuthSetting(t *testing.T) {
	clearLLMEnvironment(t)
	clearEmbeddingEnvironment(t)
	t.Setenv("OPENAI_EMBEDDING_REQUIRES_AUTH", "invalid")
	if _, err := embeddingConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "OPENAI_EMBEDDING_REQUIRES_AUTH") {
		t.Fatalf("embeddingConfigFromEnvironment() error = %v", err)
	}
}

func TestHTTPAddress(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	if got := httpAddress(); got != defaultHTTPAddr {
		t.Fatalf("httpAddress() = %q, want %q", got, defaultHTTPAddr)
	}

	t.Setenv("HTTP_ADDR", " 0.0.0.0:8080 ")
	if got := httpAddress(); got != "0.0.0.0:8080" {
		t.Fatalf("httpAddress() = %q, want %q", got, "0.0.0.0:8080")
	}
}

func clearLLMEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range llmEnvironmentNames {
		t.Setenv(name, "")
	}
}

func clearEmbeddingEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range embeddingEnvironmentNames {
		t.Setenv(name, "")
	}
}
