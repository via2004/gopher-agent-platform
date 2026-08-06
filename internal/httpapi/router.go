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
	tokens TokenVerifier,
) *gin.Engine {
	router := gin.Default()

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
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
	authenticated.POST("/conversations/:id/chat", chatHandler.Chat)
	return router
}
