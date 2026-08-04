package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"gopherai/internal/conversation"
)

type ConversationService interface {
	Create(ctx context.Context, userID uint64, title string) (*conversation.Conversation, error)
}

type ConversationHandler struct {
	conversations ConversationService
}

type conversationRequest struct {
	Title string `json:"title"`
}

type conversationResponse struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

func NewConversationHandler(conversations ConversationService) *ConversationHandler {
	return &ConversationHandler{
		conversations: conversations,
	}
}

func (h *ConversationHandler) Create(c *gin.Context) {
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

	request := &conversationRequest{}
	if err := c.ShouldBindJSON(request); err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "request is invalid",
		})
		return
	}

	newConversation, err := h.conversations.Create(c.Request.Context(), userID, request.Title)

	switch {
	case errors.Is(err, conversation.ErrInvalidTitle),
		errors.Is(err, conversation.ErrInvalidUserID):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "request is invalid",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	response := &conversationResponse{
		ID:        newConversation.ID,
		Title:     newConversation.Title,
		CreatedAt: newConversation.CreatedAt,
	}
	c.JSON(http.StatusCreated, response)
}
