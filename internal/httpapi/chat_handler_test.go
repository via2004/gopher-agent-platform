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
	"gopherai/internal/llm"
	"gopherai/internal/message"
)

type fakeChatService struct {
	calls          int
	ctx            context.Context
	userID         uint64
	conversationID uint64
	content        string
	response       *message.Message
	err            error
}

func (f *fakeChatService) ReceiveAndResponse(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	content string,
) (*message.Message, error) {
	f.calls++
	f.ctx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.content = content
	return f.response, f.err
}

func newChatHandlerTestRouter(service ChatService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/conversations/:id/chat", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewChatHandler(service).Chat)
	return router
}

func TestChatHandlerChat(t *testing.T) {
	createdAt := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	service := &fakeChatService{response: &message.Message{
		ID:             42,
		ConversationID: 9,
		Role:           message.RoleAssistant,
		Content:        "An interface describes behavior.",
		CreatedAt:      createdAt,
	}}
	router := newChatHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat", strings.NewReader(`{"content":"What is an interface?"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.calls != 1 || service.userID != 7 || service.conversationID != 9 || service.content != "What is an interface?" {
		t.Fatalf("ReceiveAndResponse() = %d calls with user ID %d, conversation ID %d, content %q", service.calls, service.userID, service.conversationID, service.content)
	}
	if service.ctx != ctx {
		t.Fatal("handler did not pass the request context to Chat Service")
	}
	var response messageResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Role != message.RoleAssistant || response.Content != service.response.Content || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
}

func TestChatHandlerRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "invalid JSON", path: "/api/v1/conversations/9/chat", body: `{"content":`},
		{name: "invalid conversation ID", path: "/api/v1/conversations/not-a-number/chat", body: `{"content":"hello"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatService{}
			router := newChatHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
			if service.calls != 0 {
				t.Fatalf("ReceiveAndResponse() calls = %d, want 0", service.calls)
			}
		})
	}
}

func TestChatHandlerRejectsMissingOrInvalidContextUserID(t *testing.T) {
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
			service := &fakeChatService{}
			router := newChatHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat", strings.NewReader(`{"content":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.calls != 0 {
				t.Fatalf("ReceiveAndResponse() calls = %d, want 0", service.calls)
			}
		})
	}
}

func TestChatHandlerMapsServiceErrors(t *testing.T) {
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
		{name: "LLM unavailable", err: llm.ErrNotConfigured, wantStatus: http.StatusServiceUnavailable, wantCode: "SERVICE_UNAVAILABLE"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatService{err: tt.err}
			router := newChatHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat", strings.NewReader(`{"content":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.calls != 1 {
				t.Fatalf("ReceiveAndResponse() calls = %d, want 1", service.calls)
			}
		})
	}
}
