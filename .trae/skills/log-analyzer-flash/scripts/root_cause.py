"""
root_cause.py - Rule-based root cause inference engine for log-analyzer.
Analyzes error patterns and stack traces to identify root causes.
"""

import re
from typing import Dict, Any, List, Optional, Tuple


# ── Built-in error pattern database ──
ERROR_PATTERNS: List[Dict[str, Any]] = [
    # Java exceptions
    {
        "id": "null_pointer",
        "name": "NullPointerException",
        "pattern": re.compile(r'java\.lang\.NullPointerException'),
        "severity": "high",
        "category": "code_bug",
        "suggestion": "检查空指针引用的对象是否已正确初始化，确认是否为上游返回null未加校验",
    },
    {
        "id": "out_of_memory",
        "name": "OutOfMemoryError",
        "pattern": re.compile(r'java\.lang\.OutOfMemoryError|OutOfMemory|OOM'),
        "severity": "critical",
        "category": "resource",
        "suggestion": "堆内存不足：检查是否有内存泄漏，考虑增加 -Xmx 或优化对象生命周期",
    },
    {
        "id": "stack_overflow",
        "name": "StackOverflowError",
        "pattern": re.compile(r'StackOverflowError'),
        "severity": "critical",
        "category": "code_bug",
        "suggestion": "递归调用过深或死循环：检查递归终止条件，或增加栈空间 -Xss",
    },
    {
        "id": "sql_exception",
        "name": "SQLException / DataAccess",
        "pattern": re.compile(r'(java\.sql\.SQLException|DataAccessException|deadlock|timeout|Connection refused)'),
        "severity": "high",
        "category": "database",
        "suggestion": "数据库异常：检查连接池、慢查询、死锁或数据库服务状态",
    },
    {
        "id": "connection_timeout",
        "name": "Connection/Read Timeout",
        "pattern": re.compile(r'(connect timeout|Read timed out|Connection refused|Connection reset|No route to host)'),
        "severity": "high",
        "category": "network",
        "suggestion": "网络连接问题：检查目标服务是否存活、防火墙规则、负载均衡状态",
    },
    {
        "id": "no_such_method",
        "name": "NoSuchMethodError",
        "pattern": re.compile(r'NoSuchMethod(Error|Exception)'),
        "severity": "high",
        "category": "deploy",
        "suggestion": "类版本不匹配：检查依赖包版本冲突，可能是编译与运行环境不一致",
    },
    {
        "id": "class_not_found",
        "name": "ClassNotFoundException / NoClassDefFoundError",
        "pattern": re.compile(r'(ClassNotFoundException|NoClassDefFoundError)'),
        "severity": "high",
        "category": "deploy",
        "suggestion": "类缺失：检查 classpath、依赖包是否完整部署、版本是否正确",
    },
    {
        "id": "index_out_of_bounds",
        "name": "IndexOutOfBoundsException",
        "pattern": re.compile(r'(IndexOutOfBoundsException|ArrayIndexOutOfBoundsException|StringIndexOutOfBoundsException)'),
        "severity": "medium",
        "category": "code_bug",
        "suggestion": "数组/列表越界：检查集合操作的索引值是否越界，或空集合访问",
    },
    {
        "id": "illegal_argument",
        "name": "IllegalArgumentException",
        "pattern": re.compile(r'IllegalArgumentException'),
        "severity": "medium",
        "category": "code_bug",
        "suggestion": "非法参数：检查调用方法时传入的参数是否合法",
    },
    # System-level patterns
    {
        "id": "disk_full",
        "name": "Disk Space Full",
        "pattern": re.compile(r'(No space left on device|disk full|Disk quota exceeded)'),
        "severity": "critical",
        "category": "system",
        "suggestion": "磁盘空间不足：清理日志/临时文件，或扩容磁盘",
    },
    {
        "id": "out_of_memory_killer",
        "name": "OOM Killer / Process Killed",
        "pattern": re.compile(r'(killed|OOM|oom-killer|Out of memory:|Memory cgroup out of memory)'),
        "severity": "critical",
        "category": "system",
        "suggestion": "系统 OOM：检查内存使用，考虑增加内存限制或优化进程内存占用",
    },
    {
        "id": "file_not_found",
        "name": "FileNotFoundException",
        "pattern": re.compile(r'(FileNotFoundException|No such file or directory)'),
        "severity": "medium",
        "category": "config",
        "suggestion": "文件不存在：检查文件路径、挂载点、权限配置",
    },
    {
        "id": "permission_denied",
        "name": "Permission Denied",
        "pattern": re.compile(r'(Permission denied|AccessDeniedException)'),
        "severity": "medium",
        "category": "config",
        "suggestion": "权限不足：检查文件/目录的读写执行权限",
    },
    # Application-level patterns
    {
        "id": "rate_limited",
        "name": "Rate Limited / Throttled",
        "pattern": re.compile(r'(rate limit|throttl|too many requests|429)'),
        "severity": "medium",
        "category": "traffic",
        "suggestion": "请求被限流：增加限流阈值、重试策略或扩容服务实例",
    },
    {
        "id": "service_unavailable",
        "name": "Service Unavailable / 503",
        "pattern": re.compile(r'(Service Unavailable|503|circuit breaker|circuit_breaker|熔断)'),
        "severity": "high",
        "category": "infra",
        "suggestion": "服务不可用：检查上游服务健康状态、熔断器配置、负载均衡",
    },
]


def infer_root_causes(records: List[Dict[str, Any]], stats: Dict[str, Any]) -> Dict[str, Any]:
    """
    Analyze records and statistics to identify root causes.

    Returns a dict with:
      - causes: list of identified root cause clusters
      - derived_errors: grouped error signatures
      - timeline: chronological first occurrence of each error type
      - recommendation: overall action recommendation
    """
    # Collect all error records with their context
    errors = [r for r in records if r.get("level") in ("ERROR", "FATAL", "CRITICAL")]

    # Match errors against known patterns
    matched_causes: Dict[str, List[Dict]] = {}
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
                        "suggestion": pat["suggestion"],
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

    # Generate recommendation
    top_cause = causes_list[0] if causes_list else None
    recommendation = _generate_recommendation(stats, causes_list)

    return {
        "causes": causes_list,
        "derived_errors": derived[:30],
        "timeline": error_timeline,
        "total_matched": sum(c["count"] for c in causes_list),
        "recommendation": recommendation,
    }


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
    else:
        parts.append(f"📊 错误率 {error_rate}%（共 {total_errors} 条错误），属于可接受范围。")

    if causes:
        top = causes[0]
        parts.append(
            f"🔍 最频繁的错误模式：「{top['name']}」(类别: {top['category']}, 严重度: {top['severity']}) "
            f"共出现 {top['count']} 次。"
        )
        parts.append(f"💡 建议: {top['suggestion']}")

        # Secondary causes
        if len(causes) > 1:
            secondary = causes[1]
            parts.append(f"📎 次要问题: {secondary['name']} ({secondary['count']}次) - {secondary['suggestion']}")

    return " ".join(parts)
