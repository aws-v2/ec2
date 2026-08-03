package middleware

import (
	"ec2-api/internal/vpcpkg"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		userID := c.GetHeader("X-User-Id")
		role := c.GetHeader("X-User-Role")
		authMethod := c.GetHeader("X-Auth-Method")
		requestID := c.GetHeader("X-Request-Id")
		token := c.GetHeader("X-Authorization")

		headers := make([]string, 5, 5)
		headers[0] = userID
		headers[1] = role
		headers[2] = authMethod
		headers[3] = requestID
		headers[4] = token 

		c.Set("userID", userID)
		c.Set("role", role)
		c.Set("authMethod", authMethod)
		c.Set("requestID", requestID)
		c.Set("token", token)

		// TODO: this solution works but its inelegant, change this to somehtign better
		for _, header := range headers {
			if header == "" {
				log.Printf("[Middleware] header %s not available, droping request", userID)
				vpcpkg.RespondError(c, http.StatusBadRequest, fmt.Errorf("Missing header values:"))
				c.Abort()
				return
			}

		}

		c.Next()
	}
}
