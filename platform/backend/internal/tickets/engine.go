package tickets

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// 工单动作错误：处理器据此返回 4xx。
var (
	// ErrForbidden 当前用户无权执行该动作。
	ErrForbidden = errors.New("无权执行该工单操作")
	// ErrVersionConflict 工单已被他人更新。
	ErrVersionConflict = errors.New("工单已被他人更新，请刷新后重试")
)

// systemActor 系统自动动作（告警恢复自动处置）的操作人。
var systemActor = &domain.User{ID: "system", Username: "system", DisplayName: "系统", Role: domain.RoleAdmin}

// Engine 工单引擎：排障会话 → 工单自动映射 + 认领/指派/处置/关闭状态机。
type Engine struct {
	mu         sync.Mutex // 建单与状态流转串行，配合 Version 做并发保护
	diag       *diagnose.Engine
	store      store.Store
	backfilled bool // 历史字段只回填一次；新工单在 fromSession 里直接写入
}

func New(de *diagnose.Engine, st store.Store) *Engine {
	return &Engine{diag: de, store: st}
}

// SyncFromSessions 扫描所有排障会话，把 status=root_caused 的会话自动生成工单（幂等）。
func (e *Engine) SyncFromSessions() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	sessions := e.diag.List(500)
	byID := make(map[string]*domain.DiagnosisSession, len(sessions))
	existing := e.loadAll()

	created := 0
	for i := range sessions {
		sess := &sessions[i]
		if !sess.CreatedAt.IsZero() {
			byID[sess.ID] = sess
		}
		// 条件：status=root_caused 或（status=investigating 且已有根因候选且 confidence≥0.3）
		hasRoot := sess.Status == "root_caused" ||
			(len(sess.Candidates) > 0 && sess.Confidence >= 0.3)
		if !hasRoot {
			continue
		}
		if existing[sess.ID] {
			continue // 已存在对应工单
		}
		t := e.fromSession(sess)
		if err := e.store.Put("ticket", t.ID, t); err == nil {
			existing[sess.ID] = true
			created++
		}
	}
	if !e.backfilled {
		e.backfillDetectedAt(byID)
		e.backfilled = true
	}
	return created
}

// backfillDetectedAt 为历史工单补「发现时间」：优先来源会话创建时间，缺失时退回建单时间。
// 已处置/已关闭但缺 ResolvedAt 的工单用最近更新时间兜底，保证处置时长可计算。
func (e *Engine) backfillDetectedAt(byID map[string]*domain.DiagnosisSession) {
	for _, t := range store.ListInto[domain.Ticket](e.store, "ticket") {
		changed := false
		if t.DetectedAt == nil {
			detected := t.CreatedAt
			if sess, ok := byID[t.SourceID]; ok {
				detected = sess.CreatedAt
			}
			t.DetectedAt = &detected
			changed = true
		}
		if t.Assignee == "" && t.AssigneeID == "" {
			if sess, ok := byID[t.SourceID]; ok && sess.Ticket != nil && sess.Ticket.Entity.Owner != "" {
				t.Assignee = sess.Ticket.Entity.Owner
				changed = true
			}
		}
		if t.ResolvedAt == nil && (t.Status == domain.TicketResolved || t.Status == domain.TicketClosed) {
			resolved := t.UpdatedAt
			t.ResolvedAt = &resolved
			changed = true
		}
		if t.Version == 0 {
			t.Version = 1
			changed = true
		}
		if len(t.History) == 0 && t.Status != "" {
			t.History = []domain.TicketEvent{{
				At:        t.CreatedAt,
				ActorID:   "system",
				ActorName: "系统",
				Action:    "created",
				To:        domain.TicketOpen,
			}}
			changed = true
		}
		if changed {
			_ = e.store.Put("ticket", t.ID, t)
		}
	}
}

