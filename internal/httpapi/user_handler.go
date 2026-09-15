package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"gopherai/internal/emailverification"
	"gopherai/internal/user"
)

type UserHandler struct {
	users  UserService
	tokens TokenIssuer
}

type registerAndLoginRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	VerificationCode string `json:"verification_code"`
}

func newRegisterAndLoginRequest() *registerAndLoginRequest {
	return &registerAndLoginRequest{}
}

type registerResponse struct {
	ID        uint64    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type meResponse struct {
	ID        uint64    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type UserService interface {
	Register(ctx context.Context, email, password, verificationCode string) (*user.User, error)
	Login(ctx context.Context, email, password string) (*user.User, error)
	GetByID(ctx context.Context, userID uint64) (*user.User, error)
}

type TokenIssuer interface {
	Issue(userID uint64) (string, error)
}

func NewUserHandler(users UserService, tokens TokenIssuer) *UserHandler {
	return &UserHandler{
		users:  users,
		tokens: tokens,
	}
}

func (h *UserHandler) Register(c *gin.Context) {
	userRequest := newRegisterAndLoginRequest()
	if err := c.ShouldBindJSON(userRequest); err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}

	returnUser, err := h.users.Register(
		c.Request.Context(),
		userRequest.Email,
		userRequest.Password,
		userRequest.VerificationCode,
	)
	switch {
	case errors.Is(err, user.ErrInvalidEmail), errors.Is(err, user.ErrInvalidPassword):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "EMAIL_OR_PASSWORD_INVALID", Message: "email or password invalid",
		})
	case errors.Is(err, user.ErrPasswordHash):
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
	case errors.Is(err, user.ErrEmailAlreadyExists):
		c.JSON(http.StatusConflict, &errorResponse{
			Code: "EMAIL_ALREADY_EXISTS", Message: "email already registered",
		})
	case errors.Is(err, emailverification.ErrInvalidCode),
		errors.Is(err, emailverification.ErrCodeInvalidOrExpired),
		errors.Is(err, emailverification.ErrTooManyAttempts):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_VERIFICATION_CODE", Message: "verification code is invalid or expired",
		})
	case errors.Is(err, emailverification.ErrNotConfigured),
		errors.Is(err, emailverification.ErrStoreFailed):
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable",
		})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, &errorResponse{
			Code: "TIMEOUT", Message: "request timed out",
		})
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
	default:
		c.JSON(http.StatusCreated, &registerResponse{
			ID:        returnUser.ID,
			Email:     returnUser.Email,
			CreatedAt: returnUser.CreatedAt,
		})
	}
}

func (h *UserHandler) Login(c *gin.Context) {
	userRequest := newRegisterAndLoginRequest()
	if err := c.ShouldBindJSON(userRequest); err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}

	returnUser, err := h.users.Login(c.Request.Context(), userRequest.Email, userRequest.Password)
	switch {
	case errors.Is(err, user.ErrInvalidEmail), errors.Is(err, user.ErrInvalidPassword):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	case errors.Is(err, user.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "INVALID_CREDENTIALS", Message: "email or password error",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	accessToken, err := h.tokens.Issue(returnUser.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, loginResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	})
}

func (h *UserHandler) Me(c *gin.Context) {
	value, exists := c.Get(userIDContextKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}

	userID, ok := value.(uint64)
	if !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}

	currUser, err := h.users.GetByID(c.Request.Context(), userID)
	switch {
	case errors.Is(err, user.ErrUserNotFound):
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, &meResponse{
		ID:        currUser.ID,
		Email:     currUser.Email,
		CreatedAt: currUser.CreatedAt,
	})
}
