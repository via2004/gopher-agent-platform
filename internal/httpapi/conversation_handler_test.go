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
	createCalls  int
	ctx          context.Context
	userID       uint64
	title        string
	created      *conversation.Conversation
	err          error
	listCalls    int
	listCtx      context.Context
	listUserID   uint64
	page         int
	pageSize     int
	listed       []*conversation.Conversation
	listErr      error
	getCalls     int
	getCtx       context.Context
	getUserID    uint64
	getID        uint64
	found        *conversation.Conversation
	getErr       error
	deleteCalls  int
	deleteCtx    context.Context
	deleteUserID uint64
	deleteID     uint64
	deleteErr    error
}

func (f *fakeConversationService) Create(ctx context.Context, userID uint64, title string) (*conversation.Conversation, error) {
	f.createCalls++
	f.ctx = ctx
	f.userID = userID
	f.title = title
	return f.created, f.err
}

func (f *fakeConversationService) List(ctx context.Context, userID uint64, page, pageSize int) ([]*conversation.Conversation, error) {
	f.listCalls++
	f.listCtx = ctx
	f.listUserID = userID
	f.page = page
	f.pageSize = pageSize
	return f.listed, f.listErr
}

func (f *fakeConversationService) GetByID(ctx context.Context, userID, conversationID uint64) (*conversation.Conversation, error) {
	f.getCalls++
	f.getCtx = ctx
	f.getUserID = userID
	f.getID = conversationID
	return f.found, f.getErr
}
func (f *fakeConversationService) Delete(ctx context.Context, userID uint64, conversationID uint64) error {
	f.deleteCalls++
	f.deleteCtx = ctx
	f.deleteUserID = userID
	f.deleteID = conversationID
	return f.deleteErr
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

func newConversationListHandlerTestRouter(service ConversationService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/conversations", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewConversationHandler(service).List)
	return router
}

func newConversationGetHandlerTestRouter(service ConversationService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/conversations/:id", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewConversationHandler(service).GetByID)
	return router
}

func newConversationDeleteHandlerTestRouter(service ConversationService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.DELETE("/api/v1/conversations/:id", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewConversationHandler(service).Delete)
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

func TestConversationHandlerListUsesDefaultPagination(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := &fakeConversationService{listed: []*conversation.Conversation{
		{ID: 2, UserID: 7, Title: "Second", CreatedAt: createdAt},
		{ID: 1, UserID: 7, Title: "First", CreatedAt: createdAt.Add(-time.Hour)},
	}}
	router := newConversationListHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.listCalls != 1 || service.listUserID != 7 || service.page != 1 || service.pageSize != 20 {
		t.Fatalf("List() = %d calls with user ID %d, page %d, page size %d", service.listCalls, service.listUserID, service.page, service.pageSize)
	}
	if service.listCtx != ctx {
		t.Fatal("handler did not pass the request context to List")
	}

	var response listResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Page != 1 || response.PageSize != 20 || len(response.Conversations) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Conversations[0].ID != 2 || response.Conversations[1].ID != 1 {
		t.Errorf("conversation order = [%d, %d], want [2, 1]", response.Conversations[0].ID, response.Conversations[1].ID)
	}
	if strings.Contains(recorder.Body.String(), "user_id") || strings.Contains(recorder.Body.String(), "updated_at") {
		t.Fatalf("response leaked internal fields: %s", recorder.Body.String())
	}
}

func TestConversationHandlerListUsesRequestedPagination(t *testing.T) {
	service := &fakeConversationService{listed: []*conversation.Conversation{}}
	router := newConversationListHandlerTestRouter(service, uint64(7), true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?page=3&page_size=5", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.page != 3 || service.pageSize != 5 {
		t.Fatalf("List() page = %d, page size = %d; want 3 and 5", service.page, service.pageSize)
	}
	if recorder.Body.String() == "" || !strings.Contains(recorder.Body.String(), `"items":[]`) {
		t.Fatalf("empty list response = %s, want items as []", recorder.Body.String())
	}
}

func TestConversationHandlerListRejectsInvalidQueryParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "invalid page", query: "?page=abc"},
		{name: "invalid page size", query: "?page_size=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeConversationService{}
			router := newConversationListHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations"+tt.query, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
			if service.listCalls != 0 {
				t.Fatalf("List() calls = %d, want 0", service.listCalls)
			}
		})
	}
}

