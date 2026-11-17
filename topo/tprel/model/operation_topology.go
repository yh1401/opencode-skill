package model

import "time"

// OperationTopology 运维对象-中间件关系元数据（单表，每日快照）
// 对应文档《运维关系拓扑图方案》§3 数据模型
func (OperationTopology) TableName() string {
	return "operation_topology"
}

// OperationTopology 关系行 = 「某实例下的某应用 → 其中间件」一条关系。
// 字段按层级分组布局（与页面「Apollo 实例 → 应用 → 中间件」三层展示对应）：
//
//	① 实例层：来源 Apollo 套（接口1 getApolloHostInfo），行级冗余，页面实例筛选/归属展示用
//	② 应用层：来源应用接口（接口2 apps），应用节点元数据；node/eng_project/dep_project 与 CMDB 关系未明确，暂为空
//	③ 中间件层：来源配置解析（接口3 namespace items + 中间件规则），含溯源与增强字段
//	④ 快照/通用：每日快照日期、环境与库内管理字段
//
// 注：各字段 gorm tag 与《运维关系拓扑图方案》§3 的 DDL 严格一致（类型/非空/默认值/注释），
// 否则 AutoMigrate 会按模型反向改表（丢 NOT NULL / DEFAULT / COMMENT）。
type OperationTopology struct {
	Id uint64 `gorm:"column:id;primary_key;auto_increment" json:"id"`

	// ===== ① 实例层（Apollo 套归属）=====
	ApolloHostId    int    `gorm:"column:apollo_host_id;type:int;size:32;not null;default:0;index:idx_host;uniqueIndex:uk_snapshot_relation;comment:Apollo 实例（套）id，顶层归属维度" json:"apolloHostId"` // Apollo 实例（套）id：顶层归属维度（uk_snapshot_relation 第 3 列）
	ApolloHostName  string `gorm:"column:apollo_host_name;size:128;not null;default:'';comment:Apollo 实例（套）名称" json:"apolloHostName"`                                                           // Apollo 实例（套）名称
	ApolloHostAddr  string `gorm:"column:apollo_host_addr;size:255;not null;default:'';comment:实例访问地址（上游 host，如 https://…:8154）" json:"apolloHostAddr"`                                         // 实例访问地址（上游 host 字段，如 https://…:8154）
	SecondProductId string `gorm:"column:second_product_id;size:255;not null;default:'';comment:二级产品线 id（上游数组逗号拼接，如 \"705,532\"）" json:"secondProductId"`                                       // 实例所属二级产品线 id（上游 secondProductId 数组，入库逗号分隔，如 "705,532"）
	HostAppCount    int    `gorm:"column:host_app_count;type:int;size:32;not null;default:0;comment:该实例下应用总数（上游 appCount）" json:"appCount"`                                                     // 该实例下应用总数（上游 appCount）

	// ===== ② 应用层 =====
	ApolloAppId   string `gorm:"column:apollo_app_id;size:128;not null;default:'';index:idx_appid;uniqueIndex:uk_snapshot_relation;comment:Apollo appid" json:"apolloAppId"` // Apollo appid（uk_snapshot_relation 第 2 列）
	ApolloAppName string `gorm:"column:apollo_app_name;size:128;not null;default:'';comment:应用名称" json:"apolloAppName"`                                                      // 应用名称（上游 app.name）
	AppDept       string `gorm:"column:app_dept;size:128;not null;default:'';comment:所属部门（上游 deptName/department/orgName 别名兜底）" json:"appDept"`                              // 所属部门（上游 orgName/deptName/department 别名）
	AppOwner      string `gorm:"column:app_owner;size:128;not null;default:'';comment:负责人（上游 ownerName/owner/appOwner 别名兜底）" json:"appOwner"`                                // 负责人（上游 ownerName/owner/appOwner 别名）
	Node          string `gorm:"column:node;size:64;not null;default:'';index:idx_node;comment:节点（机房/地域）：采集期留空，查询期由两跳关系回填" json:"node"`                                      // 节点（机房/地域）：采集期不赋值，查询期由「app_dp_relation → deploy_pinpoint_relation」回填
	EngProject    string `gorm:"column:eng_project;size:128;not null;default:'';index:idx_eng_proj;comment:工程项目名：采集期留空，查询期由两跳关系回填" json:"engProject"`                        // 工程项目名：采集期不赋值，查询期由跳② 回填
	DepProject    string `gorm:"column:dep_project;size:128;not null;default:'';index:idx_dep_proj;comment:部署项目名：采集期留空，查询期由两跳关系回填（appId 的属性）" json:"depProject"`             // 部署项目名：采集期不赋值，查询期由跳① 回填（apollo_app_id 的属性，不参与节点合并）
	PpApplication string `gorm:"column:pp_application;size:128;not null;default:'';comment:PP 应用名称（映射命中取配置值，否则回退应用名）" json:"ppApplication"`                                  // PP 应用名称：采集期取 appid_owner_map 配置值，未命中回退应用名；查询期可被跳② 命中值覆盖

	// ===== ②′ appId 关联属性（查询期由 跳①×跳② 回填；不落库、不参与唯一键）=====
	// 口径：拓扑数据集粒度恒为「appId × 中间件」，关联数据只作为 appId 组件的属性补充；
	// 属性多值时保留为列表（下方四列），绝不按属性组合把关系行展开（原 expandRows「一行 → N 行」已废弃）。
	// 上方 dep_project/eng_project/node 为属性首值（兼容既有单值字段口径），pp_application 首值缺失时保留采集期占位值。
	DepProjects    []string `gorm:"-" json:"depProjects,omitempty"`    // 部署项目名列表（跳① 命中值，去重升序）
	EngProjects    []string `gorm:"-" json:"engProjects,omitempty"`    // 工程项目名列表（跳② 汇总，去重升序）
	Nodes          []string `gorm:"-" json:"nodes,omitempty"`          // 节点（机房/地域）列表（跳② 汇总，去重升序）
	PpApplications []string `gorm:"-" json:"ppApplications,omitempty"` // PP 应用名列表（跳② 汇总，去重升序）

	// ===== ③ 中间件层 =====
	Namespace         string `gorm:"column:namespace;size:128;not null;default:'';comment:Apollo 命名空间（溯源）" json:"namespace"`                                                                        // Apollo 命名空间（溯源）
	ConfigKey         string `gorm:"column:config_key;size:255;not null;default:'';comment:解析来源配置 key（溯源）" json:"configKey"`                                                                        // 解析来源配置 key（溯源）
	MiddlewareType    string `gorm:"column:middleware_type;size:32;not null;default:'';index:idx_mw_type;uniqueIndex:uk_snapshot_relation;comment:中间件类型：mysql/redis/kafka/…" json:"middlewareType"` // 中间件类型（uk_snapshot_relation 第 5 列）
	ConnectAddr       string `gorm:"column:connect_addr;size:255;not null;default:'';uniqueIndex:uk_snapshot_relation;comment:连接地址（仅 host:port，不含密码）" json:"connectAddr"`                           // 连接地址（仅 host:port，不含密码；uk_snapshot_relation 第 6 列）
	MiddlewareDb      string `gorm:"column:middleware_db;size:128;not null;default:'';comment:连接的库名（增强溯源，不参与节点合并/唯一键）" json:"middlewareDb"`                                                         // 连接的库/数据库名（如 vcp_saas；增强溯源字段，不参与节点合并与唯一键）
	MiddlewareProject string `gorm:"column:middleware_project;size:128;not null;default:'';comment:中间件项目名称" json:"middlewareProject"`                                                               // 中间件项目名称

	// ===== ④ 快照/通用 =====
	Env        string    `gorm:"column:env;size:16;not null;default:'';index:idx_env;uniqueIndex:uk_snapshot_relation;comment:环境：PRO/SIT/DEV" json:"env"`                              // 环境：PRO/test（uk_snapshot_relation 第 4 列）
	SyncDate   string    `gorm:"column:sync_date;size:10;not null;default:'';index:idx_sync_date;uniqueIndex:uk_snapshot_relation;comment:同步日期快照（YYYY-MM-DD，字符串便于比较）" json:"syncDate"` // 同步日期快照（YYYY-MM-DD；uk_snapshot_relation 第 1 列）
	Source     string    `gorm:"column:source;size:32;not null;default:apollo;comment:数据来源" json:"source"`                                                                             // 数据来源：apollo
	Remark     string    `gorm:"column:remark;size:255;not null;default:'';comment:备注（当前未赋值）" json:"remark"`
	CreateTime time.Time `gorm:"column:create_time;type:datetime;not null;default:CURRENT_TIMESTAMP;autoCreateTime;comment:创建时间（GORM autoCreateTime）" json:"createTime"`
	UpdateTime time.Time `gorm:"column:update_time;type:datetime;not null;default:CURRENT_TIMESTAMP;autoUpdateTime;comment:更新时间（GORM autoUpdateTime）" json:"updateTime"`
}

