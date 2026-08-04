package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func NewRouter(userHandler *UserHandler) *gin.Engine {
	router := gin.Default()

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	router.POST("/api/v1/auth/register", userHandler.Register)
	router.POST("/api/v1/auth/login", userHandler.Login)

	return router
}
