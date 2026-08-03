"""
root_cause.py - Rule-based root cause inference engine.
Matches error patterns against a knowledge base to provide:
- Root cause analysis
- Suggested remediation actions
- Impact assessment
"""

import re
from typing import Dict, Any, List, Optional


class RootCauseEngine:
    """Rule-based root cause analysis engine."""

    def __init__(self):
        self.patterns = self._load_patterns()

    def _load_patterns(self) -> List[Dict[str, Any]]:
        """Load error pattern knowledge base."""
        return [
            {
                "id": "db_connection_failed",
                "name": "数据库连接失败",
                "severity": "critical",
                "patterns": [
                    r"Connection refused.*database|DB.*connection.*failed",
                    r"Cannot.*connect.*to.*database",
                    r"DatabaseConnectionException|SQLException.*connection",
                    r"MySQLNonTransientConnectionException",
                    r"PostgreSQL.*connection.*timeout",
                    r"OracleConnectionException",
                ],
                "cause": "数据库服务未启动、网络不通或连接池耗尽",
                "remediation": [
                    "检查数据库服务是否正常运行",
                    "验证数据库连接配置（主机、端口、用户名、密码）",
                    "检查网络连通性（防火墙、安全组）",
                    "查看数据库连接池配置，考虑增加最大连接数",
                    "检查数据库磁盘空间是否充足",
                ],
                "impact": "所有依赖数据库的功能不可用",
            },
            {
                "id": "out_of_memory",
                "name": "内存溢出",
                "severity": "critical",
                "patterns": [
                    r"OutOfMemoryError|java.lang.OutOfMemoryError",
                    r"OOM.*error|Memory.*leak",
                    r"GC.*overhead.*limit.*exceeded",
                    r"PermGen.*space|Metaspace.*error",
                ],
                "cause": "JVM堆内存不足或存在内存泄漏",
                "remediation": [
                    "增加JVM堆内存参数（-Xmx、-Xms）",
                    "分析堆转储文件定位内存泄漏",
                    "检查是否有大对象未释放",
                    "优化GC策略和内存使用",
                    "考虑使用内存分析工具（如VisualVM、MAT）",
                ],
                "impact": "服务进程崩溃，需要重启",
            },
            {
                "id": "network_timeout",
                "name": "网络超时",
                "severity": "high",
                "patterns": [
                    r"SocketTimeoutException|ConnectTimeoutException",
                    r"Connection.*timeout|Read.*timeout",
                    r"Timed.*out.*while.*connecting",
                    r"netty.*timeout",
                    r"HTTP.*timeout",
                ],
                "cause": "网络延迟过高、目标服务响应慢或不可达",
                "remediation": [
                    "检查网络链路状态和延迟",
                    "增加超时时间配置",
                    "检查目标服务是否正常响应",
                    "考虑引入重试和熔断机制",
                    "查看DNS解析是否正常",
                ],
                "impact": "部分请求失败，用户体验下降",
            },
            {
                "id": "thread_pool_exhausted",
                "name": "线程池耗尽",
                "severity": "high",
                "patterns": [
                    r"ThreadPoolExecutor.*rejected|RejectedExecutionException",
                    r"Thread.*pool.*exhausted|All.*threads.*busy",
                    r"ExecutorService.*rejected",
                ],
                "cause": "线程池配置过小或任务提交过快",
                "remediation": [
                    "增加线程池大小（corePoolSize、maxPoolSize）",
                    "优化任务处理逻辑，减少耗时",
                    "增加队列容量",
                    "配置合理的拒绝策略",
                    "考虑异步处理和解耦",
                ],
                "impact": "新请求被拒绝，服务不可用",
            },
            {
                "id": "file_system_full",
                "name": "磁盘空间不足",
                "severity": "high",
                "patterns": [
                    r"Disk.*full|No.*space.*left.*on.*device",
                    r"FileNotFoundException.*write",
                    r"IOException.*disk|Storage.*full",
                ],
                "cause": "磁盘空间已满或配额不足",
                "remediation": [
                    "清理日志文件和临时文件",
                    "扩展磁盘空间",
                    "配置日志自动清理策略",
                    "检查是否有异常大文件产生",
                    "设置磁盘空间告警",
                ],
                "impact": "无法写入日志和数据，服务异常",
            },
            {
                "id": "authentication_failed",
                "name": "认证失败",
                "severity": "medium",
                "patterns": [
                    r"AuthenticationException|InvalidTokenException",
                    r"Login.*failed|Unauthorized|401.*Unauthorized",
                    r"JWT.*expired|Token.*invalid",
                    r"Invalid.*credentials|Bad.*credentials",
                ],
                "cause": "用户凭证无效、令牌过期或认证配置错误",
                "remediation": [
                    "检查用户认证凭证是否正确",
                    "验证JWT令牌配置和过期时间",
                    "检查OAuth配置",
                    "查看认证服务是否正常",
                    "检查LDAP/数据库认证配置",
                ],
                "impact": "用户无法登录，部分功能受限",
            },
            {
                "id": "configuration_error",
                "name": "配置错误",
                "severity": "high",
                "patterns": [
                    r"ConfigurationException|InvalidConfiguration",
                    r"Config.*error|Property.*not.*found",
                    r"Missing.*configuration|Config.*parse.*error",
                    r"YAML.*parse.*error|JSON.*parse.*error",
                ],
                "cause": "配置文件格式错误或缺少必要配置项",
                "remediation": [
                    "检查配置文件语法和格式",
                    "验证所有必要配置项是否存在",
                    "检查配置文件编码",
                    "使用配置校验工具",
                    "回滚到之前的正确配置",
                ],
                "impact": "服务无法启动或功能异常",
            },
            {
                "id": "cache_miss",
                "name": "缓存穿透/失效",
                "severity": "medium",
                "patterns": [
                    r"Cache.*miss|Redis.*connection.*error",
                    r"Memcached.*timeout|Cache.*not.*available",
                    r"Unable.*to.*connect.*to.*cache",
                ],
                "cause": "缓存服务不可用或缓存键不存在",
                "remediation": [
                    "检查缓存服务是否正常运行",
                    "验证缓存连接配置",
                    "增加缓存预热机制",
                    "配置缓存降级策略",
                    "检查缓存键设计是否合理",
                ],
                "impact": "请求直接打到数据库，可能导致数据库压力增大",
            },
            {
                "id": "rate_limit_exceeded",
                "name": "限流触发",
                "severity": "low",
                "patterns": [
                    r"RateLimitExceeded|TooManyRequests",
                    r"429.*Too.*Many.*Requests|Rate.*limit.*exceeded",
                    r"Throttled|Quota.*exceeded",
                ],
                "cause": "请求频率超过系统限流阈值",
                "remediation": [
                    "检查限流配置是否合理",
                    "考虑增加限流阈值",
                    "优化客户端请求频率",
                    "引入熔断和降级机制",
                    "实现请求排队",
                ],
                "impact": "部分请求被拒绝，需要重试",
            },
            {
                "id": "deadlock_detected",
                "name": "死锁检测",
                "severity": "critical",
                "patterns": [
                    r"Deadlock.*detected|DeadlockException",
                    r"Thread.*deadlock|Lock.*timeout",
                    r"Circular.*wait.*detected",
                ],
                "cause": "多个线程互相持有对方需要的锁",
                "remediation": [
                    "分析线程转储定位死锁",
                    "检查锁的获取顺序",
                    "使用定时锁替代永久锁",
                    "减少锁的粒度和持有时间",
                    "考虑使用无锁数据结构",
                ],
                "impact": "线程阻塞，服务响应缓慢或无响应",
            },
            {
                "id": "ssl_certificate_error",
                "name": "SSL证书错误",
                "severity": "high",
                "patterns": [
                    r"SSLHandshakeException|CertificateException",
                    r"SSL.*error|Certificate.*expired",
                    r"Untrusted.*certificate|Invalid.*SSL",
                ],
                "cause": "SSL证书过期、不匹配或未配置",
                "remediation": [
                    "检查SSL证书有效期",
                    "验证证书域名是否匹配",
                    "更新SSL证书",
                    "检查证书链配置",
                    "验证SSL协议版本",
                ],
                "impact": "HTTPS请求失败，服务不可访问",
            },
            {
                "id": "dependency_failure",
                "name": "依赖服务失败",
                "severity": "high",
                "patterns": [
                    r"ServiceUnavailableException|503.*Service.*Unavailable",
                    r"Downstream.*error|Upstream.*timeout",
                    r"Failed.*to.*call.*service|RPC.*failed",
                    r"gRPC.*error|Dubbo.*timeout",
                ],
                "cause": "下游依赖服务不可用或响应超时",
                "remediation": [
                    "检查依赖服务健康状态",
                    "验证服务间网络连通性",
                    "配置超时和重试策略",
                    "实现熔断和降级",
                    "考虑服务降级方案",
                ],
                "impact": "依赖该服务的功能不可用",
            },
        ]

    def analyze(self, records: List[Dict[str, Any]], stats: Dict[str, Any]) -> Dict[str, Any]:
        """
        Perform root cause analysis on log records.

        Args:
            records: Parsed log records
            stats: Computed statistics

        Returns:
            Root cause analysis result with matched patterns and recommendations
        """
        error_messages = []
        for rec in records:
            if rec.get("level") in ("ERROR", "FATAL", "CRITICAL"):
                msg = rec.get("message", "")
                error_messages.append(msg)

        matched_patterns = self._match_patterns(error_messages)
        grouped_results = self._group_by_severity(matched_patterns)

        return {
            "analysis_version": "1.0",
            "total_errors_analyzed": len(error_messages),
            "patterns_matched": len(matched_patterns),
            "findings": grouped_results,
            "recommendations": self._generate_recommendations(grouped_results),
            "summary": self._generate_summary(grouped_results),
        }

    def _match_patterns(self, messages: List[str]) -> List[Dict[str, Any]]:
        """Match error messages against pattern database."""
        matched = []
        seen_patterns = set()

        for msg in messages:
            for pattern_def in self.patterns:
                pattern_id = pattern_def["id"]
                if pattern_id in seen_patterns:
                    continue

                for regex in pattern_def["patterns"]:
                    if re.search(regex, msg, re.IGNORECASE):
                        matched.append({
                            **pattern_def,
                            "sample_message": msg[:200] if msg else "",
                            "matched_regex": regex,
                        })
                        seen_patterns.add(pattern_id)
                        break

        return matched

    def _group_by_severity(self, patterns: List[Dict[str, Any]]) -> Dict[str, List[Dict[str, Any]]]:
        """Group matched patterns by severity level."""
        result = {"critical": [], "high": [], "medium": [], "low": []}
        for pattern in patterns:
            severity = pattern.get("severity", "medium")
            if severity in result:
                result[severity].append(pattern)
        return result

    def _generate_recommendations(self, findings: Dict[str, Any]) -> List[Dict[str, Any]]:
        """Generate prioritized remediation recommendations."""
        recommendations = []
        priority_order = ["critical", "high", "medium", "low"]

        for severity in priority_order:
            for finding in findings.get(severity, []):
                recommendations.append({
                    "priority": severity,
                    "issue": finding["name"],
                    "cause": finding["cause"],
                    "actions": finding["remediation"],
                    "impact": finding["impact"],
                })

        return recommendations

    def _generate_summary(self, findings: Dict[str, Any]) -> Dict[str, Any]:
        """Generate analysis summary."""
        critical_count = len(findings.get("critical", []))
        high_count = len(findings.get("high", []))
        medium_count = len(findings.get("medium", []))
        low_count = len(findings.get("low", []))

        overall_severity = "healthy"
        if critical_count > 0:
            overall_severity = "critical"
        elif high_count > 0:
            overall_severity = "warning"
        elif medium_count > 0:
            overall_severity = "caution"

        return {
            "overall_severity": overall_severity,
            "critical_issues": critical_count,
            "high_issues": high_count,
            "medium_issues": medium_count,
            "low_issues": low_count,
            "description": {
                "healthy": "未检测到明显的错误模式，系统运行正常",
                "critical": f"检测到 {critical_count} 个严重问题，需要立即处理",
                "warning": f"检测到 {high_count} 个高优先级问题，建议尽快处理",
                "caution": f"检测到 {medium_count} 个中等优先级问题，需要关注",
            }.get(overall_severity, "未知"),
        }

    def add_custom_pattern(self, pattern_def: Dict[str, Any]) -> None:
        """Add a custom error pattern to the knowledge base."""
        required_fields = ["id", "name", "severity", "patterns", "cause", "remediation", "impact"]
        if not all(field in pattern_def for field in required_fields):
            raise ValueError("Missing required fields in pattern definition")
        if pattern_def["severity"] not in ("critical", "high", "medium", "low"):
            raise ValueError("Invalid severity level")

        self.patterns.append(pattern_def)

    def list_patterns(self) -> List[Dict[str, Any]]:
        """List all registered patterns."""
        return self.patterns


def analyze_root_cause(records: List[Dict[str, Any]], stats: Dict[str, Any]) -> Dict[str, Any]:
    """Convenience function to run root cause analysis."""
    engine = RootCauseEngine()
    return engine.analyze(records, stats)