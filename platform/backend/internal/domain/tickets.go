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
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	ClosedAt    *time.Time     `json:"closedAt,omitempty"`
}