func TestConversationHandlerListRejectsMissingOrInvalidContextUserID(t *testing.T) {
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
			router := newConversationListHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.listCalls != 0 {
				t.Fatalf("List() calls = %d, want 0", service.listCalls)
			}
		})
	}
}

func TestConversationHandlerListMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid page", err: conversation.ErrInvalidPage, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid page size", err: conversation.ErrInvalidPageSize, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid user ID", err: conversation.ErrInvalidUserID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeConversationService{listErr: tt.err}
			router := newConversationListHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.listCalls != 1 {
				t.Fatalf("List() calls = %d, want 1", service.listCalls)
			}
		})
	}
}

func TestConversationHandlerGetByID(t *testing.T) {
	createdAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	service := &fakeConversationService{found: &conversation.Conversation{
		ID:        42,
		UserID:    7,
		Title:     "Go and AI",
		CreatedAt: createdAt,
	}}
	router := newConversationGetHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/42", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.getCalls != 1 || service.getUserID != 7 || service.getID != 42 {
		t.Fatalf("GetByID() = %d calls with user ID %d and conversation ID %d", service.getCalls, service.getUserID, service.getID)
	}
	if service.getCtx != ctx {
		t.Fatal("handler did not pass the request context to GetByID")
	}

	var response conversationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Title != "Go and AI" || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
}

func TestConversationHandlerGetByIDRejectsInvalidPathID(t *testing.T) {
	service := &fakeConversationService{}
	router := newConversationGetHandlerTestRouter(service, uint64(7), true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/not-a-number", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if service.getCalls != 0 {
		t.Fatalf("GetByID() calls = %d, want 0", service.getCalls)
	}
}

func TestConversationHandlerGetByIDRejectsMissingOrInvalidContextUserID(t *testing.T) {
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
			router := newConversationGetHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/42", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.getCalls != 0 {
				t.Fatalf("GetByID() calls = %d, want 0", service.getCalls)
			}
		})
	}
}

func TestConversationHandlerGetByIDMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid conversation ID", err: conversation.ErrInvalidConversationID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid user ID", err: conversation.ErrInvalidUserID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "not found", err: conversation.ErrConversationNotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeConversationService{getErr: tt.err}
			router := newConversationGetHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/42", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.getCalls != 1 {
				t.Fatalf("GetByID() calls = %d, want 1", service.getCalls)
			}
		})
	}
}

func TestConversationHandlerDelete(t *testing.T) {
	service := &fakeConversationService{}
	router := newConversationDeleteHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/42", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("response body = %q, want empty", recorder.Body.String())
	}
	if service.deleteCalls != 1 || service.deleteUserID != 7 || service.deleteID != 42 {
		t.Fatalf("Delete() = %d calls with user ID %d and conversation ID %d", service.deleteCalls, service.deleteUserID, service.deleteID)
	}
	if service.deleteCtx != ctx {
		t.Fatal("handler did not pass the request context to Delete")
	}
}

func TestConversationHandlerDeleteRejectsInvalidPathID(t *testing.T) {
	service := &fakeConversationService{}
	router := newConversationDeleteHandlerTestRouter(service, uint64(7), true)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/not-a-number", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if service.deleteCalls != 0 {
		t.Fatalf("Delete() calls = %d, want 0", service.deleteCalls)
	}
}

func TestConversationHandlerDeleteRejectsMissingOrInvalidContextUserID(t *testing.T) {
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
			router := newConversationDeleteHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/42", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.deleteCalls != 0 {
				t.Fatalf("Delete() calls = %d, want 0", service.deleteCalls)
			}
		})
	}
}

func TestConversationHandlerDeleteMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid conversation ID", err: conversation.ErrInvalidConversationID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid user ID", err: conversation.ErrInvalidUserID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "not found", err: conversation.ErrConversationNotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeConversationService{deleteErr: tt.err}
			router := newConversationDeleteHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/42", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.deleteCalls != 1 {
				t.Fatalf("Delete() calls = %d, want 1", service.deleteCalls)
			}
		})
	}
}
