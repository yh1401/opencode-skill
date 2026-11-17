package domain

import "time"

// TopoRow 对应 topo 事实表 operation_topology 的一行（仅保留平台侧需要的列）。
type TopoRow struct {
	ID                int64  `json:"id"`
	ApolloHostID      int64  `json:"apolloHostId"`
	ApolloHostName    string `json:"apolloHostName"`
	ApolloHostAddr    string `json:"apolloHostAddr"`
	ApolloAppID       string `json:"apolloAppId"`
	ApolloAppName     string `json:"apolloAppName"`
	AppDept           string `json:"appDept"`
	AppOwner          string `json:"appOwner"`
	Node              string `json:"node"`
	EngProject        string `json:"engProject"`
	DepProject        string `json:"depProject"`
	PPApplication     string `json:"ppApplication"`
	Namespace         string `json:"namespace"`
	ConfigKey         string `json:"configKey"`
	MiddlewareType    string `json:"middlewareType"`
	ConnectAddr       string `json:"connectAddr"`
	MiddlewareDb      string `json:"middlewareDb"`
	MiddlewareProject string `json:"middlewareProject"`
	Env               string `json:"env"`
	SyncDate          string `json:"syncDate"`
	Source            string `json:"source"`

	// 查询期由两跳关系回填的多值属性（file 数据源由 ADR×DPR 本地回填，http 数据源由 topo 服务返回）
	DepProjectList    []string `json:"depProjects,omitempty"`
	NodeList          []string `json:"nodes,omitempty"`
	EngProjectList    []string `json:"engProjects,omitempty"`
	PpApplicationList []string `json:"ppApplications,omitempty"`
}

// AppBrief 应用摘要（用于下拉与拓扑入口）。
type AppBrief struct {
	AppID       string   `json:"appId"`
	AppName     string   `json:"appName"`
	DepProjects []string `json:"depProjects"`
	Nodes       []string `json:"nodes"`
	MwTypes     []string `json:"mwTypes"`
	DepApps     int      `json:"depApps"`
}

// GraphNode 图节点：app:<appKey> 或 mw:<type>:<addr>。
type GraphNode struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Kind    string         `json:"kind"` // app | middleware
	Type    string         `json:"type,omitempty"`
	AppID   string         `json:"appId,omitempty"`
	Addr    string         `json:"addr,omitempty"`
	DepApps int            `json:"depApps"`
	Degree  int            `json:"degree"`
	Env     string         `json:"env,omitempty"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// GraphEdge 边：应用 → 中间件。
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// Graph 拓扑图。
type Graph struct {
	SyncDate  string      `json:"syncDate"`
	ViewMode  string      `json:"viewMode"` // folded | expanded
	Env       string      `json:"env"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Truncated bool        `json:"truncated"`
}

// ImpactApp 影响面中的应用。
type ImpactApp struct {
	AppKey     string `json:"appKey"`
	AppID      string `json:"appId"`
	AppName    string `json:"appName"`
	IsTarget   bool   `json:"isTarget"`
	DepProject string `json:"depProject,omitempty"`
}

// ImpactMW 影响面中的中间件。
type ImpactMW struct {
	Type     string `json:"type"`
	Addr     string `json:"addr"`
	DepApps  int    `json:"depApps"`
	IsTarget bool   `json:"isTarget"`
}

// Impact 影响面（爆炸半径）。
type Impact struct {
	SyncDate    string      `json:"syncDate"`
	Level       string      `json:"level"`  // app | middleware
	Target      string      `json:"target"` // appKey 或 type:addr
	Apps        []ImpactApp `json:"apps"`
	Middlewares []ImpactMW  `json:"middlewares"`
	TotalApps   int         `json:"totalApps"`
	Note        string      `json:"note,omitempty"`
}

// DiffItem 快照差异项。
type DiffItem struct {
	AppKey         string `json:"appKey"`
	AppID          string `json:"appId"`
	AppName        string `json:"appName"`
	MiddlewareType string `json:"middlewareType"`
	ConnectAddr    string `json:"connectAddr"`
	DepProject     string `json:"depProject,omitempty"`
}

