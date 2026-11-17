package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/domain"
)

const ctxUserKey = "aiops.currentUser"

// RequireAuth 解析 Bearer 令牌并按当前用户状态复核。
// AUTH_ENABLED=false 时按内置管理员放行（仅本地演示）。
func (s *Service) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.enabled {
			c.Set(ctxUserKey, &domain.User{
				ID:          anonymousUserName,
				Username:    anonymousUserName,
				DisplayName: "演示模式",
				Role:        domain.RoleAdmin,
				Source:      domain.UserSourceLocal,
				Enabled:     true,
			})
			c.Next()
			return
		}
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			// EventSource 无法自定义请求头，SSE 接口允许通过查询参数传令牌
			token = strings.TrimSpace(c.Query("token"))
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthorized.Error()})
			return
		}
		u, err := s.VerifyToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set(ctxUserKey, u)
		c.Next()
	}
}

// RequireRole 限定角色，需在 RequireAuth 之后使用。
func RequireRole(roles ...domain.Role) gin.HandlerFunc {
	allowed := make(map[domain.Role]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		u, ok := CurrentUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthorized.Error()})
			return
		}
		if !allowed[u.Role] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "当前角色无权执行该操作"})
			return
		}
		c.Next()
	}
}

// RequireWrite 只读角色（viewer）禁止任何写操作。
func RequireWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		u, ok := CurrentUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": ErrUnauthorized.Error()})
			return
		}
		if u.Role == domain.RoleViewer {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "只读账号不能执行写操作"})
			return
		}
		c.Next()
	}
}

// CurrentUser 取出当前登录用户。
func CurrentUser(c *gin.Context) (*domain.User, bool) {
	v, ok := c.Get(ctxUserKey)
	if !ok {
		return nil, false
	}
	u, ok := v.(*domain.User)
	return u, ok
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return header
}
