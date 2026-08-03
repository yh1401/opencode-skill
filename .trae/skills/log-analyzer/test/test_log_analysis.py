"""
test_log_analysis.py - Comprehensive test suite for log-analyzer skill.
Contains:
- Unit tests for parsers, stats, root_cause, reporters
- Integration tests for the complete analysis pipeline
- Performance tests for large file processing
"""

import os
import sys
import tempfile
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from scripts.parsers import parse_line, detect_format, parse_json_log, is_stack_trace_line
from scripts.stats import compute_statistics, compute_trend_analysis, compute_severity_distribution
from scripts.root_cause import analyze_root_cause, RootCauseEngine
from scripts.reporters import generate_report
from scripts.analyze import LogAnalyzer


class TestParsers(unittest.TestCase):
    """Unit tests for parsers.py"""

    def test_parse_java_app_format(self):
        line = "2026-06-14 10:30:45.123 [thread-01] ERROR a1b2c3d4 ClassName - NullPointerException: null object"
        result = parse_line(line)
        self.assertIsNotNone(result)
        self.assertEqual(result["level"], "ERROR")
        self.assertEqual(result["thread"], "thread-01")
        self.assertEqual(result["_format"], "java_app")
        self.assertEqual(result["error_type"], "NullPointerException")

    def test_parse_iso_format(self):
        line = "[2024-01-01 12:00:00,123] ERROR [com.example.Service] Something broke"
        result = parse_line(line)
        self.assertIsNotNone(result)
        self.assertEqual(result["level"], "ERROR")
        self.assertEqual(result["module"], "com.example.Service")
        self.assertEqual(result["_format"], "iso")

    def test_parse_syslog_format(self):
        line = "Jan 15 10:30:45 hostname java[12345]: ERROR: null pointer"
        result = parse_line(line)
        self.assertIsNotNone(result)
        self.assertEqual(result["level"], "ERROR")
        self.assertEqual(result["_format"], "syslog")

    def test_parse_ncsa_format(self):
        line = '192.168.1.1 - - [10/Jan/2024:13:55:36 +0000] "GET /api HTTP/1.1" 200 1234'
        result = parse_line(line)
        self.assertIsNotNone(result)
        self.assertEqual(result["http_status"], 200)
        self.assertEqual(result["http_method"], "GET")
        self.assertEqual(result["_format"], "ncsa")

    def test_parse_json_format(self):
        line = '{"level": "ERROR", "timestamp": "2024-01-01T12:00:00Z", "message": "test error"}'
        result = parse_json_log(line)
        self.assertIsNotNone(result)
        self.assertEqual(result["level"], "ERROR")
        self.assertEqual(result["_format"], "json_log")

    def test_detect_format(self):
        self.assertEqual(detect_format("2026-06-14 10:30:45 [thread] ERROR msg"), "java_app")
        self.assertEqual(detect_format("[2024-01-01 12:00:00] ERROR msg"), "iso")
        self.assertEqual(detect_format("Jan 15 10:30:45 host java: ERROR"), "syslog")
        self.assertEqual(detect_format('192.168.1.1 - - [10/Jan/2024:13:55:36 +0000] "GET / HTTP/1.1" 200 123'), "ncsa")

    def test_is_stack_trace_line(self):
        self.assertTrue(is_stack_trace_line("    at com.example.Class.method(Class.java:42)"))
        self.assertTrue(is_stack_trace_line("... 5 more"))
        self.assertFalse(is_stack_trace_line("2024-01-01 ERROR test"))

    def test_parse_empty_line(self):
        self.assertIsNone(parse_line(""))
        self.assertIsNone(parse_line("   "))


