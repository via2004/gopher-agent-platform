package httpapi

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"math"
	"net/http"
	"strconv"
	"time"
)

/*
allowed=true              放行
allowed=false             返回 429
retryAfter=35*time.Second 告诉客户端多久后重试
err!=nil                  Redis 异常，返回 503
*/
type RateLimiter interface {
	Allow(
		ctx context.Context,
		userID uint64,
	) (
		allowed bool,
		retryAfter time.Duration,
		err error,
	)
}

func RateLimitMiddleware(limiter RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, valid := checkUserIDValidity(c)
		if !valid {
			// 这种状态要Abort掉后续的这次请求，不能用c.JSON,要Abort掉这个gin.Context
			c.AbortWithStatusJSON(http.StatusUnauthorized, &errorResponse{
				Code: "UNAUTHORIZED", Message: "need to login first",
			})
			return
		}
		allow, retryAfter, err := limiter.Allow(c.Request.Context(), userID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, &errorResponse{
				Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable!",
			})
			return
		}
		seconds := int64(math.Ceil(retryAfter.Seconds()))

		if !allow {
			c.Header("Retry-After", strconv.FormatInt(seconds, 10))

			c.AbortWithStatusJSON(http.StatusTooManyRequests, &errorResponse{
				Code: "TOO_MANY_REQUESTS", Message: fmt.Sprintf("too many requests, retry after %d s", seconds),
			})
			return
		}

		c.Next()
	}
}
