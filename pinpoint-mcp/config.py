import os
from typing import Optional

# Pinpoint Web 配置
WEB_BASE_URL: str = os.environ.get("WEB_BASE_URL", "http://127.0.0.1:8080")
# 认证凭据不提供默认值（安全要求）：必须通过环境变量注入，
# 至少配置 PINPOINT_COOKIE / PINPOINT_BASIC_AUTH / PINPOINT_USER+PINPOINT_PASS 之一，
# 全部缺失时 get_client() 会直接报错
PINPOINT_USER: str = os.environ.get("PINPOINT_USER", "")
PINPOINT_PASS: str = os.environ.get("PINPOINT_PASS", "")

# Basic 认证：Base64 编码的 "user:pass"，所有请求携带 Authorization: Basic 头；
# 可与 formLogin 组合使用（网关 Basic + 应用层 Cookie）。
PINPOINT_BASIC_AUTH: str = os.environ.get("PINPOINT_BASIC_AUTH", "")

# Cookie 认证（可选）：浏览器登录后复制的完整 Cookie 串，
# 用于企业 SSO / 定制网关环境（nginx 校验会话 Cookie）。
# 配置后跳过 formLogin，所有请求直接携带该 Cookie。
PINPOINT_COOKIE: str = os.environ.get("PINPOINT_COOKIE", "")

# X-Auth-Session 头（可选）：自定义网关的会话令牌，
# 如 nginx 返回的 X-Auth-Session 响应头值，配置后随所有请求携带。
PINPOINT_X_AUTH_SESSION: str = os.environ.get("PINPOINT_X_AUTH_SESSION", "")

# SSL 证书校验：Web 为 https 且证书不受信任（自签）时设为 false
PINPOINT_SSL_VERIFY: bool = os.environ.get("PINPOINT_SSL_VERIFY", "true").lower() == "true"

# 登录端点（Spring Security formLogin / 自定义网关登录页）
LOGIN_URL: str = os.environ.get("LOGIN_URL", "/login")

# 自动登录（可选）：Cookie 过期导致 302 -> 登录页时，
# 自动基于 LOGIN_URL 重新登录（解析表单并提交账号密码）。
# 注意：登录页含验证码/动态 token 时无法自动登录，需退回手动 PINPOINT_COOKIE。
PINPOINT_AUTO_LOGIN: bool = os.environ.get("PINPOINT_AUTO_LOGIN", "false").lower() == "true"

# 登录表单字段名（若登录页字段不叫 username/password 时配置）
PINPOINT_LOGIN_USERNAME_FIELD: str = os.environ.get("PINPOINT_LOGIN_USERNAME_FIELD", "username")
PINPOINT_LOGIN_PASSWORD_FIELD: str = os.environ.get("PINPOINT_LOGIN_PASSWORD_FIELD", "password")

# MCP Server 传输配置
MCP_HOST: str = os.environ.get("MCP_HOST", "0.0.0.0")
MCP_PORT: int = int(os.environ.get("MCP_PORT", "8888"))
MCP_NAME: str = os.environ.get("MCP_NAME", "pinpoint")

# 默认查询时间范围（毫秒）
DEFAULT_RANGE_MINUTES: int = int(os.environ.get("DEFAULT_RANGE_MINUTES", "5"))

# ---------------- 架构优化数据源 ----------------

# EasyViews 平台：模块拓扑、节点列表、宿主机关系的统一代理入口（POST）
EASYVIEWS_BASE_URL: str = os.environ.get(
    "EASYVIEWS_BASE_URL", "https://easyviews.tech.ctseelink.cn"
)
EASYVIEWS_PROXY_PATH: str = os.environ.get(
    "EASYVIEWS_PROXY_PATH", "/api/third/thirdAuth/getTykjProvince"
)
EASYVIEWS_PROVINCE: str = os.environ.get("EASYVIEWS_PROVINCE", "大模型")
# 凭据不提供默认值（安全要求）：必须通过环境变量注入，缺失时客户端构造直接报错
EASYVIEWS_APP_ID: str = os.environ.get("EASYVIEWS_APP_ID", "")
EASYVIEWS_APP_SECRET: str = os.environ.get("EASYVIEWS_APP_SECRET", "")
EASYVIEWS_SSL_VERIFY: bool = (
    os.environ.get("EASYVIEWS_SSL_VERIFY", "true").lower() == "true"
)
EASYVIEWS_TIMEOUT: float = float(os.environ.get("EASYVIEWS_TIMEOUT", "60"))
# 代理接口重试：网络异常 / 5xx 时最多重试次数与指数退避基数（秒）
EASYVIEWS_RETRY: int = int(os.environ.get("EASYVIEWS_RETRY", "2"))
EASYVIEWS_RETRY_BACKOFF: float = float(
    os.environ.get("EASYVIEWS_RETRY_BACKOFF", "1.0")
)
# 超时是否重试：超时通常说明上游查询过重或卡住，重试会把等待叠加成倍（60s×3），
# 与 Prometheus 侧口径一致，默认不重试
EASYVIEWS_RETRY_TIMEOUT: bool = (
    os.environ.get("EASYVIEWS_RETRY_TIMEOUT", "false").lower() == "true"
)

