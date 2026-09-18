package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	baidutts "gopherai/internal/platform/baidutts"
	"gopherai/internal/tts"
)

type fakeTTSService struct {
	createCalls int
	createCtx   context.Context
	createdText string
	createTask  *tts.Task
	createErr   error
	getCalls    int
	getCtx      context.Context
	gotTaskID   string
	getTask     *tts.Task
	getErr      error
}

func (f *fakeTTSService) Create(ctx context.Context, text string) (*tts.Task, error) {
	f.createCalls++
	f.createCtx = ctx
	f.createdText = text
	return f.createTask, f.createErr
}

func (f *fakeTTSService) Get(ctx context.Context, taskID string) (*tts.Task, error) {
	f.getCalls++
	f.getCtx = ctx
	f.gotTaskID = taskID
	return f.getTask, f.getErr
}

func newTTSHandlerTestRouter(service TTSService, userID any, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewTTSHandler(service)
	auth := func(c *gin.Context) {
		if authenticated {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}
	router.POST("/api/v1/tts/tasks", auth, handler.Create)
	router.GET("/api/v1/tts/tasks/:id", auth, handler.Get)
	return router
}

func TestTTSHandlerCreate(t *testing.T) {
	want := &tts.Task{ID: "task-1", Status: tts.StatusRunning}
	service := &fakeTTSService{createTask: want}
	router := newTTSHandlerTestRouter(service, uint64(42), true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tts/tasks", strings.NewReader(`{"text":"欢迎使用 GopherAI"}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	var response createTTSTaskResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.TaskID != want.ID || response.Status != want.Status {
		t.Fatalf("response = %#v", response)
	}
	if service.createCalls != 1 || service.createdText != "欢迎使用 GopherAI" {
		t.Fatalf("Create() = %d calls with text %q", service.createCalls, service.createdText)
	}
}

func TestTTSHandlerGetMapsTaskStates(t *testing.T) {
	audioURL := "https://audio.example/result.mp3"
	errorCode := "provider_task_failed"
	tests := []struct {
		name string
		task *tts.Task
	}{
		{name: "running", task: &tts.Task{ID: "task-1", Status: tts.StatusRunning}},
		{name: "succeeded", task: &tts.Task{ID: "task-1", Status: tts.StatusSucceeded, AudioURL: &audioURL}},
		{name: "failed", task: &tts.Task{ID: "task-1", Status: tts.StatusFailed, ErrorCode: &errorCode}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeTTSService{getTask: test.task}
			router := newTTSHandlerTestRouter(service, uint64(42), true)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/tts/tasks/task-1", nil)

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			var response getTTSTaskResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.TaskID != test.task.ID || response.Status != test.task.Status ||
				pointerString(response.AudioURL) != pointerString(test.task.AudioURL) ||
				pointerString(response.ErrorCode) != pointerString(test.task.ErrorCode) {
				t.Fatalf("response = %#v, task = %#v", response, test.task)
			}
			if service.getCalls != 1 || service.gotTaskID != "task-1" {
				t.Fatalf("Get() = %d calls with task ID %q", service.getCalls, service.gotTaskID)
			}
		})
	}
}

func TestTTSHandlerRejectsUnauthorizedRequests(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: "/api/v1/tts/tasks", body: `{"text":"hello"}`},
		{name: "get", method: http.MethodGet, path: "/api/v1/tts/tasks/task-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeTTSService{}
			router := newTTSHandlerTestRouter(service, nil, false)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}

			router.ServeHTTP(recorder, request)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if service.createCalls != 0 || service.getCalls != 0 {
				t.Fatalf("service calls = create %d, get %d", service.createCalls, service.getCalls)
			}
		})
	}
}

func TestTTSHandlerCreateRejectsInvalidAndOversizedBodies(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "invalid JSON", body: `{`, status: http.StatusBadRequest},
		{name: "oversized", body: `{"text":"` + strings.Repeat("x", maxTTSRequestBodyBytes) + `"}`, status: http.StatusRequestEntityTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeTTSService{}
			router := newTTSHandlerTestRouter(service, uint64(42), true)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/tts/tasks", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			assertErrorResponse(t, recorder, test.status, "INVALID_REQUEST")
			if service.createCalls != 0 {
				t.Fatalf("Create() calls = %d, want 0", service.createCalls)
			}
		})
	}
}

func TestTTSHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid text", err: tts.ErrInvalidText, status: http.StatusBadRequest, code: "INVALID_REQUEST"},
		{name: "text too long", err: tts.ErrTextTooLong, status: http.StatusBadRequest, code: "INVALID_REQUEST"},
		{name: "not configured", err: tts.ErrNotConfigured, status: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
		{name: "timeout", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout, code: "TIMEOUT"},
		{name: "token", err: baidutts.ErrTokenFailed, status: http.StatusBadGateway, code: "PROVIDER_ERROR"},
		{name: "provider", err: baidutts.ErrProviderFailed, status: http.StatusBadGateway, code: "PROVIDER_ERROR"},
		{name: "large response", err: baidutts.ErrResponseTooLarge, status: http.StatusBadGateway, code: "PROVIDER_ERROR"},
		{name: "invalid response", err: baidutts.ErrResponseInvalid, status: http.StatusBadGateway, code: "PROVIDER_ERROR"},
		{name: "invalid provider result", err: tts.ErrInvalidProviderResult, status: http.StatusBadGateway, code: "PROVIDER_ERROR"},
		{name: "internal", err: errors.New("unexpected"), status: http.StatusInternalServerError, code: "INTERNAL_SERVER_ERROR"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeTTSService{createErr: test.err}
			router := newTTSHandlerTestRouter(service, uint64(42), true)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/tts/tasks", strings.NewReader(`{"text":"hello"}`))
			request.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(recorder, request)

			assertErrorResponse(t, recorder, test.status, test.code)
		})
	}
}

