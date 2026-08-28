package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopherai/internal/auth"
	"gopherai/internal/conversation"
	"gopherai/internal/health"
	"gopherai/internal/httpapi"
	platform "gopherai/internal/platform/postgresql"
	redisplatform "gopherai/internal/platform/redis"
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

	maxAttemptCount, err := positiveEnvInt64("CHAT_JOB_MAX_ATTEMPTS")
	if err != nil {
		return err
	}

	imageFeature, err := buildImageFeature()
	if err != nil {
		return err
	}
	defer func() {
		if err := imageFeature.Close(); err != nil {
			log.Printf("close image classifier: %v", err)
		}
	}()

	pool, err := connectPostgreSQL(os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	userService := user.NewService(platform.NewUserRepository(pool))
	userHandler := httpapi.NewUserHandler(userService, tokenManager)

	conversationService := conversation.NewService(platform.NewConversationRepository(pool))
	conversationHandler := httpapi.NewConversationHandler(conversationService)

	modelClient, err := buildLLMClient()
	if err != nil {
		return err
	}

	redisClient, err := connectRedis(os.Getenv("REDIS_URL"))
	if err != nil {
		return err
	}
	defer redisClient.Close()

	ragFeature, err := buildRAGFeature(redisClient)
	if err != nil {
		return err
	}
	chatRateLimiter, err := buildRateLimiter(
		redisClient,
		"CHAT_RATE_LIMIT",
		"CHAT_RATE_WINDOW_SECONDS",
		redisplatform.NewRateLimiter,
	)
	if err != nil {
		return err
	}

	chatFeature, err := buildChatFeature(
		pool,
		modelClient,
		ragFeature.service,
		maxAttemptCount,
		rabbitmqURL,
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := chatFeature.Close(); err != nil {
			log.Printf("close chat feature: %v", err)
		}
	}()

	rabbitMQContext, rabbitMQCancel := context.WithCancel(context.Background())
	defer rabbitMQCancel()
	rabbitMQErrors := chatFeature.StartConsumer(rabbitMQContext)

	readinessChecker := health.NewChecker(pool.Ping, func(ctx context.Context) error {
		return redisClient.Ping(ctx).Err()
	})

	checker := httpapi.NewHealthHandler(readinessChecker)

	router := httpapi.NewRouter(
		httpapi.RouterHandlers{
			Users:         userHandler,
			Conversations: conversationHandler,
			Messages:      chatFeature.messageHandler,
			Chat:          chatFeature.chatHandler,
			Health:        checker,
			ChatJobs:      chatFeature.jobHandler,
			Images:        imageFeature.handler,
			RAG:           ragFeature.handler,
		},
		httpapi.RouterMiddleware{
			Tokens:           tokenManager,
			ChatLimiter:      chatRateLimiter,
			RAGUploadLimiter: ragFeature.uploadLimiter,
		},
	)

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
	case err := <-rabbitMQErrors:
		if err != nil {
			return fmt.Errorf("rabbitmq error: %w", err)
		}
	case <-shutdown.Done():
		log.Printf("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rabbitMQCancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown server error: %w", err)
		}

		if err := <-serverErrors; err != nil && err != http.ErrServerClosed {
			return err
		}
		select {
		case err := <-rabbitMQErrors:
			if err != nil {
				return err
			}
		case <-ctx.Done():
			return fmt.Errorf("rabbitmq shutdown timeout: %w", ctx.Err())
		}

	}

	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
