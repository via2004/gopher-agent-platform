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
	image "gopherai/internal/image"
)

type fakeImageService struct {
	result *image.Result
	err    error
	calls  int
}

func (f *fakeImageService) Recognize(context.Context, []byte) (*image.Result, error) {
	f.calls++
	return f.result, f.err
}

func imageRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("image", "test.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/images/recognitions", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestImageHandlerRecognize(t *testing.T) {
	tests := []struct {
		name       string
		serviceErr error
		status     int
		code       string
	}{
		{name: "invalid image", serviceErr: image.ErrInvalidImage, status: http.StatusBadRequest, code: "INVALID_REQUEST"},
		{name: "classifier failure", serviceErr: errors.New("model failed"), status: http.StatusInternalServerError, code: "INTERNAL_SERVER_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeImageService{err: tt.serviceErr}
			router := gin.New()
			router.POST("/images/recognitions", NewImageHandler(service).Recognize)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, imageRequest(t, []byte("image")))
			assertErrorResponse(t, recorder, tt.status, tt.code)
			if service.calls != 1 {
				t.Fatalf("Recognize calls = %d, want 1", service.calls)
			}
		})
	}
}

func TestImageHandlerRecognizeSuccess(t *testing.T) {
	service := &fakeImageService{result: &image.Result{ClassName: "cat"}}
	router := gin.New()
	router.POST("/images/recognitions", NewImageHandler(service).Recognize)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, imageRequest(t, []byte("image")))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"class_name":"cat"`)) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestImageHandlerRejectsMissingFile(t *testing.T) {
	router := gin.New()
	router.POST("/images/recognitions", NewImageHandler(&fakeImageService{}).Recognize)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/images/recognitions", nil))
	assertErrorResponse(t, recorder, http.StatusBadRequest, "INVALID_REQUEST")
}
