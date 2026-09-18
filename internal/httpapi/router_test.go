package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopherai/internal/chat"
	"gopherai/internal/conversation"
	"gopherai/internal/message"
	"gopherai/internal/rag"
	"gopherai/internal/user"
)

func TestNewRouterHealthz(t *testing.T) {
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil)}, RouterMiddleware{})
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

func TestNewRouterReadinessRouteDoesNotRequireAuthentication(t *testing.T) {
	checker := &fakeReadinessChecker{}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Health: NewHealthHandler(checker)}, RouterMiddleware{})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if checker.calls != 1 {
		t.Fatalf("Check() calls = %d, want 1", checker.calls)
	}
}

func TestNewRouterRegistersUserRegistrationRoute(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	registrar := &fakeUserRegistrar{registered: &user.User{
		ID:        42,
		Email:     "user@example.com",
		CreatedAt: createdAt,
	}}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(registrar, nil)}, RouterMiddleware{AuthRegisterLimiter: &fakeRateLimiter{allowed: true}})
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

func TestNewRouterUsesDedicatedAnonymousLimitersForPublicAuthRoutes(t *testing.T) {
	registrar := &fakeUserRegistrar{
		registered: &user.User{ID: 42, Email: "user@example.com"},
		loggedIn:   &user.User{ID: 42, Email: "user@example.com"},
	}
	verification := &fakeEmailVerificationService{}
	registerLimiter := &fakeRateLimiter{allowed: true}
	loginLimiter := &fakeRateLimiter{allowed: true}
	emailLimiter := &fakeRateLimiter{allowed: true}
	router := NewRouter(
		RouterHandlers{
			Users:             NewUserHandler(registrar, &fakeTokenIssuer{token: "access-token"}),
			EmailVerification: NewEmailVerificationHandler(verification),
		},
		RouterMiddleware{
			AuthRegisterLimiter:      registerLimiter,
			AuthLoginLimiter:         loginLimiter,
			EmailVerificationLimiter: emailLimiter,
		},
	)

	requests := []struct {
		path string
		body string
		want int
	}{
		{path: "/api/v1/auth/register", body: `{"email":"user@example.com","password":"password123"}`, want: http.StatusCreated},
		{path: "/api/v1/auth/login", body: `{"email":"user@example.com","password":"password123"}`, want: http.StatusOK},
		{path: "/api/v1/auth/email-verification-codes", body: `{"email":"user@example.com"}`, want: http.StatusAccepted},
	}
	for _, request := range requests {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, request.path, strings.NewReader(request.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		if recorder.Code != request.want {
			t.Fatalf("POST %s status = %d, want %d; body = %s", request.path, recorder.Code, request.want, recorder.Body.String())
		}
	}

	for name, limiter := range map[string]*fakeRateLimiter{
		"register":           registerLimiter,
		"login":              loginLimiter,
		"email verification": emailLimiter,
	} {
		if limiter.calls != 1 || limiter.identity != "192.0.2.1" {
			t.Fatalf("%s limiter = %d calls with identity %q; want 1 call with 192.0.2.1", name, limiter.calls, limiter.identity)
		}
	}
}

func TestNewRouterProtectsCurrentUserRoute(t *testing.T) {
	users := &fakeUserRegistrar{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(users, nil)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(users, nil)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Conversations: NewConversationHandler(service)}, RouterMiddleware{Tokens: verifier})
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

func TestNewRouterProtectsMessageRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "create", method: http.MethodPost, body: `{"content":"hello"}`},
		{name: "list", method: http.MethodGet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{}
			verifier := &fakeTokenVerifier{}
			router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Messages: NewMessageHandler(service)}, RouterMiddleware{Tokens: verifier})
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, "/api/v1/conversations/9/messages", strings.NewReader(tt.body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if verifier.calls != 0 || service.createCalls != 0 || service.listCalls != 0 {
				t.Fatalf("calls without credentials: Verify = %d, Create = %d, List = %d; want all 0", verifier.calls, service.createCalls, service.listCalls)
			}
		})
	}
}

func TestNewRouterCreatesMessageForAuthenticatedUser(t *testing.T) {
	service := &fakeMessageService{created: &message.Message{
		ID:             42,
		ConversationID: 9,
		Role:           message.RoleUser,
		Content:        "hello",
	}}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Messages: NewMessageHandler(service)}, RouterMiddleware{Tokens: verifier})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/messages", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.createCalls != 1 || service.createUser != 7 || service.createConv != 9 {
		t.Fatalf("CreateUserMessage() = %d calls with user ID %d and conversation ID %d", service.createCalls, service.createUser, service.createConv)
	}
}

