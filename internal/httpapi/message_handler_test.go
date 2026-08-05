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

	"gopherai/internal/conversation"
	"gopherai/internal/message"
)

type fakeMessageService struct {
	createCalls int
	createCtx   context.Context
	createUser  uint64
	createConv  uint64
	content     string
	created     *message.Message
	createErr   error
	listCalls   int
	listCtx     context.Context
	listUser    uint64
	listConv    uint64
	page        int
	pageSize    int
	listed      []*message.Message
	listErr     error
}

func (f *fakeMessageService) CreateUserMessage(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	content string,
) (*message.Message, error) {
	f.createCalls++
	f.createCtx = ctx
	f.createUser = userID
	f.createConv = conversationID
	f.content = content
	return f.created, f.createErr
}

func (f *fakeMessageService) List(
	ctx context.Context,
	userID, conversationID uint64,
	page, pageSize int,
) ([]*message.Message, error) {
	f.listCalls++
	f.listCtx = ctx
	f.listUser = userID
	f.listConv = conversationID
	f.page = page
	f.pageSize = pageSize
	return f.listed, f.listErr
}

func newMessageHandlerTestRouter(service MessageService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	setIdentity := func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}
	handler := NewMessageHandler(service)
	router.POST("/api/v1/conversations/:id/messages", setIdentity, handler.CreateUserMessage)
	router.GET("/api/v1/conversations/:id/messages", setIdentity, handler.List)
	return router
}

func TestMessageHandlerCreateUserMessage(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := &fakeMessageService{created: &message.Message{
		ID:             42,
		ConversationID: 9,
		Role:           message.RoleUser,
		Content:        "hello",
		CreatedAt:      createdAt,
	}}
	router := newMessageHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/messages",
		strings.NewReader(`{"content":"hello","role":"assistant"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if service.createCalls != 1 || service.createUser != 7 || service.createConv != 9 || service.content != "hello" {
		t.Fatalf("CreateUserMessage() = %d calls with user ID %d, conversation ID %d, content %q", service.createCalls, service.createUser, service.createConv, service.content)
	}
	if service.createCtx != ctx {
		t.Fatal("handler did not pass the request context to CreateUserMessage")
	}

	var response messageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Role != message.RoleUser || response.Content != "hello" || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
}

func TestMessageHandlerCreateRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
	}{
		{name: "invalid JSON", path: "/api/v1/conversations/9/messages", body: `{"content":`, wantStatus: http.StatusBadRequest},
		{name: "invalid conversation ID", path: "/api/v1/conversations/not-a-number/messages", body: `{"content":"hello"}`, wantStatus: http.StatusBadRequest},
		{name: "oversized body", path: "/api/v1/conversations/9/messages", body: `{"content":"` + strings.Repeat("a", maxMessageRequestBodyBytes) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{}
			router := newMessageHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, "INVALID_REQUEST")
			if service.createCalls != 0 {
				t.Fatalf("CreateUserMessage() calls = %d, want 0", service.createCalls)
			}
		})
	}
}

func TestMessageHandlerCreateRejectsMissingOrInvalidContextUserID(t *testing.T) {
	tests := []struct {
		name      string
		userID    any
		setUserID bool
	}{
		{name: "missing user ID"},
		{name: "wrong user ID type", userID: "7", setUserID: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{}
			router := newMessageHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/messages", strings.NewReader(`{"content":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.createCalls != 0 {
				t.Fatalf("CreateUserMessage() calls = %d, want 0", service.createCalls)
			}
		})
	}
}

func TestMessageHandlerCreateMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid content", err: message.ErrInvalidContent, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid conversation ID", err: message.ErrInvalidConversationID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid user ID", err: conversation.ErrInvalidUserID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "conversation not found", err: conversation.ErrConversationNotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{createErr: tt.err}
			router := newMessageHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/messages", strings.NewReader(`{"content":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.createCalls != 1 {
				t.Fatalf("CreateUserMessage() calls = %d, want 1", service.createCalls)
			}
		})
	}
}

func TestMessageHandlerList(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := &fakeMessageService{listed: []*message.Message{
		{ID: 1, ConversationID: 9, Role: message.RoleUser, Content: "hello", CreatedAt: createdAt},
		nil,
		{ID: 2, ConversationID: 9, Role: message.RoleAssistant, Content: "hi", CreatedAt: createdAt.Add(time.Second)},
	}}
	router := newMessageHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/9/messages?page=2&page_size=10", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.listCalls != 1 || service.listUser != 7 || service.listConv != 9 || service.page != 2 || service.pageSize != 10 {
		t.Fatalf("List() = %d calls with user ID %d, conversation ID %d, page %d, page size %d", service.listCalls, service.listUser, service.listConv, service.page, service.pageSize)
	}
	if service.listCtx != ctx {
		t.Fatal("handler did not pass the request context to List")
	}

	var response messageListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ConversationID != 9 || len(response.Items) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Items[0].Role != message.RoleUser || response.Items[1].Role != message.RoleAssistant {
		t.Errorf("roles = [%q, %q], want [%q, %q]", response.Items[0].Role, response.Items[1].Role, message.RoleUser, message.RoleAssistant)
	}
}

func TestMessageHandlerListUsesDefaultsAndReturnsEmptyArray(t *testing.T) {
	service := &fakeMessageService{listed: []*message.Message{}}
	router := newMessageHandlerTestRouter(service, uint64(7), true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/9/messages", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.page != 1 || service.pageSize != 20 {
		t.Fatalf("List() page = %d, page size = %d; want 1 and 20", service.page, service.pageSize)
	}
	if !strings.Contains(recorder.Body.String(), `"items":[]`) || !strings.Contains(recorder.Body.String(), `"conversation_id":9`) {
		t.Fatalf("empty list response = %s", recorder.Body.String())
	}
}

func TestMessageHandlerListRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "invalid conversation ID", path: "/api/v1/conversations/not-a-number/messages"},
		{name: "invalid page", path: "/api/v1/conversations/9/messages?page=abc"},
		{name: "invalid page size", path: "/api/v1/conversations/9/messages?page_size=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{}
			router := newMessageHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
			if service.listCalls != 0 {
				t.Fatalf("List() calls = %d, want 0", service.listCalls)
			}
		})
	}
}

func TestMessageHandlerListRejectsMissingOrInvalidContextUserID(t *testing.T) {
	tests := []struct {
		name      string
		userID    any
		setUserID bool
	}{
		{name: "missing user ID"},
		{name: "wrong user ID type", userID: "7", setUserID: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{}
			router := newMessageHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/9/messages", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.listCalls != 0 {
				t.Fatalf("List() calls = %d, want 0", service.listCalls)
			}
		})
	}
}

func TestMessageHandlerListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid conversation ID", err: message.ErrInvalidConversationID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid user ID", err: conversation.ErrInvalidUserID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid page", err: conversation.ErrInvalidPage, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid page size", err: conversation.ErrInvalidPageSize, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "conversation not found", err: conversation.ErrConversationNotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeMessageService{listErr: tt.err}
			router := newMessageHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/9/messages", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.listCalls != 1 {
				t.Fatalf("List() calls = %d, want 1", service.listCalls)
			}
		})
	}
}
