package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gopherai/internal/emailverification"
)

type fakeEmailVerificationService struct {
	calls int
	ctx   context.Context
	email string
	err   error
}

func (f *fakeEmailVerificationService) Send(ctx context.Context, email string) error {
	f.calls++
	f.ctx = ctx
	f.email = email
	return f.err
}

func newEmailVerificationHandlerTestRouter(service EmailVerificationService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST(
		"/api/v1/auth/email-verification-codes",
		NewEmailVerificationHandler(service).Send,
	)
	return router
}

func TestEmailVerificationHandlerSend(t *testing.T) {
	service := &fakeEmailVerificationService{}
	router := newEmailVerificationHandlerTestRouter(service)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/email-verification-codes",
		strings.NewReader(`{"email":"User@Example.COM"}`),
	).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted || !bytes.Contains(recorder.Body.Bytes(), []byte(`"message":"verification code accepted"`)) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.calls != 1 || service.email != "User@Example.COM" || service.ctx != ctx {
		t.Fatalf("Send() = %d calls with email %q", service.calls, service.email)
	}
	if strings.Contains(recorder.Body.String(), "123456") {
		t.Fatalf("response leaked verification code: %s", recorder.Body.String())
	}
}

func TestEmailVerificationHandlerRejectsInvalidAndOversizedRequests(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "invalid JSON", body: `{`, status: http.StatusBadRequest},
		{name: "oversized", body: `{"email":"` + strings.Repeat("x", maxEmailVerificationRequestBodyBytes) + `"}`, status: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeEmailVerificationService{}
			router := newEmailVerificationHandlerTestRouter(service)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email-verification-codes", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			assertErrorResponse(t, recorder, test.status, "INVALID_REQUEST")
			if service.calls != 0 {
				t.Fatalf("Send() calls = %d, want 0", service.calls)
			}
		})
	}
}

func TestEmailVerificationHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid email", err: emailverification.ErrInvalidEmail, status: http.StatusBadRequest, code: "INVALID_REQUEST"},
		{name: "send cooldown", err: emailverification.ErrSendTooFrequent, status: http.StatusTooManyRequests, code: "TOO_MANY_REQUESTS"},
		{name: "not configured", err: emailverification.ErrNotConfigured, status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "store failed", err: emailverification.ErrStoreFailed, status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "send failed", err: emailverification.ErrSendFailed, status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "generate failed", err: emailverification.ErrGenerateCodeFailed, status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "timeout", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout, code: "TIMEOUT"},
		{name: "unknown", err: errors.New("unknown"), status: http.StatusInternalServerError, code: "INTERNAL_SERVER_ERROR"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeEmailVerificationService{err: test.err}
			router := newEmailVerificationHandlerTestRouter(service)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email-verification-codes", strings.NewReader(`{"email":"user@example.com"}`))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			assertErrorResponse(t, recorder, test.status, test.code)
			if service.calls != 1 {
				t.Fatalf("Send() calls = %d, want 1", service.calls)
			}
		})
	}
}

func TestEmailVerificationHandlerReturnsUnavailableWithoutService(t *testing.T) {
	router := newEmailVerificationHandlerTestRouter(nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email-verification-codes", strings.NewReader(`{"email":"user@example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	assertErrorResponse(t, recorder, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
}

func TestNewRouterExposesEmailVerificationRouteWithoutAuthentication(t *testing.T) {
	service := &fakeEmailVerificationService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(
		RouterHandlers{
			Users:             NewUserHandler(&fakeUserRegistrar{}, nil),
			EmailVerification: NewEmailVerificationHandler(service),
		},
		RouterMiddleware{Tokens: verifier},
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/email-verification-codes", strings.NewReader(`{"email":"user@example.com"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.calls != 1 || verifier.calls != 0 {
		t.Fatalf("calls = Send %d, Verify token %d", service.calls, verifier.calls)
	}
}
