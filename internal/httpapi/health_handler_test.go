package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type fakeReadinessChecker struct {
	err       error
	calls     int
	deadline  time.Time
	checkedAt time.Time
	ctx       context.Context
}

func (f *fakeReadinessChecker) Check(ctx context.Context) error {
	f.calls++
	f.checkedAt = time.Now()
	f.ctx = ctx
	f.deadline, _ = ctx.Deadline()
	return f.err
}

func newHealthHandlerTestRouter(checker ReadinessChecker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(requestIDContextKey, "request-123")
		c.Next()
	})
	router.GET("/readyz", NewHealthHandler(checker).Ready)
	return router
}

func TestHealthHandlerReportsReady(t *testing.T) {
	checker := &fakeReadinessChecker{}
	router := newHealthHandlerTestRouter(checker)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["code"] != "OK" || response["message"] != "service ready" {
		t.Fatalf("response = %#v, want ready response", response)
	}
	if checker.calls != 1 {
		t.Fatalf("Check() calls = %d, want 1", checker.calls)
	}
	if checker.deadline.IsZero() {
		t.Fatal("readiness context has no deadline")
	}
	timeout := checker.deadline.Sub(checker.checkedAt)
	if timeout <= 0 || timeout > 2*time.Second {
		t.Fatalf("readiness timeout = %s, want (0, 2s]", timeout)
	}
}

func TestHealthHandlerReportsNotReadyAndLogsInternalError(t *testing.T) {
	output := captureStandardLog(t)
	checker := &fakeReadinessChecker{err: errors.New("postgresql: connection refused")}
	router := newHealthHandlerTestRouter(checker)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	assertErrorResponse(t, recorder, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	if strings.Contains(recorder.Body.String(), "postgresql") || strings.Contains(recorder.Body.String(), "connection refused") {
		t.Fatalf("response body leaks dependency error: %s", recorder.Body.String())
	}
	entry := output.String()
	for _, want := range []string{
		"readiness check failed",
		"request_id=request-123",
		"error=postgresql: connection refused",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("log entry = %q, want substring %q", entry, want)
		}
	}
}

func TestHealthHandlerPassesRequestContext(t *testing.T) {
	type contextKey struct{}
	checker := &fakeReadinessChecker{}
	router := newHealthHandlerTestRouter(checker)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKey{}, "request-value"))

	router.ServeHTTP(recorder, req)

	if got := checker.ctx.Value(contextKey{}); got != "request-value" {
		t.Fatalf("context value = %v, want request-value", got)
	}
}