// TopoDiff 两个快照的依赖差异。
type TopoDiff struct {
	From    string     `json:"from"`
	To      string     `json:"to"`
	Added   []DiffItem `json:"added"`
	Removed []DiffItem `json:"removed"`
}

// MwTypeStat 中间件类型统计。
type MwTypeStat struct {
	Type      string `json:"type"`
	Rows      int    `json:"rows"`
	Instances int    `json:"instances"`
	Apps      int    `json:"apps"`
}

// Scope 巡检范围。
type Scope struct {
	AppIDs  []string `json:"appIds"`
	MwTypes []string `json:"mwTypes"`
	Env     string   `json:"env"`
	Date    string   `json:"date,omitempty"` // 锁定快照日期；空 = 最新
}

// InspectionTask 巡检任务。
type InspectionTask struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Scene          string                  `json:"scene"`
	Scope          Scope                   `json:"scope"`
	RuleSetVersion string                  `json:"ruleSetVersion"`
	Cron           string                  `json:"cron"`
	Enabled        bool                    `json:"enabled"`
	CreatedAt      time.Time               `json:"createdAt"`
	Rules          map[string]RuleOverride `json:"rules,omitempty"` // 规则覆盖：code → override
}

// RunStats 批次统计（与四态结论对齐）。
type RunStats struct {
	Items         int `json:"items"`
	Risk          int `json:"risk"`
	NotTriggered  int `json:"notTriggered"`
	NoData        int `json:"noData"`
	NotApplicable int `json:"notApplicable"`
	Error         int `json:"error"`
}