func TestNewRouterListsMessagesForAuthenticatedUser(t *testing.T) {
	service := &fakeMessageService{listed: []*message.Message{}}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Messages: NewMessageHandler(service)}, RouterMiddleware{Tokens: verifier})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/9/messages?page=2&page_size=10", nil)
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.listCalls != 1 || service.listUser != 7 || service.listConv != 9 || service.page != 2 || service.pageSize != 10 {
		t.Fatalf("List() = %d calls with user ID %d, conversation ID %d, page %d, page size %d", service.listCalls, service.listUser, service.listConv, service.page, service.pageSize)
	}
}

func TestNewRouterProtectsChatRoute(t *testing.T) {
	service := &fakeChatService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Chat: NewChatHandler(service)}, RouterMiddleware{Tokens: verifier, ChatLimiter: &fakeRateLimiter{allowed: true}})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if verifier.calls != 0 || service.calls != 0 {
		t.Fatalf("calls without credentials: Verify = %d, Chat = %d; want both 0", verifier.calls, service.calls)
	}
}

func TestNewRouterServesChatForAuthenticatedUser(t *testing.T) {
	service := &fakeChatService{response: &chat.Result{
		ID:      42,
		Role:    message.RoleAssistant,
		Content: "answer",
	}}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Chat: NewChatHandler(service)}, RouterMiddleware{Tokens: verifier, ChatLimiter: &fakeRateLimiter{allowed: true}})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.calls != 1 || service.userID != 7 || service.conversationID != 9 || service.content != "hello" {
		t.Fatalf("ReceiveAndResponse() = %d calls with user ID %d, conversation ID %d, content %q", service.calls, service.userID, service.conversationID, service.content)
	}
}

func TestNewRouterServesStreamingChatForAuthenticatedUser(t *testing.T) {
	service := &fakeChatService{
		streamDeltas: []string{"answer"},
		streamResponse: &chat.Result{
			ID:   42,
			Role: message.RoleAssistant,
		},
	}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), Chat: NewChatHandler(service)}, RouterMiddleware{Tokens: verifier, ChatLimiter: &fakeRateLimiter{allowed: true}})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat/stream", strings.NewReader(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer access-token")

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if verifier.calls != 1 || verifier.token != "access-token" {
		t.Fatalf("Verify() = %d calls with %q, want 1 call with %q", verifier.calls, verifier.token, "access-token")
	}
	if service.streamCalls != 1 || service.userID != 7 || service.conversationID != 9 || service.content != "hello" {
		t.Fatalf("ChatStreaming() = %d calls with user ID %d, conversation ID %d, content %q", service.streamCalls, service.userID, service.conversationID, service.content)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "event:delta\n") || !strings.Contains(body, "event:done\n") {
		t.Fatalf("SSE response = %q, want delta and done events", body)
	}
}

type panicUserRegistrar struct{}

func (panicUserRegistrar) Register(context.Context, string, string, string) (*user.User, error) {
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
	router := NewRouter(RouterHandlers{Users: NewUserHandler(panicUserRegistrar{}, nil)}, RouterMiddleware{AuthRegisterLimiter: &fakeRateLimiter{allowed: true}})
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

func TestNewRouterProtectsRAGDocumentRoute(t *testing.T) {
	service := &fakeRAGService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), RAG: NewRAGHandler(service)}, RouterMiddleware{Tokens: verifier})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/rag/documents", nil)
	router.ServeHTTP(recorder, request)
	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	if service.calls != 0 {
		t.Fatalf("Upload() calls = %d, want 0", service.calls)
	}
}

func TestNewRouterUsesDedicatedRAGUploadLimiter(t *testing.T) {
	service := &fakeRAGService{result: &rag.Document{Filename: "notes.md", Size: 5}}
	verifier := &fakeTokenVerifier{userID: 42}
	chatLimiter := &fakeRateLimiter{allowed: false}
	ragLimiter := &fakeRateLimiter{allowed: true}
	router := NewRouter(RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), RAG: NewRAGHandler(service)}, RouterMiddleware{Tokens: verifier, ChatLimiter: chatLimiter, RAGUploadLimiter: ragLimiter})
	request := ragDocumentRequest(t, "notes.md", []byte("hello"))
	request.Header.Set("Authorization", "Bearer access-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if ragLimiter.calls != 1 || ragLimiter.identity != "42" {
		t.Fatalf("RAG limiter = %d calls with identity %q", ragLimiter.calls, ragLimiter.identity)
	}
	if chatLimiter.calls != 0 {
		t.Fatalf("chat limiter calls = %d, want 0", chatLimiter.calls)
	}
}
