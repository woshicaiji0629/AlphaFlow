package api

import (
	"net/http"
	"strings"
	"time"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthOptions struct {
	Service        *service.AuthService
	CookieName     string
	CSRFCookieName string
	SecureCookie   bool
}

func sessionMiddleware(options AuthOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(options.CookieName)
		if err != nil {
			writeError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		session, err := options.Service.Authenticate(c.Request.Context(), token)
		if err != nil {
			clearCookie(c, options.CookieName, true, options.SecureCookie)
			writeError(c, http.StatusUnauthorized, "unauthorized", "登录已失效")
			return
		}
		requestcontext.SetSession(c, session)
		c.Next()
	}
}

func csrfMiddleware(options AuthOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(options.CSRFCookieName)
		header := c.GetHeader("X-CSRF-Token")
		if err != nil || header == "" || !strings.EqualFold(cookie, header) || !service.VerifyCSRF(currentSession(c), header) {
			writeError(c, http.StatusForbidden, "csrf_failed", "请求安全校验失败")
			return
		}
		c.Next()
	}
}

func currentSession(c *gin.Context) domain.Session {
	return requestcontext.Session(c)
}

func clearCookie(c *gin.Context, name string, httpOnly, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: httpOnly, Secure: secure, SameSite: http.SameSiteLaxMode})
}
