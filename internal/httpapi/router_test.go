package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopherai/internal/conversation"
	"gopherai/internal/user"
)

func TestNewRouterHealthz(t *testing.T) {
	router := NewRouter(NewUserHandler(&fakeUserRegistrar{}, nil), nil, nil)
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
	router := NewRouter(NewUserHandler(registrar, nil), nil, nil)
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
	router := NewRouter(NewUserHandler(users, nil), nil, verifier)
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
	router := NewRouter(NewUserHandler(users, nil), nil, verifier)
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

func TestNewRouterProtectsCreateConversationRoute(t *testing.T) {
	service := &fakeConversationService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations",
		strings.NewReader(`{"title":"Go and AI"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 0 {
		t.Fatalf("Verify() calls = %d, want 0", verifier.calls)
	}
	if service.createCalls != 0 {
		t.Fatalf("Create() calls = %d, want 0", service.createCalls)
	}
}

func TestNewRouterCreatesConversationForAuthenticatedUser(t *testing.T) {
	service := &fakeConversationService{created: &conversation.Conversation{
		ID:        42,
		UserID:    7,
		Title:     "Go and AI",
		CreatedAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
	}}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations",
		strings.NewReader(`{"title":"Go and AI"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.createCalls != 1 || service.userID != 7 {
		t.Fatalf("Create() = %d calls with user ID %d, want 1 call with user ID 7", service.createCalls, service.userID)
	}
}

func TestNewRouterProtectsConversationListRoute(t *testing.T) {
	service := &fakeConversationService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 0 || service.listCalls != 0 {
		t.Fatalf("calls without credentials: Verify = %d, List = %d; want both 0", verifier.calls, service.listCalls)
	}
}

func TestNewRouterListsConversationsForAuthenticatedUser(t *testing.T) {
	service := &fakeConversationService{listed: []*conversation.Conversation{}}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?page=2&page_size=10", nil)
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.listCalls != 1 || service.listUserID != 7 || service.page != 2 || service.pageSize != 10 {
		t.Fatalf("List() = %d calls with user ID %d, page %d, page size %d", service.listCalls, service.listUserID, service.page, service.pageSize)
	}
}

func TestNewRouterProtectsConversationDetailRoute(t *testing.T) {
	service := &fakeConversationService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/42", nil)

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 0 || service.getCalls != 0 {
		t.Fatalf("calls without credentials: Verify = %d, GetByID = %d; want both 0", verifier.calls, service.getCalls)
	}
}

func TestNewRouterGetsConversationForAuthenticatedUser(t *testing.T) {
	service := &fakeConversationService{found: &conversation.Conversation{
		ID:     42,
		UserID: 7,
		Title:  "Go and AI",
	}}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/42", nil)
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.getCalls != 1 || service.getUserID != 7 || service.getID != 42 {
		t.Fatalf("GetByID() = %d calls with user ID %d and conversation ID %d", service.getCalls, service.getUserID, service.getID)
	}
}

func TestNewRouterProtectsConversationDeleteRoute(t *testing.T) {
	service := &fakeConversationService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/42", nil)

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 0 || service.deleteCalls != 0 {
		t.Fatalf("calls without credentials: Verify = %d, Delete = %d; want both 0", verifier.calls, service.deleteCalls)
	}
}

func TestNewRouterDeletesConversationForAuthenticatedUser(t *testing.T) {
	service := &fakeConversationService{}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		NewConversationHandler(service),
		verifier,
	)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/42", nil)
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.deleteCalls != 1 || service.deleteUserID != 7 || service.deleteID != 42 {
		t.Fatalf("Delete() = %d calls with user ID %d and conversation ID %d", service.deleteCalls, service.deleteUserID, service.deleteID)
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
	router := NewRouter(NewUserHandler(panicUserRegistrar{}, nil), nil, nil)
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
