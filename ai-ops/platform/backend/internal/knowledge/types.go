package knowledge

import "time"

// Case 历史故障案例：由排障会话归档而来，供检索与相似案例推荐。
type Case struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	AppID           string     `json:"appId,omitempty"`
	MiddlewareType  string     `json:"middlewareType,omitempty"`
	Addr            string     `json:"addr,omitempty"`
	Symptom         string     `json:"symptom"`
	RootCause       string     `json:"rootCause,omitempty"`
	Confidence      float64    `json:"confidence"`
	Status          string     `json:"status"`
	Steps           int        `json:"steps"`
	Evidence        []Evidence `json:"evidence,omitempty"`
	TicketID        string     `json:"ticketId,omitempty"`
	TicketStatus    string     `json:"ticketStatus,omitempty"`
	Reviewed        bool       `json:"reviewed"`
	Accurate        bool       `json:"accurate"`
	ActualRootCause string     `json:"actualRootCause,omitempty"`
	Keywords        []string   `json:"keywords,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	FinishedAt      time.Time  `json:"finishedAt,omitempty"`
	Score           int        `json:"score,omitempty"`
}

// Evidence 证据链摘要（不含大体量原始结果）。
type Evidence struct {
	No      int    `json:"no"`
	Source  string `json:"source"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Latency int64  `json:"latencyMs"`
}

// Stats 知识库概览。
type Stats struct {
	Total    int        `json:"total"`
	Apps     int        `json:"apps"`
	Reviewed int        `json:"reviewed"`
	Accurate int        `json:"accurate"`
	Accuracy float64    `json:"accuracy"`
	TopApps  []AppCount `json:"topApps"`
}

// AppCount 应用案例数。
type AppCount struct {
	AppID string `json:"appId"`
	Count int    `json:"count"`
}
