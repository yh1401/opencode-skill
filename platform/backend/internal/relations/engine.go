package relations

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strings"
	"time"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/topo"
)

// 建议类型：缺失依赖 / 失效依赖 / CMDB 孤儿。
const (
	KindMissing = "missing"
	KindStale   = "stale"
	KindOrphan  = "orphan"
)

// 建议状态：待处理 / 已通过 / 已驳回 / 已执行。
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusApplied  = "applied"
)

// Proposal 关系修正建议：由拓扑快照 Diff 或资源稽核孤儿生成。
type Proposal struct {
	ID             string     `json:"id"`
	Fingerprint    string     `json:"fingerprint"`
	Kind           string     `json:"kind"`
	AppID          string     `json:"appId,omitempty"`
	AppName        string     `json:"appName,omitempty"`
	MiddlewareType string     `json:"middlewareType,omitempty"`
	Addr           string     `json:"connectAddr,omitempty"`
	DepProject     string     `json:"depProject,omitempty"`
	Source         string     `json:"source"` // topo_diff | audit
	Detail         string     `json:"detail"`
	Suggestion     string     `json:"suggestion"`
	AutoFix        bool       `json:"autoFix"`
	Status         string     `json:"status"`
	Note           string     `json:"note,omitempty"`
	FromDate       string     `json:"fromDate,omitempty"`
	ToDate         string     `json:"toDate,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	AppliedAt      *time.Time `json:"appliedAt,omitempty"`
}

// ScanResult 一轮扫描的统计。
type ScanResult struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	Added    int       `json:"added"`
	Removed  int       `json:"removed"`
	Orphans  int       `json:"orphans"`
	Created  int       `json:"created"`
	Skipped  int       `json:"skipped"`
	Total    int       `json:"total"`
	At       time.Time `json:"at"`
	Warnings []string  `json:"warnings,omitempty"`
}

// Stats 建议状态分布。
type Stats struct {
	Pending    int `json:"pending"`
	Approved   int `json:"approved"`
	Rejected   int `json:"rejected"`
	Applied    int `json:"applied"`
	AutoFix    int `json:"autoFix"`
	ManualOnly int `json:"manualOnly"`
	Total      int `json:"total"`
}

// Override 已执行的关系修正记录（追加式覆盖，供拓扑侧对账）。
type Override struct {
	Fingerprint    string    `json:"fingerprint"`
	Kind           string    `json:"kind"`
	Action         string    `json:"action"` // add | remove | manual
	AppID          string    `json:"appId,omitempty"`
	AppName        string    `json:"appName,omitempty"`
	MiddlewareType string    `json:"middlewareType,omitempty"`
	Addr           string    `json:"connectAddr,omitempty"`
	DepProject     string    `json:"depProject,omitempty"`
	ProposalID     string    `json:"proposalId"`
	FromDate       string    `json:"fromDate,omitempty"`
	ToDate         string    `json:"toDate,omitempty"`
	AppliedAt      time.Time `json:"appliedAt"`
}

// Engine 关系修正引擎：Diff / 稽核孤儿 → 建议 → 审批 → 执行留痕。
type Engine struct {
	topo  topo.Provider
	store store.Store
}

func New(tp topo.Provider, st store.Store) *Engine {
	return &Engine{topo: tp, store: st}
}

// Scan 基于拓扑 Diff 与最近一次稽核的 C3 孤儿生成建议；已存在的建议保持原状态。
func (e *Engine) Scan(from, to, env string) (*ScanResult, error) {
	if from == "" || to == "" {
		dates := e.topo.Dates()
		if from == "" && len(dates) >= 2 {
			from = dates[len(dates)-2]
		}
		if to == "" && len(dates) >= 1 {
			to = dates[len(dates)-1]
		}
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("至少需要两个拓扑快照才能生成关系修正建议")
	}
	diff, err := e.topo.Diff(from, to, env)
	if err != nil {
		return nil, err
	}

	res := &ScanResult{From: from, To: to, Added: len(diff.Added), Removed: len(diff.Removed), At: time.Now()}
	for _, it := range diff.Added {
		if e.upsert(diffItemProposal(it, KindMissing, from, to)) {
			res.Created++
		} else {
			res.Skipped++
		}
	}
	for _, it := range diff.Removed {
		if e.upsert(diffItemProposal(it, KindStale, from, to)) {
			res.Created++
		} else {
			res.Skipped++
		}
	}

	// 稽核 C3 反向孤儿：CMDB 有、拓扑无。无法自动修正，只生成人工建议。
	orphans, created, warn := e.orphans()
	if warn != "" {
		res.Warnings = append(res.Warnings, warn)
	}
	res.Orphans = orphans
	res.Created += created
	res.Skipped += orphans - created

	all := e.List("", "")
	res.Total = len(all)
	return res, nil
}

// diffItemProposal 新增依赖 → 建议补录；消失依赖 → 建议清理。
func diffItemProposal(it domain.DiffItem, kind, from, to string) Proposal {
	p := Proposal{
		Kind:           kind,
		AppID:          it.AppID,
		AppName:        it.AppName,
		MiddlewareType: it.MiddlewareType,
		Addr:           it.ConnectAddr,
		DepProject:     it.DepProject,
		Source:         "topo_diff",
		AutoFix:        true,
		Status:         StatusPending,
		FromDate:       from,
		ToDate:         to,
	}
	p.Fingerprint = fingerprint(kind, it.AppID, it.MiddlewareType, it.ConnectAddr, it.DepProject)
	p.ID = proposalID(p.Fingerprint)
	if kind == KindMissing {
		p.Detail = fmt.Sprintf("快照 %s→%s 新增依赖：%s → %s(%s)", from, to, firstNonEmpty(it.AppName, it.AppID), it.MiddlewareType, it.ConnectAddr)
		p.Suggestion = "确认该依赖已在拓扑中登记；如确为漏采，执行修正补录关系"
	} else {
		p.Detail = fmt.Sprintf("快照 %s→%s 消失依赖：%s → %s(%s)", from, to, firstNonEmpty(it.AppName, it.AppID), it.MiddlewareType, it.ConnectAddr)
		p.Suggestion = "确认该依赖已下线；若仍被引用，执行修正清理关系"
	}
	return p
}

// orphans 读取最近一次稽核的 C3 RISK 发现，返回（孤儿数, 新建建议数, 提示）。
func (e *Engine) orphans() (int, int, string) {
	runs := store.ListInto[domain.AuditRun](e.store, "audit_run")
	if len(runs) == 0 {
		return 0, 0, "未找到稽核批次，本次仅基于拓扑 Diff 生成建议"
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	latest := runs[0]
	findings := store.ListInto[domain.AuditFinding](e.store, "audit_finding")
	n, created := 0, 0
	for _, f := range findings {
		if f.RunID != latest.ID || f.CheckCode != "AUDIT_C3" || f.Verdict != domain.AuditVerdictRisk {
			continue
		}
		fp := fingerprint(KindOrphan, "", "server", f.ObjectKey, "")
		p := Proposal{
			Kind:       KindOrphan,
			Addr:       f.ObjectKey,
			Source:     "audit",
			AutoFix:    false,
			Status:     StatusPending,
			Detail:     fmt.Sprintf("CMDB 在线资源未被拓扑引用（%s）：%s", f.Severity, f.Detail),
			Suggestion: "无法自动修正：需人工确认该资源是应纳管进拓扑，还是已在 CMDB 侧清退",
			FromDate:   latest.Date,
			ToDate:     latest.Date,
		}
		p.Fingerprint = fp
		p.ID = proposalID(fp)
		if e.upsert(p) {
			created++
		}
		n++
	}
	return n, created, ""
}

// upsert 幂等写入：已存在则保持状态不变（重新扫描不得覆盖人工审批结果）。
func (e *Engine) upsert(p Proposal) bool {
	var existing Proposal
	if store.GetInto(e.store, "relation_proposal", p.ID, &existing) {
		return false
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	return e.store.Put("relation_proposal", p.ID, p) == nil
}

// List 按状态 / 类型 / 关键字过滤，待处理优先，其余按创建时间倒序。
func (e *Engine) List(status, kind string) []Proposal {
	out := store.ListInto[Proposal](e.store, "relation_proposal")
	for i := range out {
		normalize(&out[i])
	}
	if status != "" {
		filtered := out[:0]
		for _, p := range out {
			if p.Status == status {
				filtered = append(filtered, p)
			}
		}
		out = filtered
	}
	if kind != "" {
		filtered := out[:0]
		for _, p := range out {
			if p.Kind == kind {
				filtered = append(filtered, p)
			}
		}
		out = filtered
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status == StatusPending
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Get 按 ID 取建议。
func (e *Engine) Get(id string) (*Proposal, bool) {
	var p Proposal
	if store.GetInto(e.store, "relation_proposal", id, &p) {
		normalize(&p)
		return &p, true
	}
	return nil, false
}

// normalize 兼容旧数据：未执行的建议不应带零值 appliedAt。
func normalize(p *Proposal) {
	if p.AppliedAt != nil && p.AppliedAt.IsZero() {
		p.AppliedAt = nil
	}
}

// UpdateStatus 审批：pending/approved → approved 或 rejected。
func (e *Engine) UpdateStatus(id, status, note string) (*Proposal, error) {
	p, ok := e.Get(id)
	if !ok {
		return nil, fmt.Errorf("建议不存在: %s", id)
	}
	switch status {
	case StatusApproved, StatusRejected:
	default:
		return nil, fmt.Errorf("非法状态: %s（仅支持 approved / rejected）", status)
	}
	if p.Status == StatusApplied {
		return nil, fmt.Errorf("建议已执行，不能再审批")
	}
	p.Status = status
	p.Note = note
	p.UpdatedAt = time.Now()
	if err := e.store.Put("relation_proposal", p.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Apply 执行修正：仅 approved 且可自动修正的建议；写入覆盖记录并置为 applied。
func (e *Engine) Apply(id string) (*Proposal, error) {
	p, ok := e.Get(id)
	if !ok {
		return nil, fmt.Errorf("建议不存在: %s", id)
	}
	if p.Status == StatusApplied {
		return p, nil // 幂等
	}
	if p.Status != StatusApproved {
		return nil, fmt.Errorf("仅已通过的建议可执行，当前状态: %s", p.Status)
	}
	if !p.AutoFix {
		return nil, fmt.Errorf("该建议无法自动修正，请人工确认纳管或清退")
	}
	action := "add"
	if p.Kind == KindStale {
		action = "remove"
	}
	now := time.Now()
	ov := Override{
		Fingerprint:    p.Fingerprint,
		Kind:           p.Kind,
		Action:         action,
		AppID:          p.AppID,
		AppName:        p.AppName,
		MiddlewareType: p.MiddlewareType,
		Addr:           p.Addr,
		DepProject:     p.DepProject,
		ProposalID:     p.ID,
		FromDate:       p.FromDate,
		ToDate:         p.ToDate,
		AppliedAt:      now,
	}
	if err := e.store.Put("relation_override", p.ID, ov); err != nil {
		return nil, err
	}
	p.Status = StatusApplied
	p.AppliedAt = &now
	p.UpdatedAt = now
	if err := e.store.Put("relation_proposal", p.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Stats 建议状态分布。
func (e *Engine) Stats() Stats {
	var s Stats
	for _, p := range e.List("", "") {
		switch p.Status {
		case StatusPending:
			s.Pending++
		case StatusApproved:
			s.Approved++
		case StatusRejected:
			s.Rejected++
		case StatusApplied:
			s.Applied++
		}
		if p.AutoFix {
			s.AutoFix++
		} else {
			s.ManualOnly++
		}
		s.Total++
	}
	return s
}

// Overrides 已执行修正（按执行时间倒序）。
func (e *Engine) Overrides() []Override {
	out := store.ListInto[Override](e.store, "relation_override")
	sort.Slice(out, func(i, j int) bool { return out[i].AppliedAt.After(out[j].AppliedAt) })
	return out
}

func fingerprint(kind, appID, mwType, addr, dep string) string {
	return strings.Join([]string{kind, strings.TrimSpace(appID), strings.ToLower(strings.TrimSpace(mwType)), strings.TrimSpace(addr), strings.TrimSpace(dep)}, "|")
}

// proposalID 由指纹派生，保证同一关系反复扫描只产生一条建议。
func proposalID(fp string) string {
	sum := sha1.Sum([]byte(fp))
	return fmt.Sprintf("rel-%x", sum)[:20]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
