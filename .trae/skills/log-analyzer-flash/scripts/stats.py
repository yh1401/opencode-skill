"""
stats.py - Error statistics engine for log-analyzer.
Aggregates parsed log records into structured statistics.
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
      - top_errors: most frequent error messages
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
    hour_buckets: Counter[str] = Counter()
    exception_counts: Counter[str] = Counter()
    status_counts: Counter[int] = Counter()
    http_paths: Counter[str] = Counter()

    timestamps: List[str] = []
    error_lines: List[str] = []

    for rec in records:
        level = rec.get("level", "UNKNOWN")
        level_counts[level] += 1

        msg = rec.get("message", "")
        if level in ("ERROR", "FATAL", "CRITICAL"):
            # Normalize error messages for grouping (strip variable parts)
            normalized = _normalize_error(msg)
            message_counts[normalized] += 1
            error_lines.append(msg)

        mod = rec.get("module", "")
        if mod and level in ("ERROR", "FATAL", "WARN", "WARNING", "CRITICAL"):
            module_counts[mod] += 1

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
    # Replace numeric IDs
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
        "time_range": {},
        "exceptions": {},
        "http_status": {},
        "http_errors": {},
    }
