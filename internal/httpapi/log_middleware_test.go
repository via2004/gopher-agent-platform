package httpapi

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLogMiddlewareLogsAuthenticatedRequest(t *testing.T) {
	output := captureStandardLog(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(requestIDContextKey, "request-123")
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	router.GET("/test", LogMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))

	entry := output.String()
	for _, want := range []string{
		"Path: /test",
		"Method: GET",
		"RequestID: request-123",
		"Status: 201",
		"UserID: 42",
		"SpendTime:",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("log entry = %q, want substring %q", entry, want)
		}
	}
}

func TestLogMiddlewareLogsRequestWithoutAuthenticatedUser(t *testing.T) {
	output := captureStandardLog(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(requestIDContextKey, "request-123")
		c.Next()
	})
	router.GET("/test", LogMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))

	entry := output.String()
	if !strings.Contains(entry, "UserID: didn't set") {
		t.Fatalf("log entry = %q, want missing-user marker", entry)
	}
	if strings.Contains(entry, "UserID: 0") {
		t.Fatalf("log entry = %q, must not identify unauthenticated request as user 0", entry)
	}
}

func TestLogMiddlewareLogsRecoveredPanicWithRequestID(t *testing.T) {
	output := captureStandardLog(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestIDMiddleware())
	router.Use(LogMiddleware())
	router.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))

	requestID := recorder.Header().Get(requestIDHeader)
	if requestID == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
	entry := output.String()
	for _, want := range []string{
		"Path: /panic",
		"RequestID: " + requestID,
		"Status: 500",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("log entry = %q, want substring %q", entry, want)
		}
	}
}

func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})
	return &output
}