# Prometheus：容器副本、CPU 限流、内存、HPA、负载、OOM 等运行指标
PROMETHEUS_BASE_URL: str = os.environ.get(
    "PROMETHEUS_BASE_URL", "http://183.56.210.90:8082"
)
PROMETHEUS_SSL_VERIFY: bool = (
    os.environ.get("PROMETHEUS_SSL_VERIFY", "true").lower() == "true"
)
PROMETHEUS_TIMEOUT: float = float(os.environ.get("PROMETHEUS_TIMEOUT", "30"))
# 查询重试：网络异常 / 5xx 时最多重试次数与指数退避基数（秒）
PROMETHEUS_RETRY: int = int(os.environ.get("PROMETHEUS_RETRY", "2"))
PROMETHEUS_RETRY_BACKOFF: float = float(
    os.environ.get("PROMETHEUS_RETRY_BACKOFF", "1.0")
)

# 架构分析默认环境标签与产品名（可按节点覆盖）
ARCH_ENV: str = os.environ.get("ARCH_ENV", "生产")
ARCH_PRODUCT_NAME: str = os.environ.get("ARCH_PRODUCT_NAME", "天翼看家")

# 节点级标签口径覆盖：部分节点的业务指标不带 cmdb_node 标签（如广州4d），
# 改用 cluster 标签定位。格式 "节点=cluster值;节点2=值2"，环境变量可覆盖或追加。
# 命中映射的节点，规则查询与探测一律不再附加 cmdb_node 条件。
_RAW_NODE_CLUSTER = os.environ.get("ARCH_NODE_CLUSTER_MAP", "")
ARCH_NODE_CLUSTER_MAP: dict[str, str] = {}
for _item in _RAW_NODE_CLUSTER.split(";"):
    if "=" in _item:
        _node, _cluster = _item.split("=", 1)
        if _node.strip() and _cluster.strip():
            ARCH_NODE_CLUSTER_MAP[_node.strip()] = _cluster.strip()

# ---------------- 架构优化规则阈值（方案 4.1.4：阈值配置化 + 规则版本） ----------------

# 规则版本，随阈值口径调整递增，写入每份分析报告
# V2.3：副本阈值 3→2（业务确认）、OOM 回看 7d、内存 limit 新旧指标名自动回退
ARCH_RULE_VERSION: str = os.environ.get("ARCH_RULE_VERSION", "V2.3")

# 规则内部 rate() 采样窗口，与"持续 15 分钟"的观察窗口解耦
# 方案 1.2.2 / 1.2.3 使用 5m 速率；用 15m 速率再叠加 15 分钟采样会双重平滑掉尖峰
ARCH_RATE_WINDOW: str = os.environ.get("ARCH_RATE_WINDOW", "5m")

# 指标名可切换：不同 cAdvisor / kube-state-metrics 版本口径不同，联调时用环境变量覆盖
# 限流比例分子/分母必须成对切换；若使用 *_seconds_total 变体，比值语义会变化，需同步调整阈值
ARCH_METRIC_CPU_THROTTLED: str = os.environ.get(
    "ARCH_METRIC_CPU_THROTTLED", "container_cpu_cfs_throttled_periods_total"
)
ARCH_METRIC_CPU_PERIODS: str = os.environ.get(
    "ARCH_METRIC_CPU_PERIODS", "container_cpu_cfs_periods_total"
)
# 内存 limit：新版 KSM 为 kube_pod_container_resource_limits{resource="memory"}，
# 旧版为 kube_pod_container_resource_limits_memory_bytes（无 resource 标签）；
# 主口径无数据时自动回退另一口径，无需手工切换
ARCH_METRIC_MEMORY_LIMIT: str = os.environ.get(
    "ARCH_METRIC_MEMORY_LIMIT", "kube_pod_container_resource_limits"
)

