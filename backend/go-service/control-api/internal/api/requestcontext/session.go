package requestcontext

import (
	"alphaflow/go-service/control-api/internal/domain"
	"github.com/gin-gonic/gin"
)

const sessionKey = "session"

func SetSession(c *gin.Context, session domain.Session) {
	c.Set(sessionKey, session)
}

func Session(c *gin.Context) domain.Session {
	value, _ := c.Get(sessionKey)
	session, _ := value.(domain.Session)
	return session
}
