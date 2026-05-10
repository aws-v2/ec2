package middleware

import (

	"github.com/gin-gonic/gin"
)
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // already being set somewhere — keep it
        c.Set("userID",     c.GetHeader("X-User-Id"))
        c.Set("userRole",   c.GetHeader("X-User-Role"))
        c.Set("authMethod", c.GetHeader("X-Auth-Method"))
 
        c.Next()
    }
}