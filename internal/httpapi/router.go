package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func NewRouter(
	userHandler *UserHandler,
	conversationHandler *ConversationHandler,
	messageHandler *MessageHandler,
	chatHandler *ChatHandler,
	checker *HealthHandler,
	tokens TokenVerifier,
	limiter ChatRateLimiter,
) *gin.Engine {
	router := gin.New()

	router.Use(RequestIDMiddleware())
	router.Use(LogMiddleware())
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	router.GET("/readyz", checker.Ready)
	router.POST("/api/v1/auth/register", userHandler.Register)
	router.POST("/api/v1/auth/login", userHandler.Login)

	authenticated := router.Group("/api/v1")
	authenticated.Use(AuthMiddleware(tokens))
	authenticated.GET("/users/me", userHandler.Me)
	authenticated.POST("/conversations", conversationHandler.Create)
	authenticated.GET("/conversations", conversationHandler.List)
	authenticated.GET("/conversations/:id", conversationHandler.GetByID)
	authenticated.DELETE("/conversations/:id", conversationHandler.Delete)

	authenticated.GET("/conversations/:id/messages", messageHandler.List)
	authenticated.POST("/conversations/:id/messages", messageHandler.CreateUserMessage)

	chatRateLimit := ChatRateLimitMiddleware(limiter)

	authenticated.POST("/conversations/:id/chat", chatRateLimit, chatHandler.Chat)
	authenticated.POST("/conversations/:id/chat/stream", chatRateLimit, chatHandler.ChatStreaming)
	return router
}
