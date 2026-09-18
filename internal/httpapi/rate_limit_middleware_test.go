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

type fakeRateLimiter struct {
	allowed    bool
	retryAfter time.Duration
	err        error
	calls      int
	identity   string
	ctx        context.Context
}

func (f *fakeRateLimiter) Allow(ctx context.Context, identity string) (bool, time.Duration, error) {
	f.calls++
	f.identity = identity
	f.ctx = ctx
	return f.allowed, f.retryAfter, f.err
}

func TestRateLimitMiddlewareAllowsRequest(t *testing.T) {
	limiter := &fakeRateLimiter{allowed: true}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/chat", nil)
	req = req.WithContext(context.WithValue(req.Context(), testContextKey{}, "request-value"))
	routerWithUserID := gin.New()
	routerWithUserID.Use(func(c *gin.Context) {
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	routerWithUserID.GET("/chat", RateLimitMiddleware(limiter), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	routerWithUserID.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if limiter.calls != 1 || limiter.identity != "42" {
		t.Fatalf("Allow() = %d calls with identity %q, want 1 call with identity 42", limiter.calls, limiter.identity)
	}
	if got := limiter.ctx.Value(testContextKey{}); got != "request-value" {
		t.Fatalf("context value = %v, want request-value", got)
	}
}

func TestRateLimitMiddlewareRejectsUnauthenticatedRequest(t *testing.T) {
	limiter := &fakeRateLimiter{allowed: true}
	router := gin.New()
	router.GET("/chat", RateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chat", nil))

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if limiter.calls != 0 {
		t.Fatalf("Allow() calls = %d, want 0", limiter.calls)
	}
}

func TestRateLimitMiddlewareRejectsOverLimit(t *testing.T) {
	limiter := &fakeRateLimiter{retryAfter: 1500 * time.Millisecond}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	router.GET("/chat", RateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chat", nil))

	assertErrorResponse(t, recorder, http.StatusTooManyRequests, "TOO_MANY_REQUESTS")
	if got := recorder.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
}

func TestRateLimitMiddlewareReturnsServiceUnavailableOnLimiterError(t *testing.T) {
	limiter := &fakeRateLimiter{allowed: true, err: errors.New("redis unavailable")}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(userIDContextKey, uint64(42))
		c.Next()
	})
	router.GET("/chat", RateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/chat", nil))

	assertErrorResponse(t, recorder, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
}

type testContextKey struct{}

func TestAnonymousRateLimitMiddlewareUsesRemoteIPAndIgnoresForwardedHeaders(t *testing.T) {
	limiter := &fakeRateLimiter{allowed: true}
	router := gin.New()
	router.POST("/register", AnonymousRateLimitMiddleware(limiter), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/register", nil)
	request.RemoteAddr = "[::ffff:192.0.2.44]:54321"
	request.Header.Set("X-Forwarded-For", "203.0.113.10")
	request.Header.Set("X-Real-IP", "203.0.113.11")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if limiter.calls != 1 || limiter.identity != "192.0.2.44" {
		t.Fatalf("Allow() = %d calls with identity %q", limiter.calls, limiter.identity)
	}
}

func TestAnonymousRateLimitMiddlewareRejectsOverLimit(t *testing.T) {
	limiter := &fakeRateLimiter{retryAfter: 1500 * time.Millisecond}
	router := gin.New()
	router.POST("/register", AnonymousRateLimitMiddleware(limiter), func(c *gin.Context) {
		t.Fatal("next handler was called")
	})
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/register", nil))

	assertErrorResponse(t, recorder, http.StatusTooManyRequests, "TOO_MANY_REQUESTS")
	if got := recorder.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
}

func TestAnonymousRateLimitMiddlewareFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		limiter    RateLimiter
		remoteAddr string
		status     int
		code       string
	}{
		{name: "missing limiter", remoteAddr: "192.0.2.1:1234", status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "limiter error", limiter: &fakeRateLimiter{allowed: true, err: errors.New("redis unavailable")}, remoteAddr: "192.0.2.1:1234", status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "invalid remote address", limiter: &fakeRateLimiter{allowed: true}, remoteAddr: "not-an-address", status: http.StatusInternalServerError, code: "INTERNAL_SERVER_ERROR"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/register", AnonymousRateLimitMiddleware(test.limiter), func(c *gin.Context) {
				t.Fatal("next handler was called")
			})
			request := httptest.NewRequest(http.MethodPost, "/register", nil)
			request.RemoteAddr = test.remoteAddr
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			assertErrorResponse(t, recorder, test.status, test.code)
		})
	}
}