// OperationTopologyListReq 关系列表查询参数（§6.1 /list）
type OperationTopologyListReq struct {
	Page           int    `json:"page" form:"page"`                     // 页码（默认 1）
	PageSize       int    `json:"pageSize" form:"pageSize"`             // 每页条数（默认 20）
	Env            string `json:"env" form:"env"`                       // 环境
	MiddlewareType string `json:"middlewareType" form:"middlewareType"` // 中间件类型
	// —— appId 属性条件（属性属 appId 级：条件命中则保留该 appId 的全部中间件关系行）——
	// 拓扑表这几列为采集期留空，真实来源是 跳①（appId ↔ 部署项目）×跳②（DPR），
	// 故需经关联索引翻译为 apollo_app_id 集合下推，并在属性补充后按 appId 复核。
	DepProject     string `json:"depProject" form:"depProject"`         // 部署项目名（模糊）
	EngProject     string `json:"engProject" form:"engProject"`         // 工程项目名（精确）
	Node           string `json:"node" form:"node"`                     // 节点/机房（精确）
	PpApplication  string `json:"ppApplication" form:"ppApplication"`   // PP 应用名（精确）
	ApolloAppId    string `json:"apolloAppId" form:"apolloAppId"`       // Apollo appid（精确）
	ApolloAppName  string `json:"apolloAppName" form:"apolloAppName"`   // 应用名称（模糊）
	ApolloHostName string `json:"apolloHostName" form:"apolloHostName"` // Apollo 实例（套）名称（模糊）
	AppDept        string `json:"appDept" form:"appDept"`               // 所属部门（精确）
	AppOwner       string `json:"appOwner" form:"appOwner"`             // 负责人（精确）
	ConnectAddr    string `json:"connectAddr" form:"connectAddr"`       // 连接地址（模糊）
	SyncDate       string `json:"syncDate" form:"syncDate"`             // 快照日期（默认最新）
	Source         string `json:"source" form:"source"`                 // 本次请求数据源覆盖（db|file，空 = 按 datastore.mode）
	// ExpandDepNode 部署点展开（指针，nil/true = 默认展开；显式 false/0 = 折叠）：
	// 展开时行粒度取 (apollo_app_id, 部署项目, 节点) × 中间件，一行按该 appId 的部署点复制为 N 行
	// （N = 部署点个数），dep_project/node 单值；折叠时「appId × 中间件」不扩行，属性多值聚合为列表。
	ExpandDepNode *bool `json:"expandDepNode" form:"expandDepNode"`
}

