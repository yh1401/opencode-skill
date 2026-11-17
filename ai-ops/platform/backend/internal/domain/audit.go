package domain

import "time"

// 稽核四态结论（与巡检口径对齐）。
const (
	AuditVerdictRisk          = "RISK"
	AuditVerdictNotTriggered  = "NOT_TRIGGERED"
	AuditVerdictNoData        = "NO_DATA"
	AuditVerdictNotApplicable = "NOT_APPLICABLE"
)

// AuditFinding 资源稽核发现（四态结论，对齐巡检口径）。
type AuditFinding struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	CheckCode  string         `json:"checkCode"`  // AUDIT_C1 存在性 / C2 状态一致性 / C3 反向孤儿 / C4 指标佐证
	ObjectType string         `json:"objectType"` // middleware | server
	ObjectKey  string         `json:"objectKey"`
	Severity   string         `json:"severity"` // P1 | P2 | P3
	Verdict    string         `json:"verdict"`  // RISK | NOT_TRIGGERED | NO_DATA | NOT_APPLICABLE
	Detail     string         `json:"detail"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AuditStats 稽核批次统计。
type AuditStats struct {
	Items         int `json:"items"`
	Risk          int `json:"risk"`
	NotTriggered  int `json:"notTriggered"`
	NoData        int `json:"noData"`
	NotApplicable int `json:"notApplicable"`
}

// AuditRun 资源稽核批次。
type AuditRun struct {
	ID         string     `json:"id"`
	Date       string     `json:"date"` // topo 快照日期
	Env        string     `json:"env"`
	Status     string     `json:"status"` // running | succeeded | failed
	Stats      AuditStats `json:"stats"`
	Message    string     `json:"message,omitempty"`
	Sources    []string   `json:"sources"` // 参与的数据源：topo/cmdb/prometheus
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt time.Time  `json:"finishedAt"`
}
