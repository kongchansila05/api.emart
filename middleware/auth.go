package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"project-api/utils"
)

// Context keys used by downstream handlers.
const (
	CtxUserID = "userID"
	CtxRoleID = "roleID"
	CtxRole   = "role"
)

// Authenticate validates the Bearer token on every protected route.
// On success it injects userID, roleID, and role into the Gin context.
func Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header is required",
			})
			return
		}

		raw := strings.TrimPrefix(authHeader, "Bearer ")
		if raw == authHeader {
			// Prefix was missing.
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Token must be prefixed with 'Bearer '",
			})
			return
		}

		claims, err := utils.ParseToken(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid or expired token",
			})
			return
		}

		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxRoleID, claims.RoleID)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}
