package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gopherai/internal/chat"
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
	response       *chat.Result
	err            error
	streamCalls    int
	streamDeltas   []string
	streamResponse *chat.Result
	streamErr      error
}

func (f *fakeChatService) Chat(
	ctx context.Context,
	userID uint64,
	conversationID uint64,
	content string,
) (*chat.Result, error) {
	f.calls++
	f.ctx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.content = content

	if f.err != nil {
		return nil, f.err
	}

	return f.response, nil
}

func (f *fakeChatService) ChatStreaming(ctx context.Context, userID uint64,
	conversationID uint64, content string,
	onDelta func(string) error) (*chat.Result, error) {
	f.streamCalls++
	f.ctx = ctx
	f.userID = userID
	f.conversationID = conversationID
	f.content = content
	for _, delta := range f.streamDeltas {
		if err := onDelta(delta); err != nil {
			return nil, err
		}
	}

	if f.streamErr != nil {
		return nil, f.streamErr
	}

	return f.streamResponse, nil
}

type chatResponseBody struct {
	ID           uint64       `json:"id"`
	Role         message.Role `json:"role"`
	Content      string       `json:"content"`
	CreatedAt    time.Time    `json:"created_at"`
	Model        string       `json:"model"`
	InputTokens  int64        `json:"input_tokens"`
	OutputTokens int64        `json:"output_tokens"`
	TotalTokens  int64        `json:"total_tokens"`
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
	router.POST("/api/v1/conversations/:id/chat/stream", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewChatHandler(service).ChatStreaming)
	return router
}

type chatContextKey string

func assertChatContext(t *testing.T, ctx context.Context, key chatContextKey, wantValue string) {
	t.Helper()
	if ctx == nil {
		t.Fatal("Chat Service received a nil context")
	}
	if got := ctx.Value(key); got != wantValue {
		t.Fatalf("context value = %v, want %q", got, wantValue)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("Chat Service context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > streamMaxDuration {
		t.Fatalf("context deadline remaining = %v, want within (0, %v]", remaining, streamMaxDuration)
	}
}

func TestChatHandlerChat(t *testing.T) {
	createdAt := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	service := &fakeChatService{response: &chat.Result{
		ID:           42,
		Role:         message.RoleAssistant,
		Content:      "An interface describes behavior.",
		CreatedAt:    createdAt,
		Model:        "gpt-test-actual",
		InputTokens:  20,
		OutputTokens: 10,
		TotalTokens:  30,
	}}
	router := newChatHandlerTestRouter(service, uint64(7), true)
	key := chatContextKey("request-id")
	ctx := context.WithValue(context.Background(), key, "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat", strings.NewReader(`{"content":"What is an interface?"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.calls != 1 || service.userID != 7 || service.conversationID != 9 || service.content != "What is an interface?" {
		t.Fatalf("Chat() = %d calls with user ID %d, conversation ID %d, content %q", service.calls, service.userID, service.conversationID, service.content)
	}
	assertChatContext(t, service.ctx, key, "request-1")
	var response chatResponseBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != service.response.ID || response.Role != service.response.Role ||
		response.Content != service.response.Content || !response.CreatedAt.Equal(service.response.CreatedAt) ||
		response.Model != service.response.Model || response.InputTokens != service.response.InputTokens ||
		response.OutputTokens != service.response.OutputTokens || response.TotalTokens != service.response.TotalTokens {
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
				t.Fatalf("Chat() calls = %d, want 0", service.calls)
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
				t.Fatalf("Chat() calls = %d, want 0", service.calls)
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
		{name: "timeout", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantCode: "TIMEOUT"},
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
				t.Fatalf("Chat() calls = %d, want 1", service.calls)
			}
		})
	}
}

func TestChatHandlerChatStreaming(t *testing.T) {
	createdAt := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	service := &fakeChatService{
		streamDeltas: []string{"An interface", " describes behavior."},
		streamResponse: &chat.Result{
			ID:           42,
			Role:         message.RoleAssistant,
			Content:      "An interface describes behavior.",
			CreatedAt:    createdAt,
			Model:        "gpt-test-actual",
			InputTokens:  20,
			OutputTokens: 10,
			TotalTokens:  30,
		},
	}
	router := newChatHandlerTestRouter(service, uint64(7), true)
	key := chatContextKey("request-id")
	ctx := context.WithValue(context.Background(), key, "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat/stream", strings.NewReader(`{"content":"What is an interface?"}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	mediaType, _, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want SSE; parse error = %v", recorder.Header().Get("Content-Type"), err)
	}
	if service.streamCalls != 1 || service.userID != 7 || service.conversationID != 9 || service.content != "What is an interface?" {
		t.Fatalf("ChatStreaming() = %d calls with user ID %d, conversation ID %d, content %q", service.streamCalls, service.userID, service.conversationID, service.content)
	}
	assertChatContext(t, service.ctx, key, "request-1")
	body := recorder.Body.String()
	for _, want := range []string{
		"event:delta\ndata:{\"delta\":\"An interface\"}\n\n",
		"event:delta\ndata:{\"delta\":\" describes behavior.\"}\n\n",
		"event:done\n",
		`"id":42`,
		`"role":"assistant"`,
		`"model":"gpt-test-actual"`,
		`"input_tokens":20`,
		`"output_tokens":10`,
		`"total_tokens":30`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE response does not contain %q; body = %q", want, body)
		}
	}
	if strings.Index(body, "event:delta") > strings.Index(body, "event:done") {
		t.Fatalf("done event was written before delta events; body = %q", body)
	}
}

func TestChatHandlerChatStreamingRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		userID     any
		setUserID  bool
		wantStatus int
		wantCode   string
	}{
		{name: "missing user ID", path: "/api/v1/conversations/9/chat/stream", body: `{"content":"hello"}`, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "invalid JSON", path: "/api/v1/conversations/9/chat/stream", body: `{"content":`, userID: uint64(7), setUserID: true, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid conversation ID", path: "/api/v1/conversations/not-a-number/chat/stream", body: `{"content":"hello"}`, userID: uint64(7), setUserID: true, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatService{}
			router := newChatHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.streamCalls != 0 {
				t.Fatalf("ChatStreaming() calls = %d, want 0", service.streamCalls)
			}
		})
	}
}

func TestChatHandlerChatStreamingMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "invalid content", err: message.ErrInvalidContent, wantCode: "INVALID_REQUEST"},
		{name: "conversation not found", err: conversation.ErrConversationNotFound, wantCode: "NOT_FOUND"},
		{name: "LLM unavailable", err: llm.ErrNotConfigured, wantCode: "SERVICE_UNAVAILABLE"},
		{name: "incomplete response", err: llm.ErrResponseNotCompleted, wantCode: "RESPONSE_NOT_COMPLETED"},
		{name: "timeout", err: context.DeadlineExceeded, wantCode: "TIMEOUT"},
		{name: "provider failure", err: llm.ErrResponseFailed, wantCode: "RESPONSE_FAILED"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatService{streamErr: tt.err}
			router := newChatHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat/stream", strings.NewReader(`{"content":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			body := recorder.Body.String()
			if !strings.Contains(body, "event:error\n") || !strings.Contains(body, `"code":"`+tt.wantCode+`"`) {
				t.Fatalf("SSE error response = %q, want code %q", body, tt.wantCode)
			}
			if strings.Contains(body, "event:done\n") {
				t.Fatalf("SSE error response contains done event: %q", body)
			}
		})
	}
}
