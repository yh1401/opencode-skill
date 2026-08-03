"""
root_cause.py - Rule-based root cause inference engine for log-analyzer-skill.
Analyzes error patterns and stack traces to identify root causes with detailed information.
Includes: cause, impact, and remediation steps.
Zero external dependencies.
"""

import re
from typing import Dict, Any, List, Optional


# ── Built-in error pattern database with detailed information ──
ERROR_PATTERNS: List[Dict[str, Any]] = [
    # Java exceptions
    {
        "id": "null_pointer",
        "name": "NullPointerException",
        "pattern": re.compile(r'(?:java\.lang\.)?NullPointerException|NPE|null pointer'),
        "severity": "high",
        "category": "code_bug",
        "cause": "对象引用为 null 但被访问，通常是未初始化、返回 null 未校验或并发修改",
        "impact": "当前请求失败，可能导致级联错误",
        "remediation": [
            "检查空指针引用的对象是否已正确初始化",
            "确认是否为上游返回 null 未加校验",
            "使用 Optional 包装可能为 null 的返回值",
            "添加非空检查 (if (obj != null))",
            "审查代码中的对象生命周期管理",
        ],
    },
    {
        "id": "out_of_memory",
        "name": "OutOfMemoryError",
        "pattern": re.compile(r'(?:java\.lang\.)?OutOfMemoryError|OutOfMemory|OOM'),
        "severity": "critical",
        "category": "resource",
        "cause": "JVM 堆内存不足或存在内存泄漏（大量对象未释放）",
        "impact": "服务进程崩溃，需要重启",
        "remediation": [
            "增加 JVM 堆内存参数（-Xmx、-Xms）",
            "分析堆转储文件（Heap Dump）定位内存泄漏",
            "检查是否有大对象未释放（集合类、缓存、连接池）",
            "优化 GC 策略和内存使用",
            "考虑使用内存分析工具（VisualVM、MAT）",
        ],
    },
    {
        "id": "stack_overflow",
        "name": "StackOverflowError",
        "pattern": re.compile(r'StackOverflowError'),
        "severity": "critical",
        "category": "code_bug",
        "cause": "递归调用过深或死循环导致栈空间耗尽",
        "impact": "服务进程崩溃，需要重启",
        "remediation": [
            "检查递归终止条件是否正确",
            "将递归改为迭代实现",
            "增加栈空间（-Xss 参数）",
            "检查是否存在方法间循环调用",
        ],
    },
    {
        "id": "sql_exception",
        "name": "SQLException / DataAccess",
        "pattern": re.compile(r'(?:java\.sql\.)?SQLException|DataAccessException|deadlock|pool timeout|HikariPool'),
        "severity": "high",
        "category": "database",
        "cause": "数据库连接异常，可能原因：连接池耗尽、SQL 执行超时、死锁、数据库服务异常",
        "impact": "依赖数据库的操作失败，可能导致服务不可用",
        "remediation": [
            "检查数据库连接池配置（最大连接数、空闲超时）",
            "分析慢查询，优化 SQL 执行计划",
            "检查是否存在死锁（查看数据库锁等待）",
            "验证数据库服务状态和磁盘空间",
            "增加连接超时和查询超时配置",
        ],
    },
    {
        "id": "db_connection_failed",
        "name": "数据库连接失败",
        "pattern": re.compile(r'Connection refused.*database|DB.*connection.*failed|Cannot.*connect.*to.*database|DatabaseConnectionException|MySQL.*connection.*timeout|PostgreSQL.*connection.*timeout'),
        "severity": "critical",
        "category": "database",
        "cause": "数据库服务未启动、网络不通或连接池耗尽",
        "impact": "所有依赖数据库的功能不可用",
        "remediation": [
            "检查数据库服务是否正常运行",
            "验证数据库连接配置（主机、端口、用户名、密码）",
            "检查网络连通性（防火墙、安全组）",
            "查看数据库连接池配置，考虑增加最大连接数",
            "检查数据库磁盘空间是否充足",
        ],
    },
    {
        "id": "connection_timeout",
        "name": "Connection/Read Timeout",
        "pattern": re.compile(r'(connect timeout|Read timed out|timeout|Connection refused|Connection reset|No route to host|refused|SocketTimeoutException|ConnectTimeoutException)'),
        "severity": "high",
        "category": "network",
        "cause": "网络延迟过高、目标服务响应慢或不可达",
        "impact": "部分请求失败，用户体验下降",
        "remediation": [
            "检查网络链路状态和延迟",
            "增加超时时间配置",
            "检查目标服务是否正常响应",
            "考虑引入重试和熔断机制",
            "查看 DNS 解析是否正常",
        ],
    },
    {
        "id": "no_such_method",
        "name": "NoSuchMethodError",
        "pattern": re.compile(r'NoSuchMethod(Error|Exception)'),
        "severity": "high",
        "category": "deploy",
        "cause": "类版本不匹配，可能是编译与运行环境不一致",
        "impact": "特定功能不可用，可能导致服务启动失败",
        "remediation": [
            "检查依赖包版本是否一致",
            "清理并重新构建项目",
            "检查 classpath 中的 jar 包版本",
            "确认编译环境与运行环境一致",
        ],
    },
    {
        "id": "class_not_found",
        "name": "ClassNotFoundException / NoClassDefFoundError",
        "pattern": re.compile(r'(ClassNotFoundException|NoClassDefFoundError)'),
        "severity": "high",
        "category": "deploy",
        "cause": "类缺失：依赖包未正确部署、版本冲突或 classpath 配置错误",
        "impact": "服务启动失败或特定功能不可用",
        "remediation": [
            "检查 classpath 配置是否正确",
            "确认所有依赖包已完整部署",
            "检查是否存在版本冲突",
            "清理并重新部署依赖包",
        ],
    },
    {
        "id": "index_out_of_bounds",
        "name": "IndexOutOfBoundsException",
        "pattern": re.compile(r'(IndexOutOfBoundsException|ArrayIndex|StringIndex|越界)'),
        "severity": "medium",
        "category": "code_bug",
        "cause": "数组、列表或字符串操作时索引越界",
        "impact": "当前请求失败",
        "remediation": [
            "检查集合操作的索引值是否在有效范围内",
            "添加边界检查",
            "使用 try-catch 捕获异常并做容错处理",
        ],
    },
    {
        "id": "illegal_argument",
        "name": "IllegalArgumentException",
        "pattern": re.compile(r'IllegalArgumentException|参数.*错误|不能为空'),
        "severity": "medium",
        "category": "code_bug",
        "cause": "方法接收到了非法参数值",
        "impact": "当前请求失败",
        "remediation": [
            "检查调用方法时传入的参数是否合法",
            "在方法入口添加参数校验",
            "参考异常消息定位具体问题",
        ],
    },
    {
        "id": "class_cast",
        "name": "ClassCastException",
        "pattern": re.compile(r'ClassCastException'),
        "severity": "medium",
        "category": "code_bug",
        "cause": "类型转换不安全，尝试将对象转换为不兼容的类型",
        "impact": "当前请求失败",
        "remediation": [
            "检查类型转换前使用 instanceof 预检",
            "确保对象类型符合预期",
            "使用泛型避免强制类型转换",
        ],
    },
    # System-level patterns
    {
        "id": "disk_full",
        "name": "Disk Space Full",
        "pattern": re.compile(r'(No space left on device|disk full|Disk quota exceeded)'),
        "severity": "critical",
        "category": "system",
        "cause": "磁盘空间不足，可能由日志文件过大、临时文件未清理或数据文件增长过快",
        "impact": "写入操作失败，可能导致服务不可用",
        "remediation": [
            "清理日志文件（保留策略：只保留最近 N 天）",
            "清理临时文件和缓存目录",
            "检查数据文件增长趋势",
            "考虑扩容磁盘或清理旧数据",
        ],
    },
    {
        "id": "out_of_memory_killer",
        "name": "OOM Killer / Process Killed",
        "pattern": re.compile(r'(killed|OOM killer|oom-killer|Out of memory:|Memory cgroup out of memory)'),
        "severity": "critical",
        "category": "system",
        "cause": "系统内存不足，OOM Killer 杀死了进程",
        "impact": "服务进程被强制终止",
        "remediation": [
            "检查进程内存使用情况",
            "增加容器或机器的内存限制",
            "优化进程内存占用",
            "配置更严格的内存限制告警",
        ],
    },
    {
        "id": "file_not_found",
        "name": "FileNotFoundException",
        "pattern": re.compile(r'(FileNotFoundException|No such file or directory|找不到文件)'),
        "severity": "medium",
        "category": "config",
        "cause": "文件不存在：路径错误、文件未创建、目录挂载问题或权限不足",
        "impact": "依赖该文件的操作失败",
        "remediation": [
            "检查文件路径是否正确",
            "确认文件是否已创建",
            "检查目录挂载点是否正常",
            "验证文件读取权限",
        ],
    },
    {
        "id": "permission_denied",
        "name": "Permission Denied",
        "pattern": re.compile(r'(Permission denied|AccessDeniedException|权限.*拒绝)'),
        "severity": "medium",
        "category": "config",
        "cause": "当前用户或进程没有访问文件/目录的权限",
        "impact": "读取或写入操作失败",
        "remediation": [
            "检查文件/目录的权限设置",
            "确认运行进程的用户身份",
            "使用 chmod/chown 修改权限或所有者",
        ],
    },
    {
        "id": "thread_pool_exhausted",
        "name": "线程池耗尽",
        "pattern": re.compile(r'ThreadPoolExecutor.*rejected|RejectedExecutionException|Thread.*pool.*exhausted|All.*threads.*busy'),
        "severity": "high",
        "category": "infra",
        "cause": "线程池配置过小或任务提交过快，超出了处理能力",
        "impact": "新请求被拒绝，服务不可用",
        "remediation": [
            "增加线程池大小（corePoolSize、maxPoolSize）",
            "优化任务处理逻辑，减少耗时",
            "增加队列容量",
            "配置合理的拒绝策略",
            "考虑异步处理和解耦",
        ],
    },
    # Application-level patterns
    {
        "id": "rate_limited",
        "name": "Rate Limited / Throttled",
        "pattern": re.compile(r'(rate limit|throttl|too many requests|429)'),
        "severity": "medium",
        "category": "traffic",
        "cause": "请求频率超出服务限制，被限流保护",
        "impact": "部分请求被拒绝",
        "remediation": [
            "实现指数退避重试策略",
            "考虑增加限流阈值",
            "扩容服务实例分散压力",
            "引入请求合并/批处理机制",
        ],
    },
    {
        "id": "service_unavailable",
        "name": "Service Unavailable / 503",
        "pattern": re.compile(r'(Service Unavailable|503|circuit breaker|circuit_breaker|熔断|降级)'),
        "severity": "high",
        "category": "infra",
        "cause": "上游服务不可用或触发了熔断保护",
        "impact": "依赖该服务的功能不可用",
        "remediation": [
            "检查上游服务的健康状态",
            "查看熔断器配置和触发条件",
            "检查负载均衡配置",
            "等待熔断恢复或手动重置熔断器",
        ],
    },
    {
        "id": "validation_error",
        "name": "Validation Error",
        "pattern": re.compile(r'(Validation|校验失败|参数验证|validation error|BindException|MethodArgumentNotValid|不能为空)'),
        "severity": "medium",
        "category": "code_bug",
        "cause": "请求参数不符合接口约束或校验规则",
        "impact": "请求被拒绝，无法完成处理",
        "remediation": [
            "检查前端传入参数是否符合接口约束",
            "参考错误消息中的具体校验规则",
            "加强接口参数的输入校验",
            "提供更友好的参数校验提示",
        ],
    },
    {
        "id": "base_back_runtime",
        "name": "BaseBackRuntimeException",
        "pattern": re.compile(r'BaseBackRuntimeException|RuntimeException'),
        "severity": "high",
        "category": "code_bug",
        "cause": "后端运行时异常，通常是业务逻辑错误或未处理的边界条件",
        "impact": "当前业务请求失败",
        "remediation": [
            "查看具体错误消息定位问题",
            "检查业务逻辑中的异常处理",
            "审查代码中的边界条件处理",
            "添加更详细的日志记录",
        ],
    },
]