// loadAll 直接从存储加载全部工单（不触发 SyncFromSessions）。
func (e *Engine) loadAll() map[string]bool {
	out := store.ListInto[domain.Ticket](e.store, "ticket")
	existing := map[string]bool{}
	for _, t := range out {
		if t.SourceType == "diagnosis" {
			existing[t.SourceID] = true
		}
	}
	return existing
}

// fromSession 把排障会话映射成工单。
func (e *Engine) fromSession(sess *domain.DiagnosisSession) domain.Ticket {
	now := time.Now()
	detected := sess.CreatedAt
	if detected.IsZero() {
		detected = now
	}
	t := domain.Ticket{
		ID:          store.NewID("ticket"),
		SourceType:  "diagnosis",
		SourceID:    sess.ID,
		Title:       firstNonEmpty(sess.RootCause, sess.Symptom),
		AppID:       sess.AppID,
		MwType:      sess.MiddlewareType,
		Addr:        sess.Addr,
		Confidence:  sess.Confidence,
		Status:      domain.TicketOpen,
		Symptom:     sess.Symptom,
		RootCause:   sess.RootCause,
		EvidenceRef: fmt.Sprintf("/diagnosis/%s", sess.ID),
		DetectedAt:  &detected,
		CreatedAt:   now,
		UpdatedAt:   now,
		Version:     1,
		History: []domain.TicketEvent{{
			At:        now,
			ActorID:   "system",
			ActorName: "系统",
			Action:    "created",
			To:        domain.TicketOpen,
			Note:      "排障会话定位到根因，自动生成工单",
		}},
	}
	// 处理人建议：沿用问题单实体上由 CMDB 资产匹配到的负责人（仍需本人认领）
	if sess.Ticket != nil {
		t.Assignee = sess.Ticket.Entity.Owner
	}
	// 优先级：confidence ≥ 0.8 → P0；≥ 0.6 → P1；≥ 0.4 → P2；其余 P3
	switch {
	case sess.Confidence >= 0.8:
		t.Priority = domain.TicketP0
	case sess.Confidence >= 0.6:
		t.Priority = domain.TicketP1
	case sess.Confidence >= 0.4:
		t.Priority = domain.TicketP2
	default:
		t.Priority = domain.TicketP3
	}
	// 根因候选作为建议
	if len(sess.Candidates) > 0 {
		c := sess.Candidates[0]
		t.Suggestion = fmt.Sprintf("[%s] %s\n建议: %s", c.Source, c.Title, c.Reason)
	}
	// 证据链步骤摘要
	for _, step := range sess.Steps {
		t.Findings = append(t.Findings, fmt.Sprintf("#%d [%s] %s (%s)", step.No, step.Source, step.Title, step.Status))
	}
	return t
}

