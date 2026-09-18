package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	redisclient "github.com/redis/go-redis/v9"

	"gopherai/internal/agent"
	"gopherai/internal/chat"
	"gopherai/internal/chatjob"
	"gopherai/internal/emailverification"
	"gopherai/internal/httpapi"
	"gopherai/internal/image"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
	baiduttsplatform "gopherai/internal/platform/baidutts"
	filesystem "gopherai/internal/platform/filesystem"
	mcpplatform "gopherai/internal/platform/mcp"
	onnxplatform "gopherai/internal/platform/onnx"
	openaiplatform "gopherai/internal/platform/openai"
	platform "gopherai/internal/platform/postgresql"
	rabbitmqplatform "gopherai/internal/platform/rabbitmq"
	redisplatform "gopherai/internal/platform/redis"
	smtpplatform "gopherai/internal/platform/smtp"
	"gopherai/internal/rag"
	"gopherai/internal/tts"
)

const (
	dependencyPingTimeout                  = 5 * time.Second
	rabbitMQConnectAttempts                = 15
	rabbitMQConnectRetryInterval           = time.Second
	defaultMCPCallTimeout                  = 10 * time.Second
	defaultBaiduTTSTimeout                 = 10 * time.Second
	defaultChatRateLimit             int64 = 10
	defaultChatRateWindow                  = time.Minute
	defaultRAGUploadRateLimit        int64 = 3
	defaultRAGUploadRateWindow             = time.Hour
	defaultTTSRateLimit              int64 = 5
	defaultTTSRateWindow                   = time.Hour
	defaultEmailVerificationCodeTTL        = 10 * time.Minute
	defaultEmailVerificationResend         = time.Minute
	defaultEmailVerificationAttempts       = 5
	defaultSMTPHost                        = "smtp.qq.com"
	defaultSMTPPort                        = 587
	defaultSMTPFromName                    = "GopherAI"
	defaultSMTPTimeout                     = 10 * time.Second
	defaultAuthRegisterRateLimit     int64 = 10
	defaultAuthRegisterRateWindow          = 10 * time.Minute
	defaultAuthLoginRateLimit        int64 = 20
	defaultAuthLoginRateWindow             = 5 * time.Minute
	defaultEmailVerificationIPLimit  int64 = 10
	defaultEmailVerificationIPWindow       = time.Hour
	mcpConnectAttempts                     = 15
	mcpConnectRetryInterval                = time.Second
)

type rabbitMQConnector func(string) (*rabbitmqplatform.RabbitMQClient, error)
type mcpConnector func(context.Context, mcpplatform.Config) (*mcpplatform.Client, error)

type imageFeature struct {
	handler    *httpapi.ImageHandler
	classifier *onnxplatform.Classifier
}

type modelFeature struct {
	client llm.ModelClient
	close  func() error
}

func buildModelFeature(ctx context.Context) (*modelFeature, error) {
	base, err := buildLLMClient()
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimSpace(os.Getenv("MCP_SERVER_URL"))
	if endpoint == "" {
		return &modelFeature{client: base}, nil
	}

	toolModel, ok := base.(agent.Model)
	if !ok {
		return nil, errors.New("configured model does not support tool calling")
	}
	timeout, err := positiveDurationEnvOrDefault("MCP_CALL_TIMEOUT_SECONDS", defaultMCPCallTimeout)
	if err != nil {
		return nil, err
	}
	tools, err := connectMCPWithRetry(ctx, mcpplatform.Config{
		Endpoint: endpoint,
		Timeout:  timeout,
	}, mcpplatform.NewClient, mcpConnectAttempts, mcpConnectRetryInterval)
	if err != nil {
		return nil, fmt.Errorf("new MCP client: %w", err)
	}
	agentClient, err := agent.NewClient(toolModel, tools)
	if err != nil {
		_ = tools.Close()
		return nil, fmt.Errorf("new agent client: %w", err)
	}
	return &modelFeature{client: agentClient, close: tools.Close}, nil
}