def infer_root_causes(records: List[Dict[str, Any]], stats: Dict[str, Any]) -> Dict[str, Any]:
    """
    Analyze records and statistics to identify root causes with detailed information.

    Returns a dict with:
      - causes: list of identified root cause clusters (with cause, impact, remediation)
      - derived_errors: grouped error signatures
      - timeline: chronological first occurrence of each error type
      - recommendation: overall action recommendation
      - findings: issues grouped by severity
    """
    # Collect all error records with their context
    errors = [r for r in records if r.get("level") in ("ERROR", "FATAL", "CRITICAL")]

    # Match errors against known patterns
    matched_causes: Dict[str, Dict[str, Any]] = {}
    error_timeline: Dict[str, str] = {}

    for err in errors:
        msg = err.get("message", "")
        ts = err.get("timestamp", "")
        module = err.get("module", "")

        for pat in ERROR_PATTERNS:
            if pat["pattern"].search(msg):
                pid = pat["id"]
                if pid not in matched_causes:
                    matched_causes[pid] = {
                        "id": pid,
                        "name": pat["name"],
                        "severity": pat["severity"],
                        "category": pat["category"],
                        "cause": pat.get("cause", "未知原因"),
                        "impact": pat.get("impact", "影响未知"),
                        "remediation": pat.get("remediation", ["暂无修复建议"]),
                        "suggestion": pat.get("suggestion", ""),
                        "occurrences": [],
                        "count": 0,
                    }
                    error_timeline[pid] = ts

                matched_causes[pid]["occurrences"].append({
                    "timestamp": ts,
                    "message": msg[:200],
                    "module": module,
                })
                matched_causes[pid]["count"] += 1

    # Build derived error signatures from top error messages
    derived = []
    for err_entry in stats.get("top_errors", []):
        msg = err_entry["message"]
        count = err_entry["count"]
        matched_pattern = None
        for pat in ERROR_PATTERNS:
            if pat["pattern"].search(msg):
                matched_pattern = pat["name"]
                break
        derived.append({
            "signature": msg[:150],
            "count": count,
            "matched_pattern": matched_pattern or "未知模式",
        })

    # Build result
    causes_list = sorted(
        matched_causes.values(),
        key=lambda c: {"critical": 4, "high": 3, "medium": 2, "low": 1}.get(c["severity"], 0),
        reverse=True,
    )

    # Group findings by severity
    findings = {
        "critical": [],
        "high": [],
        "medium": [],
        "low": [],
    }
    for cause in causes_list:
        sev = cause["severity"]
        if sev in findings:
            findings[sev].append(cause)

    # Generate recommendation
    recommendation = _generate_recommendation(stats, causes_list)

    return {
        "causes": causes_list,
        "derived_errors": derived[:30],
        "timeline": error_timeline,
        "total_matched": sum(c["count"] for c in causes_list),
        "recommendation": recommendation,
        "findings": findings,
        "summary": {
            "total_issues": len(causes_list),
            "critical_count": len(findings["critical"]),
            "high_count": len(findings["high"]),
            "medium_count": len(findings["medium"]),
            "low_count": len(findings["low"]),
            "description": _generate_summary_description(findings, causes_list),
        },
    }