// List 列出全部工单（按创建时间倒序），并附带 MTTA/MTTR。
func (e *Engine) List() []domain.Ticket {
	// 每次 List 前做一次同步，保证新 root_caused 会话自动生成工单
	e.SyncFromSessions()
	out := store.ListInto[domain.Ticket](e.store, "ticket")
	for i := range out {
		decorate(&out[i])
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Get 按 ID 取工单。
func (e *Engine) Get(id string) (*domain.Ticket, bool) {
	var t domain.Ticket
	if store.GetInto(e.store, "ticket", id, &t) {
		decorate(&t)
		return &t, true
	}
	return nil, false
}

// ---------- 状态机动作 ----------

// Claim 认领：待处理工单由 ops/admin 认领，进入 assigned。
func (e *Engine) Claim(id string, actor *domain.User, version *int) (*domain.Ticket, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return e.transition(id, version, func(t *domain.Ticket) error {
		if t.Status != domain.TicketOpen {
			return fmt.Errorf("仅待处理工单可认领，当前状态: %s", t.Status)
		}
		now := time.Now()
		t.AssigneeID = actor.ID
		t.Assignee = displayName(actor)
		t.AssignedAt = &now
		t.Status = domain.TicketAssigned
		e.record(t, actor, "claim", domain.TicketOpen, domain.TicketAssigned, "")
		return nil
	})
}

// Assign 指派 / 改派（仅管理员）：待处理或已认领工单可指定处理人。
func (e *Engine) Assign(id string, assignee *domain.User, actor *domain.User, note string, version *int) (*domain.Ticket, error) {
	if actor == nil || actor.Role != domain.RoleAdmin {
		return nil, ErrForbidden
	}
	if assignee == nil {
		return nil, fmt.Errorf("处理人不存在")
	}
	return e.transition(id, version, func(t *domain.Ticket) error {
		if t.Status != domain.TicketOpen && t.Status != domain.TicketAssigned {
			return fmt.Errorf("仅待处理/已指派工单可指派，当前状态: %s", t.Status)
		}
		from := t.Status
		now := time.Now()
		t.AssigneeID = assignee.ID
		t.Assignee = displayName(assignee)
		t.AssignedAt = &now
		t.Status = domain.TicketAssigned
		action := "assign"
		if from == domain.TicketAssigned {
			action = "reassign"
		}
		e.record(t, actor, action, from, domain.TicketAssigned, note)
		return nil
	})
}

// Start 开始处置：认领后仅处理人本人或管理员可操作；待处理工单直接开始则自动认领。
func (e *Engine) Start(id string, actor *domain.User, version *int) (*domain.Ticket, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return e.transition(id, version, func(t *domain.Ticket) error {
		switch t.Status {
		case domain.TicketOpen:
			// 未认领直接开始：视为本人认领
			now := time.Now()
			t.AssigneeID = actor.ID
			t.Assignee = displayName(actor)
			t.AssignedAt = &now
			t.Status = domain.TicketAssigned
			e.record(t, actor, "claim", domain.TicketOpen, domain.TicketAssigned, "")
		case domain.TicketAssigned:
			if !canHandle(t, actor) {
				return ErrForbidden
			}
		default:
			return fmt.Errorf("当前状态不可开始处置: %s", t.Status)
		}
		from := t.Status
		now := time.Now()
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
		t.Status = domain.TicketInProgress
		e.record(t, actor, "start", from, domain.TicketInProgress, "")
		return nil
	})
}

// Resolve 标记已处置：处置中工单由处理人本人或管理员完成，进入 resolved（待人工确认关闭）。
func (e *Engine) Resolve(id string, actor *domain.User, note string, version *int) (*domain.Ticket, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return e.transition(id, version, func(t *domain.Ticket) error {
		if t.Status != domain.TicketInProgress {
			return fmt.Errorf("仅处置中工单可标记已处置，当前状态: %s", t.Status)
		}
		if !canHandle(t, actor) {
			return ErrForbidden
		}
		now := time.Now()
		t.Status = domain.TicketResolved
		t.ResolvedAt = &now
		e.record(t, actor, "resolve", domain.TicketInProgress, domain.TicketResolved, note)
		return nil
	})
}

// Close 关闭工单：必须由人工对 resolved 工单确认关闭。
func (e *Engine) Close(id string, actor *domain.User, note string, version *int) (*domain.Ticket, error) {
	if actor == nil {
		return nil, ErrForbidden
	}
	return e.transition(id, version, func(t *domain.Ticket) error {
		if t.Status != domain.TicketResolved {
			return fmt.Errorf("仅已处置工单可关闭，当前状态: %s", t.Status)
		}
		if !canHandle(t, actor) {
			return ErrForbidden
		}
		now := time.Now()
		t.Status = domain.TicketClosed
		t.ClosedAt = &now
		e.record(t, actor, "close", domain.TicketResolved, domain.TicketClosed, note)
		return nil
	})
}

// Reopen 重开（仅管理员）：已处置/已关闭工单回到待处理，清空处理人与时间字段。
func (e *Engine) Reopen(id string, actor *domain.User, note string, version *int) (*domain.Ticket, error) {
	if actor == nil || actor.Role != domain.RoleAdmin {
		return nil, ErrForbidden
	}
	return e.transition(id, version, func(t *domain.Ticket) error {
		if t.Status != domain.TicketResolved && t.Status != domain.TicketClosed {
			return fmt.Errorf("仅已处置/已关闭工单可重开，当前状态: %s", t.Status)
		}
		from := t.Status
		t.Status = domain.TicketOpen
		t.Assignee = ""
		t.AssigneeID = ""
		t.AssignedAt = nil
		t.StartedAt = nil
		t.ResolvedAt = nil
		t.ClosedAt = nil
		t.ReopenCount++
		e.record(t, actor, "reopen", from, domain.TicketOpen, note)
		return nil
	})
}

// AutoResolve 告警恢复时由系统自动标记已处置；不自动关闭，必须人工确认。
func (e *Engine) AutoResolve(id string, reason string) bool {
	changed := false
	_, err := e.transition(id, nil, func(t *domain.Ticket) error {
		switch t.Status {
		case domain.TicketOpen, domain.TicketAssigned, domain.TicketInProgress:
		case domain.TicketResolved, domain.TicketClosed:
			return errNoop // 已处置/已关闭保持幂等
		default:
			return errNoop
		}
		from := t.Status
		now := time.Now()
		t.Status = domain.TicketResolved
		t.ResolvedAt = &now
		t.Findings = append(t.Findings, fmt.Sprintf("%s（%s），系统自动标记已处置，待人工确认关闭",
			reason, now.Format("2006-01-02 15:04:05")))
		e.record(t, systemActor, "auto_resolve", from, domain.TicketResolved, reason)
		changed = true
		return nil
	})
	return err == nil && changed
}

var errNoop = errors.New("noop")

// transition 统一加锁读取 → 校验 → 变更 → 落库，保证并发安全与版本一致。
func (e *Engine) transition(id string, version *int, fn func(*domain.Ticket) error) (*domain.Ticket, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var t domain.Ticket
	if !store.GetInto(e.store, "ticket", id, &t) {
		return nil, fmt.Errorf("工单不存在: %s", id)
	}
	if version != nil && *version > 0 && t.Version != *version {
		return nil, ErrVersionConflict
	}
	if err := fn(&t); err != nil {
		if errors.Is(err, errNoop) {
			return &t, nil
		}
		return nil, err
	}
	t.UpdatedAt = time.Now()
	t.Version++
	if err := e.store.Put("ticket", t.ID, t); err != nil {
		return nil, err
	}
	decorate(&t)
	return &t, nil
}

// record 追加操作历史。
func (e *Engine) record(t *domain.Ticket, actor *domain.User, action string, from, to domain.TicketStatus, note string) {
	t.History = append(t.History, domain.TicketEvent{
		At:        time.Now(),
		ActorID:   actor.ID,
		ActorName: displayName(actor),
		Action:    action,
		From:      from,
		To:        to,
		Note:      note,
	})
}

// canHandle 处理人本人或管理员可推进工单。
func canHandle(t *domain.Ticket, actor *domain.User) bool {
	if actor.Role == domain.RoleAdmin {
		return true
	}
	return t.AssigneeID != "" && t.AssigneeID == actor.ID
}

// decorate 计算 MTTA / MTTR（秒），指标只随读取返回。
func decorate(t *domain.Ticket) {
	t.MTTASeconds, t.MTTRSeconds = 0, 0
	if t.DetectedAt == nil {
		return
	}
	if t.AssignedAt != nil {
		t.MTTASeconds = int64(t.AssignedAt.Sub(*t.DetectedAt).Seconds())
		if t.MTTASeconds < 0 {
			t.MTTASeconds = 0
		}
	}
	if t.ResolvedAt != nil {
		t.MTTRSeconds = int64(t.ResolvedAt.Sub(*t.DetectedAt).Seconds())
		if t.MTTRSeconds < 0 {
			t.MTTRSeconds = 0
		}
	}
}

func displayName(u *domain.User) string {
	if u == nil {
		return "系统"
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
