"""
reporters.py - Multi-format report generators for log-analyzer.
Supports Markdown, JSON, and HTML report generation.
"""

import json
import html as html_mod
from datetime import datetime
from typing import Dict, Any, Optional, List


def generate_markdown(stats: Dict[str, Any], root_cause: Dict[str, Any],
                      file_path: str, duration_sec: float) -> str:
    """Generate a Markdown analysis report."""
    lines = []
    lines.append("# 📋 日志分析报告")
    lines.append("")
    lines.append(f"- **源文件**: `{file_path}`")
    lines.append(f"- **分析时间**: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append(f"- **耗时**: {duration_sec:.2f}s")
    lines.append("")

    total = stats.get("total_lines", 0)
    errors = stats.get("error_lines", 0)
    rate = stats.get("error_rate", 0)
    tr = stats.get("time_range", {})

    lines.append("## 1. 概览")
    lines.append("")
    lines.append(f"| 指标 | 值 |")
    lines.append(f"|------|-----|")
    lines.append(f"| 总行数 | {total:,} |")
    lines.append(f"| 错误行数 | {errors:,} |")
    lines.append(f"| 错误率 | {rate}% |")
    if tr.get("first"):
        lines.append(f"| 时间范围 | {tr.get('first', '')} ~ {tr.get('last', '')} |")
    lines.append("")

    level_icons = {"FATAL": "🔥", "ERROR": "❌", "WARN": "⚠️", "WARNING": "⚠️",
                   "INFO": "ℹ️", "DEBUG": "🔍", "TRACE": "🔬"}
    summary = stats.get("summary", {})
    lines.append("## 2. 日志级别分布")
    lines.append("")
    lines.append(f"| 级别 | 数量 | 占比 |")
    lines.append(f"|------|------|------|")
    for level in ["FATAL", "ERROR", "WARN", "WARNING", "INFO", "DEBUG", "TRACE"]:
        count = summary.get(level, 0)
        if count > 0:
            icon = level_icons.get(level, "")
            pct = f"{count / total * 100:.1f}%" if total > 0 else ""
            lines.append(f"| {icon} {level} | {count:,} | {pct} |")
    lines.append("")

    top_errors = stats.get("top_errors", [])
    if top_errors:
        n = min(10, len(top_errors))
        lines.append(f"## 3. 高频错误 Top {n}")
        lines.append("")
        lines.append(f"| # | 错误签名 | 出现次数 |")
        lines.append(f"|---|---------|---------:|")
        for i, err in enumerate(top_errors[:10], 1):
            msg = err.get("message", "")
            cnt = err.get("count", 0)
            lines.append(f"| {i} | `{_truncate(msg, 80)}` | {cnt} |")
        lines.append("")

    causes = root_cause.get("causes", [])
    if causes:
        lines.append("## 4. 根因分析")
        lines.append("")
        for cause in causes:
            severity = cause.get("severity", "unknown")
            icon = {"critical": "🔥", "high": "⚠️", "medium": "📌", "low": "💡"}.get(severity, "📌")
            lines.append(f"### {icon} {cause.get('name', 'Unknown')}")
            lines.append("")
            lines.append(f"- **类别**: {cause.get('category', '')}")
            lines.append(f"- **严重度**: {severity}")
            lines.append(f"- **出现次数**: {cause.get('count', 0)}")
            lines.append(f"- **建议**: {cause.get('suggestion', '')}")
            lines.append("")

    by_module = stats.get("by_module", {})
    if by_module:
        lines.append("## 5. 模块分布 (错误相关)")
        lines.append("")
        lines.append(f"| 模块 | 错误数 |")
        lines.append(f"|------|-------:|")
        for module, count in list(by_module.items())[:20]:
            lines.append(f"| `{module}` | {count} |")
        lines.append("")

    by_hour = stats.get("by_hour", {})
    if by_hour:
        lines.append("## 6. 时间线趋势 (按小时)")
        lines.append("")
        max_count = max(by_hour.values()) if by_hour else 1
        sorted_hours = sorted(by_hour.items())
        for hour, count in sorted_hours[-30:]:
            bar_len = max(1, int(count / max_count * 20))
            bar = "█" * bar_len
            lines.append(f"| {hour} | {bar} {count} |")
        lines.append("")

    rec = root_cause.get("recommendation", "")
    if rec:
        lines.append("## 7. 综合建议")
        lines.append("")
        lines.append(f"> {rec}")
        lines.append("")

    return "\n".join(lines)


def generate_json_report(stats: Dict[str, Any], root_cause: Dict[str, Any],
                         file_path: str, duration_sec: float) -> str:
    """Generate a JSON structured report."""
    report = {
        "report_meta": {
            "tool": "log-analyzer",
            "source_file": file_path,
            "generated_at": datetime.now().isoformat(),
            "duration_sec": round(duration_sec, 2),
        },
        "overview": {
            "total_lines": stats.get("total_lines"),
            "error_lines": stats.get("error_lines"),
            "error_rate": stats.get("error_rate"),
            "time_range": stats.get("time_range", {}),
        },
        "level_distribution": stats.get("summary", {}),
        "top_errors": stats.get("top_errors", [])[:20],
        "by_module": stats.get("by_module", {}),
        "by_hour": stats.get("by_hour", {}),
        "root_causes": [
            {
                "id": c.get("id"),
                "name": c.get("name"),
                "severity": c.get("severity"),
                "category": c.get("category"),
                "count": c.get("count"),
                "suggestion": c.get("suggestion"),
            }
            for c in root_cause.get("causes", [])
        ],
        "recommendation": root_cause.get("recommendation", ""),
    }
    return json.dumps(report, ensure_ascii=False, indent=2)


def generate_html_report(stats: Dict[str, Any], root_cause: Dict[str, Any],
                         file_path: str, duration_sec: float) -> str:
    """Generate an HTML report with visual formatting."""
    parts = []
    summary = stats.get("summary", {})
    total = stats.get("total_lines", 0)
    errors = stats.get("error_lines", 0)
    rate = stats.get("error_rate", 0)
    tr = stats.get("time_range", {})
    causes = root_cause.get("causes", [])
    top_errors = stats.get("top_errors", [])[:10]
    by_module = stats.get("by_module", {})
    by_hour = stats.get("by_hour", {})

    severity_colors = {"critical": "#dc3545", "high": "#fd7e14",
                       "medium": "#ffc107", "low": "#17a2b8"}
    level_icons = {"FATAL": "🔥", "ERROR": "❌", "WARN": "⚠️", "WARNING": "⚠️",
                   "INFO": "ℹ️", "DEBUG": "🔍", "TRACE": "🔬"}

    def esc(s):
        return html_mod.escape(str(s))

    parts.append("""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>日志分析报告</title>
<style>
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 960px; margin: 0 auto; padding: 20px; background: #f8f9fa; color: #333; }
h1 { color: #1a1a2e; border-bottom: 3px solid #0d6efd; padding-bottom: 10px; }
h2 { color: #1a1a2e; margin-top: 30px; }
table { width: 100%; border-collapse: collapse; margin: 10px 0; }
th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid #dee2e6; }
th { background: #0d6efd; color: white; }
tr:hover { background: #f1f3f5; }
.card { background: white; border-radius: 8px; padding: 20px; margin: 15px 0; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
.stat-row { display: flex; gap: 16px; flex-wrap: wrap; }
.stat-box { flex: 1; min-width: 120px; text-align: center; padding: 16px; background: white; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
.stat-value { font-size: 28px; font-weight: bold; color: #0d6efd; }
.stat-label { font-size: 13px; color: #6c757d; margin-top: 4px; }
.cause-box { padding: 12px 16px; margin: 10px 0; background: white; border-radius: 4px; box-shadow: 0 1px 3px rgba(0,0,0,0.08); }
.rec-box { background: #e8f4f8; border-radius: 8px; padding: 16px; margin: 10px 0; border-left: 4px solid #17a2b8; }
.footer { text-align: center; color: #6c757d; font-size: 12px; margin-top: 30px; }
</style>
</head>
<body>""")

    parts.append(f"<h1>📋 日志分析报告</h1>")
    parts.append(f"<p><strong>源文件:</strong> <code>{esc(file_path)}</code> &nbsp;|&nbsp;"
                 f" <strong>分析时间:</strong> {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}"
                 f" &nbsp;|&nbsp; <strong>耗时:</strong> {duration_sec:.2f}s</p>")

    err_color = "#dc3545" if errors > 0 else "#198754"
    rate_color = "#dc3545" if rate > 20 else "#fd7e14" if rate > 5 else "#198754"
    first_ts = tr.get("first", "")
    parts.append(f"""<div class="stat-row">
  <div class="stat-box"><div class="stat-value">{total:,}</div><div class="stat-label">总行数</div></div>
  <div class="stat-box"><div class="stat-value" style="color:{err_color}">{errors:,}</div><div class="stat-label">错误行数</div></div>
  <div class="stat-box"><div class="stat-value" style="color:{rate_color}">{rate}%</div><div class="stat-label">错误率</div></div>""")
    if first_ts:
        parts.append(f"""  <div class="stat-box"><div class="stat-value" style="font-size:14px">{esc(first_ts)}</div><div class="stat-label">起始时间</div></div>""")
    parts.append("</div>")

    # Level distribution
    parts.append("""<div class="card"><h2>📊 日志级别分布</h2><table>
<tr><th>级别</th><th>数量</th><th>占比</th></tr>""")
    for level in ["FATAL", "ERROR", "WARN", "WARNING", "INFO", "DEBUG", "TRACE"]:
        cnt = summary.get(level, 0)
        if cnt > 0:
            icon = level_icons.get(level, level)
            pct = f"{cnt / total * 100:.1f}%" if total > 0 else "0%"
            parts.append(f"<tr><td>{icon} {level}</td><td>{cnt:,}</td><td>{pct}</td></tr>")
    parts.append("</table></div>")

    # Top errors
    if top_errors:
        parts.append(f"""<div class="card"><h2>🔥 高频错误 Top {min(10, len(top_errors))}</h2><table>
<tr><th>#</th><th>错误签名</th><th>次数</th></tr>""")
        for i, e in enumerate(top_errors[:10], 1):
            msg = esc(e.get("message", ""))[:80]
            cnt = e.get("count", 0)
            parts.append(f"<tr><td>{i}</td><td><code>{msg}</code></td><td>{cnt}</td></tr>")
        parts.append("</table></div>")

    # Root causes
    if causes:
        parts.append("""<div class="card"><h2>🔍 根因分析</h2>""")
        for c in causes[:5]:
            sev = c.get("severity", "medium")
            border_color = severity_colors.get(sev, "#6c757d")
            icon = "🔥" if sev == "critical" else "⚠️" if sev == "high" else "📌"
            name = esc(c.get("name", ""))
            cat = esc(c.get("category", ""))
            cnt = c.get("count", 0)
            sug = esc(c.get("suggestion", ""))
            parts.append(f"""<div class="cause-box" style="border-left: 4px solid {border_color};">
  <h3 style="margin:0 0 4px">{icon} {name}</h3>
  <p style="margin:4px 0;color:#666;font-size:14px">
    类别: {cat} &nbsp;|&nbsp; 严重度: <strong style="color:{border_color}">{sev}</strong> &nbsp;|&nbsp; 出现 {cnt} 次
  </p>
  <p style="margin:4px 0;padding:8px;background:#f8f9fa;border-radius:4px;font-size:14px">{sug}</p>
</div>""")
        parts.append("</div>")

    # Hourly trend
    if by_hour:
        parts.append("""<div class="card"><h2>⏱ 时间线趋势</h2><table>
<tr><th>时间</th><th>分布</th></tr>""")
        max_c = max(by_hour.values())
        for hour, cnt in list(sorted(by_hour.items()))[-30:]:
            pct = int(cnt / max_c * 100) if max_c > 0 else 0
            parts.append(f"""<tr><td>{esc(hour)}</td><td><div style="background:#e9ecef;border-radius:4px;overflow:hidden">
<div style="width:{pct}%;background:#0d6efd;color:white;padding:2px 6px;font-size:12px">{cnt}</div></div></td></tr>""")
        parts.append("</table></div>")

    # Module breakdown
    if by_module:
        parts.append("""<div class="card"><h2>📦 模块分布</h2><table>
<tr><th>模块</th><th>错误数</th></tr>""")
        for mod, cnt in list(by_module.items())[:20]:
            parts.append(f"<tr><td><code>{esc(mod)}</code></td><td>{cnt}</td></tr>")
        parts.append("</table></div>")

    # Recommendation
    rec = root_cause.get("recommendation", "")
    if rec:
        parts.append(f"""<div class="rec-box"><h2>💡 综合建议</h2>
<p style="font-size:15px;line-height:1.6">{esc(rec)}</p></div>""")

    parts.append("""<div class="footer">Generated by <strong>log-analyzer</strong> · 数据仅供分析参考</div>
</body></html>""")

    return "\n".join(parts)


def _truncate(text: str, max_len: int) -> str:
    return text[:max_len] + "..." if len(text) > max_len else text