// GraphHost Apollo 实例（套）归属（图顶层实例筛选/归属层用）
type GraphHost struct {
	Id       int    `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`     // 实例访问地址（上游 host）
	AppCount int    `json:"appCount"` // 实例下应用总数（上游 appCount）
}

// GraphNode 拓扑图节点（§6.2）
type GraphNode struct {
	Id                string `json:"id"`                // 应用: app:{apollo_app_id}（缺失时回退 dep_project）；中间件: mw:{type}:{addr}
	Category          string `json:"category"`          // application / middleware（server 预留）
	Name              string `json:"name"`              // 标题：应用名称（缺失时回退合并键） / 类型+地址
	Label             string `json:"label"`             // 副标题：appid / 地址
	Node              string `json:"node"`              // 机房/节点（应用节点，查询期由两跳关系回填；关系链未命中时为空）
	ApolloAppId       string `json:"apolloAppId"`       // appid（应用节点）
	ApolloAppName     string `json:"apolloAppName"`     // 应用名称（应用节点，上游 app.name）
	AppDept           string `json:"appDept"`           // 所属部门（应用节点）
	AppOwner          string `json:"appOwner"`          // 负责人（应用节点）
	EngProject        string `json:"engProject"`        // 工程名（应用节点属性首值，由两跳关系关联补齐）
	DepProject        string `json:"depProject"`        // 部署项目（应用节点属性首值，取自跳① app_dp_relation）
	DeployProjectName string `json:"deployProjectName"` // 部署项目名（与 depProject 同值，取自跳① app_dp_relation）
	PpApp             string `json:"ppApp"`             // PP 应用（应用节点属性首值）
	// 应用节点属性多值列表（appId 组件属性；属性多值时逐项列出，不扩行/不扩节点）
	EngProjects []string    `json:"engProjects,omitempty"` // 工程名列表
	DepProjects []string    `json:"depProjects,omitempty"` // 部署项目列表
	PpApps      []string    `json:"ppApps,omitempty"`      // PP 应用列表
	Nodes       []string    `json:"nodes,omitempty"`       // 节点（机房/地域）列表
	ApolloHosts []GraphHost `json:"apolloHosts"`           // Apollo 实例归属（应用节点，可能来自多套实例）
	AppIds      []string    `json:"appIds"`                // 中间件节点被哪些 app 依赖
	MwType      string      `json:"mwType"`                // 中间件类型
	Addr        string      `json:"addr"`                  // 连接地址
	MwProject   string      `json:"mwProject"`             // 中间件项目名
	DepCount    int         `json:"depCount"`              // 被依赖数（中间件节点角标）
}

