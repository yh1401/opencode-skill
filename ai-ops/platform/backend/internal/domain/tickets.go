package domain

import "time"

// TicketPriority 工单优先级。
type TicketPriority string

const (
	TicketP0 TicketPriority = "P0"
	TicketP1 TicketPriority = "P1"
	TicketP2 TicketPriority = "P2"
	TicketP3 TicketPriority = "P3"
)

// TicketStatus 工单状态机：open → assigned → in_progress → resolved → closed。
// 认领 / 指派进入 assigned，处置完成后由人工确认关闭；重开仅管理员可执行。
type TicketStatus string

const (
	TicketOpen       TicketStatus = "open"
	TicketAssigned   TicketStatus = "assigned"
	TicketInProgress TicketStatus = "in_progress"
	TicketResolved   TicketStatus = "resolved"
	TicketClosed     TicketStatus = "closed"
)

// TicketEvent 工单操作留痕：谁、何时、做了什么、从什么状态到什么状态。
type TicketEvent struct {
	At        time.Time    `json:"at"`
	ActorID   string       `json:"actorId"`
	ActorName string       `json:"actorName"`
	Action    string       `json:"action"`
	From      TicketStatus `json:"from,omitempty"`
	To        TicketStatus `json:"to,omitempty"`
	Note      string       `json:"note,omitempty"`
}

// Ticket 隐患/故障工单，来源于排障会话自动生成。
type Ticket struct {
	ID          string         `json:"id"`
	SourceType  string         `json:"sourceType"` // diagnosis
	SourceID    string         `json:"sourceId"`   // diagnosis session id
	Title       string         `json:"title"`
	AppID       string         `json:"appId,omitempty"`
	MwType      string         `json:"mwType,omitempty"`
	Addr        string         `json:"addr,omitempty"`
	Confidence  float64        `json:"confidence"`
	Status      TicketStatus   `json:"status"`
	Priority    TicketPriority `json:"priority"`
	Symptom     string         `json:"symptom"`
	RootCause   string         `json:"rootCause"`
	Suggestion  string         `json:"suggestion,omitempty"`
	Assignee    string         `json:"assignee,omitempty"`
	AssigneeID  string         `json:"assigneeId,omitempty"`
	Findings    []string       `json:"findings"`
	EvidenceRef string         `json:"evidenceRef"`
	// DetectedAt 发现时间：取来源排障会话的创建时间（即告警首次被定位的时刻）。
	DetectedAt *time.Time `json:"detectedAt,omitempty"`
	// AssignedAt 认领/指派时间，用于计算 MTTA。
	AssignedAt *time.Time `json:"assignedAt,omitempty"`
	// StartedAt 开始处置时间。
	StartedAt *time.Time `json:"startedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	// ResolvedAt 处置完成时间：标记「已处置」时写入，用于计算处置时长。
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	ClosedAt   *time.Time `json:"closedAt,omitempty"`
	// ReopenCount 重开次数。
	ReopenCount int `json:"reopenCount"`
	// Version 并发写版本号，动作接口可带上做乐观锁校验。
	Version int `json:"version"`
	// History 操作历史，按时间顺序追加。
	History []TicketEvent `json:"history"`
	// MTTASeconds / MTTRSeconds 读取时计算的指标，不参与持久化语义。
	MTTASeconds int64 `json:"mttaSeconds,omitempty"`
	MTTRSeconds int64 `json:"mttrSeconds,omitempty"`
}
