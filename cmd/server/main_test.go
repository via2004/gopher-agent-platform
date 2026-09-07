package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	redisclient "github.com/redis/go-redis/v9"

	"gopherai/internal/agent"
	"gopherai/internal/llm"
	baiduttsplatform "gopherai/internal/platform/baidutts"
	mcpplatform "gopherai/internal/platform/mcp"
	openaiplatform "gopherai/internal/platform/openai"
	rabbitmqplatform "gopherai/internal/platform/rabbitmq"
	"gopherai/internal/weather"
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

var ttsEnvironmentNames = []string{
	"BAIDU_TTS_API_KEY",
	"BAIDU_TTS_SECRET_KEY",
	"BAIDU_TTS_BASE_URL",
	"BAIDU_TTS_TIMEOUT_SECONDS",
	"TTS_RATE_LIMIT",
	"TTS_RATE_WINDOW_SECONDS",
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

func TestBuildModelFeatureWithoutMCPKeepsBaseModel(t *testing.T) {
	clearLLMEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_MODEL", "gpt-test")
	t.Setenv("MCP_SERVER_URL", "")

	feature, err := buildModelFeature(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := feature.client.(*openaiplatform.Client); !ok {
		t.Fatalf("model client = %T, want *openai.Client", feature.client)
	}
	if err := feature.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildModelFeatureWrapsToolModelWithAgent(t *testing.T) {
	clearLLMEnvironment(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_MODEL", "gpt-test")
	t.Setenv("MCP_CALL_TIMEOUT_SECONDS", "1")

	server, err := mcpplatform.NewWeatherServer(bootstrapWeatherClient{})
	if err != nil {
		t.Fatal(err)
	}
	handler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	t.Setenv("MCP_SERVER_URL", httpServer.URL)

	feature, err := buildModelFeature(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := feature.client.(*agent.Client); !ok {
		t.Fatalf("model client = %T, want *agent.Client", feature.client)
	}
	if err := feature.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildModelFeatureRejectsMCPWithoutToolModel(t *testing.T) {
	clearLLMEnvironment(t)
	t.Setenv("MCP_SERVER_URL", "http://mcp.example/mcp")
	if _, err := buildModelFeature(t.Context()); err == nil || !strings.Contains(err.Error(), "does not support tool calling") {
		t.Fatalf("buildModelFeature() error = %v", err)
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

func TestPositiveEnvInt64OrDefault(t *testing.T) {
	t.Setenv("TEST_OPTIONAL_POSITIVE_INT", "")
	if got, err := positiveEnvInt64OrDefault("TEST_OPTIONAL_POSITIVE_INT", 5); err != nil || got != 5 {
		t.Fatalf("default value = %d, %v; want 5", got, err)
	}
	t.Setenv("TEST_OPTIONAL_POSITIVE_INT", " 7 ")
	if got, err := positiveEnvInt64OrDefault("TEST_OPTIONAL_POSITIVE_INT", 5); err != nil || got != 7 {
		t.Fatalf("configured value = %d, %v; want 7", got, err)
	}
}

func TestPositiveDurationEnvOrDefault(t *testing.T) {
	t.Setenv("TEST_DURATION_SECONDS", "")
	if got, err := positiveDurationEnvOrDefault("TEST_DURATION_SECONDS", 3*time.Second); err != nil || got != 3*time.Second {
		t.Fatalf("default duration = %v, %v", got, err)
	}
	t.Setenv("TEST_DURATION_SECONDS", " 7 ")
	if got, err := positiveDurationEnvOrDefault("TEST_DURATION_SECONDS", time.Second); err != nil || got != 7*time.Second {
		t.Fatalf("configured duration = %v, %v", got, err)
	}
	for _, value := range []string{"invalid", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TEST_DURATION_SECONDS", value)
			if _, err := positiveDurationEnvOrDefault("TEST_DURATION_SECONDS", time.Second); err == nil {
				t.Fatalf("duration %q error = nil", value)
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

func TestConnectRabbitMQWithRetryEventuallySucceeds(t *testing.T) {
	attempts := 0
	wantClient := &rabbitmqplatform.RabbitMQClient{}
	client, err := connectRabbitMQWithRetry(
		context.Background(),
		"amqp://rabbitmq",
		func(string) (*rabbitmqplatform.RabbitMQClient, error) {
			attempts++
			if attempts < 3 {
				return nil, rabbitmqplatform.ErrAMQPDialFailed
			}
			return wantClient, nil
		},
		3,
		0,
	)
	if err != nil || client != wantClient || attempts != 3 {
		t.Fatalf("connectRabbitMQWithRetry() = %p, %v after %d attempts", client, err, attempts)
	}
}

func TestConnectRabbitMQWithRetryStopsAtAttemptLimit(t *testing.T) {
	attempts := 0
	_, err := connectRabbitMQWithRetry(
		context.Background(),
		"amqp://rabbitmq",
		func(string) (*rabbitmqplatform.RabbitMQClient, error) {
			attempts++
			return nil, rabbitmqplatform.ErrAMQPDialFailed
		},
		3,
		0,
	)
	if !errors.Is(err, rabbitmqplatform.ErrAMQPDialFailed) || attempts != 3 {
		t.Fatalf("connectRabbitMQWithRetry() error = %v after %d attempts", err, attempts)
	}
}

func TestConnectRabbitMQWithRetryRejectsInvalidURLImmediately(t *testing.T) {
	attempts := 0
	_, err := connectRabbitMQWithRetry(
		context.Background(),
		"",
		func(string) (*rabbitmqplatform.RabbitMQClient, error) {
			attempts++
			return nil, rabbitmqplatform.ErrURLInvalid
		},
		3,
		time.Hour,
	)
	if !errors.Is(err, rabbitmqplatform.ErrURLInvalid) || attempts != 1 {
		t.Fatalf("connectRabbitMQWithRetry() error = %v after %d attempts", err, attempts)
	}
}

func TestConnectMCPWithRetryEventuallySucceeds(t *testing.T) {
	attempts := 0
	want := &mcpplatform.Client{}
	client, err := connectMCPWithRetry(t.Context(), mcpplatform.Config{}, func(context.Context, mcpplatform.Config) (*mcpplatform.Client, error) {
		attempts++
		if attempts < 3 {
			return nil, mcpplatform.ErrConnectFailed
		}
		return want, nil
	}, 3, 0)
	if err != nil || client != want || attempts != 3 {
		t.Fatalf("connectMCPWithRetry() = %p, %v after %d attempts", client, err, attempts)
	}
}

func TestConnectMCPWithRetryStopsForNonRetryableError(t *testing.T) {
	attempts := 0
	_, err := connectMCPWithRetry(t.Context(), mcpplatform.Config{}, func(context.Context, mcpplatform.Config) (*mcpplatform.Client, error) {
		attempts++
		return nil, mcpplatform.ErrWeatherToolNotFound
	}, 3, time.Hour)
	if !errors.Is(err, mcpplatform.ErrWeatherToolNotFound) || attempts != 1 {
		t.Fatalf("connectMCPWithRetry() error = %v after %d attempts", err, attempts)
	}
}

func TestConnectMCPWithRetryStopsAtAttemptLimit(t *testing.T) {
	attempts := 0
	_, err := connectMCPWithRetry(t.Context(), mcpplatform.Config{}, func(context.Context, mcpplatform.Config) (*mcpplatform.Client, error) {
		attempts++
		return nil, mcpplatform.ErrListToolsFailed
	}, 3, 0)
	if !errors.Is(err, mcpplatform.ErrListToolsFailed) || attempts != 3 {
		t.Fatalf("connectMCPWithRetry() error = %v after %d attempts", err, attempts)
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

func TestBuildTTSProviderKeepsFeatureDisabledWithoutCredentials(t *testing.T) {
	clearTTSEnvironment(t)
	t.Setenv("BAIDU_TTS_BASE_URL", "://ignored-while-disabled")
	t.Setenv("BAIDU_TTS_TIMEOUT_SECONDS", "invalid")

	provider, err := buildTTSProvider()
	if err != nil {
		t.Fatalf("buildTTSProvider() error = %v", err)
	}
	if provider != nil {
		t.Fatalf("buildTTSProvider() provider = %T, want nil", provider)
	}
}

func TestBuildTTSProviderRejectsIncompleteCredentials(t *testing.T) {
	tests := []struct {
		name       string
		apiKey     string
		secretKey  string
		missingEnv string
	}{
		{name: "missing API key", secretKey: "secret", missingEnv: "BAIDU_TTS_API_KEY"},
		{name: "missing secret key", apiKey: "key", missingEnv: "BAIDU_TTS_SECRET_KEY"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearTTSEnvironment(t)
			t.Setenv("BAIDU_TTS_API_KEY", test.apiKey)
			t.Setenv("BAIDU_TTS_SECRET_KEY", test.secretKey)
			provider, err := buildTTSProvider()
			if err == nil || !strings.Contains(err.Error(), test.missingEnv) {
				t.Fatalf("buildTTSProvider() = %T, %v; want error naming %s", provider, err, test.missingEnv)
			}
			if provider != nil {
				t.Fatalf("buildTTSProvider() provider = %T, want nil", provider)
			}
		})
	}
}

func TestBuildTTSProviderBuildsConfiguredBaiduClient(t *testing.T) {
	clearTTSEnvironment(t)
	t.Setenv("BAIDU_TTS_API_KEY", "key")
	t.Setenv("BAIDU_TTS_SECRET_KEY", "secret")
	t.Setenv("BAIDU_TTS_BASE_URL", "https://tts.example.com")
	t.Setenv("BAIDU_TTS_TIMEOUT_SECONDS", "7")

	provider, err := buildTTSProvider()
	if err != nil {
		t.Fatalf("buildTTSProvider() error = %v", err)
	}
	if _, ok := provider.(*baiduttsplatform.Client); !ok {
		t.Fatalf("buildTTSProvider() provider = %T, want *baidutts.Client", provider)
	}
}

func TestBuildTTSProviderRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		baseURL string
		wantErr error
	}{
		{name: "invalid timeout", timeout: "invalid"},
		{name: "invalid base URL", baseURL: "://bad", wantErr: baiduttsplatform.ErrInvalidBaseURL},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearTTSEnvironment(t)
			t.Setenv("BAIDU_TTS_API_KEY", "key")
			t.Setenv("BAIDU_TTS_SECRET_KEY", "secret")
			t.Setenv("BAIDU_TTS_TIMEOUT_SECONDS", test.timeout)
			t.Setenv("BAIDU_TTS_BASE_URL", test.baseURL)
			provider, err := buildTTSProvider()
			if err == nil {
				t.Fatalf("buildTTSProvider() provider = %T, error = nil", provider)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("buildTTSProvider() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestBuildTTSFeatureUsesDefaultAndConfiguredRateLimits(t *testing.T) {
	tests := []struct {
		name   string
		limit  string
		window string
	}{
		{name: "defaults"},
		{name: "configured", limit: "7", window: "120"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearTTSEnvironment(t)
			t.Setenv("TTS_RATE_LIMIT", test.limit)
			t.Setenv("TTS_RATE_WINDOW_SECONDS", test.window)
			redisClient := redisclient.NewClient(&redisclient.Options{Addr: "127.0.0.1:6379"})
			t.Cleanup(func() { _ = redisClient.Close() })

			feature, err := buildTTSFeature(redisClient)
			if err != nil {
				t.Fatalf("buildTTSFeature() error = %v", err)
			}
			if feature == nil || feature.handler == nil || feature.limiter == nil {
				t.Fatalf("buildTTSFeature() feature = %#v", feature)
			}
		})
	}
}

func TestBuildTTSFeatureRejectsInvalidRateLimit(t *testing.T) {
	clearTTSEnvironment(t)
	t.Setenv("TTS_RATE_LIMIT", "0")
	redisClient := redisclient.NewClient(&redisclient.Options{Addr: "127.0.0.1:6379"})
	t.Cleanup(func() { _ = redisClient.Close() })

	if _, err := buildTTSFeature(redisClient); err == nil || !strings.Contains(err.Error(), "TTS_RATE_LIMIT") {
		t.Fatalf("buildTTSFeature() error = %v", err)
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

func clearTTSEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range ttsEnvironmentNames {
		t.Setenv(name, "")
	}
}

type bootstrapWeatherClient struct{}

func (bootstrapWeatherClient) Get(context.Context, string) (*weather.Result, error) {
	return &weather.Result{Location: "test"}, nil
}
