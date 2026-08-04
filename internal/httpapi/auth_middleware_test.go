package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeTokenVerifier struct {
	calls  int
	token  string
	userID uint64
	err    error
}

func (f *fakeTokenVerifier) Verify(token string) (uint64, error) {
	f.calls++
	f.token = token
	return f.userID, f.err
}

func newAuthMiddlewareTestRouter(tokens TokenVerifier, next gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/protected", AuthMiddleware(tokens), next)
	return router
}

func TestAuthMiddlewareAllowsValidToken(t *testing.T) {
	verifier := &fakeTokenVerifier{userID: 42}
	nextCalled := false
	router := newAuthMiddlewareTestRouter(verifier, func(c *gin.Context) {
		nextCalled = true
		userID, exists := c.Get(userIDContextKey)
		if !exists {
			t.Fatal("user ID is missing from context")
		}
		if userID != uint64(42) {
			t.Errorf("context user ID = %#v, want uint64(42)", userID)
		}
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if !nextCalled {
		t.Fatal("next handler was not called")
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
}

func TestAuthMiddlewareRejectsInvalidAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{name: "missing header"},
		{name: "missing token", header: "Bearer"},
		{name: "wrong scheme", header: "Basic access-token"},
		{name: "extra field", header: "Bearer access-token extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifier := &fakeTokenVerifier{}
			nextCalled := false
			router := newAuthMiddlewareTestRouter(verifier, func(c *gin.Context) {
				nextCalled = true
			})
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if verifier.calls != 0 {
				t.Fatalf("Verify() calls = %d, want 0", verifier.calls)
			}
			if nextCalled {
				t.Fatal("next handler was called after invalid authorization header")
			}
		})
	}
}

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	verifier := &fakeTokenVerifier{err: errors.New("token expired")}
	nextCalled := false
	router := newAuthMiddlewareTestRouter(verifier, func(c *gin.Context) {
		nextCalled = true
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "bearer invalid-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 1 || verifier.token != "invalid-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "invalid-token")
	}
	if nextCalled {
		t.Fatal("next handler was called after token verification failed")
	}
}
