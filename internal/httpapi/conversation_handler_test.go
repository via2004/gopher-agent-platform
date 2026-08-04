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
)

type fakeConversationService struct {
	createCalls int
	ctx         context.Context
	userID      uint64
	title       string
	created     *conversation.Conversation
	err         error
}

func (f *fakeConversationService) Create(ctx context.Context, userID uint64, title string) (*conversation.Conversation, error) {
	f.createCalls++
	f.ctx = ctx
	f.userID = userID
	f.title = title
	return f.created, f.err
}

func newConversationHandlerTestRouter(service ConversationService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/conversations", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewConversationHandler(service).Create)
	return router
}

func TestConversationHandlerCreate(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := &fakeConversationService{created: &conversation.Conversation{
		ID:        42,
		UserID:    7,
		Title:     "Go and AI",
		CreatedAt: createdAt,
	}}
	router := newConversationHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations",
		strings.NewReader(`{"title":"Go and AI"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if service.createCalls != 1 || service.userID != 7 || service.title != "Go and AI" {
		t.Fatalf("Create() = %d calls with user ID %d and title %q", service.createCalls, service.userID, service.title)
	}
	if service.ctx != ctx {
		t.Fatal("handler did not pass the request context to Create")
	}

	var response conversationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Title != "Go and AI" || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
}

func TestConversationHandlerCreateRejectsInvalidJSON(t *testing.T) {
	service := &fakeConversationService{}
	router := newConversationHandlerTestRouter(service, uint64(7), true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", strings.NewReader(`{"title":`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if service.createCalls != 0 {
		t.Fatalf("Create() calls = %d, want 0", service.createCalls)
	}
}

func TestConversationHandlerCreateRejectsMissingOrInvalidContextUserID(t *testing.T) {
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
			service := &fakeConversationService{}
			router := newConversationHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations",
				strings.NewReader(`{"title":"Go and AI"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.createCalls != 0 {
				t.Fatalf("Create() calls = %d, want 0", service.createCalls)
			}
		})
	}
}

func TestConversationHandlerCreateMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid title", err: conversation.ErrInvalidTitle, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid user ID", err: conversation.ErrInvalidUserID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeConversationService{err: tt.err}
			router := newConversationHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations",
				strings.NewReader(`{"title":"Go and AI"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.createCalls != 1 {
				t.Fatalf("Create() calls = %d, want 1", service.createCalls)
			}
		})
	}
}