class TestStats(unittest.TestCase):
    """Unit tests for stats.py"""

    def test_compute_statistics_empty(self):
        result = compute_statistics([])
        self.assertEqual(result["total_lines"], 0)
        self.assertEqual(result["error_rate"], 0.0)

    def test_compute_statistics_basic(self):
        records = [
            {"level": "ERROR", "message": "test error", "module": "com.example", "timestamp": "2024-01-01T12:00:00"},
            {"level": "INFO", "message": "test info", "module": "com.example", "timestamp": "2024-01-01T12:00:01"},
            {"level": "WARN", "message": "test warn", "timestamp": "2024-01-01T12:00:02"},
        ]
        result = compute_statistics(records)
        self.assertEqual(result["total_lines"], 3)
        self.assertEqual(result["summary"]["ERROR"], 1)
        self.assertEqual(result["summary"]["INFO"], 1)
        self.assertEqual(result["summary"]["WARN"], 1)
        self.assertAlmostEqual(result["error_rate"], 33.33, places=2)

    def test_error_normalization(self):
        from scripts.stats import _normalize_error
        msg1 = "User 12345 not found with id 0xabc123"
        msg2 = "User 67890 not found with id 0xdef456"
        norm1 = _normalize_error(msg1)
        norm2 = _normalize_error(msg2)
        self.assertEqual(norm1, norm2)

    def test_trend_analysis(self):
        stats = {"by_hour": {"2024-01-01 08:00": 10, "2024-01-01 09:00": 50, "2024-01-01 10:00": 10}}
        result = compute_trend_analysis(stats)
        self.assertEqual(result["trend_type"], "spike")

    def test_health_score(self):
        stats = {"summary": {"ERROR": 0, "WARN": 0, "INFO": 100}, "total_lines": 100}
        result = compute_severity_distribution(stats)
        self.assertEqual(result["health_score"], 100)

        stats = {"summary": {"ERROR": 10, "WARN": 10, "INFO": 80}, "total_lines": 100}
        result = compute_severity_distribution(stats)
        self.assertTrue(0 <= result["health_score"] <= 100)


class TestRootCause(unittest.TestCase):
    """Unit tests for root_cause.py"""

    def test_analyze_empty(self):
        result = analyze_root_cause([], {})
        self.assertEqual(result["total_errors_analyzed"], 0)
        self.assertEqual(result["summary"]["overall_severity"], "healthy")

    def test_analyze_database_error(self):
        records = [
            {"level": "ERROR", "message": "Cannot connect to database: Connection refused"},
        ]
        stats = {}
        result = analyze_root_cause(records, stats)
        self.assertGreater(len(result["findings"]["critical"]), 0)

    def test_analyze_memory_error(self):
        records = [
            {"level": "ERROR", "message": "java.lang.OutOfMemoryError: Java heap space"},
        ]
        stats = {}
        result = analyze_root_cause(records, stats)
        self.assertGreater(len(result["findings"]["critical"]), 0)

    def test_add_custom_pattern(self):
        engine = RootCauseEngine()
        original_count = len(engine.list_patterns())
        engine.add_custom_pattern({
            "id": "test_pattern",
            "name": "Test Pattern",
            "severity": "medium",
            "patterns": [r"TestException.*"],
            "cause": "Test cause",
            "remediation": ["Test action"],
            "impact": "Test impact",
        })
        self.assertEqual(len(engine.list_patterns()), original_count + 1)

    def test_list_patterns(self):
        engine = RootCauseEngine()
        patterns = engine.list_patterns()
        self.assertGreater(len(patterns), 0)


class TestReporters(unittest.TestCase):
    """Unit tests for reporters.py"""

    def test_generate_markdown(self):
        data = {
            "analysis_time": "2024-01-01T12:00:00",
            "file_path": "/test/log.log",
            "stats": {
                "summary": {"ERROR": 1, "INFO": 9},
                "error_rate": 10.0,
                "total_lines": 10,
                "top_errors": [{"message": "test error", "count": 1}],
                "by_module": {"com.example": 1},
                "by_hour": {"2024-01-01 12:00": 10},
            },
            "severity": {"health_score": 90, "critical": 0, "high": 1, "medium": 0, "low": 9},
            "trend": {"trend_type": "stable", "description": "stable"},
            "root_cause": {"summary": {"description": "test"}, "findings": {}, "recommendations": []},
        }
        reports = generate_report(data, ["markdown", "json"])
        self.assertIn("markdown", reports)
        self.assertIn("json", reports)
        self.assertTrue(os.path.exists(reports["markdown"]))
        self.assertTrue(os.path.exists(reports["json"]))

    def test_generate_html(self):
        data = {
            "analysis_time": "2024-01-01T12:00:00",
            "file_path": "/test/log.log",
            "stats": {"summary": {"ERROR": 1, "INFO": 9}},
            "severity": {"health_score": 90},
            "trend": {"description": "stable"},
            "root_cause": {"summary": {}, "findings": {}},
        }
        reports = generate_report(data, ["html"])
        self.assertIn("html", reports)
        self.assertTrue(os.path.exists(reports["html"]))


