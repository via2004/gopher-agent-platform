package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"gopherai/internal/conversation"
	"gopherai/internal/message"
)

const maxMessageRequestBodyBytes = 256 * 1024

type MessageService interface {
	CreateUserMessage(ctx context.Context, userID uint64,
		conversationID uint64, content string) (*message.Message, error)
	List(ctx context.Context,
		userID, conversationID uint64,
		page, pageSize int) ([]*message.Message, error)
}

type messageRequest struct {
	Content string `json:"content"`
}

type MessageHandler struct {
	messages MessageService
}

type messageResponse struct {
	ID        uint64       `json:"id"`
	Role      message.Role `json:"role"`
	Content   string       `json:"content"`
	CreatedAt time.Time    `json:"created_at"`
}

type messageListResponse struct {
	Items          []*messageResponse `json:"items"`
	ConversationID uint64             `json:"conversation_id"`
}

func NewMessageHandler(messages MessageService) *MessageHandler {
	return &MessageHandler{
		messages: messages,
	}
}

func (h *MessageHandler) CreateUserMessage(c *gin.Context) {
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

	newMessage, err := h.messages.CreateUserMessage(c.Request.Context(), userID,
		conversationID, request.Content)
	switch {
	case errors.Is(err, message.ErrInvalidContent),
		errors.Is(err, message.ErrInvalidConversationID),
		errors.Is(err, conversation.ErrInvalidUserID):
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
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	default:
		response := &messageResponse{
			ID:        newMessage.ID,
			Role:      newMessage.Role,
			Content:   newMessage.Content,
			CreatedAt: newMessage.CreatedAt,
		}
		c.JSON(http.StatusCreated, response)
	}
}

func (h *MessageHandler) List(c *gin.Context) {
	userID, ok := checkUserIDValidity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
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

	pageString, pageSizeString := c.DefaultQuery("page", "1"), c.DefaultQuery("page_size", "20")
	page, err := strconv.Atoi(pageString)
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "page must be a positive number",
		})
		return
	}
	pageSize, err := strconv.Atoi(pageSizeString)
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "page size must be a positive number",
		})
		return
	}

	results, err := h.messages.List(c.Request.Context(), userID,
		conversationID, page, pageSize)

	switch {
	case errors.Is(err, conversation.ErrInvalidUserID),
		errors.Is(err, message.ErrInvalidConversationID),
		errors.Is(err, conversation.ErrInvalidPage),
		errors.Is(err, conversation.ErrInvalidPageSize):
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
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	default:
		responses := &messageListResponse{
			Items:          make([]*messageResponse, 0),
			ConversationID: conversationID,
		}
		for _, r := range results {
			if r != nil {
				responses.Items = append(responses.Items, &messageResponse{
					ID:        r.ID,
					Role:      r.Role,
					Content:   r.Content,
					CreatedAt: r.CreatedAt,
				})
			}
		}

		c.JSON(http.StatusOK, responses)
	}
}
