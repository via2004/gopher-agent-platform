package httpapi

import (
	"github.com/gin-gonic/gin"
	"log"
	"time"
)

func LogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID, _ := c.Get(requestIDContextKey)

		now := time.Now()
		c.Next()
		path := c.Request.URL.Path
		method := c.Request.Method
		status := c.Writer.Status()
		userID, exists := checkUserIDValidity(c)
		spendTime := time.Since(now)
		if exists {
			log.Printf("Path: %s, Method: %s, RequestID: %s, Status: %d, UserID: %d, SpendTime: %d ms",
				path, method, requestID, status, userID, spendTime.Milliseconds())
		} else {
			log.Printf("Path: %s, Method: %s, RequestID: %s, Status: %d, UserID: %s, SpendTime: %d ms",
				path, method, requestID, status, "didn't set", spendTime.Milliseconds())
		}
	}
}
