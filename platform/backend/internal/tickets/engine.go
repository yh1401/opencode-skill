package tickets

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// Engine 工单引擎：排障会话 → 工单自动映射 + 状态流转。
type Engine struct {
	mu    sync.Mutex // 同步建单需串行，避免并发请求为同一会话重复建单
	diag  *diagnose.Engine
	store store.Store
}

func New(de *diagnose.Engine, st store.Store) *Engine {
	return &Engine{diag: de, store: st}
}

// SyncFromSessions 扫描所有排障会话，把 status=root_caused 的会话自动生成工单（幂等）。
func (e *Engine) SyncFromSessions() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	sessions := e.diag.List(500)
	existing := e.loadAll()

	created := 0
	for _, sess := range sessions {
		// 条件：status=root_caused 或（status=investigating 且已有根因候选且 confidence≥0.3）
		hasRoot := sess.Status == "root_caused" ||
			(len(sess.Candidates) > 0 && sess.Confidence >= 0.3)
		if !hasRoot {
			continue
		}
		if existing[sess.ID] {
			continue // 已存在对应工单
		}
		t := e.fromSession(&sess)
		if err := e.store.Put("ticket", t.ID, t); err == nil {
			existing[sess.ID] = true
			created++
		}
	}
	return created
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
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
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

// List 列出全部工单（按创建时间倒序）。
func (e *Engine) List() []domain.Ticket {
	// 每次 List 前做一次同步，保证新 root_caused 会话自动生成工单
	e.SyncFromSessions()
	out := store.ListInto[domain.Ticket](e.store, "ticket")
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Get 按 ID 取工单。
func (e *Engine) Get(id string) (*domain.Ticket, bool) {
	var t domain.Ticket
	if store.GetInto(e.store, "ticket", id, &t) {
		return &t, true
	}
	return nil, false
}

// UpdateStatus 状态变更：带基本状态机校验。
func (e *Engine) UpdateStatus(id string, newStatus domain.TicketStatus, assignee string) (*domain.Ticket, error) {
	t, ok := e.Get(id)
	if !ok {
		return nil, fmt.Errorf("工单不存在: %s", id)
	}
	if !validTransition(t.Status, newStatus) {
		return nil, fmt.Errorf("状态流转非法: %s → %s", t.Status, newStatus)
	}
	t.Status = newStatus
	if assignee != "" {
		t.Assignee = assignee
	}
	t.UpdatedAt = time.Now()
	if newStatus == domain.TicketClosed {
		now := time.Now()
		t.ClosedAt = &now
	}
	if err := e.store.Put("ticket", t.ID, t); err != nil {
		return nil, err
	}
	return t, nil
}

// Resolve 标记工单已处置（直接 resolved → closed，跳过中间态）。
func (e *Engine) Resolve(id string) (*domain.Ticket, error) {
	t, ok := e.Get(id)
	if !ok {
		return nil, fmt.Errorf("工单不存在: %s", id)
	}
	if t.Status == domain.TicketClosed {
		return t, nil // 幂等
	}
	t.Status = domain.TicketResolved
	t.UpdatedAt = time.Now()
	if err := e.store.Put("ticket", t.ID, t); err != nil {
		return nil, err
	}
	return t, nil
}

// validTransition 简单状态机校验。
func validTransition(from, to domain.TicketStatus) bool {
	if from == to {
		return true // 允许同状态 no-op
	}
	switch to {
	case domain.TicketOpen:
		return false // open 是初始态，只允许从非 closed 回退
	case domain.TicketAssigned:
		return from == domain.TicketOpen || from == domain.TicketInProgress
	case domain.TicketInProgress:
		return from == domain.TicketAssigned || from == domain.TicketOpen
	case domain.TicketResolved:
		return from == domain.TicketInProgress || from == domain.TicketOpen || from == domain.TicketAssigned
	case domain.TicketClosed:
		return from == domain.TicketResolved || from == domain.TicketOpen
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
