package domain

import "encoding/json"

// 运维关系拓扑能力面（对齐 topo 服务 /operationTopology 契约）。
// file 模式下由平台本地从快照计算；http 模式下平台作为代理透传 topo 服务响应。

// OpGraphHost Apollo 实例（套）归属。
type OpGraphHost struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	AppCount int    `json:"appCount"`
}

// OpGraphNode 拓扑图节点：application / middleware。
type OpGraphNode struct {
	ID                string `json:"id"`
	Category          string `json:"category"` // application | middleware
	Name              string `json:"name"`
	Label             string `json:"label"`
	Node              string `json:"node"`
	ApolloAppID       string `json:"apolloAppId"`
	ApolloAppName     string `json:"apolloAppName"`
	AppDept           string `json:"appDept"`
	AppOwner          string `json:"appOwner"`
	EngProject        string `json:"engProject"`
	DepProject        string `json:"depProject"`
	DeployProjectName string `json:"deployProjectName"`
	PpApp             string `json:"ppApp"`

	// 应用节点属性多值列表（折叠口径返回，展开口径为单值）
	EngProjects []string `json:"engProjects,omitempty"`
	DepProjects []string `json:"depProjects,omitempty"`
	PpApps      []string `json:"ppApps,omitempty"`
	Nodes       []string `json:"nodes,omitempty"`

	ApolloHosts []OpGraphHost `json:"apolloHosts"`
	AppIDs      []string      `json:"appIds"`
	MwType      string        `json:"mwType"`
	Addr        string        `json:"addr"`
	MwProject   string        `json:"mwProject"`
	DepCount    int           `json:"depCount"`

	// 运维状态引用：用于拓扑图按排障 / 工单状态着色。
	DiagRefs   []OpNodeDiagRef   `json:"diagRefs,omitempty"`
	TicketRefs []OpNodeTicketRef `json:"ticketRefs,omitempty"`
}

// OpNodeDiagRef 节点关联的排障会话摘要。
type OpNodeDiagRef struct {
	SessionID  string  `json:"sessionId"`
	Status     string  `json:"status"`     // open | investigating | root_caused | failed
	Confidence float64 `json:"confidence"` // 0-1
}

// OpNodeTicketRef 节点关联的工单摘要。
type OpNodeTicketRef struct {
	ID     string `json:"id"`
	Status string `json:"status"` // open | in_progress | resolved | closed
}

// OpGraphEdge 边：应用 → 中间件。
type OpGraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

// OpGraphCounts 规模汇总（与过滤条件一致）。
type OpGraphCounts struct {
	ApolloHosts int `json:"apolloHosts"`
	Application int `json:"application"`
	Middleware  int `json:"middleware"`
	Edges       int `json:"edges"`
	Nodes       int `json:"nodes"`
}

// OpGraph 拓扑图响应。
type OpGraph struct {
	Nodes       []OpGraphNode `json:"nodes"`
	Edges       []OpGraphEdge `json:"edges"`
	SyncDate    string        `json:"syncDate"`
	Env         string        `json:"env"`
	Partial     bool          `json:"partial"`
	ApolloHosts []OpGraphHost `json:"apolloHosts"`
	DataSource  string        `json:"dataSource"` // file=本地快照 / db=topo 服务
	Counts      OpGraphCounts `json:"counts"`
}

// OpNodeDetailMw 详情中的中间件引用。
type OpNodeDetailMw struct {
	MwType    string `json:"mwType"`
	Addr      string `json:"addr"`
	MwDb      string `json:"mwDb"`
	MwProject string `json:"mwProject"`
	DepCount  int    `json:"depCount"`
}

// OpNodeDetailApp 详情中的应用引用。
type OpNodeDetailApp struct {
	NodeId        string        `json:"nodeId"`
	ApolloAppID   string        `json:"apolloAppId"`
	ApolloAppName string        `json:"apolloAppName"`
	AppDept       string        `json:"appDept"`
	AppOwner      string        `json:"appOwner"`
	EngProject    string        `json:"engProject"`
	DepProject    string        `json:"depProject"`
	Node          string        `json:"node"`
	PpApp         string        `json:"ppApp"`
	ApolloHosts   []OpGraphHost `json:"apolloHosts"`
}

// OpNodeDetail 节点详情响应（/detail）。
type OpNodeDetail struct {
	ID       string `json:"id"`
	Category string `json:"category"` // application | middleware | host
	Name     string `json:"name"`
	SyncDate string `json:"syncDate"`
	Env      string `json:"env"`

	ApolloAppID       string `json:"apolloAppId"`
	ApolloAppName     string `json:"apolloAppName"`
	AppDept           string `json:"appDept"`
	AppOwner          string `json:"appOwner"`
	EngProject        string `json:"engProject"`
	DepProject        string `json:"depProject"`
	DeployProjectName string `json:"deployProjectName"`
	Node              string `json:"node"`
	PpApp             string `json:"ppApp"`

	EngProjects []string `json:"engProjects,omitempty"`
	DepProjects []string `json:"depProjects,omitempty"`
	PpApps      []string `json:"ppApps,omitempty"`
	Nodes       []string `json:"nodes,omitempty"`

	MwType    string `json:"mwType"`
	Addr      string `json:"addr"`
	MwDb      string `json:"mwDb"`
	MwProject string `json:"mwProject"`
	DepCount  int    `json:"depCount"`

	HostID       int64  `json:"hostId"`
	HostName     string `json:"hostName"`
	HostAddr     string `json:"hostAddr"`
	HostAppCount int    `json:"hostAppCount"`

	ApolloHosts []OpGraphHost     `json:"apolloHosts"`
	Middlewares []OpNodeDetailMw  `json:"middlewares"`
	Apps        []OpNodeDetailApp `json:"apps"`
}

// OpSyncStats 同步任务实时统计。
type OpSyncStats struct {
	Hosts      int `json:"hosts"`
	Apps       int `json:"apps"`
	Middleware int `json:"middleware"`
	Relations  int `json:"relations"`
}

// OpSyncState 异步同步任务状态（/sync 与 /sync/status）。
type OpSyncState struct {
	Running   bool            `json:"running"`
	Supported bool            `json:"supported"`
	TriggerBy string          `json:"triggerBy"`
	StartTime string          `json:"startTime"`
	EndTime   string          `json:"endTime"`
	CostMs    int64           `json:"costMs"`
	Error     string          `json:"error"`
	StageText string          `json:"stageText"`
	Percent   int             `json:"percent"` // 0~100；-1=当前阶段无定量进度
	Stats     *OpSyncStats    `json:"stats,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// OpSyncLogInfo 同步日志文件条目。
type OpSyncLogInfo struct {
	Date      string `json:"date"`
	File      string `json:"file"`
	SizeBytes int64  `json:"sizeBytes"`
}