def _generate_summary_description(findings: Dict, causes: List[Dict]) -> str:
    """Generate a summary description of the analysis."""
    critical = len(findings["critical"])
    high = len(findings["high"])
    total = len(causes)

    if total == 0:
        return "未检测到已知错误模式。"

    if critical > 0:
        return f"发现 {critical} 个严重问题、{high} 个高风险问题，需要立即处理。"
    elif high > 0:
        return f"发现 {high} 个高风险问题，建议优先处理。"
    else:
        return f"发现 {total} 个问题，整体风险可控。"


def _generate_recommendation(stats: Dict[str, Any], causes: List[Dict]) -> str:
    """Generate a human-readable recommendation based on analysis."""
    parts = []

    total_errors = stats.get("error_lines", 0)
    error_rate = stats.get("error_rate", 0)

    if total_errors == 0:
        return "未检测到错误日志，无需处理。"

    if error_rate > 50:
        parts.append(f"⚠️ 错误率高达 {error_rate}%，系统处于严重异常状态，建议立即介入排查。")
    elif error_rate > 20:
        parts.append(f"📈 错误率 {error_rate}%，需要优先处理。")
    elif error_rate > 5:
        parts.append(f"📊 错误率 {error_rate}%（共 {total_errors} 条错误），建议关注。")
    else:
        parts.append(f"✅ 错误率 {error_rate}%（共 {total_errors} 条错误），属于较低水平。")

    if causes:
        top = causes[0]
        parts.append(
            f"\n🔍 最频繁的错误：「{top['name']}」(类别: {top['category']}, 严重度: {top['severity']}) "
            f"共出现 {top['count']} 次。"
        )
        # Include cause, impact, and first remediation step
        if top.get("cause"):
            parts.append(f"\n📌 原因: {top['cause']}")
        if top.get("impact"):
            parts.append(f"\n⚡ 影响: {top['impact']}")
        if top.get("remediation") and top["remediation"]:
            parts.append(f"\n💡 修复建议:")
            for i, step in enumerate(top["remediation"][:3], 1):
                parts.append(f"   {i}. {step}")
            if len(top["remediation"]) > 3:
                parts.append(f"   ... (共 {len(top['remediation'])} 步)")

        if len(causes) > 1:
            secondary = causes[1]
            parts.append(f"\n📎 次要问题: {secondary['name']} ({secondary['count']}次)")
            if secondary.get("cause"):
                parts.append(f"   原因: {secondary['cause']}")

        if len(causes) > 2:
            others = sum(c['count'] for c in causes[2:])
            parts.append(f"\n📋 其他问题: {len(causes)-2} 种, 共 {others} 次")

    return " ".join(parts)
