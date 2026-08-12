package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRequestIDMiddlewareSetsUUIDv4InContextAndResponseHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var contextRequestID string
	router.GET("/test", RequestIDMiddleware(), func(c *gin.Context) {
		value, exists := c.Get(requestIDContextKey)
		if !exists {
			t.Fatal("request ID is missing from context")
		}
		var ok bool
		contextRequestID, ok = value.(string)
		if !ok {
			t.Fatalf("context request ID type = %T, want string", value)
		}
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	headerRequestID := recorder.Header().Get(requestIDHeader)
	if headerRequestID == "" {
		t.Fatal("X-Request-ID response header is empty")
	}
	if contextRequestID != headerRequestID {
		t.Fatalf("context request ID = %q, header request ID = %q", contextRequestID, headerRequestID)
	}
	parsed, err := uuid.Parse(headerRequestID)
	if err != nil {
		t.Fatalf("parse request ID %q: %v", headerRequestID, err)
	}
	if version := parsed.Version(); version != 4 {
		t.Fatalf("request ID UUID version = %d, want 4", version)
	}
}

func TestRequestIDMiddlewareGeneratesDifferentIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test", RequestIDMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	requestID := func() string {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))
		return recorder.Header().Get(requestIDHeader)
	}
	first := requestID()
	second := requestID()

	if first == "" || second == "" {
		t.Fatalf("request IDs = %q and %q, want non-empty", first, second)
	}
	if first == second {
		t.Fatalf("request IDs are both %q, want different IDs", first)
	}
}
