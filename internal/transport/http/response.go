package transport

import (

	"github.com/gin-gonic/gin"
)

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func SendSuccess(c *gin.Context, code int, message string, data interface{}) {
	c.JSON(code, APIResponse{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

func SendError(c *gin.Context, code int, message string) {
	c.JSON(code, APIResponse{
		Code:    code,
		Message: message,
	})
}