func TestTTSHandlerGetMapsInvalidTaskID(t *testing.T) {
	service := &fakeTTSService{getErr: tts.ErrInvalidTaskID}
	router := newTTSHandlerTestRouter(service, uint64(42), true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/tts/tasks/%20", nil))
	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
}

func TestTTSHandlerRejectsNilSuccessfulResults(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "create", method: http.MethodPost, path: "/api/v1/tts/tasks", body: `{"text":"hello"}`},
		{name: "get", method: http.MethodGet, path: "/api/v1/tts/tasks/task-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := newTTSHandlerTestRouter(&fakeTTSService{}, uint64(42), true)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(recorder, request)
			assertErrorResponse(t, recorder, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")
		})
	}
}

func TestNewRouterRegistersAuthenticatedTTSRoutes(t *testing.T) {
	audioURL := "https://audio.example/result.mp3"
	service := &fakeTTSService{
		createTask: &tts.Task{ID: "task-1", Status: tts.StatusRunning},
		getTask:    &tts.Task{ID: "task-1", Status: tts.StatusSucceeded, AudioURL: &audioURL},
	}
	verifier := &fakeTokenVerifier{userID: 42}
	ttsLimiter := &fakeRateLimiter{allowed: true}
	router := NewRouter(
		RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), TTS: NewTTSHandler(service)},
		RouterMiddleware{Tokens: verifier, TTSLimiter: ttsLimiter},
	)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tts/tasks", bytes.NewBufferString(`{"text":"hello"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Authorization", "Bearer access-token")
	router.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tts/tasks/task-1", nil)
	getRequest.Header.Set("Authorization", "Bearer access-token")
	router.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}

	if verifier.calls != 2 || service.createCalls != 1 || service.getCalls != 1 || ttsLimiter.calls != 1 {
		t.Fatalf("calls = verify %d, create %d, get %d, TTS limit %d", verifier.calls, service.createCalls, service.getCalls, ttsLimiter.calls)
	}
	if ttsLimiter.identity != "42" {
		t.Fatalf("TTS limiter identity = %q, want 42", ttsLimiter.identity)
	}
}

func TestNewRouterRejectsTTSCreateOverLimitWithoutBlockingQuery(t *testing.T) {
	audioURL := "https://audio.example/result.mp3"
	service := &fakeTTSService{
		getTask: &tts.Task{ID: "task-1", Status: tts.StatusSucceeded, AudioURL: &audioURL},
	}
	verifier := &fakeTokenVerifier{userID: 42}
	ttsLimiter := &fakeRateLimiter{retryAfter: time.Minute}
	router := NewRouter(
		RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), TTS: NewTTSHandler(service)},
		RouterMiddleware{Tokens: verifier, TTSLimiter: ttsLimiter},
	)

	createRecorder := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tts/tasks", strings.NewReader(`{"text":"hello"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Authorization", "Bearer access-token")
	router.ServeHTTP(createRecorder, createRequest)
	assertErrorResponse(t, createRecorder, http.StatusTooManyRequests, "TOO_MANY_REQUESTS")
	if service.createCalls != 0 {
		t.Fatalf("Create() calls = %d, want 0", service.createCalls)
	}

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tts/tasks/task-1", nil)
	getRequest.Header.Set("Authorization", "Bearer access-token")
	router.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRecorder.Code, getRecorder.Body.String())
	}
	if ttsLimiter.calls != 1 || service.getCalls != 1 {
		t.Fatalf("calls = TTS limit %d, get %d; query must not consume create quota", ttsLimiter.calls, service.getCalls)
	}
}

func TestNewRouterProtectsTTSRoutes(t *testing.T) {
	service := &fakeTTSService{}
	verifier := &fakeTokenVerifier{}
	router := NewRouter(
		RouterHandlers{Users: NewUserHandler(&fakeUserRegistrar{}, nil), TTS: NewTTSHandler(service)},
		RouterMiddleware{Tokens: verifier, TTSLimiter: &fakeRateLimiter{allowed: true}},
	)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/tts/tasks", strings.NewReader(`{"text":"hello"}`)),
		httptest.NewRequest(http.MethodGet, "/api/v1/tts/tasks/task-1", nil),
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
	}
	if verifier.calls != 0 || service.createCalls != 0 || service.getCalls != 0 {
		t.Fatalf("calls = verify %d, create %d, get %d", verifier.calls, service.createCalls, service.getCalls)
	}
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
