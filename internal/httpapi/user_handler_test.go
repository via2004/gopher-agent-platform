package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gopherai/internal/user"
)

type fakeUserRegistrar struct {
	registerCalls int
	ctx           context.Context
	email         string
	password      string
	registered    *user.User
	err           error
}

func (f *fakeUserRegistrar) Register(ctx context.Context, email, password string) (*user.User, error) {
	f.registerCalls++
	f.ctx = ctx
	f.email = email
	f.password = password
	return f.registered, f.err
}

func newUserHandlerTestRouter(registrar UserRegistrar) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/register", NewUserHandler(registrar).Register)
	return router
}

func TestUserHandlerRegister(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	registrar := &fakeUserRegistrar{registered: &user.User{
		ID:           42,
		Email:        "user@example.com",
		PasswordHash: "must-not-be-returned",
		CreatedAt:    createdAt,
	}}
	router := newUserHandlerTestRouter(registrar)

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"User@Example.COM","password":"password123"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if registrar.registerCalls != 1 {
		t.Fatalf("Register() calls = %d, want 1", registrar.registerCalls)
	}
	if registrar.ctx != ctx {
		t.Fatal("handler did not pass the request context to Register")
	}
	if registrar.email != "User@Example.COM" {
		t.Errorf("Register() email = %q, want %q", registrar.email, "User@Example.COM")
	}
	if registrar.password != "password123" {
		t.Errorf("Register() password = %q, want %q", registrar.password, "password123")
	}

	var response registerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Email != "user@example.com" || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("must-not-be-returned")) ||
		bytes.Contains(recorder.Body.Bytes(), []byte("password")) {
		t.Fatalf("response leaked password data: %s", recorder.Body.String())
	}
}

func TestUserHandlerRegisterRejectsInvalidJSON(t *testing.T) {
	registrar := &fakeUserRegistrar{}
	router := newUserHandlerTestRouter(registrar)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if registrar.registerCalls != 0 {
		t.Fatalf("Register() calls = %d, want 0", registrar.registerCalls)
	}
}

func TestUserHandlerRegisterMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid email", err: user.ErrInvalidEmail, wantStatus: http.StatusBadRequest, wantCode: "EMAIL_OR_PASSWORD_INVALID"},
		{name: "invalid password", err: user.ErrInvalidPassword, wantStatus: http.StatusBadRequest, wantCode: "EMAIL_OR_PASSWORD_INVALID"},
		{name: "duplicate email", err: user.ErrEmailAlreadyExists, wantStatus: http.StatusConflict, wantCode: "EMAIL_ALREADY_EXISTS"},
		{name: "password hash failure", err: user.ErrPasswordHash, wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registrar := &fakeUserRegistrar{err: tt.err}
			router := newUserHandlerTestRouter(registrar)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
				strings.NewReader(`{"email":"user@example.com","password":"password123"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if registrar.registerCalls != 1 {
				t.Fatalf("Register() calls = %d, want 1", registrar.registerCalls)
			}
		})
	}
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, wantStatus, recorder.Body.String())
	}

	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != wantCode {
		t.Errorf("error code = %q, want %q", response.Code, wantCode)
	}
	if response.Message == "" {
		t.Error("error message is empty")
	}
}
