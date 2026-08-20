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

	"gopherai/internal/chatjob"
	"gopherai/internal/conversation"
)

type fakeChatJobService struct {
	createCalls          int
	createCtx            context.Context
	createUserID         uint64
	createConversationID uint64
	createContent        string
	created              *chatjob.Job
	createErr            error

	getCalls  int
	getCtx    context.Context
	getUserID uint64
	getJobID  uint64
	got       *chatjob.Job
	getErr    error
}

func (f *fakeChatJobService) Create(
	ctx context.Context,
	userID, conversationID uint64,
	content string,
) (*chatjob.Job, error) {
	f.createCalls++
	f.createCtx = ctx
	f.createUserID = userID
	f.createConversationID = conversationID
	f.createContent = content
	return f.created, f.createErr
}

func (f *fakeChatJobService) GetByID(ctx context.Context, userID, jobID uint64) (*chatjob.Job, error) {
	f.getCalls++
	f.getCtx = ctx
	f.getUserID = userID
	f.getJobID = jobID
	return f.got, f.getErr
}

func newChatJobHandlerTestRouter(service ChatJobService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	setContextUser := func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}
	handler := NewChatJobHandler(service)
	router.POST("/api/v1/conversations/:id/chat-jobs", setContextUser, handler.Create)
	router.GET("/api/v1/chat-jobs/:id", setContextUser, handler.Get)
	return router
}

