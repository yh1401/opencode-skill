package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

const usersDocType = "user"

var (
	// ErrInvalidCredentials 用户名或密码错误（不区分用户是否存在）。
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	// ErrUserDisabled 账号已禁用。
	ErrUserDisabled = errors.New("账号已禁用，请联系管理员")
	// ErrTooManyAttempts 连续失败过多，暂时锁定。
	ErrTooManyAttempts = errors.New("登录失败次数过多，请 5 分钟后再试")
	// ErrUnauthorized 令牌无效或已失效。
	ErrUnauthorized = errors.New("登录状态已失效，请重新登录")
)

// failureWindow / lockDuration 登录失败策略：5 分钟内连续失败 5 次，锁定 5 分钟。
const (
	failureWindow     = 5 * time.Minute
	maxFailures       = 5
	lockDuration      = 5 * time.Minute
	anonymousUserName = "anonymous"
)

type failureState struct {
	count       int
	firstAt     time.Time
	lockedUntil time.Time
}

// Service 认证与用户管理：本地账号 + JWT + 登录锁定，并预留企业统一认证。
type Service struct {
	store    store.Store
	provider Provider
	corp     *CorporateProvider
	secret   []byte
	ttl      time.Duration
	enabled  bool

	mu       sync.Mutex
	failures map[string]*failureState
}

// NewService 构建认证服务并确保初始管理员存在。
func NewService(cfg config.Config, st store.Store) *Service {
	secret := []byte(strings.TrimSpace(cfg.AuthSecret))
	if len(secret) == 0 {
		secret = randomSecret()
		log.Printf("[auth] AUTH_SECRET 未配置，已生成临时密钥；重启后令牌会失效，生产环境请显式配置")
	}
	s := &Service{
		store:    st,
		provider: NewLocalProvider(st),
		corp:     NewCorporateProvider(cfg),
		secret:   secret,
		ttl:      cfg.AuthTokenTTL,
		enabled:  cfg.AuthEnabled,
		failures: map[string]*failureState{},
	}
	if s.ttl <= 0 {
		s.ttl = 8 * time.Hour
	}
	if s.enabled {
		if err := s.ensureInitialAdmin(cfg.AuthInitAdminUser, cfg.AuthInitAdminPassword); err != nil {
			log.Printf("[auth] 初始化管理员失败: %v", err)
		}
		s.seedDemoUsers(cfg.AuthDemoUsers)
	}
	return s
}

// Enabled 是否启用鉴权。关闭时所有请求按内置管理员放行，仅用于本地演示。
func (s *Service) Enabled() bool { return s.enabled }

// CorporateProvider 暴露企业统一认证占位实现。
func (s *Service) CorporateProvider() *CorporateProvider { return s.corp }

func (s *Service) ensureInitialAdmin(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		log.Printf("[auth] 未配置 AUTH_INIT_ADMIN_USER/PASSWORD，跳过初始管理员创建")
		return nil
	}
	if u := findUserByUsername(s.store, username); u != nil {
		if u.Role != domain.RoleAdmin || !u.Enabled {
			u.Role = domain.RoleAdmin
			u.Enabled = true
			u.TokenVersion++
			return s.store.Put(usersDocType, u.ID, u)
		}
		return nil
	}
	hash, err := hashPasswordLoose(password)
	if err != nil {
		return err
	}
	now := time.Now()
	u := domain.User{
		ID:           store.NewID("user"),
		Username:     username,
		DisplayName:  username,
		PasswordHash: hash,
		Role:         domain.RoleAdmin,
		Source:       domain.UserSourceLocal,
		Enabled:      true,
		CreatedAt:    now,
	}
	if err := s.store.Put(usersDocType, u.ID, u); err != nil {
		return err
	}
	log.Printf("[auth] 已创建初始管理员 username=%s", username)
	return nil
}

// seedDemoUsers 按 AUTH_DEMO_USERS 创建演示账号（username:password:role），
// 已存在的账号保持不变，避免覆盖线上改过的密码。
func (s *Service) seedDemoUsers(entries []string) {
	for _, raw := range entries {
		parts := strings.Split(strings.TrimSpace(raw), ":")
		if len(parts) != 3 {
			log.Printf("[auth] 演示账号格式无效（应为 username:password:role）: %s", raw)
			continue
		}
		username := strings.TrimSpace(parts[0])
		password := strings.TrimSpace(parts[1])
		role := domain.Role(strings.TrimSpace(parts[2]))
		if username == "" || len(password) < 8 || !role.Valid() {
			log.Printf("[auth] 演示账号配置无效: %s", raw)
			continue
		}
		if findUserByUsername(s.store, username) != nil {
			continue
		}
		if _, err := s.CreateUser(CreateUserInput{
			Username:    username,
			DisplayName: username,
			Password:    password,
			Role:        role,
		}); err != nil {
			log.Printf("[auth] 创建演示账号 %s 失败: %v", username, err)
			continue
		}
		log.Printf("[auth] 已创建演示账号 username=%s role=%s", username, role)
	}
}

