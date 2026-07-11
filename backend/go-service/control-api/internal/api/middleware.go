package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDKey = "request_id"

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" || len(id) > 128 {
			id = newRequestID()
		}
		c.Set(requestIDKey, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func accessLogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		slog.Info("http request",
			"request_id", requestID(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(startedAt),
		)
	}
}

func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.Error("http panic recovered", "request_id", requestID(c), "panic", recovered)
		writeError(c, http.StatusInternalServerError, "internal_error", "服务内部错误")
	})
}

func requestID(c *gin.Context) string {
	id, _ := c.Get(requestIDKey)
	value, _ := id.(string)
	return value
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "request-id-unavailable"
	}
	return hex.EncodeToString(value[:])
}
