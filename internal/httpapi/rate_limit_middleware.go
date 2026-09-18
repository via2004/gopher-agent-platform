package httpapi

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

/*
allowed=true              放行
allowed=false             返回 429
retryAfter=35*time.Second 告诉客户端多久后重试
err!=nil                  Redis 异常，返回 503
*/
type RateLimiter interface {
	Allow(ctx context.Context, identity string) (allowed bool, retryAfter time.Duration, err error)
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
		enforceRateLimit(c, limiter, strconv.FormatUint(userID, 10))
	}
}

// AnonymousRateLimitMiddleware 使用直连请求的 RemoteAddr IP 作为匿名限流身份。
// 当前不信任 X-Forwarded-For 或 X-Real-IP，避免客户端伪造 Header 绕过限流。
func AnonymousRateLimitMiddleware(limiter RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := remoteIPIdentity(c.Request.RemoteAddr)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, &errorResponse{
				Code: "INTERNAL_SERVER_ERROR", Message: "internal server error",
			})
			return
		}
		enforceRateLimit(c, limiter, identity)
	}
}

func enforceRateLimit(c *gin.Context, limiter RateLimiter, identity string) {
	if limiter == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable!",
		})
		return
	}
	allowed, retryAfter, err := limiter.Allow(c.Request.Context(), identity)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, &errorResponse{
			Code: "SERVICE_UNAVAILABLE", Message: "service is unavailable!",
		})
		return
	}
	if !allowed {
		seconds := int64(math.Ceil(retryAfter.Seconds()))
		c.Header("Retry-After", strconv.FormatInt(seconds, 10))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, &errorResponse{
			Code: "TOO_MANY_REQUESTS", Message: fmt.Sprintf("too many requests, retry after %d s", seconds),
		})
		return
	}
	c.Next()
}

func remoteIPIdentity(remoteAddr string) (string, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return "", false
	}
	// 把host转成IP地址
	address, err := netip.ParseAddr(host)
	if err != nil {
		return "", false
	}

	// Unmap把IPv4映射形式的IPv6还原成普通Ipv4: ::ffff:127.0.0.1 -> 127.0.0.1
	return address.Unmap().String(), true
}
