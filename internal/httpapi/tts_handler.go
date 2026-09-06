package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"gopherai/internal/tts"
)

const maxTTSRequestBodyBytes = 1 << 20 // 1 MiB

type TTSService interface {
	Create(ctx context.Context, text string) (*tts.Task, error)
	Get(ctx context.Context, taskID string) (*tts.Task, error)
}

type TTSHandler struct {
	tasks TTSService
}

type createTTSTaskRequest struct {
	Text string `json:"text"`
}

type createTTSTaskResponse struct {
	TaskID string     `json:"task_id"`
	Status tts.Status `json:"status"`
}

type getTTSTaskResponse struct {
	TaskID    string     `json:"task_id"`
	Status    tts.Status `json:"status"`
	AudioURL  *string    `json:"audio_url"`
	ErrorCode *string    `json:"error_code"`
}

func NewTTSHandler(tasks TTSService) *TTSHandler {
	return &TTSHandler{tasks: tasks}
}

// Create 提交一个异步语音合成任务。
func (h *TTSHandler) Create(c *gin.Context) {
	if _, ok := checkUserIDValidity(c); !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTTSRequestBodyBytes)
	request := &createTTSTaskRequest{}
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

	task, err := h.tasks.Create(c.Request.Context(), request.Text)
	if writeTTSError(c, err) {
		return
	}
	if task == nil {
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	c.JSON(http.StatusAccepted, &createTTSTaskResponse{
		TaskID: task.ID,
		Status: task.Status,
	})
}

// Get 查询一个语音合成任务的当前状态。
func (h *TTSHandler) Get(c *gin.Context) {
	if _, ok := checkUserIDValidity(c); !ok {
		c.JSON(http.StatusUnauthorized, &errorResponse{
			Code: "UNAUTHORIZED", Message: "need to login first",
		})
		return
	}

	task, err := h.tasks.Get(c.Request.Context(), c.Param("id"))
	if writeTTSError(c, err) {
		return
	}
	if task == nil {
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, &getTTSTaskResponse{
		TaskID:    task.ID,
		Status:    task.Status,
		AudioURL:  task.AudioURL,
		ErrorCode: task.ErrorCode,
	})
}

func writeTTSError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, tts.ErrInvalidText),
		errors.Is(err, tts.ErrTextTooLong),
		errors.Is(err, tts.ErrInvalidTaskID):
		c.JSON(http.StatusBadRequest, &errorResponse{
			Code: "INVALID_REQUEST", Message: "input is invalid",
		})
	case errors.Is(err, tts.ErrNotConfigured):
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable",
		})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, &errorResponse{
			Code: "TIMEOUT", Message: "response timed out",
		})
	case errors.Is(err, tts.ErrProviderUnavailable),
		errors.Is(err, tts.ErrInvalidProviderResult):
		c.JSON(http.StatusBadGateway, &errorResponse{
			Code: "PROVIDER_ERROR", Message: "TTS provider error",
		})
	default:
		c.JSON(http.StatusInternalServerError, &errorResponse{
			Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
		})
	}
	return true
}
