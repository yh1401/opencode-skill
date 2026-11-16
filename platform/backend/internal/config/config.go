package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 平台运行时配置，全部可由环境变量覆盖。
type Config struct {
	Addr string

	// 拓扑数据源：file（读取 topo/tprel/data，零依赖）| http（调用 topo 服务）
	TopoMode    string
	TopoBaseURL string
	TopoToken   string
	TopoDataDir string

	// 存储：文件为主，Postgres 为备份/兜底
	StoreDir    string
	PostgresDSN string

	// MCP
	MCPEnabled       bool
	MCPTimeout       time.Duration
	PinpointURL      string
	PrometheusMCPURL string
	CMDBMCPURL       string
	// ESMCPURL 为空表示未接入 Elasticsearch MCP（T10 会给出 NO_DATA）。
	ESMCPURL      string
	MCPDenyTools  []string
	MCPAllowTools []string

	// 巡检
	ExplosionThreshold int
	DefaultViewMode    string
	DefaultEnv         string
	InspectCronEnabled bool
	InspectCronTick    time.Duration

	// 巡检 MCP（T7 主机存活 / T9 CMDB 一致性 / T8 调用链 / T10 日志 / T11 指标阈值）：
	// 批量任务必须限量、限时，同一主机在本批次内只查询一次。
	InspectMCPEnabled   bool
	InspectMCPMaxHosts  int
	InspectMCPMaxApps   int
	InspectMCPTimeout   time.Duration
	InspectCMDBPageSize int

	// 排障第 2 步「确认与定界」的数据源验真：先确认 Prometheus / ES 自身可用、
	// 候选主机确实被采集，再解释信号，避免把「未纳管 / 采集异常」误判成「主机宕机」。
	DiagVerifyEnabled bool
	DiagVerifyTimeout time.Duration

	// T10 日志巡检：ES MCP 的索引 / 字段约定（不同集群可用环境变量覆盖）。
	ESLogIndex              string
	ESLogTimeField          string
	ESLogLevelField         string
	ESLogHostField          string
	InspectESLookbackMin    int
	InspectESErrorThreshold int

	// 告警中心（断点⑤）
	AlarmEnabled       bool
	AlarmPollInterval  time.Duration
	AlarmCooldown      time.Duration
	AlarmMaxAutoDiag   int
	AlarmAutoDiag      bool
	AlarmMissTolerance int

	// Alertmanager 接入（S4）：webhook 入站 + /api/v2/alerts 主动拉
	AlertmanagerEnabled    bool
	AlertmanagerURL        string
	AlertmanagerTimeout    time.Duration
	AlertmanagerGroupLabel string

	// 指标劣化扫描（S1）：range_query 算趋势，命中阈值生成 degradation 告警
	DegradationEnabled      bool
	DegradationInterval     time.Duration
	DegradationWindowMin    int
	DegradationStepSec      int
	DegradationThresholdPct float64
	DegradationMaxTargets   int
	DegradationQuery        string

	// 告警通知出站（T11）：通用 webhook，URL 为空表示不发送
	AlarmNotifyURL     string
	AlarmNotifyTimeout time.Duration
	AlarmNotifyToken   string

	// 外部业务接口监控（T12）
	BizMonEnabled  bool
	BizMonInterval time.Duration

	// 质差分析（T13）
	QualityEnabled   bool
	QualityWindowMin int

	// 日志证据（断点④）：调用外部日志文件分析服务
	LogAnalyzerEnabled bool
	LogAnalyzerURL     string
	LogPathTemplate    string
	LogAnalyzeTimeout  time.Duration
	LogAnalyzePoll     time.Duration

	// 身份映射（跨 MCP 字段贯通）：应用 / 主机统一身份，落 store 的两个 docType
	IdentityEnabled       bool
	IdentityRebuildOnBoot bool
	IdentityCMDBPages     int
	IdentityCMDBPageSize  int
	IdentityDNSResolve    bool

	// 排障默认观测时间窗（分钟）
	DiagWindowMin int

	// 知识库语义重排（公网大模型，OpenAI 兼容 /v1/chat/completions）
	// API Key 为空表示关闭，检索完全沿用字面打分。
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string
	LLMTimeout time.Duration
}

