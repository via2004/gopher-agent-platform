package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	requestIDContextKey = "requestID"
	requestIDHeader     = "X-Request-ID"
)

// get or generate Request ID
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.New().String()
		c.Set(requestIDContextKey, requestID)
		c.Header(requestIDHeader, requestID)

		c.Next()
	}
}
