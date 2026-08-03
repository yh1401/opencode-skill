"""
stats.py - Error statistics engine for log-analyzer-skill.
Aggregates parsed log records into structured statistics.
Zero external dependencies.
"""

import re
from collections import Counter
from datetime import datetime
from typing import Dict, Any, List, Optional


def compute_statistics(records: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    Compute full statistics from a list of parsed log records.

    Returns a dict with:
      - summary: total counts by level
      - by_module: error counts per module
      - by_hour: error counts per hour bucket
      - top_errors: most frequent error messages (normalized)
      - time_range: first and last timestamp
      - error_rate: percentage of ERROR/FATAL lines
      - extra: format-specific stats
    """
    total = len(records)
    if total == 0:
        return _empty_stats()

    level_counts: Counter[str] = Counter()
    module_counts: Counter[str] = Counter()
    message_counts: Counter[str] = Counter()
    error_type_counts: Counter[str] = Counter()
    hour_buckets: Counter[str] = Counter()
    exception_counts: Counter[str] = Counter()
    status_counts: Counter[int] = Counter()
    http_paths: Counter[str] = Counter()

    timestamps: List[str] = []

    for rec in records:
        level = rec.get("level", "UNKNOWN")
        level_counts[level] += 1

        msg = rec.get("message", "")
        if level in ("ERROR", "FATAL", "CRITICAL"):
            # Normalize error messages for grouping (strip variable parts)
            normalized = _normalize_error(msg)
            message_counts[normalized] += 1

        # Error type stats
        err_type = rec.get("error_type")
        if err_type:
            error_type_counts[err_type] += 1

        # Module/class stats
        mod = rec.get("module", "")
        if mod and level in ("ERROR", "FATAL", "WARN", "WARNING", "CRITICAL"):
            module_counts[mod] += 1

        # Timestamp and hour buckets
        ts = rec.get("timestamp")
        if ts:
            timestamps.append(ts)
            try:
                dt = datetime.fromisoformat(ts[:26])
                hour_buckets[dt.strftime("%Y-%m-%d %H:00")] += 1
            except (ValueError, IndexError):
                pass

        # Format-specific extra stats
        exc = rec.get("exception", "")
        if exc:
            exception_counts[exc] += 1

        status = rec.get("http_status")
        if status:
            status_counts[status] += 1
            path = rec.get("http_path", "")
            if path and status >= 400:
                http_paths[f"{status} {path}"] += 1

    # Time range
    time_range = {}
    if timestamps:
        timestamps.sort()
        time_range = {"first": timestamps[0], "last": timestamps[-1]}

    # Error rate
    error_count = level_counts.get("ERROR", 0) + level_counts.get("FATAL", 0) + level_counts.get("CRITICAL", 0)
    error_rate = round(error_count / total * 100, 2) if total > 0 else 0.0

    # Top errors
    top_errors = message_counts.most_common(20)
    top_error_summary = [{"message": msg, "count": cnt, "severity": "ERROR"} for msg, cnt in top_errors]

    return {
        "summary": dict(level_counts.most_common()),
        "total_lines": total,
        "error_lines": error_count,
        "error_rate": error_rate,
        "by_module": dict(module_counts.most_common(30)),
        "by_hour": dict(sorted(hour_buckets.items())),
        "top_errors": top_error_summary,
        "error_types": dict(error_type_counts.most_common(30)),
        "time_range": time_range,
        "exceptions": dict(exception_counts.most_common(10)),
        "http_status": dict(status_counts.most_common()),
        "http_errors": dict(http_paths.most_common(15)),
    }


def _normalize_error(msg: str) -> str:
    """Normalize error messages by replacing variable parts (IDs, numbers, paths)."""
    # Replace UUIDs
    norm = re.sub(
        r'\b[0-9a-f]{8}[-_][0-9a-f]{4}[-_][0-9a-f]{4}[-_][0-9a-f]{4}[-_][0-9a-f]{12}\b',
        '{uuid}', msg, flags=re.IGNORECASE
    )
    # Replace hex numbers
    norm = re.sub(r'\b0x[0-9a-fA-F]+\b', '{hex}', norm)
    # Replace numeric IDs (id=123, userId=456, etc)
    norm = re.sub(r'\b(id|userId|orderId|taskId)[=:]\s*\d+\b', r'\1={id}', norm, flags=re.IGNORECASE)
    # Replace file paths
    norm = re.sub(r'(?:/[^/\s]+)+', '{path}', norm)
    # Replace IP addresses
    norm = re.sub(r'\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b', '{ip}', norm)
    # Collapse multiple spaces
    norm = re.sub(r'\s+', ' ', norm).strip()
    return norm


def _empty_stats() -> Dict[str, Any]:
    return {
        "summary": {},
        "total_lines": 0,
        "error_lines": 0,
        "error_rate": 0.0,
        "by_module": {},
        "by_hour": {},
        "top_errors": [],
        "error_types": {},
        "time_range": {},
        "exceptions": {},
        "http_status": {},
        "http_errors": {},
    }


def compute_trend_analysis(stats: Dict[str, Any]) -> Dict[str, Any]:
    """
    Compute trend analysis from statistics based on hourly data distribution.
    
    Returns:
      - trend_type: "spike" | "increasing" | "decreasing" | "stable" | "unknown"
      - description: Human-readable description
      - peak_hours: Hours with maximum activity
      - low_hours: Hours with minimum activity
      - avg_count: Average hourly count
      - max_count: Maximum hourly count
      - min_count: Minimum hourly count
    """
    by_hour = stats.get("by_hour", {})
    if not by_hour:
        return {
            "trend_type": "unknown",
            "description": "暂无时间分布数据",
            "peak_hours": [],
            "low_hours": [],
            "avg_count": 0,
            "max_count": 0,
            "min_count": 0,
        }

    hours = sorted(by_hour.items())
    values = [v for _, v in hours]
    avg_value = sum(values) / len(values) if values else 0
    max_value = max(values) if values else 0
    min_value = min(values) if values else 0

    # Determine trend type based on thresholds
    trend_type = "stable"
    if max_value > avg_value * 2:
        trend_type = "spike"
    elif max_value > avg_value * 1.5:
        trend_type = "increasing"
    elif min_value < avg_value * 0.3:
        trend_type = "decreasing"

    peak_hours = [h for h, v in hours if v == max_value]
    low_hours = [h for h, v in hours if v == min_value]

    descriptions = {
        "stable": "日志量保持稳定，无明显波动",
        "spike": f"检测到异常峰值，峰值时间: {', '.join(peak_hours[:3])}",
        "increasing": "日志量呈上升趋势，需要关注",
        "decreasing": "日志量呈下降趋势，可能服务异常或流量减少",
        "unknown": "暂无时间分布数据",
    }

    return {
        "trend_type": trend_type,
        "description": descriptions.get(trend_type, "未知趋势"),
        "peak_hours": peak_hours,
        "low_hours": low_hours,
        "avg_count": round(avg_value, 2),
        "max_count": max_value,
        "min_count": min_value,
    }


def compute_severity_distribution(stats: Dict[str, Any]) -> Dict[str, Any]:
    """
    Compute severity distribution and health score from statistics.
    
    Returns:
      - critical: Count of FATAL + CRITICAL
      - high: Count of ERROR
      - medium: Count of WARN + WARNING
      - low: Count of INFO + DEBUG + TRACE
      - ratios: Percentage of each severity level
      - health_score: 0-100 health score
    """
    summary = stats.get("summary", {})
    total = stats.get("total_lines", 0)

    error_total = summary.get("ERROR", 0) + summary.get("FATAL", 0) + summary.get("CRITICAL", 0)
    warn_total = summary.get("WARN", 0) + summary.get("WARNING", 0)
    info_total = summary.get("INFO", 0)
    debug_total = summary.get("DEBUG", 0) + summary.get("TRACE", 0)

    return {
        "critical": summary.get("FATAL", 0) + summary.get("CRITICAL", 0),
        "high": summary.get("ERROR", 0),
        "medium": warn_total,
        "low": info_total + debug_total,
        "ratios": {
            "critical": round((summary.get("FATAL", 0) + summary.get("CRITICAL", 0)) / total * 100, 2) if total > 0 else 0,
            "high": round(summary.get("ERROR", 0) / total * 100, 2) if total > 0 else 0,
            "medium": round(warn_total / total * 100, 2) if total > 0 else 0,
            "low": round((info_total + debug_total) / total * 100, 2) if total > 0 else 0,
        },
        "health_score": _calculate_health_score(error_total, warn_total, total),
    }


def _calculate_health_score(error_count: int, warn_count: int, total: int) -> int:
    """
    Calculate a health score (0-100) based on error/warning rates.
    
    Scoring formula:
      - Base score: 100
      - Each error reduces score by 5 points (max -500)
      - Each warning reduces score by 1 point (max -100)
    """
    if total == 0:
        return 100

    error_rate = error_count / total
    warn_rate = warn_count / total

    score = 100
    score -= error_rate * 500  # Errors have 5x weight
    score -= warn_rate * 100

    return max(0, min(100, round(score)))