func Load() Config {
	c := Config{
		Addr:               getenv("HTTP_ADDR", ":8080"),
		TopoMode:           strings.ToLower(getenv("TOPO_MODE", "file")),
		TopoBaseURL:        strings.TrimRight(getenv("TOPO_BASE_URL", "http://127.0.0.1:9000"), "/"),
		TopoToken:          getenv("TOPO_TOKEN", ""),
		TopoDataDir:        getenv("TOPO_DATA_DIR", ""),
		StoreDir:           getenv("STORE_DIR", "data/platform"),
		PostgresDSN:        getenv("POSTGRES_DSN", ""),
		MCPEnabled:         getbool("MCP_ENABLED", true),
		MCPTimeout:         time.Duration(getint("MCP_TIMEOUT_MS", 8000)) * time.Millisecond,
		PinpointURL:        getenv("PINPOINT_MCP_URL", "http://127.0.0.1:8888/mcp"),
		PrometheusMCPURL:   getenv("PROMETHEUS_MCP_URL", "http://127.0.0.1:8090/mcp"),
		CMDBMCPURL:         getenv("CMDB_MCP_URL", "http://127.0.0.1:8061/mcp"),
		ESMCPURL:           strings.TrimRight(getenv("ES_MCP_URL", ""), "/"),
		MCPDenyTools:       split(getenv("MCP_DENY_TOOLS", "delete_series,clean_tombstones,snapshot,reload,quit")),
		MCPAllowTools:      split(getenv("MCP_ALLOW_TOOLS", "")),
		ExplosionThreshold: getint("EXPLOSION_THRESHOLD", 10),
		DefaultViewMode:    getenv("DEFAULT_VIEW_MODE", "folded"),
		DefaultEnv:         getenv("DEFAULT_ENV", "PRO"),
		InspectCronEnabled: getbool("INSPECT_CRON_ENABLED", true),
		InspectCronTick:    time.Duration(getint("INSPECT_CRON_TICK_SEC", 20)) * time.Second,

		InspectMCPEnabled:   getbool("INSPECT_MCP_ENABLED", true),
		InspectMCPMaxHosts:  getint("INSPECT_MCP_MAX_HOSTS", 20),
		InspectMCPMaxApps:   getint("INSPECT_MCP_MAX_APPS", 10),
		InspectMCPTimeout:   time.Duration(getint("INSPECT_MCP_TIMEOUT_MS", 5000)) * time.Millisecond,
		InspectCMDBPageSize: getint("INSPECT_CMDB_PAGE_SIZE", 5),

		DiagVerifyEnabled: getbool("DIAG_VERIFY_ENABLED", true),
		DiagVerifyTimeout: time.Duration(getint("DIAG_VERIFY_TIMEOUT_MS", 2000)) * time.Millisecond,

		ESLogIndex:              getenv("ES_LOG_INDEX", "logs-*"),
		ESLogTimeField:          getenv("ES_LOG_TIME_FIELD", "@timestamp"),
		ESLogLevelField:         getenv("ES_LOG_LEVEL_FIELD", "level"),
		ESLogHostField:          getenv("ES_LOG_HOST_FIELD", "host"),
		InspectESLookbackMin:    getint("INSPECT_ES_LOOKBACK_MIN", 15),
		InspectESErrorThreshold: getint("INSPECT_ES_ERROR_THRESHOLD", 20),

		AlarmEnabled:       getbool("ALARM_ENABLED", true),
		AlarmPollInterval:  time.Duration(getint("ALARM_POLL_INTERVAL_SEC", 30)) * time.Second,
		AlarmCooldown:      time.Duration(getint("ALARM_COOLDOWN_MIN", 10)) * time.Minute,
		AlarmMaxAutoDiag:   getint("ALARM_MAX_AUTO_DIAG", 5),
		AlarmAutoDiag:      getbool("ALARM_AUTO_DIAG", true),
		AlarmMissTolerance: getint("ALARM_MISS_TOLERANCE", 2),

		AlertmanagerEnabled:    getbool("ALERTMANAGER_ENABLED", true),
		AlertmanagerURL:        strings.TrimRight(getenv("ALERTMANAGER_URL", "http://127.0.0.1:9093"), "/"),
		AlertmanagerTimeout:    time.Duration(getint("ALERTMANAGER_TIMEOUT_SEC", 3)) * time.Second,
		AlertmanagerGroupLabel: getenv("ALERTMANAGER_GROUP_LABEL", "group"),

		DegradationEnabled:      getbool("DEGRADATION_ENABLED", true),
		DegradationInterval:     time.Duration(getint("DEGRADATION_INTERVAL_SEC", 300)) * time.Second,
		DegradationWindowMin:    getint("DEGRADATION_WINDOW_MIN", 60),
		DegradationStepSec:      getint("DEGRADATION_STEP_SEC", 300),
		DegradationThresholdPct: getfloat("DEGRADATION_THRESHOLD_PCT", 30),
		DegradationMaxTargets:   getint("DEGRADATION_MAX_TARGETS", 10),
		DegradationQuery:        getenv("DEGRADATION_QUERY", `node_load1{instance=~"{host}.*"}`),

		AlarmNotifyURL:     strings.TrimRight(getenv("ALARM_NOTIFY_URL", ""), "/"),
		AlarmNotifyTimeout: time.Duration(getint("ALARM_NOTIFY_TIMEOUT_SEC", 5)) * time.Second,
		AlarmNotifyToken:   getenv("ALARM_NOTIFY_TOKEN", ""),

		BizMonEnabled:  getbool("BIZ_MON_ENABLED", true),
		BizMonInterval: time.Duration(getint("BIZ_MON_INTERVAL_SEC", 60)) * time.Second,

		QualityEnabled:   getbool("QUALITY_ENABLED", true),
		QualityWindowMin: getint("QUALITY_WINDOW_MIN", 60),

		LogAnalyzerEnabled: getbool("LOG_ANALYZER_ENABLED", true),
		LogAnalyzerURL:     strings.TrimRight(getenv("LOG_ANALYZER_URL", "http://127.0.0.1:8000"), "/"),
		LogPathTemplate:    getenv("LOG_PATH_TEMPLATE", "/tmp/aiops-logs/{host}.log"),
		LogAnalyzeTimeout:  time.Duration(getint("LOG_ANALYZE_TIMEOUT_SEC", 30)) * time.Second,
		LogAnalyzePoll:     time.Duration(getint("LOG_ANALYZE_POLL_MS", 800)) * time.Millisecond,

		IdentityEnabled:       getbool("IDENTITY_ENABLED", true),
		IdentityRebuildOnBoot: getbool("IDENTITY_REBUILD_ON_BOOT", true),
		IdentityCMDBPages:     getint("IDENTITY_CMDB_PAGES", 5),
		IdentityCMDBPageSize:  getint("IDENTITY_CMDB_PAGE_SIZE", 200),
		IdentityDNSResolve:    getbool("IDENTITY_DNS_RESOLVE", false),
		DiagWindowMin:         getint("DIAG_WINDOW_MIN", 5),

		LLMBaseURL: strings.TrimRight(getenv("LLM_BASE_URL", "https://api.deepseek.com"), "/"),
		LLMAPIKey:  getenv("LLM_API_KEY", ""),
		LLMModel:   getenv("LLM_MODEL", "deepseek-flash"),
		LLMTimeout: time.Duration(getint("LLM_TIMEOUT_SEC", 20)) * time.Second,
	}
	if c.TopoDataDir == "" {
		c.TopoDataDir = locateTopoData()
	}
	return c
}

func (c Config) String() string {
	return fmt.Sprintf("addr=%s topo=%s data=%s store=%s pg=%v mcp=%v",
		c.Addr, c.TopoMode, c.TopoDataDir, c.StoreDir, c.PostgresDSN != "", c.MCPEnabled)
}

// locateTopoData 兼容从仓库根、backend 目录、平台目录启动三种工作目录。
func locateTopoData() string {
	candidates := []string{
		filepath.Join("..", "..", "..", "topo", "tprel", "data"),
		filepath.Join("..", "..", "..", "..", "topo", "tprel", "data"),
		filepath.Join("..", "topo", "tprel", "data"),
		filepath.Join("topo", "tprel", "data"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, "operation_topology.json")); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	abs, _ := filepath.Abs(candidates[0])
	return abs
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getint(k string, def int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getbool(k string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func getfloat(k string, def float64) float64 {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func split(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