# 批量分析保护：单规则返回明细上限、风险清单上限与规则并发数
# 巡检场景只需风险明细，200 条会让单次返回膨胀到 20 万 token 量级；
# 默认 10 条/规则，完整明细改用 detail=full 或单应用 arch_rule_check 下钻
ARCH_BATCH_TOP_N: int = int(os.environ.get("ARCH_BATCH_TOP_N", "10"))
ARCH_FINDINGS_MAX: int = int(os.environ.get("ARCH_FINDINGS_MAX", "30"))
ARCH_BATCH_CONCURRENCY: int = int(os.environ.get("ARCH_BATCH_CONCURRENCY", "4"))

# 批量筛查后精确回验：副本类规则 RISK 项用单应用接口（deployment+namespace 精确过滤）
# 再判一次，防止同名/跨集群/同命名空间序列串配造成误报；最多回验 12 个应用
ARCH_BATCH_RECHECK_RISKS: bool = (
    os.environ.get("ARCH_BATCH_RECHECK_RISKS", "true").lower() == "true"
)
ARCH_BATCH_RECHECK_MAX: int = int(os.environ.get("ARCH_BATCH_RECHECK_MAX", "12"))
ARCH_BATCH_RECHECK_CONCURRENCY: int = int(
    os.environ.get("ARCH_BATCH_RECHECK_CONCURRENCY", "4")
)

# 巡检输出压缩（P1）：宿主机集中度等明细默认只回摘要，details=full 可取全量
ARCH_OUTPUT_DETAIL: str = os.environ.get("ARCH_OUTPUT_DETAIL", "summary")
ARCH_PARTIAL_LIMIT: int = int(os.environ.get("ARCH_PARTIAL_LIMIT", "5"))
ARCH_PROJECT_LIMIT: int = int(os.environ.get("ARCH_PROJECT_LIMIT", "10"))

# 多节点巡检（P2）：节点级并发、单节点摘要条数、全局风险清单上限
ARCH_NODE_CONCURRENCY: int = int(os.environ.get("ARCH_NODE_CONCURRENCY", "6"))
ARCH_PATROL_TOP_N: int = int(os.environ.get("ARCH_PATROL_TOP_N", "3"))
ARCH_PATROL_RISK_MAX: int = int(os.environ.get("ARCH_PATROL_RISK_MAX", "30"))
ARCH_PATROL_MAX_NODES: int = int(os.environ.get("ARCH_PATROL_MAX_NODES", "50"))
ARCH_PATROL_NODE_TIMEOUT: int = int(
    os.environ.get("ARCH_PATROL_NODE_TIMEOUT", "180")
)

# 任务作用域（P3）：跨会话继承 task_id 的节点白名单，TTL 分钟
ARCH_TASK_TTL_MINUTES: int = int(os.environ.get("ARCH_TASK_TTL_MINUTES", "30"))
# REST 批量入口令牌；为空时 /api/arch/* 一律 403
ARCH_API_TOKEN: str = os.environ.get("ARCH_API_TOKEN", "")

# 生产巡检默认排除的命名空间清单（逗号分隔，环境变量覆盖）
# 用于排除系统组件 / operator / 监控 / ingress / 存储 / 灰度 等非业务命名空间
# 工具层 exclude_namespace 参数未显式指定时，自动使用此默认值
_RAW_EXCLUDE = os.environ.get(
    "ARCH_EXCLUDE_NAMESPACES",
    "kube-system,kube-public,kube-node-lease,default,"
    "prometheus,grafana,monitoring,loki,tempo,efk,log,"
    "ingress-nginx,istio-system,traefik,apigw,"
    "rook-ceph,openebs,storage,nfs-ceph-nfs,"
    "jenkins,gitlab-runner,argocd,tekton,"
    "velero,harbor,registry,etcd,calico-system,cattle-system,"
    "cert-manager,kube-bench,trivy,opa,"
    "staging,canary",
)
ARCH_EXCLUDE_NAMESPACES: list[str] = [
    ns.strip() for ns in _RAW_EXCLUDE.split(",") if ns.strip()
]
# 环境变量设为空字符串时禁用默认排除
if os.environ.get("ARCH_EXCLUDE_NAMESPACES", None) == "":
    ARCH_EXCLUDE_NAMESPACES = []

# 副本冗余要求：业务确认冗余下限为 2，spec < 2（单副本）即判风险
# （实测基线 2026-10-19：min_replicas 已由 3 调为 2，见 references/prometheus_query_rules.md §9）
ARCH_RULE_MIN_REPLICAS: int = int(os.environ.get("ARCH_RULE_MIN_REPLICAS", "2"))

