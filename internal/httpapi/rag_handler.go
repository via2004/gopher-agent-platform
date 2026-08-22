package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"gopherai/internal/rag"
)

const maxRAGRequestBodyBytes = 6 << 20 // 6MB

type RAGService interface {
	Upload(
		ctx context.Context,
		userID uint64,
		filename string,
		content []byte,
	) (*rag.Document, error)
}

type RAGHandler struct {
	rag RAGService
}

type ragUploadResponse struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Chunks   int    `json:"chunks"`
}

func NewRAGHandler(rag RAGService) *RAGHandler {
	return &RAGHandler{
		rag: rag,
	}
}

func (h *RAGHandler) Upload(c *gin.Context) {
	userID, ok := checkUserIDValidity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxRAGRequestBodyBytes)
	fileHeader, err := c.FormFile("document")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, &errorResponse{
				Code: "INVALID_REQUEST", Message: "request is too large",
			})
			return
		}

		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}
	filename := fileHeader.Filename

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}

	document, err := h.rag.Upload(c.Request.Context(), userID, filename, content)
	switch {
	case errors.Is(err, rag.ErrInvalidUserID),
		errors.Is(err, rag.ErrInvalidContent),
		errors.Is(err, rag.ErrUnsupportedDocumentType),
		errors.Is(err, rag.ErrInvalidEncoding):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	case errors.Is(err, rag.ErrDocumentTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, &errorResponse{
			Code: "REQUEST_TOO_LARGE", Message: "request message is too large",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	case document == nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	default:
		c.JSON(http.StatusCreated, &ragUploadResponse{
			Filename: document.Filename,
			Size:     document.Size,
			Chunks:   len(document.Chunks),
		})
		return
	}
}
