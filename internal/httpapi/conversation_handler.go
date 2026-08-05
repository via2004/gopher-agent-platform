package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"gopherai/internal/conversation"
)

type ConversationService interface {
	Create(ctx context.Context, userID uint64, title string) (*conversation.Conversation, error)
	List(ctx context.Context, userID uint64, page, pageSize int) ([]*conversation.Conversation, error)
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

type listResponse struct {
	Conversations []*conversationResponse `json:"items"`
	Page          int                     `json:"page"`
	PageSize      int                     `json:"page_size"`
}

func newListResponse() *listResponse {
	return &listResponse{Conversations: make([]*conversationResponse, 0)}
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

func (h *ConversationHandler) List(c *gin.Context) {
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

	result, err := h.conversations.List(c.Request.Context(), userID, page, pageSize)
	switch {
	case errors.Is(err, conversation.ErrInvalidPage),
		errors.Is(err, conversation.ErrInvalidPageSize),
		errors.Is(err, conversation.ErrInvalidUserID):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "input is invalid",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}
	response := newListResponse()
	for _, r := range result {
		response.Conversations = append(response.Conversations,
			&conversationResponse{
				ID:        r.ID,
				Title:     r.Title,
				CreatedAt: r.CreatedAt,
			})
	}
	response.Page = page
	response.PageSize = pageSize

	c.JSON(http.StatusOK, response)
}
