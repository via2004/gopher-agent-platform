package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopherai/internal/user"
)

func TestNewRouterHealthz(t *testing.T) {
	router := NewRouter(NewUserHandler(&fakeUserRegistrar{}, nil))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["message"] != "pong" {
		t.Errorf("message = %q, want %q", response["message"], "pong")
	}
}

func TestNewRouterRegistersUserRegistrationRoute(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	registrar := &fakeUserRegistrar{registered: &user.User{
		ID:        42,
		Email:     "user@example.com",
		CreatedAt: createdAt,
	}}
	router := NewRouter(NewUserHandler(registrar, nil))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"user@example.com","password":"password123"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if registrar.registerCalls != 1 {
		t.Fatalf("Register() calls = %d, want 1", registrar.registerCalls)
	}
}

type panicUserRegistrar struct{}

func (panicUserRegistrar) Register(context.Context, string, string) (*user.User, error) {
	panic("register panic")
}

// just for satisfied the interface
func (panicUserRegistrar) Login(ctx context.Context, email string, password string) (*user.User, error) {
	panic("login panic")
}

func TestNewRouterRecoversFromHandlerPanic(t *testing.T) {
	router := NewRouter(NewUserHandler(panicUserRegistrar{}, nil))
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"user@example.com","password":"password123"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
