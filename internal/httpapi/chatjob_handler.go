package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gopherai/internal/chatjob"
	"gopherai/internal/conversation"
	"gopherai/internal/platform/postgresql"
	"gopherai/internal/platform/rabbitmq"
)

type ChatJobService interface {
	Create(ctx context.Context, userID, conversationID uint64, content string) (*chatjob.Job, error)
	GetByID(ctx context.Context, userID, jobID uint64) (*chatjob.Job, error)
}

type ChatJobHandler struct {
	jobs ChatJobService
}

func NewChatJobHandler(jobs ChatJobService) *ChatJobHandler {
	return &ChatJobHandler{
		jobs: jobs,
	}
}

type chatJobResponse struct {
	ID     uint64         `json:"id"`
	Status chatjob.Status `json:"status"`
}

type getJobResponse struct {
	ID                 uint64         `json:"id"`
	Status             chatjob.Status `json:"status"`
	AssistantMessageID *uint64        `json:"assistant_message_id"`
	ErrorCode          *string        `json:"error_code"`
	CreatedAt          time.Time      `json:"created_at"`
	StartedAt          *time.Time     `json:"started_at"`
	FinishedAt         *time.Time     `json:"finished_at"`
}

// POST /api/v1/conversations/:id/chat-jobs
func (h *ChatJobHandler) Create(c *gin.Context) {
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
	job, err := h.jobs.Create(c.Request.Context(), userID, conversationID, request.Content)

	switch {
	case errors.Is(err, chatjob.ErrInvalidContent),
		errors.Is(err, conversation.ErrInvalidConversationID),
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
	case errors.Is(err, platform.ErrInsertChatJobFailed),
		errors.Is(err, rabbitmq.ErrPublishFailed),
		errors.Is(err, rabbitmq.ErrPublishNotConfirmed),
		errors.Is(err, rabbitmq.ErrMarshalJobIDFailed),
		errors.Is(err, rabbitmq.ErrPublishUnroutable),
		errors.Is(err, rabbitmq.ErrJobIDInvalid):
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	response := &chatJobResponse{
		ID:     job.ID,
		Status: job.Status,
	}
	c.JSON(http.StatusAccepted, response)
}

// GET  /api/v1/chat-jobs/:jobID
func (h *ChatJobHandler) Get(c *gin.Context) {
	userID, ok := checkUserIDValidity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}

	idString := c.Param("id")
	jobID, err := strconv.ParseUint(idString, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "input is invalid",
		})
		return
	}
	job, err := h.jobs.GetByID(c.Request.Context(), userID, jobID)
	switch {
	case errors.Is(err, conversation.ErrInvalidUserID),
		errors.Is(err, chatjob.ErrInvalidJobID):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code:    "INVALID_REQUEST",
			Message: "input is invalid",
		})
		return
	case errors.Is(err, chatjob.ErrJobNotFound):
		c.JSON(http.StatusNotFound, &errorResponse{
			Code:    "NOT_FOUND",
			Message: "not found the job",
		})
		return
	case errors.Is(err, platform.ErrGetChatJobsFailed):
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	response := &getJobResponse{
		ID:                 job.ID,
		Status:             job.Status,
		AssistantMessageID: job.AssistantMessageID,
		ErrorCode:          job.ErrorCode,
		CreatedAt:          job.CreatedAt,
		StartedAt:          job.StartedAt,
		FinishedAt:         job.FinishedAt,
	}
	c.JSON(http.StatusOK, response)
}
