package httpapi

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"gopherai/internal/chat"
	"gopherai/internal/conversation"
	"gopherai/internal/llm"
	"gopherai/internal/message"
	"net/http"
	"strconv"
	"time"
)

const streamMaxDuration = 2 * time.Minute

type ChatService interface {
	Chat(ctx context.Context, userID uint64,
		conversationID uint64, content string) (*chat.Result, error)
	ChatStreaming(ctx context.Context, userID uint64,
		conversationID uint64, content string,
		onDelta func(string) error) (*chat.Result, error)
}

type ChatHandler struct {
	chats ChatService
}

func NewChatHandler(chats ChatService) *ChatHandler {
	return &ChatHandler{
		chats: chats,
	}
}

// 普通chat的入口函数,
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

	controller := http.NewResponseController(c.Writer)
	// 清除服务器默认的 HTTP 写超时，允许等待模型；下面的 context 仍限制两分钟，客户端断开也会取消请求。
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	chatCtx, cancel := context.WithTimeout(c.Request.Context(), streamMaxDuration)
	defer cancel()

	receiveMessage, err := h.chats.Chat(chatCtx, userID, conversationID, request.Content)
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
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, &errorResponse{
			Code:    "TIMEOUT",
			Message: "response timed out",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	default:
		c.JSON(http.StatusOK, gin.H{
			"id":            receiveMessage.ID,
			"role":          receiveMessage.Role,
			"content":       receiveMessage.Content,
			"created_at":    receiveMessage.CreatedAt,
			"model":         receiveMessage.Model,
			"input_tokens":  receiveMessage.InputTokens,
			"output_tokens": receiveMessage.OutputTokens,
			"total_tokens":  receiveMessage.TotalTokens,
		})
	}
}

// 实际流式Chat的接口
func (h *ChatHandler) ChatStreaming(c *gin.Context) {
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
	controller := http.NewResponseController(c.Writer)

	// 不设置截止时间
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	chatCtx, cancel := context.WithTimeout(c.Request.Context(), streamMaxDuration)
	defer cancel()

	// 调用ChatStreaming, 同时封装了onDelta
	receiveMessage, err := h.chats.ChatStreaming(
		chatCtx, userID,
		conversationID, request.Content, func(delta string) error {
			c.SSEvent("delta", gin.H{
				"delta": delta,
			})
			c.Writer.Flush()

			return chatCtx.Err()
		})

	switch {
	case errors.Is(err, message.ErrInvalidContent),
		errors.Is(err, conversation.ErrInvalidUserID),
		errors.Is(err, message.ErrInvalidConversationID),
		errors.Is(err, message.ErrInvalidLimit):
		c.SSEvent("error", gin.H{
			"code":    "INVALID_REQUEST",
			"message": "input is invalid",
		})
	case errors.Is(err, conversation.ErrConversationNotFound):
		c.SSEvent("error", gin.H{
			"code":    "NOT_FOUND",
			"message": "not found the conversation",
		})
	case errors.Is(err, llm.ErrNotConfigured):
		c.SSEvent("error", gin.H{
			"code":    "SERVICE_UNAVAILABLE",
			"message": "service is unavailable",
		})
	case errors.Is(err, llm.ErrResponseNotCompleted):
		c.SSEvent("error", gin.H{
			"code":    "RESPONSE_NOT_COMPLETED",
			"message": "response is not completed",
		})
	case errors.Is(err, context.DeadlineExceeded):
		c.SSEvent("error", gin.H{
			"code":    "TIMEOUT",
			"message": "response time out",
		})
	case errors.Is(err, llm.ErrResponseFailed):
		c.SSEvent("error", gin.H{
			"code":    "RESPONSE_FAILED",
			"message": "response failed",
		})
	case errors.Is(err, llm.ErrOnDeltaMissed):
		c.SSEvent("error", gin.H{
			"code":    "ONDELTA_MISSED",
			"message": "on delta is necessary",
		})
	case err != nil:
		c.SSEvent("error", gin.H{
			"code":    "INTERNAL_SERVER_ERROR",
			"message": "internal server error",
		})
	default:
		c.SSEvent("done", gin.H{
			"id":            receiveMessage.ID,
			"role":          receiveMessage.Role,
			"created_at":    receiveMessage.CreatedAt,
			"model":         receiveMessage.Model,
			"input_tokens":  receiveMessage.InputTokens,
			"output_tokens": receiveMessage.OutputTokens,
			"total_tokens":  receiveMessage.TotalTokens,
		})
	}
	c.Writer.Flush()
}