// InspectionRun 巡检批次。
type InspectionRun struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"taskId"`
	TaskName     string    `json:"taskName"`
	Trigger      string    `json:"trigger"` // cron | manual | api
	TopoSyncDate string    `json:"topoSyncDate"`
	ViewMode     string    `json:"viewMode"`
	Status       string    `json:"status"` // pending | running | succeeded | failed | partial
	Stats        RunStats  `json:"stats"`
	Message      string    `json:"message,omitempty"`
	StartedAt    time.Time `json:"startedAt"`
	FinishedAt   time.Time `json:"finishedAt"`
}

// Finding 巡检发现（四态结论）。
type Finding struct {
	ID         string         `json:"id"`
	RunID      string         `json:"runId"`
	RuleCode   string         `json:"ruleCode"`
	Title      string         `json:"title"`
	Severity   string         `json:"severity"` // P0..P3
	Verdict    string         `json:"verdict"`  // RISK | NOT_TRIGGERED | NO_DATA | NOT_APPLICABLE
	ObjectType string         `json:"objectType"`
	ObjectKey  string         `json:"objectKey"`
	Detail     string         `json:"detail"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	Impact     map[string]any `json:"impact,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// Step 排障步骤（也是证据链的一条）。
type Step struct {
	No        int            `json:"no"`
	Source    string         `json:"source"` // topo | pinpoint | promthus | rule
	Tool      string         `json:"tool"`
	Title     string         `json:"title"`
	Status    string         `json:"status"` // ok | warn | error | skipped
	Args      map[string]any `json:"args,omitempty"`
	Result    map[string]any `json:"result,omitempty"`
	LatencyMs int64          `json:"latencyMs"`
	Error     string         `json:"error,omitempty"`
	At        time.Time      `json:"at"`
}

// SourceHealth 数据源验真结果（五步流程第 2 步：先确认数据源本身可信，再解释信号）。
type SourceHealth struct {
	Source string    `json:"source"` // promthus | es | cmdb | pinpoint
	Status string    `json:"status"` // ok | degraded | unavailable | skipped
	Detail string    `json:"detail,omitempty"`
	Checks []string  `json:"checks,omitempty"`
	At     time.Time `json:"at"`
}

// DiagnosisTicket 问题单：五步流程第 2 步「确认与定界」的产出，把实体、级别、信号来源、
// 时间窗、影响面与可信度固化成结构，作为第 3~5 步的准入依据。
// 与 domain.Ticket（工单）区分：本结构描述「已确认的问题」，工单描述「待处理的任务」。
type DiagnosisTicket struct {
	Entity     TicketEntity `json:"entity"`
	Severity   string       `json:"severity"` // critical | warning | info
	Signals    []string     `json:"signals"`  // 信号来源：alerts/up/cmdb/metrics/trace/logs/topo
	Window     TicketWindow `json:"window"`
	Impact     TicketImpact `json:"impact"`
	Confidence string       `json:"confidence"` // confirmed | degraded | no_data
	Reason     string       `json:"reason,omitempty"`
}

// TicketEntity 问题实体：应用视角与中间件视角二选一，能落到主机时补 IP / 主机名。
type TicketEntity struct {
	AppID          string `json:"appId,omitempty"`
	AppName        string `json:"appName,omitempty"`
	MiddlewareType string `json:"middlewareType,omitempty"`
	Addr           string `json:"addr,omitempty"`
	IP             string `json:"ip,omitempty"`
	HostName       string `json:"hostName,omitempty"`
	Node           string `json:"node,omitempty"`
	Owner          string `json:"owner,omitempty"`
}

// TicketWindow 观测时间窗。
type TicketWindow struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Minutes int       `json:"minutes"`
}

// TicketImpact 影响面：主机 / 应用 / 中间件数量与清单（清单截断展示）。
type TicketImpact struct {
	Hosts       int      `json:"hosts"`
	Apps        int      `json:"apps"`
	Middlewares int      `json:"middlewares"`
	HostList    []string `json:"hostList,omitempty"`
	AppNames    []string `json:"appNames,omitempty"`
	Nodes       []string `json:"nodes,omitempty"`
}

// DiagnosisSession 排障会话。
type DiagnosisSession struct {
	ID             string      `json:"id"`
	TenantID       string      `json:"tenantId"`
	AppID          string      `json:"appId,omitempty"`
	MiddlewareType string      `json:"middlewareType,omitempty"`
	Addr           string      `json:"addr,omitempty"`
	Symptom        string      `json:"symptom"`
	Status         string      `json:"status"` // open | investigating | root_caused | failed
	RootCause      string      `json:"rootCause,omitempty"`
	Confidence     float64     `json:"confidence"`
	Candidates     []Candidate `json:"candidates,omitempty"`
	Impact         *Impact     `json:"impact,omitempty"`
	Steps          []Step      `json:"steps"`
	// SourceHealth 数据源验真结果：为空表示本次未验真（配置关闭或 MCP 未接入）。
	SourceHealth []SourceHealth `json:"sourceHealth,omitempty"`
	// Ticket 问题单（第 2 步产出）：实体 / 级别 / 信号来源 / 时间窗 / 影响面 / 可信度。
	Ticket     *DiagnosisTicket `json:"ticket,omitempty"`
	CreatedAt  time.Time        `json:"createdAt"`
	FinishedAt time.Time        `json:"finishedAt"`
	// Feedback 人工回填的根因准确率标注（T10），为空表示未标注。
	Feedback *DiagnosisFeedback `json:"feedback,omitempty"`
}

// DiagnosisFeedback 根因人工标注：判断平台结论是否正确，并可填写真实根因。
type DiagnosisFeedback struct {
	Accurate        bool      `json:"accurate"`
	ActualRootCause string    `json:"actualRootCause,omitempty"`
	Note            string    `json:"note,omitempty"`
	Operator        string    `json:"operator,omitempty"`
	At              time.Time `json:"at"`
}

// Candidate 根因候选。
type Candidate struct {
	Title      string  `json:"title"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

// MCPTool MCP 工具描述（归一）。
type MCPTool struct {
	Server      string         `json:"server"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	Allowed     bool           `json:"allowed"`
}

// MCPServerStatus MCP 服务探针结果。
type MCPServerStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Enabled   bool   `json:"enabled"`
	Reachable bool   `json:"reachable"`
	Tools     int    `json:"tools"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latencyMs"`
}
