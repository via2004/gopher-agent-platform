package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type fakeChatRateLimiter struct {
	allowed    bool
	retryAfter time.Duration
	err        error
	calls      int
	userID     uint64
	ctx        context.Context
}

func (f *fakeChatRateLimiter) Allow(ctx context.Context, userID uint64) (bool, time.Duration, error) {
	f.calls++
	f.userID = userID
	f.ctx = ctx
	return f.allowed, f.retryAfter, f.err
}

func TestChatRateLimitMiddlewareAllowsRequest(t *testing.T) {
	limiter := &fakeChatRateLimiter{allowed: true}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	req = req.WithContext(context.WithValue(req.Context(), testContextKey{}, "request-value"))
	routerWithUserID := gin.New()
	routerWithUserID.Use(func(c *gin.Context) {
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	routerWithUserID.GET("/chat", ChatRateLimitMiddleware(limiter), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	routerWithUserID.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if limiter.calls != 1 || limiter.userID != 42 {
		t.Fatalf("Allow() = %d calls for user %d, want 1 call for user 42", limiter.calls, limiter.userID)
	}
	if got := limiter.ctx.Value(testContextKey{}); got != "request-value" {
		t.Fatalf("context value = %v, want request-value", got)
	}
}

func TestChatRateLimitMiddlewareRejectsUnauthenticatedRequest(t *testing.T) {
	limiter := &fakeChatRateLimiter{allowed: true}
	router := gin.New()
	router.GET("/chat", ChatRateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chat", nil))

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if limiter.calls != 0 {
		t.Fatalf("Allow() calls = %d, want 0", limiter.calls)
	}
}

func TestChatRateLimitMiddlewareRejectsOverLimit(t *testing.T) {
	limiter := &fakeChatRateLimiter{retryAfter: 1500 * time.Millisecond}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	router.GET("/chat", ChatRateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chat", nil))

	assertErrorResponse(t, recorder, http.StatusTooManyRequests, "TOO_MANY_REQUESTS")
	if got := recorder.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
}

func TestChatRateLimitMiddlewareReturnsServiceUnavailableOnLimiterError(t *testing.T) {
	limiter := &fakeChatRateLimiter{allowed: true, err: errors.New("redis unavailable")}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	router.GET("/chat", ChatRateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chat", nil))

	assertErrorResponse(t, recorder, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
}

type testContextKey struct{}
