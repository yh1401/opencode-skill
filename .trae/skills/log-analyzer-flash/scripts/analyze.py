#!/usr/bin/env python3
"""
analyze.py - Log-analyzer main entry point.
Streams through log files, parses records, computes stats,
infers root causes, and generates multi-format reports.

Usage:
  python3 scripts/analyze.py <log-file-path> [--format markdown|json|html] [--output-dir ./] [--verbose]
"""

import os
import sys
import time
import glob
import argparse
from typing import Optional, Dict, Any
from datetime import datetime

try:
    from parsers import parse_line, detect_format, parse_json_log, is_stack_trace_line
    from parsers import parse_excel_file, parse_csv_file, parse_xml_file, parse_pdf_file, parse_json_file
    from stats import compute_statistics
    from root_cause import infer_root_causes
    from reporters import generate_markdown, generate_json_report, generate_html_report
except ImportError:
    # When running from scripts/ directory
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from parsers import parse_line, detect_format, parse_json_log, is_stack_trace_line
    from parsers import parse_excel_file, parse_csv_file, parse_xml_file, parse_pdf_file, parse_json_file
    from stats import compute_statistics
    from root_cause import infer_root_causes
    from reporters import generate_markdown, generate_json_report, generate_html_report


STREAM_THRESHOLD = 100 * 1024 * 1024  # 100 MB
CHUNK_SIZE = 64 * 1024  # 64 KB chunks


