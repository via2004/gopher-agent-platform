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

	"gopherai/internal/auth"
	"gopherai/internal/chat"
	"gopherai/internal/conversation"
	"gopherai/internal/httpapi"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	openaiplatform "gopherai/internal/platform/openai"
	platform "gopherai/internal/platform/postgresql"
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
		return errors.New("databaseURL is empty!")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return errors.New("JWT_SECRET is empty")
	}
	tokenManager, err := auth.NewTokenManager(jwtSecret, jwtIssuer, jwtTTL)
	if err != nil {
		return fmt.Errorf("new token manager: %w", err)
	}

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

	chatService := chat.NewService(messageService, modelClient, modelClient)
	chatHandler := httpapi.NewChatHandler(chatService)

	router := httpapi.NewRouter(userHandler, conversationHandler, messageHandler, chatHandler, tokenManager)

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
	case <-shutdown.Done():
		log.Printf("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown server error: %w", err)
		}

		if err := <-serverErrors; err != nil && err != http.ErrServerClosed {
			return err
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