// Login 校验凭据并签发令牌。
func (s *Service) Login(username, password string) (string, *domain.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return "", nil, ErrInvalidCredentials
	}
	if err := s.checkLock(username); err != nil {
		return "", nil, err
	}
	u, err := s.provider.Authenticate(context.Background(), username, password)
	if err != nil {
		s.recordFailure(username)
		return "", nil, ErrInvalidCredentials
	}
	if !u.Enabled {
		return "", nil, ErrUserDisabled
	}
	s.clearFailures(username)

	now := time.Now()
	u.LastLoginAt = &now
	_ = s.store.Put(usersDocType, u.ID, u)

	token, err := signJWT(s.secret, u.ID, u.TokenVersion, s.ttl)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// IssueToken 为用户签发令牌（改密后重新签发，避免当前会话被踢出）。
func (s *Service) IssueToken(u *domain.User) (string, error) {
	return signJWT(s.secret, u.ID, u.TokenVersion, s.ttl)
}

// VerifyToken 解析令牌并按当前用户状态复核（禁用 / 改密 / 改角色立即生效）。
func (s *Service) VerifyToken(token string) (*domain.User, error) {
	p, err := parseJWT(s.secret, token)
	if err != nil {
		return nil, ErrUnauthorized
	}
	u, ok := s.GetUser(p.Sub)
	if !ok || !u.Enabled {
		return nil, ErrUnauthorized
	}
	if u.TokenVersion != p.Ver {
		return nil, ErrUnauthorized
	}
	return u, nil
}

// GetUser 按 ID 读取用户。
func (s *Service) GetUser(id string) (*domain.User, bool) {
	var u domain.User
	if store.GetInto(s.store, usersDocType, id, &u) {
		return &u, true
	}
	return nil, false
}

// GetUserByUsername 按用户名读取用户。
func (s *Service) GetUserByUsername(username string) (*domain.User, bool) {
	u := findUserByUsername(s.store, username)
	return u, u != nil
}

// ListUsers 列出用户，role 非空时按角色过滤。
func (s *Service) ListUsers(role string) []domain.UserView {
	users := store.ListInto[domain.User](s.store, usersDocType)
	sort.Slice(users, func(i, j int) bool {
		if users[i].Role != users[j].Role {
			return users[i].Role < users[j].Role
		}
		return users[i].Username < users[j].Username
	})
	out := make([]domain.UserView, 0, len(users))
	for _, u := range users {
		if role != "" && string(u.Role) != role {
			continue
		}
		out = append(out, u.View())
	}
	return out
}

// CreateUserInput 新建用户入参。
type CreateUserInput struct {
	Username    string
	DisplayName string
	Password    string
	Role        domain.Role
	Source      domain.UserSource
	ExternalID  string
	Enabled     *bool
}

// CreateUser 新建本地账号（企业账号由统一认证回调创建，当前预留）。
func (s *Service) CreateUser(in CreateUserInput) (*domain.User, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return nil, fmt.Errorf("用户名不能为空")
	}
	if !in.Role.Valid() {
		return nil, fmt.Errorf("角色非法: %s", in.Role)
	}
	if findUserByUsername(s.store, username) != nil {
		return nil, fmt.Errorf("用户名已存在: %s", username)
	}
	source := in.Source
	if source == "" {
		source = domain.UserSourceLocal
	}
	if source == domain.UserSourceLocal {
		if strings.TrimSpace(in.Password) == "" {
			return nil, fmt.Errorf("本地账号必须设置初始密码")
		}
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = username
	}
	u := domain.User{
		ID:           store.NewID("user"),
		Username:     username,
		DisplayName:  display,
		PasswordHash: hash,
		Role:         in.Role,
		Source:       source,
		ExternalID:   strings.TrimSpace(in.ExternalID),
		Enabled:      enabled,
		CreatedAt:    time.Now(),
	}
	if err := s.store.Put(usersDocType, u.ID, u); err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateUserInput 更新用户入参，nil 表示不改。
type UpdateUserInput struct {
	DisplayName *string
	Role        *domain.Role
	Enabled     *bool
	Password    *string
	ExternalID  *string
}