def stream_parse(file_path: str, verbose: bool = False) -> list:
    """
    Stream-parse a log file. For large files (>100MB), uses chunked streaming
    to avoid loading the entire file into memory at once.
    Returns a list of parsed record dicts.
    """
    file_size = os.path.getsize(file_path)
    use_stream = file_size > STREAM_THRESHOLD

    if verbose:
        print(f"📄 文件: {file_path} ({_fmt_size(file_size)})", file=sys.stderr)
        print(f"🚰 流式模式: {'启用' if use_stream else '关闭(全量加载)'}", file=sys.stderr)

    records = []
    line_no = 0
    start = time.time()

    # Determine file type and use appropriate parser
    file_ext = os.path.splitext(file_path)[1].lower()

    try:
        if file_ext == '.xlsx':
            # Excel file parsing
            if verbose:
                print(f"📊 解析 Excel 文件...", file=sys.stderr)
            for line_no, rec in enumerate(parse_excel_file(file_path), 1):
                if rec:
                    rec["_line_no"] = line_no
                    records.append(rec)
                if verbose and line_no % 10000 == 0:
                    elapsed = time.time() - start
                    print(f"  已处理 {line_no:,} 行 ({elapsed:.1f}s)", file=sys.stderr)

        elif file_ext == '.csv':
            # CSV file parsing
            if verbose:
                print(f"📊 解析 CSV 文件...", file=sys.stderr)
            for line_no, rec in enumerate(parse_csv_file(file_path), 1):
                if rec:
                    rec["_line_no"] = line_no
                    records.append(rec)
                if verbose and line_no % 10000 == 0:
                    elapsed = time.time() - start
                    print(f"  已处理 {line_no:,} 行 ({elapsed:.1f}s)", file=sys.stderr)

        elif file_ext == '.xml':
            # XML file parsing
            if verbose:
                print(f"📊 解析 XML 文件...", file=sys.stderr)
            for line_no, rec in enumerate(parse_xml_file(file_path), 1):
                if rec:
                    rec["_line_no"] = line_no
                    records.append(rec)
                if verbose and line_no % 10000 == 0:
                    elapsed = time.time() - start
                    print(f"  已处理 {line_no:,} 行 ({elapsed:.1f}s)", file=sys.stderr)

        elif file_ext == '.pdf':
            # PDF file parsing
            if verbose:
                print(f"📊 解析 PDF 文件...", file=sys.stderr)
            for line_no, rec in enumerate(parse_pdf_file(file_path), 1):
                if rec:
                    rec["_line_no"] = line_no
                    records.append(rec)
                if verbose and line_no % 10000 == 0:
                    elapsed = time.time() - start
                    print(f"  已处理 {line_no:,} 行 ({elapsed:.1f}s)", file=sys.stderr)

        elif file_ext == '.json':
            # JSON file parsing
            if verbose:
                print(f"📊 解析 JSON 文件...", file=sys.stderr)
            for line_no, rec in enumerate(parse_json_file(file_path), 1):
                if rec:
                    rec["_line_no"] = line_no
                    records.append(rec)
                if verbose and line_no % 10000 == 0:
                    elapsed = time.time() - start
                    print(f"  已处理 {line_no:,} 行 ({elapsed:.1f}s)", file=sys.stderr)

        else:
            # Default: parse as text log file
            if use_stream:
                # Streaming mode: process chunk by chunk
                with open(file_path, "r", errors="replace") as f:
                    buffer = ""
                    while True:
                        chunk = f.read(CHUNK_SIZE)
                        if not chunk:
                            break
                        buffer += chunk
                        lines = buffer.split("\n")
                        buffer = lines.pop()  # Keep incomplete last line in buffer

                        for line in lines:
                            if not line.strip():
                                line_no += 1
                                continue
                            line_no += 1
                            rec = parse_line(line)
                            if rec:
                                rec["_line_no"] = line_no
                                records.append(rec)

                # Process remaining buffer
                if buffer.strip():
                    line_no += 1
                    if verbose and line_no % 100000 == 0:
                        elapsed = time.time() - start
                        print(f"  已处理 {line_no:,} 行 ({elapsed:.1f}s)", file=sys.stderr)
                    rec = parse_line(buffer)
                    if rec:
                        rec["_line_no"] = line_no
                        records.append(rec)
            else:
                # Small file mode: load all lines at once
                with open(file_path, "r", errors="replace") as f:
                    lines = f.readlines()
                total = len(lines)
                for i, line in enumerate(lines):
                    line_no = i + 1
                    if not line.strip():
                        continue
                    rec = parse_line(line)
                    if rec:
                        rec["_line_no"] = line_no
                        records.append(rec)

                    if verbose and line_no % 500000 == 0 and line_no > 0:
                        elapsed = time.time() - start
                        pct = min(100, line_no / total * 100)
                        print(f"  进度: {pct:.0f}% ({line_no:,}/{total:,}, {elapsed:.1f}s)", file=sys.stderr)

    except ImportError as e:
        print(f"❌ 缺少必要的库: {e}", file=sys.stderr)
        print(f"💡 请安装所需依赖: pip install openpyxl PyPDF2", file=sys.stderr)
        return []
    except Exception as e:
        print(f"❌ 解析文件失败: {e}", file=sys.stderr)
        return []

    elapsed = time.time() - start
    if verbose:
        total_lines = line_no
        parsed = len(records)
        print(f"✅ 解析完成: {total_lines:,} 行 → {parsed} 条结构化记录 ({elapsed:.2f}s)", file=sys.stderr)

    return records


def _fmt_size(size_bytes: int) -> str:
    for unit in ("B", "KB", "MB", "GB"):
        if size_bytes < 1024:
            return f"{size_bytes:.1f} {unit}"
        size_bytes /= 1024
    return f"{size_bytes:.1f} TB"


def find_log_files(path: str) -> list:
    """Find log files by path. Supports single files, directories, and glob patterns."""
    if os.path.isfile(path):
        return [path]
    if os.path.isdir(path):
        # Find all .log, .txt, .out, .err files in directory
        extensions = ("*.log", "*.txt", "*.out", "*.err", "*.output", "*.syslog")
        files = []
        for ext in extensions:
            files.extend(glob.glob(os.path.join(path, ext)))
            files.extend(glob.glob(os.path.join(path, "**", ext), recursive=True))
        return sorted(set(files))
    # Treat as glob pattern
    files = glob.glob(path, recursive=True)
    return sorted(files)


