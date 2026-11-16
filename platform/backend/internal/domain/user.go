package domain

import "time"

// Role 平台角色：admin 管理全部；ops 负责排障与工单处置；viewer 只读。
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleOps    Role = "ops"
	RoleViewer Role = "viewer"
)

// Valid 判断角色是否合法。
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOps, RoleViewer:
		return true
	}
	return false
}

// UserSource 账号来源：local 本地账号；corp 公司统一用户服务（OIDC/SSO）。
type UserSource string

const (
	UserSourceLocal UserSource = "local"
	UserSourceCorp  UserSource = "corp"
)

// User 平台用户。PasswordHash 只用于本地账号，接口返回时统一走 View() 脱敏。
type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"displayName"`
	PasswordHash string     `json:"passwordHash,omitempty"`
	Role         Role       `json:"role"`
	Source       UserSource `json:"source"`
	// ExternalID 公司统一认证体系内的用户标识，本地账号为空。
	ExternalID string `json:"externalId,omitempty"`
	Enabled    bool   `json:"enabled"`
	// TokenVersion 改密 / 禁用 / 改角色时自增，使已签发的令牌立即失效。
	TokenVersion int        `json:"tokenVersion"`
	CreatedAt    time.Time  `json:"createdAt"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
}

// UserView 对外返回的用户信息（不含密码散列）。
type UserView struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        Role       `json:"role"`
	Source      UserSource `json:"source"`
	ExternalID  string     `json:"externalId,omitempty"`
	Enabled     bool       `json:"enabled"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// View 转为可安全返回前端的结构。
func (u User) View() UserView {
	return UserView{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Source:      u.Source,
		ExternalID:  u.ExternalID,
		Enabled:     u.Enabled,
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}
