package controller

import (
	"errors"
	"net"
	"net/http"
	"time"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	apiresponse "alphaflow/go-service/control-api/internal/api/response"
	"alphaflow/go-service/control-api/internal/domain"
	"alphaflow/go-service/control-api/internal/service"
	"github.com/gin-gonic/gin"
)

type Auth struct {
	service                    *service.AuthService
	cookieName, csrfCookieName string
	secure                     bool
}

func NewAuth(service *service.AuthService, cookieName, csrfCookieName string, secure bool) *Auth {
	return &Auth{service: service, cookieName: cookieName, csrfCookieName: csrfCookieName, secure: secure}
}
func (a *Auth) Login(c *gin.Context) {
	var input struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			apiresponse.Error(c, http.StatusRequestEntityTooLarge, "request_too_large", "请求内容过大")
			return
		}
		apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	ip, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	result, err := a.service.Login(c.Request.Context(), input.Email, input.Password, c.Request.UserAgent(), ip)
	if errors.Is(err, domain.ErrInvalidCredentials) {
		apiresponse.Error(c, http.StatusUnauthorized, "invalid_credentials", "邮箱或密码错误")
		return
	}
	if errors.Is(err, domain.ErrRateLimited) {
		apiresponse.Error(c, http.StatusTooManyRequests, "rate_limited", "登录尝试过于频繁，请稍后再试")
		return
	}
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "服务内部错误")
		return
	}
	setCookie(c, a.cookieName, result.SessionToken, result.ExpiresAt, true, a.secure)
	setCookie(c, a.csrfCookieName, result.CSRFToken, result.ExpiresAt, false, a.secure)
	c.JSON(http.StatusOK, gin.H{"user": result.User})
}
func (a *Auth) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": requestcontext.Session(c).User})
}
func (a *Auth) Logout(c *gin.Context) {
	ip, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	err := a.service.Logout(c.Request.Context(), requestcontext.Session(c), ip, c.Request.UserAgent())
	clearCookie(c, a.cookieName, true, a.secure)
	clearCookie(c, a.csrfCookieName, false, a.secure)
	if err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal_error", "服务内部错误")
		return
	}
	c.Status(http.StatusNoContent)
}
func setCookie(c *gin.Context, name, value string, expires time.Time, httpOnly, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, MaxAge: int(time.Until(expires).Seconds()), HttpOnly: httpOnly, Secure: secure, SameSite: http.SameSiteLaxMode})
}
func clearCookie(c *gin.Context, name string, httpOnly, secure bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: httpOnly, Secure: secure, SameSite: http.SameSiteLaxMode})
}
