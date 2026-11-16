package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/auth"
	"aiops-platform/backend/internal/domain"
)

// ---------- 登录鉴权 ----------

func (s *Server) authLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	token, u, err := s.auth.Login(req.Username, req.Password)
	if err != nil {
		switch err {
		case auth.ErrTooManyAttempts:
			c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		case auth.ErrUserDisabled:
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": u.View()})
}

func (s *Server) authMe(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": auth.ErrUnauthorized.Error()})
		return
	}
	c.JSON(http.StatusOK, u.View())
}

// authLogout 令牌为无状态 JWT，登出由前端丢弃令牌完成；如需强制失效可修改密码。
func (s *Server) authLogout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) authChangePassword(c *gin.Context) {
	u, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": auth.ErrUnauthorized.Error()})
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if err := s.auth.ChangePassword(u.ID, req.OldPassword, req.NewPassword); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	updated, ok := s.auth.GetUser(u.ID)
	if !ok {
		fail(c, http.StatusInternalServerError, nil)
		return
	}
	token, err := s.auth.IssueToken(updated)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "user": updated.View()})
}

// authSSOAuthorize 公司统一认证占位：未配置 OIDC 时返回 501，配置后跳转授权地址。
func (s *Server) authSSOAuthorize(c *gin.Context) {
	corp := s.auth.CorporateProvider()
	if corp == nil || !corp.Configured() {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "企业统一认证尚未配置（OIDC_ISSUER/OIDC_CLIENT_ID）"})
		return
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":    "企业统一认证已预留配置，授权码流程待接入",
		"redirect": buildAuthorizeURL(s.cfg.OIDCIssuer, s.cfg.OIDCClientID, s.cfg.OIDCRedirectURL),
	})
}

func buildAuthorizeURL(issuer, clientID, redirectURL string) string {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" || clientID == "" {
		return ""
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("scope", "openid profile email")
	if redirectURL != "" {
		q.Set("redirect_uri", redirectURL)
	}
	return issuer + "/authorize?" + q.Encode()
}

// ---------- 用户管理（admin） ----------

func (s *Server) userList(c *gin.Context) {
	users := s.auth.ListUsers(strings.TrimSpace(c.Query("role")))
	c.JSON(http.StatusOK, gin.H{"total": len(users), "users": users})
}

func (s *Server) userCreate(c *gin.Context) {
	var req struct {
		Username    string            `json:"username"`
		DisplayName string            `json:"displayName"`
		Password    string            `json:"password"`
		Role        domain.Role       `json:"role"`
		Source      domain.UserSource `json:"source"`
		ExternalID  string            `json:"externalId"`
		Enabled     *bool             `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	u, err := s.auth.CreateUser(auth.CreateUserInput{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		Role:        req.Role,
		Source:      req.Source,
		ExternalID:  req.ExternalID,
		Enabled:     req.Enabled,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, u.View())
}

func (s *Server) userUpdate(c *gin.Context) {
	var req struct {
		DisplayName *string      `json:"displayName"`
		Role        *domain.Role `json:"role"`
		Enabled     *bool        `json:"enabled"`
		Password    *string      `json:"password"`
		ExternalID  *string      `json:"externalId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	u, err := s.auth.UpdateUser(c.Param("id"), auth.UpdateUserInput{
		DisplayName: req.DisplayName,
		Role:        req.Role,
		Enabled:     req.Enabled,
		Password:    req.Password,
		ExternalID:  req.ExternalID,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, u.View())
}

func (s *Server) userDelete(c *gin.Context) {
	if err := s.auth.DeleteUser(c.Param("id")); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
