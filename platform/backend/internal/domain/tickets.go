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
type TicketStatus string

const (
	TicketOpen       TicketStatus = "open"
	TicketAssigned   TicketStatus = "assigned"
	TicketInProgress TicketStatus = "in_progress"
	TicketResolved   TicketStatus = "resolved"
	TicketClosed     TicketStatus = "closed"
)

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
	Findings    []string       `json:"findings"`
	EvidenceRef string         `json:"evidenceRef"`
	// DetectedAt 发现时间：取来源排障会话的创建时间（即告警首次被定位的时刻）。
	DetectedAt *time.Time `json:"detectedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	// ResolvedAt 处置完成时间：标记「已处置」时写入，用于计算处置时长。
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	ClosedAt   *time.Time `json:"closedAt,omitempty"`
}
