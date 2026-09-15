package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"gopherai/internal/emailverification"
)

const maxEmailVerificationRequestBodyBytes = 16 << 10 // 16 KiB

type EmailVerificationService interface {
	Send(ctx context.Context, email string) error
}

type EmailVerificationHandler struct {
	verification EmailVerificationService
}

type sendEmailVerificationRequest struct {
	Email string `json:"email"`
}

func NewEmailVerificationHandler(verification EmailVerificationService) *EmailVerificationHandler {
	return &EmailVerificationHandler{verification: verification}
}

// Send 接收公开的验证码请求；响应中不会返回验证码。
func (h *EmailVerificationHandler) Send(c *gin.Context) {
	if h == nil || h.verification == nil {
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable",
		})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxEmailVerificationRequestBodyBytes)
	request := &sendEmailVerificationRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
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

	err := h.verification.Send(c.Request.Context(), request.Email)
	switch {
	case errors.Is(err, emailverification.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable",
		})
	case errors.Is(err, emailverification.ErrInvalidEmail):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "email is invalid",
		})
	case errors.Is(err, emailverification.ErrSendTooFrequent):
		c.JSON(http.StatusTooManyRequests, &errorResponse{
			Code: "TOO_MANY_REQUESTS", Message: "verification code was sent too recently",
		})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, &errorResponse{
			Code: "TIMEOUT", Message: "request timed out",
		})
	case errors.Is(err, emailverification.ErrStoreFailed),
		errors.Is(err, emailverification.ErrSendFailed),
		errors.Is(err, emailverification.ErrGenerateCodeFailed):
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable",
		})
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
	default:
		c.JSON(http.StatusAccepted, gin.H{
			"message": "verification code accepted",
		})
	}
}