class TestIntegration(unittest.TestCase):
    """Integration tests for the complete analysis pipeline"""

    def test_complete_analysis(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".log", delete=False) as f:
            f.write("2026-06-14 10:30:45.123 [thread-01] ERROR ClassName - NullPointerException: null\n")
            f.write("2026-06-14 10:30:46.123 [thread-02] INFO Service started\n")
            f.write("2026-06-14 10:30:47.123 [thread-03] WARN Deprecated method called\n")
            temp_file = f.name

        try:
            analyzer = LogAnalyzer()
            result = analyzer.analyze_file(temp_file, ["markdown", "json"])

            self.assertEqual(result["total_lines_processed"], 3)
            self.assertGreater(result["parsed_records"], 0)
            self.assertEqual(result["stats"]["summary"]["ERROR"], 1)
            self.assertEqual(result["stats"]["summary"]["INFO"], 1)
            self.assertEqual(result["stats"]["summary"]["WARN"], 1)
            self.assertIn("reports", result)
        finally:
            os.unlink(temp_file)

    def test_batch_analysis(self):
        import glob
        with tempfile.TemporaryDirectory() as tmpdir:
            files = []
            for i in range(2):
                filepath = os.path.join(tmpdir, f"test_{i}.log")
                with open(filepath, "w") as f:
                    f.write(f"2026-06-14 10:30:4{i}.123 [thread-01] ERROR Class{i} - Error {i}\n")
                files.append(filepath)

            analyzer = LogAnalyzer()
            results = analyzer.analyze_batch(f"{tmpdir}/test_*.log", ["json"])
            self.assertEqual(len(results), 2)

    def test_large_file_streaming(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".log", delete=False) as f:
            for i in range(50000):
                f.write(f"2026-06-14 10:{(i//60)%60:02d}:{i%60:02d}.{i%1000:03d} [thread-{i%10}] INFO Message {i}\n")
            temp_file = f.name

        try:
            start_time = time.time()
            analyzer = LogAnalyzer(chunk_size=10000)
            result = analyzer.analyze_file(temp_file, ["json"])

            elapsed = time.time() - start_time
            self.assertEqual(result["total_lines_processed"], 50000)
            self.assertGreater(result["parse_rate"], 1000)
            self.assertLess(elapsed, 30)
        finally:
            os.unlink(temp_file)


class TestPerformance(unittest.TestCase):
    """Performance tests for large file processing"""

    def test_performance_10k_lines(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".log", delete=False) as f:
            for i in range(10000):
                f.write(f"2026-06-14 10:{i//60%60}:{i%60}.{i%1000} [thread-{i%10}] INFO Message {i}\n")
            temp_file = f.name

        try:
            start_time = time.time()
            analyzer = LogAnalyzer()
            result = analyzer.analyze_file(temp_file, ["json"])
            elapsed = time.time() - start_time

            self.assertLess(elapsed, 5)
            self.assertGreater(result["parse_rate"], 2000)
        finally:
            os.unlink(temp_file)

    def test_performance_with_errors(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".log", delete=False) as f:
            for i in range(10000):
                level = "ERROR" if i % 100 == 0 else "INFO"
                f.write(f"2026-06-14 10:{(i//60)%60:02d}:{i%60:02d}.{i%1000:03d} [thread-{i%10}] {level} Message {i}\n")
            temp_file = f.name

        try:
            start_time = time.time()
            analyzer = LogAnalyzer()
            result = analyzer.analyze_file(temp_file, ["json"])
            elapsed = time.time() - start_time

            self.assertLess(elapsed, 5)
            self.assertEqual(result["stats"]["summary"]["ERROR"], 100)
        finally:
            os.unlink(temp_file)


if __name__ == "__main__":
    unittest.main()
