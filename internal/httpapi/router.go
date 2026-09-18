package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RouterHandlers 汇总 HTTP Router 直接注册的业务 Handler。
type RouterHandlers struct {
	Users             *UserHandler
	Conversations     *ConversationHandler
	Messages          *MessageHandler
	Chat              *ChatHandler
	Health            *HealthHandler
	ChatJobs          *ChatJobHandler
	Images            *ImageHandler
	RAG               *RAGHandler
	TTS               *TTSHandler
	EmailVerification *EmailVerificationHandler
}

// RouterMiddleware 汇总 Router 使用的认证与业务限流依赖。
type RouterMiddleware struct {
	Tokens                   TokenVerifier
	ChatLimiter              RateLimiter
	RAGUploadLimiter         RateLimiter
	TTSLimiter               RateLimiter
	AuthRegisterLimiter      RateLimiter
	AuthLoginLimiter         RateLimiter
	EmailVerificationLimiter RateLimiter
}

func NewRouter(
	handlers RouterHandlers,
	middleware RouterMiddleware,
) *gin.Engine {
	router := gin.New()

	router.Use(RequestIDMiddleware())
	router.Use(LogMiddleware())
	router.Use(gin.Recovery())

	registerPublicRoutes(router, handlers, middleware)

	authenticated := router.Group("/api/v1")
	authenticated.Use(AuthMiddleware(middleware.Tokens))
	registerAuthenticatedRoutes(authenticated, handlers, middleware)
	return router
}

func registerPublicRoutes(router *gin.Engine, handlers RouterHandlers, middleware RouterMiddleware) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	router.GET("/readyz", handlers.Health.Ready)
	router.POST("/api/v1/auth/register", AnonymousRateLimitMiddleware(middleware.AuthRegisterLimiter), handlers.Users.Register)
	router.POST("/api/v1/auth/login", AnonymousRateLimitMiddleware(middleware.AuthLoginLimiter), handlers.Users.Login)
	router.POST("/api/v1/auth/email-verification-codes", AnonymousRateLimitMiddleware(middleware.EmailVerificationLimiter), handlers.EmailVerification.Send)
}

func registerAuthenticatedRoutes(group *gin.RouterGroup, handlers RouterHandlers, middleware RouterMiddleware) {
	group.GET("/users/me", handlers.Users.Me)
	group.POST("/conversations", handlers.Conversations.Create)
	group.GET("/conversations", handlers.Conversations.List)
	group.GET("/conversations/:id", handlers.Conversations.GetByID)
	group.DELETE("/conversations/:id", handlers.Conversations.Delete)

	group.GET("/conversations/:id/messages", handlers.Messages.List)
	group.POST("/conversations/:id/messages", handlers.Messages.CreateUserMessage)

	chatRateLimit := RateLimitMiddleware(middleware.ChatLimiter)
	ragUploadRateLimit := RateLimitMiddleware(middleware.RAGUploadLimiter)
	ttsRateLimit := RateLimitMiddleware(middleware.TTSLimiter)

	group.POST("/conversations/:id/chat", chatRateLimit, handlers.Chat.Chat)
	group.POST("/conversations/:id/chat/stream", chatRateLimit, handlers.Chat.ChatStreaming)
	group.POST("/conversations/:id/chat-jobs", chatRateLimit, handlers.ChatJobs.Create)
	group.GET("/chat-jobs/:id", handlers.ChatJobs.Get)

	group.POST("/images/recognitions", handlers.Images.Recognize)
	group.POST("/rag/documents", ragUploadRateLimit, handlers.RAG.Upload)
	group.POST("/tts/tasks", ttsRateLimit, handlers.TTS.Create)
	group.GET("/tts/tasks/:id", handlers.TTS.Get)
}
