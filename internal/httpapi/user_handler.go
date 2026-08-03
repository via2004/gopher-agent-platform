package httpapi

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"gopherai/internal/user"
	"net/http"
	"time"
)

type UserHandler struct {
	users UserRegistrar
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func newRegisterRequest() *registerRequest {
	return &registerRequest{}
}

type registerResponse struct {
	ID        uint64    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type UserRegistrar interface {
	Register(ctx context.Context, email, password string) (*user.User, error)
}

func NewUserHandler(users UserRegistrar) *UserHandler {
	return &UserHandler{users: users}
}

func (h *UserHandler) Register(c *gin.Context) {
	userRequest := newRegisterRequest()
	if err := c.ShouldBindJSON(userRequest); err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}

	returnUser, err := h.users.Register(c.Request.Context(), userRequest.Email, userRequest.Password)
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
