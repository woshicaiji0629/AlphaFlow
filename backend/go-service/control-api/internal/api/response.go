package api

import (
	apiresponse "alphaflow/go-service/control-api/internal/api/response"
	"github.com/gin-gonic/gin"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func writeError(c *gin.Context, status int, code, message string) {
	apiresponse.Error(c, status, code, message)
}
