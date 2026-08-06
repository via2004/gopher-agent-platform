package httpapi

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"gopherai/internal/conversation"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	"net/http"
	"strconv"
)

type ChatService interface {
	ReceiveAndResponse(ctx context.Context, userID uint64,
		conversationID uint64, content string) (*message.Message, error)
}

type ChatHandler struct {
	chats ChatService
}

func NewChatHandler(chats ChatService) *ChatHandler {
	return &ChatHandler{
		chats: chats,
	}
}

func (h *ChatHandler) Chat(c *gin.Context) {
	userID, ok := checkUserIDValidity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxMessageRequestBodyBytes)
	request := &messageRequest{}
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

	idString := c.Param("id")
	conversationID, err := strconv.ParseUint(idString, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "input is invalid",
		})
		return
	}

	receiveMessage, err := h.chats.ReceiveAndResponse(c.Request.Context(), userID, conversationID, request.Content)
	switch {
	case errors.Is(err, message.ErrInvalidContent),
		errors.Is(err, conversation.ErrInvalidUserID),
		errors.Is(err, message.ErrInvalidConversationID),
		errors.Is(err, message.ErrInvalidLimit):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "input is invalid",
		})
		return
	case errors.Is(err, conversation.ErrConversationNotFound):
		c.JSON(http.StatusNotFound, &errorResponse{
			Code:    "NOT_FOUND",
			Message: "not found the conversation",
		})
		return
	case errors.Is(err, llm.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code:    "SERVICE_UNAVAILABLE",
			Message: "service is unavailable",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	default:
		response := &messageResponse{
			ID:        receiveMessage.ID,
			Role:      receiveMessage.Role,
			Content:   receiveMessage.Content,
			CreatedAt: receiveMessage.CreatedAt,
		}
		c.JSON(http.StatusOK, response)
	}
}
