package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"alphaflow/go-service/control-api/internal/api/requestcontext"
	"alphaflow/go-service/control-api/internal/domain"
	"github.com/gin-gonic/gin"
)

func TestAdminMiddlewareRejectsUser(t *testing.T) {
	recorder := runAdminMiddleware("user")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestAdminMiddlewareAcceptsAdmin(t *testing.T) {
	recorder := runAdminMiddleware("admin")
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func runAdminMiddleware(role string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		requestcontext.SetSession(c, domain.Session{User: domain.User{Role: role}})
		c.Next()
	})
	router.GET("/admin", adminMiddleware(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin", nil))
	return recorder
}