// UpdateUser 更新用户；改角色 / 禁用 / 改密都会提升 TokenVersion 使旧令牌失效。
func (s *Service) UpdateUser(id string, in UpdateUserInput) (*domain.User, error) {
	u, ok := s.GetUser(id)
	if !ok {
		return nil, fmt.Errorf("用户不存在: %s", id)
	}
	prevRole, prevEnabled := u.Role, u.Enabled
	revoke := false

	if in.DisplayName != nil {
		name := strings.TrimSpace(*in.DisplayName)
		if name == "" {
			return nil, fmt.Errorf("显示名不能为空")
		}
		u.DisplayName = name
	}
	if in.ExternalID != nil {
		u.ExternalID = strings.TrimSpace(*in.ExternalID)
	}
	if in.Role != nil {
		if !in.Role.Valid() {
			return nil, fmt.Errorf("角色非法: %s", *in.Role)
		}
		if *in.Role != u.Role {
			u.Role = *in.Role
			revoke = true
		}
	}
	if in.Enabled != nil && *in.Enabled != u.Enabled {
		u.Enabled = *in.Enabled
		revoke = true
	}
	if in.Password != nil {
		if u.Source != domain.UserSourceLocal {
			return nil, fmt.Errorf("企业账号密码由统一认证维护")
		}
		hash, err := HashPassword(*in.Password)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = hash
		revoke = true
	}

	if prevRole == domain.RoleAdmin && prevEnabled && (u.Role != domain.RoleAdmin || !u.Enabled) {
		if !s.hasOtherAdmin(u.ID) {
			return nil, fmt.Errorf("系统至少保留一个启用状态的管理员")
		}
	}
	if revoke {
		u.TokenVersion++
	}
	if err := s.store.Put(usersDocType, u.ID, u); err != nil {
		return nil, err
	}
	return u, nil
}

// DeleteUser 删除用户，最后一个启用的管理员不可删除。
func (s *Service) DeleteUser(id string) error {
	u, ok := s.GetUser(id)
	if !ok {
		return fmt.Errorf("用户不存在: %s", id)
	}
	if u.Role == domain.RoleAdmin && u.Enabled && !s.hasOtherAdmin(u.ID) {
		return fmt.Errorf("系统至少保留一个启用状态的管理员")
	}
	return s.store.Delete(usersDocType, id)
}

// ChangePassword 用户本人修改密码，成功后旧令牌全部失效。
func (s *Service) ChangePassword(userID, oldPassword, newPassword string) error {
	u, ok := s.GetUser(userID)
	if !ok {
		return fmt.Errorf("用户不存在")
	}
	if u.Source != domain.UserSourceLocal {
		return fmt.Errorf("企业账号密码由统一认证维护")
	}
	if _, err := s.provider.Authenticate(context.Background(), u.Username, oldPassword); err != nil {
		return fmt.Errorf("原密码错误")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	u.TokenVersion++
	return s.store.Put(usersDocType, u.ID, u)
}

// hasOtherAdmin 是否还存在其他启用的管理员。
func (s *Service) hasOtherAdmin(excludeID string) bool {
	for _, u := range store.ListInto[domain.User](s.store, usersDocType) {
		if u.ID != excludeID && u.Role == domain.RoleAdmin && u.Enabled {
			return true
		}
	}
	return false
}

// ---------- 登录锁定 ----------

func (s *Service) checkLock(username string) error {
	key := strings.ToLower(strings.TrimSpace(username))
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.failures[key]
	if !ok {
		return nil
	}
	now := time.Now()
	if st.lockedUntil.After(now) {
		return ErrTooManyAttempts
	}
	if st.lockedUntil.IsZero() && now.Sub(st.firstAt) > failureWindow {
		delete(s.failures, key)
	}
	return nil
}

func (s *Service) recordFailure(username string) {
	key := strings.ToLower(strings.TrimSpace(username))
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	st, ok := s.failures[key]
	if !ok || (st.lockedUntil.IsZero() && now.Sub(st.firstAt) > failureWindow) {
		st = &failureState{firstAt: now}
		s.failures[key] = st
	}
	st.count++
	if st.count >= maxFailures {
		st.lockedUntil = now.Add(lockDuration)
		st.count = 0
		st.firstAt = now
	}
}

func (s *Service) clearFailures(username string) {
	key := strings.ToLower(strings.TrimSpace(username))
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, key)
}

func randomSecret() []byte {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return []byte(fmt.Sprintf("aiops-%d", time.Now().UnixNano()))
	}
	return buf
}

// hashPasswordLoose 初始管理员密码来自环境变量，允许短于界面修改时的长度限制。
func hashPasswordLoose(pwd string) (string, error) {
	if strings.TrimSpace(pwd) == "" {
		return "", fmt.Errorf("密码不能为空")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
