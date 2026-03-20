package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireRole returns a middleware that allows access only when the
// authenticated user's role matches one of the provided allowed roles.
// Must be used AFTER Authenticate().
//
// Example:
//
//	admin := r.Group("/admin")
//	admin.Use(middleware.Authenticate(), middleware.RequireRole("administrator", "admin"))
func RequireRole(allowedRoles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[r] = struct{}{}
	}

	return func(c *gin.Context) {
		role := c.GetString(CtxRole)
		if _, ok := allowed[role]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "You do not have permission to perform this action",
			})
			return
		}
		c.Next()
	}
}

// RequireAdministrator allows only the single super-admin account.
// Use for destructive or system-level operations:
//   - Managing roles & permissions
//   - Managing categories
//   - Managing staff users
//   - Deleting any data permanently
func RequireAdministrator() gin.HandlerFunc {
	return RequireRole("administrator")
}

// RequireAdmin allows administrator OR admin (staff) role.
// Use for general admin panel access:
//   - Viewing dashboard stats
//   - Managing posts (moderate, delete)
//   - Managing client users
//   - Creating client accounts
func RequireAdmin() gin.HandlerFunc {
	return RequireRole("administrator", "admin")
}

// RequireAny allows administrator, admin, or client.
// Use for endpoints any authenticated panel user can call.
func RequireAny() gin.HandlerFunc {
	return RequireRole("administrator", "admin", "client")
}