def analyze(file_path: str, output_format: str = "markdown",
            output_dir: Optional[str] = None, verbose: bool = False) -> Dict[str, Any]:
    """
    Full analysis pipeline: parse → stats → root cause → report.
    Returns the report metadata dict.
    """
    t0 = time.time()
    # Phase 1: Parse
    records = stream_parse(file_path, verbose=verbose)

    if not records:
        print("⚠️  未解析到任何结构化日志记录。文件可能为空或格式不兼容。", file=sys.stderr)
        return {"status": "empty", "file": file_path}

    # Phase 2: Statistics
    stats = compute_statistics(records)
    t1 = time.time()

    # Phase 3: Root cause inference
    root_cause = infer_root_causes(records, stats)
    t2 = time.time()

    # Phase 4: Generate report
    duration = time.time() - t0
    report = {}

    if output_format == "json":
        report["_format"] = "json"
        content = generate_json_report(stats, root_cause, file_path, duration)
    elif output_format == "html":
        report["_format"] = "html"
        content = generate_html_report(stats, root_cause, file_path, duration)
    else:
        report["_format"] = "markdown"
        content = generate_markdown(stats, root_cause, file_path, duration)

    # Output
    if output_dir:
        basename = os.path.splitext(os.path.basename(file_path))[0]
        ext_map = {"markdown": "md", "json": "json", "html": "html"}
        ext = ext_map.get(output_format, "md")
        out_path = os.path.join(output_dir, f"{basename}_report.{ext}")
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(content)
        print(f"📝 报告已保存: {out_path}", file=sys.stderr)
        report["output_path"] = out_path
    else:
        print(content)

    if verbose:
        print(f"\n⚡ 性能: 解析 {t1-t0:.2f}s → 统计 {t2-t1:.2f}s → 报告 {time.time()-t2:.2f}s", file=sys.stderr)

    report.update({
        "status": "ok",
        "file": file_path,
        "total_lines": stats.get("total_lines", 0),
        "error_lines": stats.get("error_lines", 0),
        "error_rate": stats.get("error_rate", 0.0),
        "formatted_causes": root_cause.get("recommendation", ""),
        "duration_sec": round(duration, 2),
    })
    return report


def main():
    parser = argparse.ArgumentParser(
        description="日志深度分析工具 - 流式解析、错误统计、根因推断及多格式报告生成",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python3 scripts/analyze.py app.log
  python3 scripts/analyze.py app.log --format json --output-dir ./reports
  python3 scripts/analyze.py /var/log/nginx/access.log --format html --verbose
  python3 scripts/analyze.py ./logs/ --format markdown --output-dir ./reports
        """,
    )
    parser.add_argument("path", help="日志文件路径、目录或 glob 通配符")
    parser.add_argument("--format", choices=["markdown", "json", "html"], default="markdown",
                        help="输出报告格式（默认 markdown）")
    parser.add_argument("--output-dir", "-o", help="报告输出目录（不指定则打印到终端）")
    parser.add_argument("--verbose", "-v", action="store_true", help="显示详细信息")

    args = parser.parse_args()

    files = find_log_files(args.path)
    if not files:
        print(f"❌ 未找到日志文件: {args.path}", file=sys.stderr)
        sys.exit(1)

    if args.output_dir:
        os.makedirs(args.output_dir, exist_ok=True)

    reports = []
    for fpath in files:
        if args.verbose:
            print(f"\n{'='*60}", file=sys.stderr)
            print(f"分析: {fpath}", file=sys.stderr)
            print(f"{'='*60}", file=sys.stderr)
        report = analyze(fpath, args.format, args.output_dir, args.verbose)
        reports.append(report)

    # Summary across multiple files
    if len(files) > 1:
        total_err = sum(r.get("error_lines", 0) for r in reports)
        total_lines = sum(r.get("total_lines", 0) for r in reports)
        total_time = sum(r.get("duration_sec", 0) for r in reports)
        print(f"\n📊 批量分析完成: {len(files)} 个文件, "
              f"共 {total_lines:,} 行, {total_err:,} 条错误, "
              f"耗时 {total_time:.2f}s", file=sys.stderr)


if __name__ == "__main__":
    main()
