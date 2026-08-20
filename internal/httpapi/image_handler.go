package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	image "gopherai/internal/image"
)

const maxImageRequestBodyBytes = 10 << 20 // 10MB

type ImageService interface {
	Recognize(ctx context.Context, data []byte) (*image.Result, error)
}

type ImageHandler struct {
	images ImageService
}

func NewImageHandler(images ImageService) *ImageHandler {
	return &ImageHandler{
		images: images,
	}
}

func (h *ImageHandler) Recognize(c *gin.Context) {

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImageRequestBodyBytes)

	fileHeader, err := c.FormFile("image")
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

	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}

	result, err := h.images.Recognize(c.Request.Context(), data)
	switch {
	case errors.Is(err, image.ErrEmptyImage),
		errors.Is(err, image.ErrInvalidImage),
		errors.Is(err, image.ErrUnsupportedImage):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return

	}
	c.JSON(http.StatusOK, gin.H{
		"class_name": result.ClassName,
	})
}
