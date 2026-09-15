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

	"gopherai/internal/emailverification"
	"gopherai/internal/user"
)

type fakeUserRegistrar struct {
	registerCalls    int
	loginCalls       int
	ctx              context.Context
	email            string
	password         string
	verificationCode string
	registered       *user.User
	loggedIn         *user.User
	err              error
	getByIDCalls     int
	queriedUserID    uint64
	queriedUser      *user.User
	queryErr         error
}

func (f *fakeUserRegistrar) Register(ctx context.Context, email, password, verificationCode string) (*user.User, error) {
	f.registerCalls++
	f.ctx = ctx
	f.email = email
	f.password = password
	f.verificationCode = verificationCode
	return f.registered, f.err
}

func (f *fakeUserRegistrar) Login(ctx context.Context, email string, password string) (*user.User, error) {
	f.loginCalls++
	f.ctx = ctx
	f.email = email
	f.password = password
	return f.loggedIn, f.err
}

func (f *fakeUserRegistrar) GetByID(ctx context.Context, userID uint64) (*user.User, error) {
	f.getByIDCalls++
	f.ctx = ctx
	f.queriedUserID = userID
	return f.queriedUser, f.queryErr
}

type fakeTokenIssuer struct {
	calls  int
	userID uint64
	token  string
	err    error
}

func (f *fakeTokenIssuer) Issue(userID uint64) (string, error) {
	f.calls++
	f.userID = userID
	return f.token, f.err
}

func newUserHandlerTestRouter(registrar UserService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/register", NewUserHandler(registrar, nil).Register)
	return router
}

func newLoginHandlerTestRouter(registrar UserService, tokens TokenIssuer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/auth/login", NewUserHandler(registrar, tokens).Login)
	return router
}

func newMeHandlerTestRouter(users UserService, userID any, setUserID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/users/me", func(c *gin.Context) {
		if setUserID {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewUserHandler(users, nil).Me)
	return router
}

func TestUserHandlerRegister(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	registrar := &fakeUserRegistrar{registered: &user.User{
		ID:           42,
		Email:        "user@example.com",
		PasswordHash: "must-not-be-returned",
		CreatedAt:    createdAt,
	}}
	router := newUserHandlerTestRouter(registrar)

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"User@Example.COM","password":"password123"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if registrar.registerCalls != 1 {
		t.Fatalf("Register() calls = %d, want 1", registrar.registerCalls)
	}
	if registrar.ctx != ctx {
		t.Fatal("handler did not pass the request context to Register")
	}
	if registrar.email != "User@Example.COM" {
		t.Errorf("Register() email = %q, want %q", registrar.email, "User@Example.COM")
	}
	if registrar.password != "password123" {
		t.Errorf("Register() password = %q, want %q", registrar.password, "password123")
	}
	if registrar.verificationCode != "" {
		t.Errorf("Register() verification code = %q, want empty", registrar.verificationCode)
	}

	var response registerResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Email != "user@example.com" || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("must-not-be-returned")) ||
		bytes.Contains(recorder.Body.Bytes(), []byte("password")) {
		t.Fatalf("response leaked password data: %s", recorder.Body.String())
	}
}

func TestUserHandlerRegisterRejectsInvalidJSON(t *testing.T) {
	registrar := &fakeUserRegistrar{}
	router := newUserHandlerTestRouter(registrar)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if registrar.registerCalls != 0 {
		t.Fatalf("Register() calls = %d, want 0", registrar.registerCalls)
	}
}

func TestUserHandlerRegisterMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid email", err: user.ErrInvalidEmail, wantStatus: http.StatusBadRequest, wantCode: "EMAIL_OR_PASSWORD_INVALID"},
		{name: "invalid password", err: user.ErrInvalidPassword, wantStatus: http.StatusBadRequest, wantCode: "EMAIL_OR_PASSWORD_INVALID"},
		{name: "duplicate email", err: user.ErrEmailAlreadyExists, wantStatus: http.StatusConflict, wantCode: "EMAIL_ALREADY_EXISTS"},
		{name: "password hash failure", err: user.ErrPasswordHash, wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
		{name: "invalid verification code", err: emailverification.ErrInvalidCode, wantStatus: http.StatusBadRequest, wantCode: "INVALID_VERIFICATION_CODE"},
		{name: "expired verification code", err: emailverification.ErrCodeInvalidOrExpired, wantStatus: http.StatusBadRequest, wantCode: "INVALID_VERIFICATION_CODE"},
		{name: "too many verification attempts", err: emailverification.ErrTooManyAttempts, wantStatus: http.StatusBadRequest, wantCode: "INVALID_VERIFICATION_CODE"},
		{name: "verification unavailable", err: emailverification.ErrStoreFailed, wantStatus: http.StatusServiceUnavailable, wantCode: "SERVICE_UNAVAILABLE"},
		{name: "verification timeout", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantCode: "TIMEOUT"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registrar := &fakeUserRegistrar{err: tt.err}
			router := newUserHandlerTestRouter(registrar)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
				strings.NewReader(`{"email":"user@example.com","password":"password123"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if registrar.registerCalls != 1 {
				t.Fatalf("Register() calls = %d, want 1", registrar.registerCalls)
			}
		})
	}
}

func TestUserHandlerRegisterPassesVerificationCode(t *testing.T) {
	registrar := &fakeUserRegistrar{registered: &user.User{ID: 42, Email: "user@example.com"}}
	router := newUserHandlerTestRouter(registrar)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"user@example.com","password":"password123","verification_code":"123456"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if registrar.verificationCode != "123456" {
		t.Fatalf("Register() verification code = %q, want 123456", registrar.verificationCode)
	}
}

func TestUserHandlerLogin(t *testing.T) {
	registrar := &fakeUserRegistrar{loggedIn: &user.User{ID: 42, Email: "user@example.com"}}
	tokens := &fakeTokenIssuer{token: "access-token"}
	router := newLoginHandlerTestRouter(registrar, tokens)

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"User@Example.COM","password":"password123"}`),
	).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if registrar.loginCalls != 1 {
		t.Fatalf("Login() calls = %d, want 1", registrar.loginCalls)
	}
	if registrar.ctx != ctx {
		t.Fatal("handler did not pass the request context to Login")
	}
	if registrar.email != "User@Example.COM" || registrar.password != "password123" {
		t.Errorf("Login() credentials = (%q, %q), want (%q, %q)", registrar.email, registrar.password, "User@Example.COM", "password123")
	}
	if tokens.calls != 1 || tokens.userID != 42 {
		t.Fatalf("Issue() = %d calls with user ID %d, want 1 call with user ID 42", tokens.calls, tokens.userID)
	}

	var response loginResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.AccessToken != "access-token" || response.TokenType != "Bearer" {
		t.Errorf("response = %#v, want access token and Bearer token type", response)
	}
}

