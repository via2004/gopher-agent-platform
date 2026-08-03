package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopherai/internal/httpapi"
	platform "gopherai/internal/platform/postgresql"
	"gopherai/internal/user"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	IPAddr = "127.0.0.1"
	Port   = ":8080"
)

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("databaseURL is empty!")
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

	router := httpapi.NewRouter(httpapi.NewUserHandler(
		user.NewService(platform.NewUserRepository(pool))))

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

func main() {
	if err := run(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