// GraphEdge 拓扑图边（应用 → 中间件）
type GraphEdge struct {
	Source string `json:"source"` // 应用节点 id
	Target string `json:"target"` // 中间件节点 id
	Kind   string `json:"kind"`   // depend（依赖）
}

// GraphQuery 拓扑图查询参数（§6.1 /graph）
type GraphQuery struct {
	Env string `json:"env" form:"env"`
	// —— appId 属性条件（与 /list 同口径：属性属 appId 级，命中即保留该 appId 的全部关系行与节点）——
	DepProject     string `json:"depProject" form:"depProject"`       // 部署项目（模糊）
	EngProject     string `json:"engProject" form:"engProject"`       // 工程项目（精确）
	Node           string `json:"node" form:"node"`                   // 节点/机房（精确）
	PpApplication  string `json:"ppApplication" form:"ppApplication"` // PP 应用（精确）
	MiddlewareType string `json:"middlewareType" form:"middlewareType"`
	SyncDate       string `json:"syncDate" form:"syncDate"`
	Source         string `json:"source" form:"source"` // 本次请求数据源覆盖（db|file，空 = 按 datastore.mode）
	// ExpandDepNode 部署点展开（指针，nil/true = 默认展开；显式 false/0 = 折叠）：
	// 展开时应用节点粒度取 (apollo_app_id, 部署项目, 节点)，一个部署点一个节点
	// （node/dep_project 单值，多值列表字段不再返回）；折叠时按 appId 收敛为一个节点，
	// 属性多值聚合为列表。
	ExpandDepNode *bool `json:"expandDepNode" form:"expandDepNode"`
}

