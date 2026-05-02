package transport

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"fmt"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware extracts the user ID from the Authorization header (JWT)
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := GetTokenFromRequest(c)

		if tokenString != "" {
			userID, err := ExtractUserIDFromToken(tokenString)
			if err == nil {
				// Set userID in context
				c.Set("userID", userID)
			}
		}
		
		c.Next()
	}
}

// GetTokenFromRequest extracts the token from the Authorization header or token query parameter
func GetTokenFromRequest(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}

	return c.Query("token")
}

// ExtractUserIDFromToken extracts the user ID (sub or user_name) from a JWT token string
func ExtractUserIDFromToken(tokenString string) (string, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid token format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("invalid token payload")
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse token claims")
	}

	// Extract user ID (try 'userId' first as specific to this app's JWT, then 'sub' or others)
	if uid, ok := claims["userId"].(string); ok && uid != "" {
		return uid, nil
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		// Fallback to 'user_name' or other claims if your auth service uses them
		if sub, ok := claims["user_name"].(string); ok {
			userID = sub
		} else {
			return "", fmt.Errorf("user ID not found in token")
		}
	}

	return userID, nil
}
