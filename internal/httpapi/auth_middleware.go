package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

const userIDContextKey = "userID"

type TokenVerifier interface {
	Verify(token string) (uint64, error)
}

func AuthMiddleware(tokens TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		authorization := c.GetHeader("Authorization")

		// 1. 检查 Header 格式：Bearer <token>
		tokenString, ok := parseBearerToken(authorization)
		if !ok {
			// 返回错误并阻止后续 Handler 执行。
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{
				Code:    "UNAUTHORIZED",
				Message: "authentication required",
			})
			return
		}

		// 2. 验证签名、issuer、过期时间，并取得 userID
		userID, err := tokens.Verify(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{
				Code:    "UNAUTHORIZED",
				Message: "authentication required",
			})
			return
		}

		// 3. 把身份传给后面的 Handler
		c.Set(userIDContextKey, userID)

		// 4. 放行
		c.Next() // 认证成功，让请求继续进入实际 Handler。
	}
}

func parseBearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 {
		return "", false
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}
