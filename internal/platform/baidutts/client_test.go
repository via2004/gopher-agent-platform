package baidutts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopherai/internal/tts"
)

func TestNewClientValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{name: "missing API key", config: Config{SecretKey: "secret"}, wantErr: ErrInvalidAPIKey},
		{name: "missing secret key", config: Config{APIKey: "key"}, wantErr: ErrInvalidSecretKey},
		{name: "invalid URL", config: Config{APIKey: "key", SecretKey: "secret", BaseURL: "://bad"}, wantErr: ErrInvalidBaseURL},
		{name: "unsupported URL scheme", config: Config{APIKey: "key", SecretKey: "secret", BaseURL: "ftp://example.com"}, wantErr: ErrInvalidBaseURL},
		{name: "URL with query", config: Config{APIKey: "key", SecretKey: "secret", BaseURL: "https://example.com?x=1"}, wantErr: ErrInvalidBaseURL},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(test.config)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewClient() error = %v, want %v", err, test.wantErr)
			}
			if client != nil {
				t.Fatalf("NewClient() client = %#v, want nil", client)
			}
		})
	}
}

func TestClientCreateUsesBaiduProtocolAndCachesToken(t *testing.T) {
	var tokenCalls, createCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPath:
			tokenCalls++
			if r.Method != http.MethodPost || r.URL.Query().Get("grant_type") != "client_credentials" ||
				r.URL.Query().Get("client_id") != "api-key" || r.URL.Query().Get("client_secret") != "secret-key" {
				t.Errorf("token request = %s %s", r.Method, r.URL.RequestURI())
			}
			writeJSON(t, w, `{"access_token":"access-token","expires_in":3600}`)
		case createPath:
			createCalls++
			if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != "access-token" {
				t.Errorf("create request = %s %s", r.Method, r.URL.RequestURI())
			}
			if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
				t.Errorf("create headers = %#v", r.Header)
			}
			var request createRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode create request: %v", err)
			}
			if len(request.Text) != 1 || request.Text[0] != "欢迎使用 GopherAI" || request.Format != "mp3-16k" ||
				request.Voice != 4194 || request.Language != "zh" || request.Speed != 5 || request.Pitch != 5 ||
				request.Volume != 5 || request.EnableSubtitle != 0 {
				t.Errorf("create request = %#v", request)
			}
			writeJSON(t, w, `{"task_id":"task-1","task_status":"Running"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	for i := 0; i < 2; i++ {
		task, err := client.Create(context.Background(), "  欢迎使用 GopherAI  ")
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if task.ID != "task-1" || task.Status != tts.StatusRunning || task.AudioURL != nil || task.ErrorCode != nil {
			t.Fatalf("Create() task = %#v", task)
		}
	}
	if tokenCalls != 1 || createCalls != 2 {
		t.Fatalf("requests = token %d, create %d; want 1, 2", tokenCalls, createCalls)
	}
}

func TestClientSerializesConcurrentTokenRefresh(t *testing.T) {
	var tokenCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPath:
			tokenCalls.Add(1)
			time.Sleep(10 * time.Millisecond)
			writeJSON(t, w, `{"access_token":"access-token","expires_in":3600}`)
		case createPath:
			writeJSON(t, w, `{"task_id":"task-1","task_status":"Running"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	const callers = 12
	errorsChannel := make(chan error, callers)
	var wait sync.WaitGroup
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := client.Create(context.Background(), "hello")
			errorsChannel <- err
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestClientRefreshesTokenNearExpiry(t *testing.T) {
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPath:
			tokenCalls++
			writeJSON(t, w, fmt.Sprintf(`{"access_token":"access-token-%d","expires_in":30}`, tokenCalls))
		case createPath:
			writeJSON(t, w, `{"task_id":"task-1","task_status":"Running"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)

	for i := 0; i < 2; i++ {
		if _, err := client.Create(context.Background(), "hello"); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if tokenCalls != 2 {
		t.Fatalf("token requests = %d, want 2", tokenCalls)
	}
}

func TestClientGetMapsProviderTaskStates(t *testing.T) {
	tests := []struct {
		name          string
		providerTask  string
		wantStatus    tts.Status
		wantAudioURL  string
		wantErrorCode string
	}{
		{name: "running", providerTask: `{"task_id":"task-1","task_status":"Running"}`, wantStatus: tts.StatusRunning},
		{name: "succeeded", providerTask: `{"task_id":"task-1","task_status":"Success","task_result":{"speech_url":"https://audio.example/result.mp3"}}`, wantStatus: tts.StatusSucceeded, wantAudioURL: "https://audio.example/result.mp3"},
		{name: "failed", providerTask: `{"task_id":"task-1","task_status":"Failure","task_result":{"err_no":336200,"err_msg":"internal error"}}`, wantStatus: tts.StatusFailed, wantErrorCode: "provider_task_failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var queryCalls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tokenPath:
					writeJSON(t, w, `{"access_token":"access-token","expires_in":3600}`)
				case queryPath:
					queryCalls++
					if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != "access-token" {
						t.Errorf("query request = %s %s", r.Method, r.URL.RequestURI())
					}
					var request queryRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Errorf("decode query request: %v", err)
					}
					if len(request.TaskIDs) != 1 || request.TaskIDs[0] != "task-1" {
						t.Errorf("query request = %#v", request)
					}
					writeJSON(t, w, fmt.Sprintf(`{"tasks_info":[%s]}`, test.providerTask))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			task, err := newTestClient(t, server.URL).Get(context.Background(), "  task-1  ")
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if task.ID != "task-1" || task.Status != test.wantStatus {
				t.Fatalf("Get() task = %#v", task)
			}
			if pointerValue(task.AudioURL) != test.wantAudioURL || pointerValue(task.ErrorCode) != test.wantErrorCode {
				t.Fatalf("Get() task = %#v", task)
			}
			if queryCalls != 1 {
				t.Fatalf("query requests = %d, want 1", queryCalls)
			}
		})
	}
}

func TestClientRejectsProviderErrorsAndMalformedResponses(t *testing.T) {
	tests := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		apiStatus   int
		apiBody     string
		wantErr     error
	}{
		{name: "token HTTP status", tokenStatus: http.StatusBadGateway, tokenBody: "bad", wantErr: ErrTokenFailed},
		{name: "token invalid JSON", tokenBody: "{", wantErr: ErrResponseInvalid},
		{name: "token provider error", tokenBody: `{"error":"invalid_client","error_description":"bad credentials"}`, wantErr: ErrTokenFailed},
		{name: "token missing fields", tokenBody: `{}`, wantErr: ErrResponseInvalid},
		{name: "token lifetime too large", tokenBody: `{"access_token":"access-token","expires_in":31536001}`, wantErr: ErrResponseInvalid},
		{name: "create HTTP status", tokenBody: validTokenResponse, apiStatus: http.StatusTooManyRequests, apiBody: "busy", wantErr: ErrProviderFailed},
		{name: "create invalid JSON", tokenBody: validTokenResponse, apiBody: "{", wantErr: ErrResponseInvalid},
		{name: "create provider error", tokenBody: validTokenResponse, apiBody: `{"error_code":336204,"error_msg":"quota exceeded"}`, wantErr: ErrProviderFailed},
		{name: "create missing task", tokenBody: validTokenResponse, apiBody: `{}`, wantErr: ErrResponseInvalid},
		{name: "create unexpected status", tokenBody: validTokenResponse, apiBody: `{"task_id":"task-1","task_status":"Failure"}`, wantErr: ErrResponseInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tokenPath:
					if test.tokenStatus != 0 {
						w.WriteHeader(test.tokenStatus)
					}
					_, _ = w.Write([]byte(test.tokenBody))
				case createPath:
					if test.apiStatus != 0 {
						w.WriteHeader(test.apiStatus)
					}
					_, _ = w.Write([]byte(test.apiBody))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			task, err := newTestClient(t, server.URL).Create(context.Background(), "hello")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, test.wantErr)
			}
			if task != nil {
				t.Fatalf("Create() task = %#v, want nil", task)
			}
		})
	}
}

func TestClientGetRejectsInvalidProviderResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "provider error", body: `{"error_code":336201,"error_msg":"unknown task"}`},
		{name: "missing task", body: `{"tasks_info":[]}`},
		{name: "multiple tasks", body: `{"tasks_info":[{"task_id":"task-1","task_status":"Running"},{"task_id":"task-2","task_status":"Running"}]}`},
		{name: "mismatched task ID", body: `{"tasks_info":[{"task_id":"task-2","task_status":"Running"}]}`},
		{name: "unknown status", body: `{"tasks_info":[{"task_id":"task-1","task_status":"Unknown"}]}`},
		{name: "success without result", body: `{"tasks_info":[{"task_id":"task-1","task_status":"Success"}]}`},
		{name: "success with invalid URL", body: `{"tasks_info":[{"task_id":"task-1","task_status":"Success","task_result":{"speech_url":"not-a-url"}}]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newStaticProviderServer(t, http.StatusOK, test.body)
			defer server.Close()
			task, err := newTestClient(t, server.URL).Get(context.Background(), "task-1")
			wantErr := ErrResponseInvalid
			if test.name == "provider error" {
				wantErr = ErrProviderFailed
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("Get() error = %v, want %v", err, wantErr)
			}
			if task != nil {
				t.Fatalf("Get() task = %#v, want nil", task)
			}
		})
	}
}

func TestClientLimitsProviderResponseSize(t *testing.T) {
	server := newStaticProviderServer(t, http.StatusOK, strings.Repeat("x", maxResponseBytes+1))
	defer server.Close()

	_, err := newTestClient(t, server.URL).Create(context.Background(), "hello")
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Create() error = %v, want %v", err, ErrResponseTooLarge)
	}
}

