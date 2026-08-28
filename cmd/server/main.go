package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"gopherai/internal/auth"
	"gopherai/internal/chat"
	"gopherai/internal/chatjob"
	"gopherai/internal/conversation"
	"gopherai/internal/health"
	"gopherai/internal/httpapi"
	"gopherai/internal/image"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	"gopherai/internal/modelcall"
	filesystem "gopherai/internal/platform/filesystem"
	onnx "gopherai/internal/platform/onnx"
	openaiplatform "gopherai/internal/platform/openai"
	platform "gopherai/internal/platform/postgresql"
	rabbitmq "gopherai/internal/platform/rabbitmq"
	redis_ "gopherai/internal/platform/redis"
	"gopherai/internal/rag"
	"gopherai/internal/user"
)

const (
	IPAddr    = "127.0.0.1"
	Port      = ":8080"
	jwtIssuer = "gopher-agent-platform"
	jwtTTL    = time.Hour
)

func run() error {
	if err := loadEnvironment(); err != nil {
		return err
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is empty!")
	}

	rabbitmqURL := os.Getenv("RABBITMQ_URL")
	if rabbitmqURL == "" {
		return errors.New("RABBITMQ_URL is empty!")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return errors.New("JWT_SECRET is empty")
	}
	tokenManager, err := auth.NewTokenManager(jwtSecret, jwtIssuer, jwtTTL)
	if err != nil {
		return fmt.Errorf("new token manager: %w", err)
	}

	maxAttemptCountString := os.Getenv("CHAT_JOB_MAX_ATTEMPTS")
	maxAttemptCount, err := strconv.ParseInt(maxAttemptCountString, 10, 64)
	if err != nil {
		return fmt.Errorf("parse CHAT_JOB_MAX_ATTEMPTS: %w", err)
	}
	if maxAttemptCount <= 0 {
		return fmt.Errorf("CHAT_JOB_MAX_ATTEMPTS must be positive")
	}

	// 接ONNX,图像分类模型
	classifier, err := onnx.NewClassifier(onnx.Config{
		SharedLibraryPath: os.Getenv(
			"ONNXRUNTIME_SHARED_LIBRARY_PATH",
		),
		ModelPath:  os.Getenv("IMAGE_MODEL_PATH"),
		LabelsPath: os.Getenv("IMAGE_LABELS_PATH"),
	})
	if err != nil {
		return fmt.Errorf("initialize image classifier: %w", err)
	}
	defer func() {
		if err := classifier.Close(); err != nil {
			log.Printf("close image classifier: %v", err)
		}
	}()

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return fmt.Errorf("new pgxpool error: %w", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := pool.Ping(ctx); err != nil {
		cancel()
		return fmt.Errorf("pgxpool ping error: %w", err)
	}
	cancel()

	userService := user.NewService(platform.NewUserRepository(pool))
	userHandler := httpapi.NewUserHandler(userService, tokenManager)

	conversationService := conversation.NewService(platform.NewConversationRepository(pool))
	conversationHandler := httpapi.NewConversationHandler(conversationService)

	messageService := message.NewService(platform.NewMessageRepository(pool))
	messageHandler := httpapi.NewMessageHandler(messageService)

	modelClient, err := buildLLMClient()
	if err != nil {
		return err
	}
	txManager := platform.NewTxManager(pool)
	unitOfWork := platform.NewChatUnitOfWork(txManager)

	modelRepository := platform.NewModelRepository(pool)
	modelCall := modelcall.NewService(modelRepository)

	chatJobRepository := platform.NewChatJobsRepository(pool)

	imageService := image.NewService(classifier)
	imageHandler := httpapi.NewImageHandler(imageService)

	limitString := os.Getenv("CHAT_RATE_LIMIT")
	limit, err := strconv.ParseInt(limitString, 10, 64)
	if err != nil {
		return err
	}
	if limit <= 0 {
		return errors.New("CHAT_RATE_LIMIT must be positive")
	}

	windowString := os.Getenv("CHAT_RATE_WINDOW_SECONDS")
	window, err := strconv.ParseInt(windowString, 10, 64)
	if err != nil {
		return err
	}
	if window <= 0 {
		return errors.New("CHAT_RATE_WINDOW_SECONDS must be positive")
	}

	ragUploadLimitString := os.Getenv("RAG_UPLOAD_RATE_LIMIT")
	ragUploadLimit, err := strconv.ParseInt(ragUploadLimitString, 10, 64)
	if err != nil {
		return fmt.Errorf("parse RAG_UPLOAD_RATE_LIMIT: %w", err)
	}
	if ragUploadLimit <= 0 {
		return errors.New("RAG_UPLOAD_RATE_LIMIT must be positive")
	}

	ragUploadWindowString := os.Getenv("RAG_UPLOAD_RATE_WINDOW_SECONDS")
	ragUploadWindow, err := strconv.ParseInt(ragUploadWindowString, 10, 64)
	if err != nil {
		return fmt.Errorf("parse RAG_UPLOAD_RATE_WINDOW_SECONDS: %w", err)
	}
	if ragUploadWindow <= 0 {
		return errors.New("RAG_UPLOAD_RATE_WINDOW_SECONDS must be positive")
	}

	redisURLString := os.Getenv("REDIS_URL")
	options, err := redis.ParseURL(redisURLString)
	if err != nil {
		return fmt.Errorf("parse REDIS_URL: %w", err)
	}

	client := redis.NewClient(options)
	defer client.Close()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if err := client.Ping(ctx).Err(); err != nil {
		cancel()
		return fmt.Errorf("ping redis error: %w", err)
	}
	cancel()

	documentStore, err := filesystem.NewRagStore(
		os.Getenv("RAG_STORAGE_ROOT"),
	)
	if err != nil {
		return err
	}

	chunkRepository, err := redis_.NewRAGChunkRepository(client)
	if err != nil {
		return err
	}

	embedder, err := buildEmbedder()
	if err != nil {
		return err
	}

	ragService, err := rag.NewService(
		documentStore,
		chunkRepository,
		embedder,
	)
	if err != nil {
		return err
	}

	ragHandler := httpapi.NewRAGHandler(ragService)

	chatService := chat.NewService(messageService, modelClient, modelCall, unitOfWork, ragService)
	chatHandler := httpapi.NewChatHandler(chatService)

	rabbitMqClient, err := rabbitmq.NewRabbitMQClient(rabbitmqURL)
	if err != nil {
		return fmt.Errorf("new rabbitmq client: %w", err)
	}
	defer rabbitMqClient.Close()

	cancelRabbitMqCtx, RabbitMqCancel := context.WithCancel(context.Background())
	defer RabbitMqCancel()

	chatJobService := chatjob.NewService(chatJobRepository, rabbitMqClient, chatService, maxAttemptCount)
	chatJobHandler := httpapi.NewChatJobHandler(chatJobService)
	chanRabbitMqErr := make(chan error, 1)
	go func() {
		log.Printf("rabbitmq consumer started")
		processJob := func(ctx context.Context, jobID uint64) error {
			jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			return chatJobService.Process(jobCtx, jobID)
		}

		chanRabbitMqErr <- rabbitMqClient.ConsumeChatJobs(cancelRabbitMqCtx, processJob)
	}()

	chatRateLimiter, err := redis_.NewRateLimiter(client, limit, time.Duration(window)*time.Second)
	if err != nil {
		return err
	}
	ragUploadRateLimiter, err := redis_.NewRAGUploadRateLimiter(
		client,
		ragUploadLimit,
		time.Duration(ragUploadWindow)*time.Second,
	)
	if err != nil {
		return err
	}

	readinessChecker := health.NewChecker(pool.Ping, func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	})

	checker := httpapi.NewHealthHandler(readinessChecker)

	router := httpapi.NewRouter(userHandler, conversationHandler, messageHandler, chatHandler,
		checker, chatJobHandler, imageHandler, ragHandler, tokenManager,
		chatRateLimiter, ragUploadRateLimiter)

	server := &http.Server{
		Addr:           IPAddr + Port,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", IPAddr+Port)
		serverErrors <- server.ListenAndServe()
	}()

	shutdown, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
	case err := <-chanRabbitMqErr:
		if err != nil {
			return fmt.Errorf("rabbitmq error: %w", err)
		}
	case <-shutdown.Done():
		log.Printf("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		RabbitMqCancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown server error: %w", err)
		}

		if err := <-serverErrors; err != nil && err != http.ErrServerClosed {
			return err
		}
		select {
		case err := <-chanRabbitMqErr:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return fmt.Errorf("rabbitmq shutdown timeout: %w", ctx.Err())
		}

	}

	return nil
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
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_EMBEDDING_MODEL")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	requiresAuth, err := envBool("OPENAI_REQUIRES_AUTH", true)
	if err != nil {
		return nil, err
	}
	embedder, err := openaiplatform.NewEmbedderWithConfig(
		openaiplatform.EmbeddingConfig{
			APIKey:       apiKey,
			Model:        model,
			BaseURL:      baseURL,
			RequiresAuth: requiresAuth,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"new OpenAI embedder: %w", err,
		)
	}

	return embedder, nil
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

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