// GraphCounts 拓扑图响应中“各列表字段的规模汇总”
// 字段名与同返回体的 list 字段一一对应（apolloHosts/nodes中两类/edges），
// 均为过滤后实际返回的集合大小，供前端直接展示“总体数据”（节点总数=nodes 长度）。
type GraphCounts struct {
	ApolloHosts int `json:"apolloHosts"` // apolloHosts 列表长度（本次返回图内出现的 Apollo 实例总数）
	Application int `json:"application"` // nodes 中 application 节点数
	Middleware  int `json:"middleware"`  // nodes 中 middleware 节点数
	Edges       int `json:"edges"`       // edges 列表长度（去重后的应用→中间件连接关系数）
	Nodes       int `json:"nodes"`       // 节点总数 = application + middleware = len(nodes)
}

// GraphResp 拓扑图响应（§6.2）
type GraphResp struct {
	Nodes    []GraphNode `json:"nodes"`
	Edges    []GraphEdge `json:"edges"`
	SyncDate string      `json:"syncDate"` // 本次展示所用快照日期
	Env      string      `json:"env"`
	Partial  bool        `json:"partial"` // 同步是否部分完成（预算超时）
	// ApolloHosts 本次快照图内出现的 Apollo 实例汇总（供前端实例筛选下拉）
	ApolloHosts []GraphHost `json:"apolloHosts"`
	// DataSource 本次返回的数据源（db=数据库 / file=库不可用时的本地快照）。
	// 原 X-Data-Source 响应头已下线，数据源仅在本返回体中携带。
	DataSource string `json:"dataSource"`
	// Counts 各列表字段规模汇总（与过滤条件一致；无数据时返回全 0）
	Counts GraphCounts `json:"counts"`
}

// SyncResult 同步结果（§6.1 /sync）
// 除过程计数外，携带「本次同步整体效果」信息（触发/耗时/结果/类型分布等，
// 与日志中的“执行计划总览/结果总览”一致），供前端一次展示整批效果。
type SyncResult struct {
	HostCount      int      `json:"hostCount"`      // 扫描的 Apollo 套数
	AvailableHosts int      `json:"availableHosts"` // 可用套数（探活后）
	AppCount       int      `json:"appCount"`       // 扫描的应用总数
	NamespaceCount int      `json:"namespaceCount"` // 扫描的 namespace 数
	RequestCount   int      `json:"requestCount"`   // 实际请求数
	InsertCount    int      `json:"insertCount"`    // 入库关系条数
	Partial        bool     `json:"partial"`        // 是否部分完成
	UnfinishedApps []string `json:"unfinishedApps"` // 未完成应用列表
	NewRelations   []string `json:"newRelations"`   // 与上次快照相比新增的关系
	LostRelations  []string `json:"lostRelations"`  // 与上次快照相比消失的关系

	// —— ns 学习定向（命中学习集定向扫 / 无记录全名单首扫 / 超期全名单兜底重猜；计数单位为 hostId|env|appId 三元组）——
	NsTargeted  int `json:"nsTargeted"`  // 定向扫描（命中学习集）的三元组数
	NsBootstrap int `json:"nsBootstrap"` // 全名单首扫三元组数（无学习记录，本轮建立基线）
	NsRecheck   int `json:"nsRecheck"`   // 全名单兜底重猜三元组数（超过 ns_guess_days 未全量）

	// —— 跳① 关系同步（appId ↔ 部署项目，与拓扑主表同批刷新，供查询期展开）——
	RelationRows  int    `json:"relationRows"`  // 关系接口本次拉取并过滤 del_flag 后的有效关系行数
	RelationWrote bool   `json:"relationWrote"` // 是否执行了 app_dp_relation「清空 + 重写」（数据无变化跳过替换时为 false，属正常）
	RelationErr   bool   `json:"relationErr"`   // 是否异常（上游拉取失败/关系表缺失或写库失败），需运维介入
	RelationMsg   string `json:"relationMsg"`   // 关系同步结果说明（跳过/失败原因；空=正常）

	// —— 本次同步整体效果 ——
	TriggerBy      string         `json:"triggerBy"`      // 触发方式（手动同步/cron定时/启动自检/e2e自检）
	StartTime      string         `json:"startTime"`      // 开始时间 2006-01-02 15:04:05.000
	EndTime        string         `json:"endTime"`        // 结束时间
	CostMs         int64          `json:"costMs"`         // 总耗时 ms
	ResultText     string         `json:"resultText"`     // 结果描述：成功 / 部分完成(未执行任务/未完成应用)
	AvgRequestMs   int64          `json:"avgRequestMs"`   // 单请求平均耗时 ms
	MaxRequestMs   int64          `json:"maxRequestMs"`   // 单请求最大耗时 ms
	TotalRelations int            `json:"totalRelations"` // 本次解析去重后的总关系数
	TypeStats      map[string]int `json:"typeStats"`      // 中间件类型分布（关系数，含实例重复）
	NodeStats      map[string]int `json:"nodeStats"`      // 中间件类型分布（类型+地址 去重节点数，与图节点口径一致）
	NewCount       int            `json:"newCount"`       // 相对上次快照新增关系数
	LostCount      int            `json:"lostCount"`      // 相对上次快照消失关系数
	PrevDate       string         `json:"prevDate"`       // 对比的上次快照日期（空=首次入库）
	KeptRows       int64          `json:"keptRows"`       // 本次无解析产物时保留的当日旧快照行数

	// —— 无中间件应用（已扫描但未解析出任何中间件关系；不含未完成/未执行应用）——
	NoMiddlewareCount int       `json:"noMiddlewareCount"` // 数量
	NoMiddlewareApps  []NoMwApp `json:"noMiddlewareApps"`  // 明细（appId+应用名，按 appId 升序）
}

