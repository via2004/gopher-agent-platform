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
	router := NewRouter(NewUserHandler(&fakeUserRegistrar{}, nil), nil)
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
	router := NewRouter(NewUserHandler(registrar, nil), nil)
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

func TestNewRouterProtectsCurrentUserRoute(t *testing.T) {
	users := &fakeUserRegistrar{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(NewUserHandler(users, nil), verifier)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 0 {
		t.Fatalf("Verify() calls = %d, want 0", verifier.calls)
	}
	if users.getByIDCalls != 0 {
		t.Fatalf("GetByID() calls = %d, want 0", users.getByIDCalls)
	}
}

func TestNewRouterServesCurrentUserForValidToken(t *testing.T) {
	users := &fakeUserRegistrar{queriedUser: &user.User{ID: 42, Email: "user@example.com"}}
	verifier := &fakeTokenVerifier{userID: 42}
	router := NewRouter(NewUserHandler(users, nil), verifier)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if users.getByIDCalls != 1 || users.queriedUserID != 42 {
		t.Fatalf("GetByID() = %d calls with user ID %d, want 1 call with user ID 42", users.getByIDCalls, users.queriedUserID)
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

func (panicUserRegistrar) GetByID(ctx context.Context, userID uint64) (*user.User, error) {
	return nil, nil
}

func TestNewRouterRecoversFromHandlerPanic(t *testing.T) {
	router := NewRouter(NewUserHandler(panicUserRegistrar{}, nil), nil)
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
