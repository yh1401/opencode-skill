package domain

import "time"

// ChangeStep 变更方案中的一个步骤。
type ChangeStep struct {
	No         int    `json:"no"`
	Title      string `json:"title"`
	CheckTool  string `json:"checkTool"`  // topo-impact | topo-diff | prometheus-baseline | manual
	ActionHint string `json:"actionHint"` // 对该步骤要做什么的自然语言提示
	Rollback   string `json:"rollback"`   // 该步骤失败时的回退预案
}

// ChangeTemplate 内置割接模板。
type ChangeTemplate struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	TargetType  string       `json:"targetType"` // middleware | app | apollo | service | datacenter
	DefaultEnv  string       `json:"defaultEnv"`
	Description string       `json:"description"`
	Steps       []ChangeStep `json:"steps"`
}

// ChangeTarget 用户输入的变更目标。
type ChangeTarget struct {
	TemplateID string `json:"templateId"`
	TargetType string `json:"targetType"`
	AppID      string `json:"appId,omitempty"`
	MwType     string `json:"mwType,omitempty"`
	MwAddr     string `json:"mwAddr,omitempty"`
	DatePrev   string `json:"datePrev"`  // 对比快照日期，空 = 自动取上一个
	DateToday  string `json:"dateToday"` // 当前快照日期，空 = 最新
	Env        string `json:"env"`
	Notes      string `json:"notes,omitempty"`
}

// RiskResult ValidateRisk 打分结果。
type RiskResult struct {
	Score         float64  `json:"score"`    // 0-1，越高越危险
	Level         string   `json:"level"`    // low | medium | high | critical
	Blockers      []string `json:"blockers"` // 禁止执行条件
	ExplosionApps int      `json:"explosionApps"`
	DriftRatio    float64  `json:"driftRatio"`   // 快照漂移比例 0-1
	RecentFaults  int      `json:"recentFaults"` // 近 30 天同 appId 排障会话数
	Notes         string   `json:"notes,omitempty"`
}

// ChangePlan 生成的变更方案。
type ChangePlan struct {
	ID           string       `json:"id"`
	TemplateID   string       `json:"templateId"`
	TemplateName string       `json:"templateName"`
	Target       ChangeTarget `json:"target"`
	Steps        []ChangeStep `json:"steps"`
	Risk         RiskResult   `json:"risk"`
	CreatedAt    time.Time    `json:"createdAt"`
	CreatedBy    string       `json:"createdBy"`
}