func TestClientPreservesContextCancellationAndTimeout(t *testing.T) {
	t.Run("caller cancellation", func(t *testing.T) {
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		}))
		defer server.Close()
		client := newTestClientWithTimeout(t, server.URL, time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := client.Create(ctx, "hello")
			result <- err
		}()
		<-started
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Create() error = %v, want context canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Create() did not stop after cancellation")
		}
	})

	t.Run("client timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		client := newTestClientWithTimeout(t, server.URL, 10*time.Millisecond)
		_, err := client.Create(context.Background(), "hello")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Create() error = %v, want deadline exceeded", err)
		}
	})
}

func TestClientErrorsDoNotExposeCredentials(t *testing.T) {
	const apiKey = "sensitive-api-key"
	const secretKey = "sensitive-secret-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: apiKey, SecretKey: secretKey, BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Create(context.Background(), "hello")
	if err == nil {
		t.Fatal("Create() error = nil")
	}
	if strings.Contains(err.Error(), apiKey) || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("Create() error exposes credentials: %v", err)
	}
}

func TestClientPreservesBaseURLPathPrefix(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/proxy" + tokenPath:
			writeJSON(t, w, validTokenResponse)
		case "/proxy" + createPath:
			writeJSON(t, w, `{"task_id":"task-1","task_status":"Running"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/proxy/")
	if _, err := client.Create(context.Background(), "hello"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := strings.Join(paths, ","); got != "/proxy/oauth/2.0/token,/proxy/rpc/2.0/tts/v1/create" {
		t.Fatalf("request paths = %q", got)
	}
}

const validTokenResponse = `{"access_token":"access-token","expires_in":3600}`

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	return newTestClientWithTimeout(t, baseURL, time.Second)
}

func newTestClientWithTimeout(t *testing.T, baseURL string, timeout time.Duration) *Client {
	t.Helper()
	client, err := NewClient(Config{
		APIKey:    "api-key",
		SecretKey: "secret-key",
		BaseURL:   baseURL,
		Timeout:   timeout,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func newStaticProviderServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tokenPath:
			writeJSON(t, w, validTokenResponse)
		case createPath, queryPath:
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