# CPU 限流周期比例上限（> 20%）
ARCH_RULE_CPU_THROTTLE_RATIO: float = float(
    os.environ.get("ARCH_RULE_CPU_THROTTLE_RATIO", "0.20")
)

# 内存工作集 / 有效 limit 上限（> 85%）
ARCH_RULE_MEMORY_RATIO: float = float(os.environ.get("ARCH_RULE_MEMORY_RATIO", "0.85"))

# 同规格 Pod CPU 最大/平均比值上限（> 2）
ARCH_RULE_POD_CPU_IMBALANCE: float = float(
    os.environ.get("ARCH_RULE_POD_CPU_IMBALANCE", "2.0")
)

# 持续型规则的观察窗口（分钟），方案要求"持续 15 分钟"
ARCH_RULE_DURATION_MINUTES: int = int(os.environ.get("ARCH_RULE_DURATION_MINUTES", "15"))

# OOM 回看窗口：主查询子查询与重启/事件增量证据共用（实测口径 D4 默认 7 天）
ARCH_RULE_OOM_RESTART_WINDOW: str = os.environ.get(
    "ARCH_RULE_OOM_RESTART_WINDOW", "7d"
)
# OOM 的 7d 辅助查询（restarts / oom_events）改为单点即时查询：
# increase(...[7d]) 在 now 处的值即"过去 7 天增长量"，判定只取 any(>0)，
# 无需 15 个采样点各评估一次 7 天窗口（实测为单次复核耗时主因）。
ARCH_RULE_OOM_AUX_INSTANT: bool = (
    os.environ.get("ARCH_RULE_OOM_AUX_INSTANT", "true").lower() == "true"
)

# 单次规则复核的总时限（秒）：超过则中止并把未完成部分记为数据不足，
# 保证在网关/客户端空闲超时（常见 60s）之前一定返回；0 表示不限制
ARCH_RULE_TIMEOUT: float = float(os.environ.get("ARCH_RULE_TIMEOUT", "45"))

# 持续型规则的最低采样覆盖率：有效点占比低于该值判为数据不足
ARCH_RULE_MIN_COVERAGE: float = float(os.environ.get("ARCH_RULE_MIN_COVERAGE", "0.8"))

# Pod 负载不均衡的均值下限（核），均值低于该值视为比值失真不计入判定
ARCH_RULE_IMBALANCE_MIN_AVG_CORES: float = float(
    os.environ.get("ARCH_RULE_IMBALANCE_MIN_AVG_CORES", "0.01")
)

# 第一阶段仅检查名称以 -dep 结尾的应用（方案 1.2 硬约束）
ARCH_DEP_SUFFIX_ONLY: bool = (
    os.environ.get("ARCH_DEP_SUFFIX_ONLY", "true").lower() == "true"
)
ARCH_DEP_SUFFIX: str = os.environ.get("ARCH_DEP_SUFFIX", "-dep")

# 灰度 / 临时发布应用名模式（逗号分隔，按正则片段匹配应用名，大小写不敏感）
# 命中者从 -dep 批量分析中剔除，避免灰度副本被当作正式应用计入冗余/容量判定；
# 显式指定 deployment（单应用复核）时不剔除；设为空字符串可关闭该过滤
_RAW_GRAY = os.environ.get("ARCH_GRAY_PATTERNS", "gray,canary,shadow")
ARCH_GRAY_PATTERNS: list[str] = [
    p.strip() for p in _RAW_GRAY.split(",") if p.strip()
]

# Pinpoint ↔ EasyViews 应用名映射：最短可匹配长度，避免短名误配
ARCH_MAPPING_MIN_LEN: int = int(os.environ.get("ARCH_MAPPING_MIN_LEN", "6"))
# 映射最低置信分（0~1），低于该值不纳入映射结果
ARCH_MAPPING_MIN_SCORE: float = float(os.environ.get("ARCH_MAPPING_MIN_SCORE", "0.7"))

# ---------------- 链路拓扑图计算阈值（方案 1.3，默认值为建议值，待方案最终确认） ----------------

