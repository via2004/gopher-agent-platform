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

	"github.com/joho/godotenv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpplatform "gopherai/internal/platform/mcp"
	"gopherai/internal/weather"
)

const (
	defaultHTTPAddr        = "127.0.0.1:8081"
	defaultWeatherTimeout  = 5 * time.Second
	shutdownTimeout        = 5 * time.Second
	maxMCPRequestBodyBytes = 1 << 20
)

// run 组装天气 Provider、MCP Server 和 HTTP 生命周期，直到服务停止。
func run() error {
	if err := loadEnvironment(); err != nil {
		return err
	}

	timeout, err := durationFromSeconds("WEATHER_API_TIMEOUT_SECONDS", defaultWeatherTimeout)
	if err != nil {
		return err
	}
	weatherClient, err := weather.NewClient(weather.Config{
		BaseURL: strings.TrimSpace(os.Getenv("WEATHER_API_BASE_URL")),
		Timeout: timeout,
	})
	if err != nil {
		return fmt.Errorf("new weather client: %w", err)
	}
	mcpServer, err := mcpplatform.NewWeatherServer(weatherClient)
	if err != nil {
		return fmt.Errorf("new weather MCP server: %w", err)
	}

	mcpHandler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return mcpServer },
		&mcpsdk.StreamableHTTPOptions{
			// 使用无会话模式，每次请求能够独立处理
			Stateless: true,
			// MCP Client取消请求后，取消信号会一直传播到weatherClient.Get(ctx, city)
			PropagateRequestCancellation: true,
			MaxRequestBodyBytes:          maxMCPRequestBodyBytes,
		},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"pong"}`))
	})

	server := &http.Server{
		Addr:    envOrDefault("MCP_HTTP_ADDR", defaultHTTPAddr),
		Handler: mux,
		// 读取请求头最多 5 秒
		ReadHeaderTimeout: 5 * time.Second,
		// 读取完整请求最多 10 秒
		ReadTimeout: 10 * time.Second,
		// 写出完整响应最多 15 秒
		WriteTimeout: 15 * time.Second,
		// 空闲连接最多保留 60 秒
		IdleTimeout: 60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("MCP weather server listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdown, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("MCP server error: %w", err)
		}
	case <-shutdown.Done():
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown MCP server: %w", err)
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("MCP server error after shutdown: %w", err)
		}
	}

	return nil
}

// loadEnvironment 加载本地 .env；Compose 环境没有该文件时也允许启动。
func loadEnvironment() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	return nil
}

// envOrDefault 读取并清理环境变量，空值时返回默认值。
func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// durationFromSeconds 将正整数秒数配置转换为 time.Duration。
func durationFromSeconds(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 {
		if err == nil {
			err = errors.New("must be positive")
		}
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return time.Duration(seconds) * time.Second, nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("MCP server stopped: %v", err)
		os.Exit(1)
	}
}
