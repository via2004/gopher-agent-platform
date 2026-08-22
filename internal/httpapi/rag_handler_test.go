package httpapi

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gopherai/internal/rag"
)

type fakeRAGService struct {
	userID   uint64
	filename string
	content  []byte
	result   *rag.Document
	err      error
	calls    int
}

func (f *fakeRAGService) Upload(_ context.Context, userID uint64, filename string, content []byte) (*rag.Document, error) {
	f.calls++
	f.userID = userID
	f.filename = filename
	f.content = append([]byte(nil), content...)
	return f.result, f.err
}

func newRAGHandlerTestRouter(service RAGService, userID any, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/rag/documents", func(c *gin.Context) {
		if authenticated {
			c.Set(userIDContextKey, userID)
		}
		c.Next()
	}, NewRAGHandler(service).Upload)
	return router
}

func ragDocumentRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/rag/documents", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func TestRAGHandlerUpload(t *testing.T) {
	service := &fakeRAGService{result: &rag.Document{
		Filename: "notes.md",
		Size:     5,
		Chunks:   []rag.Chunk{{Index: 0}},
	}}
	router := newRAGHandlerTestRouter(service, uint64(42), true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, ragDocumentRequest(t, "notes.md", []byte("hello")))

	if recorder.Code != http.StatusCreated || !bytes.Contains(recorder.Body.Bytes(), []byte(`"chunks":1`)) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if service.calls != 1 || service.userID != 42 || service.filename != "notes.md" || string(service.content) != "hello" {
		t.Fatalf("Upload() args = calls:%d user:%d filename:%q content:%q", service.calls, service.userID, service.filename, service.content)
	}
}

func TestRAGHandlerRejectsUnauthorizedAndMissingDocument(t *testing.T) {
	service := &fakeRAGService{}
	router := newRAGHandlerTestRouter(service, nil, false)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/rag/documents", nil))
	assertErrorResponse(t, recorder, http.StatusUnauthorized, "UNAUTHORIZED")

	router = newRAGHandlerTestRouter(service, uint64(42), true)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/rag/documents", nil))
	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
	if service.calls != 0 {
		t.Fatalf("Upload() calls = %d, want 0", service.calls)
	}
}

func TestRAGHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "invalid type", err: rag.ErrUnsupportedDocumentType, status: http.StatusBadRequest, code: "INVALID_REQUEST"},
		{name: "too large", err: rag.ErrDocumentTooLarge, status: http.StatusRequestEntityTooLarge, code: "REQUEST_TOO_LARGE"},
		{name: "dependency", err: errors.New("redis unavailable"), status: http.StatusInternalServerError, code: "INTERNAL_SERVER_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeRAGService{err: tt.err}
			router := newRAGHandlerTestRouter(service, uint64(42), true)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, ragDocumentRequest(t, "notes.md", []byte("hello")))
			assertErrorResponse(t, recorder, tt.status, tt.code)
		})
	}
}

func TestRAGHandlerRejectsOversizedMultipartRequest(t *testing.T) {
	service := &fakeRAGService{}
	router := newRAGHandlerTestRouter(service, uint64(42), true)
	recorder := httptest.NewRecorder()
	content := bytes.Repeat([]byte{'x'}, maxRAGRequestBodyBytes)
	router.ServeHTTP(recorder, ragDocumentRequest(t, "large.txt", content))
	assertErrorResponse(t, recorder, http.StatusRequestEntityTooLarge, "INVALID_REQUEST")
	if service.calls != 0 {
		t.Fatalf("Upload() calls = %d, want 0", service.calls)
	}
}

func TestRAGHandlerHandlesNilSuccessfulResult(t *testing.T) {
	service := &fakeRAGService{}
	router := newRAGHandlerTestRouter(service, uint64(42), true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, ragDocumentRequest(t, "notes.md", []byte("hello")))
	assertErrorResponse(t, recorder, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR")
}
