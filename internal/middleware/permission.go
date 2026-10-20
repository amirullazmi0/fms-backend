package middleware

import (
	"net/http"
	"slices"

	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/pkg/response"
	"github.com/gin-gonic/gin"
)

// RequirePermission lets the request through when the token carries any of allowed.
// An administrator user (users.is_administrator) always passes.
func RequirePermission(allowed ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := ActorFrom(c)
		if !ok {
			response.Error(c, http.StatusUnauthorized, "Authorization required", nil)
			c.Abort()
			return
		}
		if actor.IsAdministrator {
			c.Next()
			return
		}
		for _, p := range allowed {
			if slices.Contains(actor.Permissions, p) {
				c.Next()
				return
			}
		}
		response.Error(c, http.StatusForbidden, "Akun Anda tidak memiliki akses untuk aksi ini", nil)
		c.Abort()
	}
}

// RequireAdministrator guards the fms-administrator endpoints (roles, permissions, users):
// administrator users, or users whose role may open the app (e.g. OWNER).
func RequireAdministrator() gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, ok := ActorFrom(c)
		if !ok {
			response.Error(c, http.StatusUnauthorized, "Authorization required", nil)
			c.Abort()
			return
		}
		if !actor.IsAdministrator && !actor.AdminApp {
			response.Error(c, http.StatusForbidden, "Hanya role administrator yang boleh mengakses halaman ini", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

// ActorFrom is the authenticated user of the request (set by JWTAuth).
func ActorFrom(c *gin.Context) (*model.Actor, bool) {
	id := c.GetString(ctxUserID)
	if id == "" {
		return nil, false
	}
	actor := &model.Actor{ID: id, Role: c.GetString(ctxUserRole), IsAdministrator: c.GetBool(ctxUserIsAdmin), AdminApp: c.GetBool(ctxUserAdminApp)}
	if v, ok := c.Get(ctxUserPermissions); ok {
		if list, ok := v.([]string); ok {
			actor.Permissions = list
		}
	}
	return actor, true
}
