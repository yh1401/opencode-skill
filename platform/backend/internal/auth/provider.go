package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// ErrNotConfigured 企业统一认证尚未接入。
var ErrNotConfigured = errors.New("企业统一认证尚未接入")

// Provider 认证来源抽象：本地账号现在实现，公司统一用户服务（OIDC/SSO）后续接入。
type Provider interface {
	// Name 认证来源标识（local / corp）。
	Name() string
	// Authenticate 校验凭据并返回用户；本地实现负责 bcrypt 比对。
	Authenticate(ctx context.Context, username, password string) (*domain.User, error)
	// ChangePassword 仅本地账号支持修改密码。
	ChangePassword(ctx context.Context, user *domain.User, newPassword string) error
}

// LocalProvider 本地账号：用户名 + bcrypt 密码。
type LocalProvider struct {
	store store.Store
}

func NewLocalProvider(st store.Store) *LocalProvider {
	return &LocalProvider{store: st}
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) Authenticate(_ context.Context, username, password string) (*domain.User, error) {
	u := findUserByUsername(p.store, username)
	if u == nil {
		return nil, ErrInvalidCredentials
	}
	if u.PasswordHash == "" {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

func (p *LocalProvider) ChangePassword(_ context.Context, user *domain.User, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	return nil
}

// CorporateProvider 公司统一用户服务占位：只保留配置，接入 OIDC 时补齐授权码换令牌与用户映射。
type CorporateProvider struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string
}

func NewCorporateProvider(cfg config.Config) *CorporateProvider {
	return &CorporateProvider{
		issuer:       cfg.OIDCIssuer,
		clientID:     cfg.OIDCClientID,
		clientSecret: cfg.OIDCClientSecret,
		redirectURL:  cfg.OIDCRedirectURL,
	}
}

func (p *CorporateProvider) Name() string { return "corp" }

// Configured 是否已填写 OIDC 配置，供 /api/auth/sso/authorize 判断。
func (p *CorporateProvider) Configured() bool {
	return strings.TrimSpace(p.issuer) != "" && strings.TrimSpace(p.clientID) != ""
}

func (p *CorporateProvider) Authenticate(_ context.Context, _, _ string) (*domain.User, error) {
	return nil, ErrNotConfigured
}

func (p *CorporateProvider) ChangePassword(_ context.Context, _ *domain.User, _ string) error {
	return ErrNotConfigured
}

// HashPassword bcrypt 散列，成本使用默认值。
func HashPassword(pwd string) (string, error) {
	if len(strings.TrimSpace(pwd)) < 8 {
		return "", fmt.Errorf("密码长度至少 8 位")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// findUserByUsername 在 user 文档集中按用户名查找（忽略大小写）。
func findUserByUsername(st store.Store, username string) *domain.User {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}
	for _, u := range store.ListInto[domain.User](st, usersDocType) {
		if strings.EqualFold(u.Username, username) {
			found := u
			return &found
		}
	}
	return nil
}