# 链路拓扑统计窗口（分钟）：默认 15，5 分钟窗口在低峰期样本过少容易误判
ARCH_LINK_WINDOW_MINUTES: int = int(
    os.environ.get("ARCH_LINK_WINDOW_MINUTES", "15")
)
# 集中依赖：单应用被依赖方数量上限
ARCH_LINK_HIGH_INDEGREE: int = int(os.environ.get("ARCH_LINK_HIGH_INDEGREE", "5"))
# 下游依赖过多：单应用依赖方数量上限
ARCH_LINK_HIGH_OUTDEGREE: int = int(os.environ.get("ARCH_LINK_HIGH_OUTDEGREE", "5"))
# 长调用链：最长路径节点数上限
ARCH_LINK_LONG_PATH: int = int(os.environ.get("ARCH_LINK_LONG_PATH", "8"))
# 异常调用边：错误率上限
ARCH_LINK_ERROR_RATE: float = float(os.environ.get("ARCH_LINK_ERROR_RATE", "0.05"))
# 慢调用：平均耗时上限（毫秒）
ARCH_LINK_SLOW_MS: float = float(os.environ.get("ARCH_LINK_SLOW_MS", "1000"))
# 慢调用：慢请求占比上限（Pinpoint serverMap 的 slowCount/totalCount）
ARCH_LINK_SLOW_RATE: float = float(os.environ.get("ARCH_LINK_SLOW_RATE", "0.1"))
# 上下游影响范围：单节点异常波及的上游节点数达到该值即标记（0 表示关闭该判定）
ARCH_LINK_IMPACT_MIN_NODES: int = int(
    os.environ.get("ARCH_LINK_IMPACT_MIN_NODES", "5")
)
# 关键依赖：经过该节点的源→汇路径数达到该值即标记（0 表示关闭该判定）
ARCH_LINK_CRITICAL_PATHS: int = int(
    os.environ.get("ARCH_LINK_CRITICAL_PATHS", "3")
)
# CPU 限流 × 调用延迟交叉验证：P99 延迟 PromQL 模板，留空表示不启用
# 支持占位符 $NODE / $ENV / $DEPLOYMENT / $WINDOW；结果需为毫秒（秒级指标请 *1000）
ARCH_LATENCY_PROMQL: str = os.environ.get("ARCH_LATENCY_PROMQL", "")
# P99 延迟阈值（毫秒），用于判断限流是否已造成业务影响
ARCH_LATENCY_P99_MS: float = float(os.environ.get("ARCH_LATENCY_P99_MS", "1000"))

# Pod 归属：优先用 kube_pod_owner 解析真实 Pod 列表，失败或为空时回退名称前缀匹配
ARCH_POD_OWNER_LOOKUP: bool = (
    os.environ.get("ARCH_POD_OWNER_LOOKUP", "true").lower() == "true"
)
# 单次解析的 Pod 数上限，超过则回退前缀匹配，避免生成超长正则
ARCH_POD_OWNER_MAX_PODS: int = int(os.environ.get("ARCH_POD_OWNER_MAX_PODS", "200"))
# Pod 归属解析结果缓存有效期（秒）；0 表示不缓存
ARCH_POD_OWNER_CACHE_TTL: int = int(
    os.environ.get("ARCH_POD_OWNER_CACHE_TTL", "300")
)

# 链路拓扑工具总时限（秒）：网关空闲超时常见 60s，工具执行期间响应流为空，
# 超时返回数据不足而不是让调用方长时间无输出后被掐断；0 表示不限制
ARCH_LINK_TIMEOUT: float = float(os.environ.get("ARCH_LINK_TIMEOUT", "45"))
# 链路拓扑分批取数：单批应用数上限（0 表示不分批，按原样一次性请求）
# 上游耗时随应用数 × 窗口线性增长，121 应用 × 15 分钟单次请求会超出网关空闲超时
ARCH_LINK_BATCH_SIZE: int = int(os.environ.get("ARCH_LINK_BATCH_SIZE", "30"))
# 链路拓扑分批取数的并发批数（批数越多总耗时越低，但加大对上游监控平台的压力）
ARCH_LINK_BATCH_CONCURRENCY: int = int(
    os.environ.get("ARCH_LINK_BATCH_CONCURRENCY", "4")
)

# 图计算规模保护：超过该节点数跳过最长路径与关键依赖计算
ARCH_LINK_MAX_NODES: int = int(os.environ.get("ARCH_LINK_MAX_NODES", "800"))
# 结果条数上限，避免超大拓扑撑爆上下文
ARCH_LINK_TOP_N: int = int(os.environ.get("ARCH_LINK_TOP_N", "20"))

# 省节点集中度：单一宿主机承载占比达到该值视为部分集中（方案 2.5 未定阈值）
ARCH_HOST_CONCENTRATION_RATIO: float = float(
    os.environ.get("ARCH_HOST_CONCENTRATION_RATIO", "0.5")
)
