package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"time"
)

type ReadinessChecker interface {
	Check(ctx context.Context) error
}

type HealthHandler struct {
	checker ReadinessChecker
}

func NewHealthHandler(checker ReadinessChecker) *HealthHandler {
	return &HealthHandler{
		checker: checker,
	}
}

func (h *HealthHandler) Ready(c *gin.Context) {
	pingCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.checker.Check(pingCtx); err != nil {
		requestID, _ := c.Get(requestIDContextKey)
		log.Printf(
			"readiness check failed: request_id=%v error=%v",
			requestID,
			err,
		)
		c.JSON(http.StatusServiceUnavailable, &errorResponse{
			Code:    "SERVICE_UNAVAILABLE",
			Message: "service not ready",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    "OK",
		"message": "service ready",
	})
}
