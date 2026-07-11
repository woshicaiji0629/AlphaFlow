package api

import (
	"net/http"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	"github.com/gin-gonic/gin"
)

func adminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if requestcontext.Session(c).User.Role != "admin" {
			writeError(c, http.StatusForbidden, "admin_required", "需要管理员权限")
			return
		}
		c.Next()
	}
}