func connectMCPWithRetry(
	ctx context.Context,
	config mcpplatform.Config,
	connect mcpConnector,
	maxAttempts int,
	retryInterval time.Duration,
) (*mcpplatform.Client, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client, err := connect(ctx, config)
		if err == nil {
			return client, nil
		}
		lastErr = err
		if (!errors.Is(err, mcpplatform.ErrConnectFailed) &&
			!errors.Is(err, mcpplatform.ErrListToolsFailed)) || attempt == maxAttempts {
			break
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("wait to retry MCP connection: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("connect MCP after %d attempts: %w", maxAttempts, lastErr)
}

func (f *modelFeature) Close() error {
	if f == nil || f.close == nil {
		return nil
	}
	return f.close()
}

func buildImageFeature() (*imageFeature, error) {
	classifier, err := onnxplatform.NewClassifier(onnxplatform.Config{
		SharedLibraryPath: os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH"),
		ModelPath:         os.Getenv("IMAGE_MODEL_PATH"),
		LabelsPath:        os.Getenv("IMAGE_LABELS_PATH"),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize image classifier: %w", err)
	}
	service := image.NewService(classifier)
	return &imageFeature{
		handler:    httpapi.NewImageHandler(service),
		classifier: classifier,
	}, nil
}

func (f *imageFeature) Close() error {
	return f.classifier.Close()
}

type ragFeature struct {
	service       *rag.Service
	handler       *httpapi.RAGHandler
	uploadLimiter *redisplatform.RateLimiter
}

type ttsFeature struct {
	handler *httpapi.TTSHandler
	limiter *redisplatform.RateLimiter
}

type emailVerificationFeature struct {
	enabled bool
	service *emailverification.Service
	handler *httpapi.EmailVerificationHandler
}

type authRateLimiters struct {
	register          *redisplatform.RateLimiter
	login             *redisplatform.RateLimiter
	emailVerification *redisplatform.RateLimiter
}

type chatFeature struct {
	messageHandler *httpapi.MessageHandler
	chatHandler    *httpapi.ChatHandler
	jobHandler     *httpapi.ChatJobHandler

	jobService *chatjob.Service
	rabbitMQ   *rabbitmqplatform.RabbitMQClient
}

func buildChatFeature(
	pool *pgxpool.Pool,
	model llm.ModelClient,
	retriever chat.Retriever,
	maxAttempts int64,
	rabbitMQURL string,
) (*chatFeature, error) {
	rabbitMQ, err := connectRabbitMQWithRetry(
		context.Background(),
		rabbitMQURL,
		rabbitmqplatform.NewRabbitMQClient,
		rabbitMQConnectAttempts,
		rabbitMQConnectRetryInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("new RabbitMQ client: %w", err)
	}

	messageService := message.NewService(platform.NewMessageRepository(pool))
	modelCallService := modelcall.NewService(platform.NewModelRepository(pool))
	unitOfWork := platform.NewChatUnitOfWork(platform.NewTxManager(pool))
	chatService := chat.NewService(messageService, model, modelCallService, unitOfWork, retriever)
	jobService := chatjob.NewService(
		platform.NewChatJobsRepository(pool),
		rabbitMQ,
		chatService,
		maxAttempts,
	)

	return &chatFeature{
		messageHandler: httpapi.NewMessageHandler(messageService),
		chatHandler:    httpapi.NewChatHandler(chatService),
		jobHandler:     httpapi.NewChatJobHandler(jobService),
		jobService:     jobService,
		rabbitMQ:       rabbitMQ,
	}, nil
}

func connectRabbitMQWithRetry(
	ctx context.Context,
	url string,
	connect rabbitMQConnector,
	maxAttempts int,
	retryInterval time.Duration,
) (*rabbitmqplatform.RabbitMQClient, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client, err := connect(url)
		if err == nil {
			return client, nil
		}
		lastErr = err
		if errors.Is(err, rabbitmqplatform.ErrURLInvalid) || attempt == maxAttempts {
			break
		}

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			// ctx 已取消本轮等待，停止未使用的 Timer，避免它之后继续触发。
			timer.Stop()
			return nil, fmt.Errorf("wait to retry RabbitMQ connection: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return nil, fmt.Errorf("connect RabbitMQ after %d attempts: %w", maxAttempts, lastErr)
}

func (f *chatFeature) StartConsumer(ctx context.Context) <-chan error {
	errorsChannel := make(chan error, 1)
	go func() {
		log.Printf("rabbitmq consumer started")
		errorsChannel <- f.rabbitMQ.ConsumeChatJobs(ctx, func(ctx context.Context, jobID uint64) error {
			jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			return f.jobService.Process(jobCtx, jobID)
		})
	}()
	return errorsChannel
}

func (f *chatFeature) Close() error {
	return f.rabbitMQ.Close()
}

func buildRAGFeature(client *redisclient.Client) (*ragFeature, error) {
	documentStore, err := filesystem.NewRagStore(os.Getenv("RAG_STORAGE_ROOT"))
	if err != nil {
		return nil, fmt.Errorf("new RAG document store: %w", err)
	}
	chunkRepository, err := redisplatform.NewRAGChunkRepository(client)
	if err != nil {
		return nil, fmt.Errorf("new RAG chunk repository: %w", err)
	}
	embedder, err := buildEmbedder()
	if err != nil {
		return nil, err
	}
	service, err := rag.NewService(documentStore, chunkRepository, embedder)
	if err != nil {
		return nil, fmt.Errorf("new RAG service: %w", err)
	}
	uploadLimiter, err := buildRateLimiter(
		client,
		"RAG_UPLOAD_RATE_LIMIT",
		"RAG_UPLOAD_RATE_WINDOW_SECONDS",
		defaultRAGUploadRateLimit,
		defaultRAGUploadRateWindow,
		redisplatform.NewRAGUploadRateLimiter,
	)
	if err != nil {
		return nil, err
	}
	return &ragFeature{
		service:       service,
		handler:       httpapi.NewRAGHandler(service),
		uploadLimiter: uploadLimiter,
	}, nil
}

func buildTTSFeature(client *redisclient.Client) (*ttsFeature, error) {
	provider, err := buildTTSProvider()
	if err != nil {
		return nil, err
	}
	limiter, err := buildRateLimiter(
		client,
		"TTS_RATE_LIMIT",
		"TTS_RATE_WINDOW_SECONDS",
		defaultTTSRateLimit,
		defaultTTSRateWindow,
		redisplatform.NewTTSRateLimiter,
	)
	if err != nil {
		return nil, err
	}
	service := tts.NewService(provider)
	return &ttsFeature{
		handler: httpapi.NewTTSHandler(service),
		limiter: limiter,
	}, nil
}

func buildTTSProvider() (tts.Provider, error) {
	apiKey := strings.TrimSpace(os.Getenv("BAIDU_TTS_API_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("BAIDU_TTS_SECRET_KEY"))
	if apiKey == "" && secretKey == "" {
		return nil, nil
	}
	if apiKey == "" {
		return nil, errors.New("BAIDU_TTS_API_KEY is required when TTS is configured")
	}
	if secretKey == "" {
		return nil, errors.New("BAIDU_TTS_SECRET_KEY is required when TTS is configured")
	}
	timeout, err := positiveDurationEnvOrDefault("BAIDU_TTS_TIMEOUT_SECONDS", defaultBaiduTTSTimeout)
	if err != nil {
		return nil, err
	}
	client, err := baiduttsplatform.NewClient(baiduttsplatform.Config{
		APIKey:    apiKey,
		SecretKey: secretKey,
		BaseURL:   strings.TrimSpace(os.Getenv("BAIDU_TTS_BASE_URL")),
		Timeout:   timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("new Baidu TTS client: %w", err)
	}
	return client, nil
}

func buildEmailVerificationFeature(client *redisclient.Client) (*emailVerificationFeature, error) {
	enabled, err := envBool("EMAIL_VERIFICATION_ENABLED", false)
	if err != nil {
		return nil, err
	}
	if !enabled {
		service, err := emailverification.NewService(nil, nil, emailverification.Config{})
		if err != nil {
			return nil, fmt.Errorf("new disabled email verification service: %w", err)
		}
		return &emailVerificationFeature{
			service: service,
			handler: httpapi.NewEmailVerificationHandler(service),
		}, nil
	}

	codeTTL, err := positiveDurationEnvOrDefault(
		"EMAIL_VERIFICATION_CODE_TTL_SECONDS",
		defaultEmailVerificationCodeTTL,
	)
	if err != nil {
		return nil, err
	}
	resendInterval, err := positiveDurationEnvOrDefault(
		"EMAIL_VERIFICATION_RESEND_INTERVAL_SECONDS",
		defaultEmailVerificationResend,
	)
	if err != nil {
		return nil, err
	}
	maxAttempts, err := positiveEnvIntOrDefault(
		"EMAIL_VERIFICATION_MAX_ATTEMPTS",
		defaultEmailVerificationAttempts,
	)
	if err != nil {
		return nil, err
	}
	smtpPort, err := positiveEnvIntOrDefault("SMTP_PORT", defaultSMTPPort)
	if err != nil {
		return nil, err
	}
	smtpTimeout, err := positiveDurationEnvOrDefault("SMTP_TIMEOUT_SECONDS", defaultSMTPTimeout)
	if err != nil {
		return nil, err
	}

	store, err := redisplatform.NewEmailVerificationCodeStore(client)
	if err != nil {
		return nil, fmt.Errorf("new email verification code store: %w", err)
	}
	sender, err := smtpplatform.NewSender(smtpplatform.Config{
		Host:     envOrDefault("SMTP_HOST", defaultSMTPHost),
		Port:     smtpPort,
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
		FromName: envOrDefault("SMTP_FROM_NAME", defaultSMTPFromName),
		Timeout:  smtpTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("new SMTP sender: %w", err)
	}
	service, err := emailverification.NewService(store, sender, emailverification.Config{
		CodeTTL:        codeTTL,
		ResendInterval: resendInterval,
		MaxAttempts:    maxAttempts,
	})
	if err != nil {
		return nil, fmt.Errorf("new email verification service: %w", err)
	}
	return &emailVerificationFeature{
		enabled: true,
		service: service,
		handler: httpapi.NewEmailVerificationHandler(service),
	}, nil
}

type rateLimiterFactory func(*redisclient.Client, int64, time.Duration) (*redisplatform.RateLimiter, error)

func buildAuthRateLimiters(client *redisclient.Client) (*authRateLimiters, error) {
	register, err := buildRateLimiter(
		client,
		"AUTH_REGISTER_RATE_LIMIT",
		"AUTH_REGISTER_RATE_WINDOW_SECONDS",
		defaultAuthRegisterRateLimit,
		defaultAuthRegisterRateWindow,
		redisplatform.NewAuthRegisterRateLimiter,
	)
	if err != nil {
		return nil, err
	}
	login, err := buildRateLimiter(
		client,
		"AUTH_LOGIN_RATE_LIMIT",
		"AUTH_LOGIN_RATE_WINDOW_SECONDS",
		defaultAuthLoginRateLimit,
		defaultAuthLoginRateWindow,
		redisplatform.NewAuthLoginRateLimiter,
	)
	if err != nil {
		return nil, err
	}
	emailVerification, err := buildRateLimiter(
		client,
		"EMAIL_VERIFICATION_IP_RATE_LIMIT",
		"EMAIL_VERIFICATION_IP_RATE_WINDOW_SECONDS",
		defaultEmailVerificationIPLimit,
		defaultEmailVerificationIPWindow,
		redisplatform.NewEmailVerificationIPRateLimiter,
	)
	if err != nil {
		return nil, err
	}
	return &authRateLimiters{
		register:          register,
		login:             login,
		emailVerification: emailVerification,
	}, nil
}

func buildRateLimiter(
	client *redisclient.Client,
	limitEnv, windowEnv string,
	defaultLimit int64,
	defaultWindow time.Duration,
	factory rateLimiterFactory,
) (*redisplatform.RateLimiter, error) {
	limit, err := positiveEnvInt64OrDefault(limitEnv, defaultLimit)
	if err != nil {
		return nil, err
	}
	window, err := positiveDurationEnvOrDefault(windowEnv, defaultWindow)
	if err != nil {
		return nil, err
	}
	limiter, err := factory(client, limit, window)
	if err != nil {
		return nil, fmt.Errorf("new rate limiter for %s: %w", limitEnv, err)
	}
	return limiter, nil
}

func positiveEnvInt64(name string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func positiveEnvInt64OrDefault(name string, fallback int64) (int64, error) {
	if strings.TrimSpace(os.Getenv(name)) == "" {
		return fallback, nil
	}
	return positiveEnvInt64(name)
}

func positiveEnvIntOrDefault(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func positiveDurationEnvOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return time.Duration(seconds) * time.Second, nil
}

func connectPostgreSQL(databaseURL string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is empty")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, fmt.Errorf("new PostgreSQL pool: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dependencyPingTimeout)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return pool, nil
}

func connectRedis(redisURL string) (*redisclient.Client, error) {
	options, err := redisclient.ParseURL(strings.TrimSpace(redisURL))
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redisclient.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), dependencyPingTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return client, nil
}

func buildLLMClient() (llm.ModelClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	wireAPI := envOrDefault("OPENAI_WIRE_API", "responses")
	requiresAuth, err := envBool("OPENAI_REQUIRES_AUTH", true)
	if err != nil {
		return nil, err
	}
	disableStorage, err := envBool("OPENAI_DISABLE_RESPONSE_STORAGE", true)
	if err != nil {
		return nil, err
	}
	reasoningEffort := envOrDefault("OPENAI_REASONING_EFFORT", "low")
	if apiKey == "" && model == "" && baseURL == "" {
		return llm.UnavailableClient{}, nil
	}

	client, err := openaiplatform.NewClientWithConfig(openaiplatform.Config{
		APIKey:                 apiKey,
		Model:                  model,
		BaseURL:                baseURL,
		WireAPI:                wireAPI,
		RequiresAuth:           requiresAuth,
		ReasoningEffort:        reasoningEffort,
		DisableResponseStorage: disableStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("new OpenAI client: %w", err)
	}
	return client, nil
}

func buildEmbedder() (rag.Embedder, error) {
	config, err := embeddingConfigFromEnvironment()
	if err != nil {
		return nil, err
	}
	embedder, err := openaiplatform.NewEmbedderWithConfig(config)
	if err != nil {
		return nil, fmt.Errorf("new OpenAI embedder: %w", err)
	}
	return embedder, nil
}

func embeddingConfigFromEnvironment() (openaiplatform.EmbeddingConfig, error) {
	defaultRequiresAuth, err := envBool("OPENAI_REQUIRES_AUTH", true)
	if err != nil {
		return openaiplatform.EmbeddingConfig{}, err
	}
	requiresAuth, err := envBool("OPENAI_EMBEDDING_REQUIRES_AUTH", defaultRequiresAuth)
	if err != nil {
		return openaiplatform.EmbeddingConfig{}, err
	}

	return openaiplatform.EmbeddingConfig{
		APIKey:       envOrDefault("OPENAI_EMBEDDING_API_KEY", os.Getenv("OPENAI_API_KEY")),
		Model:        strings.TrimSpace(os.Getenv("OPENAI_EMBEDDING_MODEL")),
		BaseURL:      envOrDefault("OPENAI_EMBEDDING_BASE_URL", os.Getenv("OPENAI_BASE_URL")),
		RequiresAuth: requiresAuth,
	}, nil
}

func loadEnvironment() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func httpAddress() string {
	return envOrDefault("HTTP_ADDR", defaultHTTPAddr)
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}
