package response

import "github.com/gin-gonic/gin"

const requestIDKey = "request_id"

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func Error(c *gin.Context, status int, code, message string) {
	requestID, _ := c.Get(requestIDKey)
	c.AbortWithStatusJSON(status, errorBody{Error: errorDetail{
		Code: code, Message: message, RequestID: requestIDString(requestID),
	}})
}

func requestIDString(value any) string {
	requestID, _ := value.(string)
	return requestID
}