func TestUserHandlerLoginRejectsInvalidJSON(t *testing.T) {
	registrar := &fakeUserRegistrar{}
	tokens := &fakeTokenIssuer{}
	router := newLoginHandlerTestRouter(registrar, tokens)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if registrar.loginCalls != 0 || tokens.calls != 0 {
		t.Fatalf("calls after invalid JSON: Login = %d, Issue = %d; want both 0", registrar.loginCalls, tokens.calls)
	}
}

func TestUserHandlerLoginMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid email", err: user.ErrInvalidEmail, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid password", err: user.ErrInvalidPassword, wantStatus: http.StatusBadRequest, wantCode: "INVALID_REQUEST"},
		{name: "invalid credentials", err: user.ErrInvalidCredentials, wantStatus: http.StatusUnauthorized, wantCode: "INVALID_CREDENTIALS"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registrar := &fakeUserRegistrar{err: tt.err}
			tokens := &fakeTokenIssuer{}
			router := newLoginHandlerTestRouter(registrar, tokens)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
				strings.NewReader(`{"email":"user@example.com","password":"password123"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if registrar.loginCalls != 1 {
				t.Fatalf("Login() calls = %d, want 1", registrar.loginCalls)
			}
			if tokens.calls != 0 {
				t.Fatalf("Issue() calls = %d, want 0", tokens.calls)
			}
		})
	}
}

func TestUserHandlerLoginMapsTokenIssueError(t *testing.T) {
	registrar := &fakeUserRegistrar{loggedIn: &user.User{ID: 42}}
	tokens := &fakeTokenIssuer{err: errors.New("token signing failed")}
	router := newLoginHandlerTestRouter(registrar, tokens)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(`{"email":"user@example.com","password":"password123"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	assertErrorResponse(t, recorder, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")
	if tokens.calls != 1 || tokens.userID != 42 {
		t.Fatalf("Issue() = %d calls with user ID %d, want 1 call with user ID 42", tokens.calls, tokens.userID)
	}
}

func TestUserHandlerMe(t *testing.T) {
	createdAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	users := &fakeUserRegistrar{queriedUser: &user.User{
		ID:           42,
		Email:        "user@example.com",
		PasswordHash: "must-not-be-returned",
		CreatedAt:    createdAt,
	}}
	router := newMeHandlerTestRouter(users, uint64(42), true)
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request-id"), "request-1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if users.getByIDCalls != 1 || users.queriedUserID != 42 {
		t.Fatalf("GetByID() = %d calls with user ID %d, want 1 call with user ID 42", users.getByIDCalls, users.queriedUserID)
	}
	if users.ctx != ctx {
		t.Fatal("handler did not pass the request context to GetByID")
	}

	var response meResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ID != 42 || response.Email != "user@example.com" || !response.CreatedAt.Equal(createdAt) {
		t.Errorf("response = %#v", response)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("must-not-be-returned")) ||
		bytes.Contains(recorder.Body.Bytes(), []byte("password")) {
		t.Fatalf("response leaked password data: %s", recorder.Body.String())
	}
}

func TestUserHandlerMeRejectsMissingOrInvalidContextUserID(t *testing.T) {
	tests := []struct {
		name      string
		userID    any
		setUserID bool
	}{
		{name: "missing user ID"},
		{name: "wrong user ID type", userID: "42", setUserID: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &fakeUserRegistrar{}
			router := newMeHandlerTestRouter(users, tt.userID, tt.setUserID)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")
			if users.getByIDCalls != 0 {
				t.Fatalf("GetByID() calls = %d, want 0", users.getByIDCalls)
			}
		})
	}
}

func TestUserHandlerMeMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "user not found", err: user.ErrUserNotFound, wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
		{name: "unknown failure", err: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_SERVER_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &fakeUserRegistrar{queryErr: tt.err}
			router := newMeHandlerTestRouter(users, uint64(42), true)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			assertErrorResponse(t, recorder, tt.wantStatus, tt.wantCode)
			if users.getByIDCalls != 1 || users.queriedUserID != 42 {
				t.Fatalf("GetByID() = %d calls with user ID %d, want 1 call with user ID 42", users.getByIDCalls, users.queriedUserID)
			}
		})
	}
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, wantStatus, recorder.Body.String())
	}

	var response errorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != wantCode {
		t.Errorf("error code = %q, want %q", response.Code, wantCode)
	}
	if response.Message == "" {
		t.Error("error message is empty")
	}
}