// NoMwApp 无中间件应用条目（同步结果 / 日志“无中间件应用清单”）
type NoMwApp struct {
	AppId   string `json:"appId"`   // Apollo 应用 appId
	AppName string `json:"appName"` // 应用名称（可为空）
}

// NodeDetailQuery 节点详情查询参数（§6.2 /detail）
// id 与图节点一致：app:{apollo_app_id} / mw:{middleware_type}:{connect_addr} / host:{apollo_host_id}
type NodeDetailQuery struct {
	Id       string `json:"id" form:"id"`             // 图节点 id（必填）
	Env      string `json:"env" form:"env"`           // 环境（默认最新快照）
	SyncDate string `json:"syncDate" form:"syncDate"` // 快照日期（默认最新）
	Source   string `json:"source" form:"source"`     // 本次请求数据源覆盖（db|file，空 = 按 datastore.mode）
	// ExpandDepNode 部署点展开（指针，nil/true = 默认展开；显式 false/0 = 折叠）：
	// 与 /graph 同口径（展开 = 应用节点按 appId|部署项目|节点 粒度）。应用节点 id 自带 2 个「|」时后端自动定位，
	// 此参数主要决定 mw:/host: 节点的「引用应用列表」粒度，前端应与 /graph 同值传入。
	ExpandDepNode *bool `json:"expandDepNode" form:"expandDepNode"`
}

