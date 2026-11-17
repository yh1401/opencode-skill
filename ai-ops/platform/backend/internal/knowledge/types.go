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

	// LLM 结构化总结：手动 enrich 端点触发写入，失败时为零值，静默降级不阻塞入库。
	AISummary     string   `json:"aiSummary,omitempty"`     // 一两句故障总结（如 "RocketMQ broker 磁盘过高，扩容后恢复"）
	AICategory    string   `json:"aiCategory,omitempty"`    // 根因分类（基础设施/中间件/业务代码/配置变更/资源不足/外部依赖/待人工确认）
	AIKeywords    []string `json:"aiKeywords,omitempty"`    // 模型抽取的关键词（补充检索召回）
	AIConfidence  float64  `json:"aiConfidence,omitempty"`  // 模型置信度 0-100（对根因判断的把握）
	AIRebuildAt   time.Time `json:"aiRebuildAt,omitempty"`   // 最后一次 LLM 总结成功的时间
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