func TestChatJobHandlerCreate(t *testing.T) {
	created := &chatjob.Job{ID: 41, ConversationID: 9, Content: "question", Status: chatjob.StatusPending}
	service := &fakeChatJobService{created: created}
	router := newChatJobHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat-jobs",
		strings.NewReader(`{"content":"question"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if service.createCalls != 1 || service.createUserID != 7 || service.createConversationID != 9 || service.createContent != "question" {
		t.Fatalf("Create() = %d calls with user %d, conversation %d, content %q",
			service.createCalls, service.createUserID, service.createConversationID, service.createContent)
	}
	if service.createCtx != ctx {
		t.Fatal("handler did not pass request context to Create")
	}
	var response chatJobResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 41 || response.Status != chatjob.StatusPending {
		t.Fatalf("response = %#v", response)
	}
}

func TestChatJobHandlerCreateRejectsRequestBeforeService(t *testing.T) {
	tests := []struct {
		name       string
		userID     any
		setUserID  bool
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "missing user", path: "/api/v1/conversations/9/chat-jobs", body: `{"content":"question"}`, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "invalid conversation", userID: uint64(7), setUserID: true, path: "/api/v1/conversations/nope/chat-jobs", body: `{"content":"question"}`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid JSON", userID: uint64(7), setUserID: true, path: "/api/v1/conversations/9/chat-jobs", body: `{"content":`, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatJobService{}
			router := newChatJobHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.createCalls != 0 {
				t.Fatalf("Create() calls = %d, want 0", service.createCalls)
			}
		})
	}
}

func TestChatJobHandlerCreateMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid content", err: chatjob.ErrInvalidContent, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid conversation", err: conversation.ErrInvalidConversationID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "conversation not found", err: conversation.ErrConversationNotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "unknown failure", err: errors.New("broker unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatJobService{createErr: tt.err}
			router := newChatJobHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat-jobs",
				strings.NewReader(`{"content":"question"}`))
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

func TestChatJobHandlerGetPreservesNullableFields(t *testing.T) {
	createdAt := time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC)
	job := &chatjob.Job{ID: 41, ConversationID: 9, Status: chatjob.StatusPending, CreatedAt: createdAt}
	service := &fakeChatJobService{got: job}
	router := newChatJobHandlerTestRouter(service, uint64(7), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat-jobs/41", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if service.getCalls != 1 || service.getUserID != 7 || service.getJobID != 41 || service.getCtx != ctx {
		t.Fatalf("GetByID() = %d calls with user %d and job %d", service.getCalls, service.getUserID, service.getJobID)
	}
	var response getJobResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 41 || response.Status != chatjob.StatusPending || !response.CreatedAt.Equal(createdAt) {
		t.Fatalf("response = %#v", response)
	}
	if response.AssistantMessageID != nil || response.ErrorCode != nil || response.StartedAt != nil || response.FinishedAt != nil {
		t.Fatalf("nullable fields = assistant:%v error:%v started:%v finished:%v",
			response.AssistantMessageID, response.ErrorCode, response.StartedAt, response.FinishedAt)
	}
	for _, field := range []string{"assistant_message_id", "error_code", "started_at", "finished_at"} {
		if !strings.Contains(recorder.Body.String(), `"`+field+`":null`) {
			t.Fatalf("response does not contain nullable field %q: %s", field, recorder.Body.String())
		}
	}
}

func TestChatJobHandlerGetReturnsTerminalFields(t *testing.T) {
	assistantID := uint64(52)
	startedAt := time.Date(2026, time.August, 18, 9, 0, 1, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)
	service := &fakeChatJobService{got: &chatjob.Job{
		ID:                 41,
		Status:             chatjob.StatusCompleted,
		AssistantMessageID: &assistantID,
		CreatedAt:          startedAt.Add(-time.Second),
		StartedAt:          &startedAt,
		FinishedAt:         &finishedAt,
	}}
	router := newChatJobHandlerTestRouter(service, uint64(7), true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat-jobs/41", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response getJobResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.AssistantMessageID == nil || *response.AssistantMessageID != assistantID ||
		response.ErrorCode != nil || response.StartedAt == nil || response.FinishedAt == nil {
		t.Fatalf("response = %#v", response)
	}
}

func TestChatJobHandlerGetRejectsRequestBeforeService(t *testing.T) {
	tests := []struct {
		name       string
		userID     any
		setUserID  bool
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "missing user", path: "/api/v1/chat-jobs/41", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "invalid job", userID: uint64(7), setUserID: true, path: "/api/v1/chat-jobs/nope", wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatJobService{}
			router := newChatJobHandlerTestRouter(service, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.getCalls != 0 {
				t.Fatalf("GetByID() calls = %d, want 0", service.getCalls)
			}
		})
	}
}

func TestChatJobHandlerGetMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid job", err: chatjob.ErrInvalidJobID, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "job not found", err: chatjob.ErrJobNotFound, wantStatus: http.StatusNotFound, wantCode: "NOT_FOUND"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeChatJobService{getErr: tt.err}
			router := newChatJobHandlerTestRouter(service, uint64(7), true)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/chat-jobs/41", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if service.getCalls != 1 {
				t.Fatalf("GetByID() calls = %d, want 1", service.getCalls)
			}
		})
	}
}

func TestNewRouterRegistersChatJobRoutes(t *testing.T) {
	createdAt := time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC)
	service := &fakeChatJobService{
		created: &chatjob.Job{ID: 41, Status: chatjob.StatusPending},
		got:     &chatjob.Job{ID: 41, Status: chatjob.StatusPending, CreatedAt: createdAt},
	}
	verifier := &fakeTokenVerifier{userID: 7}
	router := NewRouter(
		NewUserHandler(&fakeUserRegistrar{}, nil),
		nil,
		nil,
		nil,
		nil,
		NewChatJobHandler(service),
		nil,
		verifier,
		&fakeChatRateLimiter{allowed: true},
	)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/9/chat-jobs",
		strings.NewReader(`{"content":"question"}`))
	createReq.Header.Set("Authorization", "Bearer access-token")
	createReq.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, want %d; body = %s", createRecorder.Code, http.StatusAccepted, createRecorder.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/chat-jobs/41", nil)
	getReq.Header.Set("Authorization", "Bearer access-token")
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}
	if service.createCalls != 1 || service.getCalls != 1 {
		t.Fatalf("service calls: Create=%d GetByID=%d, want 1 each", service.createCalls, service.getCalls)
	}
}