// NodeDetailApp 详情中的应用引用（中间件被哪些应用依赖 / 实例名下应用）
type NodeDetailApp struct {
	NodeId            string `json:"nodeId"`      // 图节点 id（app:{apollo_app_id}，缺失回退 dep_project）
	ApolloAppId       string `json:"apolloAppId"` // appid
	ApolloAppName     string `json:"apolloAppName"`
	AppDept           string `json:"appDept"`
	AppOwner          string `json:"appOwner"`
	EngProject        string `json:"engProject"`        // 工程项目（映射表关联补齐，属性首值）
	DepProject        string `json:"depProject"`        // 部署项目（取自跳① app_dp_relation，属性首值）
	DeployProjectName string `json:"deployProjectName"` // 部署项目名（与 depProject 同值，取自跳① app_dp_relation）
	Node              string `json:"node"`              // 机房/节点（取自跳① app_dp_relation，属性首值，与 graph 应用节点同源）
	PpApp             string `json:"ppApp"`             // PP 应用（属性首值）
	// 应用属性多值列表（appId 组件属性；属性多值时逐项列出，不扩行/不扩节点）
	EngProjects []string    `json:"engProjects,omitempty"` // 工程项目列表
	DepProjects []string    `json:"depProjects,omitempty"` // 部署项目列表
	PpApps      []string    `json:"ppApps,omitempty"`      // PP 应用列表
	Nodes       []string    `json:"nodes,omitempty"`       // 节点（机房/地域）列表
	ApolloHosts []GraphHost `json:"apolloHosts"`           // 所属 Apollo 实例（可能多套）
}

// NodeDetailMw 详情中的中间件引用（应用依赖了哪些中间件）
type NodeDetailMw struct {
	MwType    string `json:"mwType"` // 中间件类型
	Addr      string `json:"addr"`   // 连接地址
	MwDb      string `json:"mwDb"`   // 连接的库名
	MwProject string `json:"mwProject"`
	DepCount  int    `json:"depCount"` // 该中间件被依赖应用数
}

// NodeDetail 节点详情响应（§6.2 /detail）
// 与 GraphNode 同 id，但补充行级明细（库名、关联列表），供前端详情面板直接渲染，
// 前端无需再基于全量图数据在内存中推导（详情数据归后端，前端只负责展示）。
type NodeDetail struct {
	Id       string `json:"id"`
	Category string `json:"category"` // application / middleware / host
	Name     string `json:"name"`
	SyncDate string `json:"syncDate"`
	Env      string `json:"env"`

	// —— 应用（category=application）——
	ApolloAppId       string `json:"apolloAppId"`
	ApolloAppName     string `json:"apolloAppName"`
	AppDept           string `json:"appDept"`
	AppOwner          string `json:"appOwner"`
	EngProject        string `json:"engProject"`        // 工程项目（映射表关联补齐，属性首值）
	DepProject        string `json:"depProject"`        // 部署项目（取自跳① app_dp_relation，属性首值）
	DeployProjectName string `json:"deployProjectName"` // 部署项目名（与 depProject 同值，取自跳① app_dp_relation）
	Node              string `json:"node"`              // 机房/节点（取自跳① app_dp_relation，属性首值，与 graph 应用节点同源）
	PpApp             string `json:"ppApp"`             // PP 应用（属性首值）
	// 应用属性多值列表（appId 组件属性；属性多值时逐项列出，不扩行/不扩节点）
	EngProjects []string `json:"engProjects,omitempty"` // 工程项目列表
	DepProjects []string `json:"depProjects,omitempty"` // 部署项目列表
	PpApps      []string `json:"ppApps,omitempty"`      // PP 应用列表
	Nodes       []string `json:"nodes,omitempty"`       // 节点（机房/地域）列表

	// —— 中间件（category=middleware）——
	MwType    string `json:"mwType"`
	Addr      string `json:"addr"`
	MwDb      string `json:"mwDb"` // 连接的库名（从 jdbc/mongodb uri 提取）
	MwProject string `json:"mwProject"`
	DepCount  int    `json:"depCount"` // 被依赖应用数

	// —— 实例（category=host）——
	HostId       int    `json:"hostId"`
	HostName     string `json:"hostName"`
	HostAddr     string `json:"hostAddr"`
	HostAppCount int    `json:"hostAppCount"`

	// —— 关联明细 ——
	ApolloHosts []GraphHost     `json:"apolloHosts"` // 应用/中间件出现的 Apollo 实例去重
	Middlewares []NodeDetailMw  `json:"middlewares"` // 应用依赖的中间件（category=application 时）
	Apps        []NodeDetailApp `json:"apps"`        // 中间件被依赖应用 / 实例名下应用
}
