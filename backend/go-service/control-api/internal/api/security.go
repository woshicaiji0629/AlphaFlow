package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type SecurityOptions struct {
	AllowedOrigins []string
	MaxBodyBytes   int64
}

func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

func requestBodyLimitMiddleware(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

func originMiddleware(allowed []string) gin.HandlerFunc {
	set := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		set[origin] = struct{}{}
	}
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		if _, ok := set[origin]; !ok {
			writeError(c, http.StatusForbidden, "origin_forbidden", "请求来源不受信任")
			return
		}
		c.Next()
	}
}
