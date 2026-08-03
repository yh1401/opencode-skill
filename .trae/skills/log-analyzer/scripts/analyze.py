"""
analyze.py - Main entry point for log-analyzer skill.
Handles:
- Stream parsing of large log files
- Orchestration of analysis pipeline
- Batch processing of multiple files
- Progress tracking and error handling
"""

import os
import sys
import time
import glob
import json
from datetime import datetime
from typing import Dict, Any, List, Optional, Iterator

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from scripts.parsers import (
    parse_line, detect_format, parse_json_log, is_stack_trace_line,
    parse_excel_file, parse_csv_file, parse_xml_file, parse_pdf_file, parse_json_file
)
from scripts.stats import compute_statistics, compute_trend_analysis, compute_severity_distribution
from scripts.root_cause import analyze_root_cause
from scripts.reporters import generate_report


class LogAnalyzer:
    """Main log analyzer orchestrator."""

    def __init__(self, chunk_size: int = 10000, max_lines: Optional[int] = None):
        self.chunk_size = chunk_size
        self.max_lines = max_lines
        self.total_lines_processed = 0
        self.parsed_records: List[Dict[str, Any]] = []
        self.format_counts: Dict[str, int] = {}

    def analyze_file(self, file_path: str, output_formats: Optional[list] = None) -> Dict[str, Any]:
        """
        Analyze a single log file.

        Args:
            file_path: Path to the log file
            output_formats: List of output formats for reports

        Returns:
            Complete analysis result
        """
        start_time = time.time()

        if not os.path.exists(file_path):
            raise FileNotFoundError(f"File not found: {file_path}")

        file_size = os.path.getsize(file_path)
        print(f"开始分析文件: {file_path}")
        print(f"文件大小: {self._format_size(file_size)}")

        self._parse_file_stream(file_path)

        stats = compute_statistics(self.parsed_records)
        trend = compute_trend_analysis(stats)
        severity = compute_severity_distribution(stats)
        root_cause = analyze_root_cause(self.parsed_records, stats)

        analysis_time = datetime.now().isoformat()
        elapsed = time.time() - start_time

        result = {
            "analysis_time": analysis_time,
            "file_path": file_path,
            "file_size": file_size,
            "file_size_formatted": self._format_size(file_size),
            "total_lines_processed": self.total_lines_processed,
            "parsed_records": len(self.parsed_records),
            "format_distribution": self.format_counts,
            "parse_rate": round(self.total_lines_processed / elapsed, 2) if elapsed > 0 else 0,
            "stats": stats,
            "trend": trend,
            "severity": severity,
            "root_cause": root_cause,
            "elapsed_time": round(elapsed, 2),
            "elapsed_time_formatted": self._format_time(elapsed),
        }

        print(f"\n分析完成!")
        print(f"处理行数: {self.total_lines_processed}")
        print(f"解析记录: {len(self.parsed_records)}")
        print(f"耗时: {self._format_time(elapsed)}")
        print(f"健康度评分: {severity.get('health_score', 0)}/100")

        if output_formats:
            reports = generate_report(result, output_formats)
            print(f"\n生成报告:")
            for fmt, path in reports.items():
                print(f"  - {fmt}: {path}")
            result["reports"] = reports

        return result

    def analyze_batch(self, pattern: str, output_formats: Optional[list] = None) -> List[Dict[str, Any]]:
        """
        Analyze multiple log files matching a glob pattern.

        Args:
            pattern: Glob pattern to match files
            output_formats: List of output formats for reports

        Returns:
            List of analysis results for each file
        """
        files = sorted(glob.glob(pattern))
        if not files:
            print(f"未找到匹配模式 '{pattern}' 的文件")
            return []

        print(f"找到 {len(files)} 个匹配文件")
        results = []

        for i, file_path in enumerate(files, 1):
            print(f"\n=== [{i}/{len(files)}] {file_path} ===")
            try:
                result = self.analyze_file(file_path, output_formats)
                results.append(result)
            except Exception as e:
                print(f"分析文件失败 {file_path}: {e}")
                results.append({
                    "file_path": file_path,
                    "error": str(e),
                })

        return results

    def _parse_file_stream(self, file_path: str) -> None:
        """Parse log file using streaming approach."""
        self.total_lines_processed = 0
        self.parsed_records = []
        self.format_counts = {}

        # Determine file type and use appropriate parser
        file_ext = os.path.splitext(file_path)[1].lower()

        try:
            if file_ext == '.xlsx':
                self._parse_excel(file_path)
            elif file_ext == '.csv':
                self._parse_csv(file_path)
            elif file_ext == '.xml':
                self._parse_xml(file_path)
            elif file_ext == '.pdf':
                self._parse_pdf(file_path)
            elif file_ext == '.json':
                self._parse_json_file(file_path)
            else:
                # Default: parse as text log file
                self._parse_text_log(file_path)
        except ImportError as e:
            print(f"缺少必要的库: {e}")
            print(f"请安装所需依赖: pip install openpyxl PyPDF2")
            raise
        except Exception as e:
            print(f"解析文件失败: {e}")
            raise

    def _parse_text_log(self, file_path: str) -> None:
        """Parse standard text log files."""
        current_record: Optional[Dict[str, Any]] = None
        stack_trace: List[str] = []

        with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
            for line_no, line in enumerate(f, 1):
                if self.max_lines and line_no > self.max_lines:
                    break

                self.total_lines_processed += 1
                if line_no % self.chunk_size == 0:
                    print(f"已处理 {line_no} 行...", end="\r")

                if is_stack_trace_line(line):
                    stack_trace.append(line.strip())
                    if current_record:
                        existing_stack = current_record.get("stack_trace", "")
                        current_record["stack_trace"] = existing_stack + "\n" + line.strip()
                    continue

                if stack_trace and current_record:
                    current_record["stack_trace"] = "\n".join(stack_trace)
                    stack_trace = []

                parsed = parse_json_log(line)
                if parsed:
                    parsed["_line_no"] = line_no
                    self.parsed_records.append(parsed)
                    self._count_format("json_log")
                    current_record = parsed
                    continue

                parsed = parse_line(line)
                if parsed:
                    parsed["_line_no"] = line_no
                    self.parsed_records.append(parsed)
                    fmt = parsed.get("_format", "unknown")
                    self._count_format(fmt)
                    current_record = parsed
                else:
                    current_record = None

        if stack_trace and current_record:
            current_record["stack_trace"] = "\n".join(stack_trace)

    def _parse_excel(self, file_path: str) -> None:
        """Parse Excel log files."""
        print("正在解析 Excel 文件...")
        for line_no, record in enumerate(parse_excel_file(file_path), 1):
            if self.max_lines and line_no > self.max_lines:
                break

            self.total_lines_processed += 1
            if line_no % self.chunk_size == 0:
                print(f"已处理 {line_no} 行...", end="\r")

            record["_line_no"] = line_no
            self.parsed_records.append(record)
            self._count_format("excel")

    def _parse_csv(self, file_path: str) -> None:
        """Parse CSV log files."""
        print("正在解析 CSV 文件...")
        for line_no, record in enumerate(parse_csv_file(file_path), 1):
            if self.max_lines and line_no > self.max_lines:
                break

            self.total_lines_processed += 1
            if line_no % self.chunk_size == 0:
                print(f"已处理 {line_no} 行...", end="\r")

            record["_line_no"] = line_no
            self.parsed_records.append(record)
            self._count_format("csv")

    def _parse_xml(self, file_path: str) -> None:
        """Parse XML log files."""
        print("正在解析 XML 文件...")
        for line_no, record in enumerate(parse_xml_file(file_path), 1):
            if self.max_lines and line_no > self.max_lines:
                break

            self.total_lines_processed += 1
            if line_no % self.chunk_size == 0:
                print(f"已处理 {line_no} 行...", end="\r")

            record["_line_no"] = line_no
            self.parsed_records.append(record)
            self._count_format("xml")

    def _parse_pdf(self, file_path: str) -> None:
        """Parse PDF log files."""
        print("正在解析 PDF 文件...")
        for line_no, record in enumerate(parse_pdf_file(file_path), 1):
            if self.max_lines and line_no > self.max_lines:
                break

            self.total_lines_processed += 1
            if line_no % self.chunk_size == 0:
                print(f"已处理 {line_no} 行...", end="\r")

            record["_line_no"] = line_no
            self.parsed_records.append(record)
            self._count_format("pdf")

    def _parse_json_file(self, file_path: str) -> None:
        """Parse JSON log files."""
        print("正在解析 JSON 文件...")
        for line_no, record in enumerate(parse_json_file(file_path), 1):
            if self.max_lines and line_no > self.max_lines:
                break

            self.total_lines_processed += 1
            if line_no % self.chunk_size == 0:
                print(f"已处理 {line_no} 行...", end="\r")

            record["_line_no"] = line_no
            self.parsed_records.append(record)
            self._count_format("json_file")

    def _count_format(self, fmt: str) -> None:
        """Count format occurrences."""
        self.format_counts[fmt] = self.format_counts.get(fmt, 0) + 1

    @staticmethod
    def _format_size(bytes_: int) -> str:
        """Format file size as human-readable string."""
        if bytes_ < 1024:
            return f"{bytes_} B"
        elif bytes_ < 1024 ** 2:
            return f"{bytes_ / 1024:.2f} KB"
        elif bytes_ < 1024 ** 3:
            return f"{bytes_ / 1024 ** 2:.2f} MB"
        else:
            return f"{bytes_ / 1024 ** 3:.2f} GB"

    @staticmethod
    def _format_time(seconds: float) -> str:
        """Format time as human-readable string."""
        if seconds < 1:
            return f"{int(seconds * 1000)} ms"
        elif seconds < 60:
            return f"{seconds:.2f} 秒"
        elif seconds < 3600:
            minutes = int(seconds // 60)
            secs = int(seconds % 60)
            return f"{minutes} 分 {secs} 秒"
        else:
            hours = int(seconds // 3600)
            minutes = int((seconds % 3600) // 60)
            return f"{hours} 小时 {minutes} 分"


def main():
    """Command-line interface for log-analyzer."""
    import argparse

    parser = argparse.ArgumentParser(description="日志分析工具")
    parser.add_argument("file", help="日志文件路径或glob模式")
    parser.add_argument(
        "--output", "-o",
        nargs="+",
        choices=["markdown", "json", "html", "word", "pdf"],
        default=["markdown", "json"],
        help="输出报告格式"
    )
    parser.add_argument("--chunk-size", type=int, default=10000, help="分块大小")
    parser.add_argument("--max-lines", type=int, default=None, help="最大处理行数")
    parser.add_argument("--batch", action="store_true", help="批量模式（处理匹配的多个文件）")
    parser.add_argument("--json-output", action="store_true", help="输出JSON格式结果")

    args = parser.parse_args()

    analyzer = LogAnalyzer(chunk_size=args.chunk_size, max_lines=args.max_lines)

    try:
        if args.batch or "*" in args.file or "?" in args.file:
            results = analyzer.analyze_batch(args.file, args.output)
        else:
            results = [analyzer.analyze_file(args.file, args.output)]

        if args.json_output:
            print(json.dumps(results, ensure_ascii=False, indent=2))

        print("\n分析完成!")
    except Exception as e:
        print(f"分析失败: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()