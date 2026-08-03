package vpcpkg

import "github.com/gin-gonic/gin"

// --- Standard Response Envelope ---

type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func RespondSucces(c *gin.Context, statusCode int, message string, data interface{}) {
	c.Writer.Header().Set("Version", "1.0.0")
	c.Writer.Header().Set("Content-Type", "application/json")

	c.JSON(statusCode, apiResponse{
		Code:    statusCode,
		Message: message,
		Data:    data,
	})
}

func RespondError(c *gin.Context, statusCode int, err error) {
	c.Writer.Header().Set("Version", "1.0.0")
	c.Writer.Header().Set("Content-Type", "application/json")

	c.JSON(statusCode, apiResponse{
		Code:    statusCode,
		Message: err.Error(),
	})
	return
}
