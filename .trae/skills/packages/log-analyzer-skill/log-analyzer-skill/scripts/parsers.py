"""
parsers.py - Log format parsers for log-analyzer-skill.
Auto-detects and parses common log formats (ISO, Syslog, NCSA, Java stack, JSON).
Zero external dependencies.
"""

import json
import re
import sys
from datetime import datetime
from typing import Optional, Dict, Any, List, Tuple, Iterator

# ── Log level set ──
LOG_LEVELS = {"TRACE", "DEBUG", "INFO", "WARN", "WARNING", "ERROR", "FATAL", "CRITICAL"}

# ── Format definitions ──
# Order matters: more specific patterns must come first
FORMAT_PATTERNS: List[Tuple[str, re.Pattern]] = [
    # Java/Spring log: timestamp [thread] LEVEL trace_id class_name - message
    # e.g. 2024-01-01 12:00:00.123 [http-nio-8080-exec-1] ERROR a1b2c3d4e5f6 com.example.Service - NullPointerException
    ("java_app", re.compile(
        r'^(?P<ts>\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d{3})?)'
        r'\s+\[(?P<thread>[^\]]+)\]'
        r'\s+(?P<level>TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|CRITICAL)\b'
        r'\s+(?P<trace_id>[a-fA-F0-9-]+)?\s*'
        r'(?P<module>[\w.]+\.[\w.]+)?'
        r'\s*[:\-]?\s*(?P<message>.+)?',
        re.IGNORECASE
    )),

    # ISO-8601 / standard timestamp: [2024-01-01 12:00:00,123] LEVEL [module] message
    ("iso", re.compile(
        r'^\[?(?P<ts>\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d{3})?)\]?\s+'
        r'(?P<level>TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|CRITICAL)\b'
        r'(?:\s+\[?(?P<module>[^\]]+)\]?)?'
        r'\s*[:\-]?\s*(?P<message>.+)',
        re.IGNORECASE
    )),

    # Syslog
    ("syslog", re.compile(
        r'^(?:<\d+>)?'
        r'(?P<ts>[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?)'
        r'\s+\S+\s+(?P<module>\S+?)(?:\[\d+\])?:\s*'
        r'(?P<level>TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL)\s*'
        r'[:\-]?\s*(?P<message>.+)?',
        re.IGNORECASE
    )),

    # NCSA / Common Log Format (Apache/Nginx access log)
    ("ncsa", re.compile(
        r'^(?P<remote>\S+)\s+\S+\s+\S+\s+'
        r'\[(?P<ts>\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2}\s[+-]\d{4})\]'
        r'\s+"(?P<method>\S+)\s+(?P<path>\S+)\s+\S+"\s+'
        r'(?P<status>\d{3})\s+(?P<size>\d+|-)',
    )),

    # Java stack trace line (continuation lines after exception)
    ("java_stack", re.compile(
        r'^\s+at\s+[\w.$]+\([\w.]+\.java:\d+\)',
    )),

    # Caused by: line
    ("caused_by", re.compile(
        r'^Caused by:\s+(?P<exception>[\w.]+(?:Exception|Error)):\s*(?P<message>.*)',
    )),

    # JSON log format: {"level": "ERROR", "message": "...", "timestamp": "..."}
    ("json_log", re.compile(
        r'^\s*\{.*"level"\s*:\s*"(?P<level>[A-Z]+)".*\}',
        re.DOTALL
    )),
]


def detect_format(line: str) -> Optional[str]:
    """Auto-detect which log format a line belongs to."""
    if not line.strip():
        return None
    for name, pattern in FORMAT_PATTERNS:
        if pattern.match(line):
            return name
    return None


def parse_line(line: str) -> Optional[Dict[str, Any]]:
    """
    Parse a single log line into a structured record.
    Returns None for unrecognized lines (continuation lines, blank lines, etc.)
    """
    stripped = line.strip("\n\r")
    if not stripped:
        return None

    for name, pattern in FORMAT_PATTERNS:
        m = pattern.match(stripped)
        if not m:
            continue

        record: Dict[str, Any] = {
            "_raw": line,
            "_format": name,
            "_line_no": 0,
        }
        gd = m.groupdict()

        # Extract level
        level_raw = gd.get("level", "")
        if level_raw:
            record["level"] = level_raw.upper()
        elif name == "ncsa":
            status = int(gd.get("status", 0))
            if status >= 500:
                record["level"] = "ERROR"
            elif status >= 400:
                record["level"] = "WARN"
            else:
                record["level"] = "INFO"

        # Extract timestamp
        ts_raw = gd.get("ts")
        if ts_raw:
            record["timestamp"] = _parse_timestamp(ts_raw, name)
            record["timestamp_raw"] = ts_raw

        # Extract message
        msg = gd.get("message", "").strip()
        if msg:
            record["message"] = msg

        # Module/class info
        mod = gd.get("module")
        if mod:
            record["module"] = mod.strip("[] ")

        # Thread name (java_app format)
        thread = gd.get("thread")
        if thread:
            record["thread_name"] = thread

        # Trace ID (java_app format)
        trace_id = gd.get("trace_id")
        if trace_id:
            record["trace_id"] = trace_id

        # HTTP-specific
        if name == "ncsa":
            record["http_method"] = gd.get("method", "")
            record["http_path"] = gd.get("path", "")
            record["http_status"] = int(gd.get("status", 0))
            record["remote_addr"] = gd.get("remote", "")
            record["message"] = f'{gd.get("method","")} {gd.get("path","")} -> {gd.get("status","")}'

        if name == "caused_by":
            record["exception"] = gd.get("exception", "")
            record["is_root_cause"] = True

        if name == "java_stack":
            record["is_stack_trace"] = True

        # Extract error type from message (only for error-level records)
        level = record.get("level", "")
        if level in ("ERROR", "FATAL", "CRITICAL"):
            record["error_type"] = _extract_error_type(record.get("message", ""))
            record["error_message"] = _extract_error_message(record.get("message", ""))
        else:
            record["error_type"] = None
            record["error_message"] = None

        return record

    return None


def _extract_error_type(message: str) -> str:
    """Extract error type (Exception/Error class name) from message."""
    m = re.search(r'([\w.]+(?:Exception|Error|Fault))', message)
    if m:
        return m.group(1)
    # Fallback: check for known error keywords
    if re.search(r'(timeout|超时)', message, re.IGNORECASE):
        return "TimeoutError"
    if re.search(r'(refused|connection.*reset)', message, re.IGNORECASE):
        return "ConnectionError"
    if re.search(r'(denied|permission|access)', message, re.IGNORECASE):
        return "PermissionError"
    if re.search(r'(null|npe)', message, re.IGNORECASE):
        return "NullPointerError"
    return "UnknownError"


def _extract_error_message(message: str) -> str:
    """Extract the meaningful part of an error message."""
    # Try to get text after exception class name
    m = re.search(r'(?:Exception|Error|Fault)[:\s]+(.+?)$', message)
    if m:
        return m.group(1).strip()
    return message[:200]


def _parse_timestamp(raw: str, fmt_name: str) -> Optional[str]:
    """Parse timestamp into ISO-8601 string."""
    try:
        if fmt_name == "iso":
            ts = raw.replace("/", "-").replace("T", " ").replace(",", ".")
            return ts[:26] if "." in ts else ts[:19]

        elif fmt_name == "syslog":
            from datetime import datetime
            return datetime.strptime(raw, "%b %d %H:%M:%S").replace(
                year=datetime.now().year
            ).isoformat()

        elif fmt_name == "ncsa":
            from datetime import datetime
            return datetime.strptime(raw[:20], "%d/%b/%Y:%H:%M:%S").isoformat()
    except (ValueError, IndexError):
        pass
    return None


def is_stack_trace_line(line: str) -> bool:
    """Check if a line is a Java stack continuation line."""
    return bool(re.match(r'^\s+at\s+', line)) or bool(re.match(r'^\.\.\.\s+\d+\s+more', line))


def parse_json_log(line: str) -> Optional[Dict[str, Any]]:
    """Parse JSON-formatted log lines."""
    stripped = line.strip()
    if not stripped.startswith("{"):
        return None
    try:
        data = json.loads(stripped)
        record: Dict[str, Any] = {
            "_raw": line,
            "_format": "json_log",
            "_line_no": 0,
        }
        if "level" in data:
            record["level"] = str(data["level"]).upper()
        if "message" in data:
            record["message"] = str(data["message"])
        if "timestamp" in data or "time" in data:
            ts_key = "timestamp" if "timestamp" in data else "time"
            record["timestamp_raw"] = str(data[ts_key])
            record["timestamp"] = _normalize_json_timestamp(data[ts_key])
        if "module" in data or "logger" in data or "service" in data:
            for key in ["module", "logger", "service"]:
                if key in data:
                    record["module"] = str(data[key])
                    break
        if "error" in data:
            record["error_type"] = str(data["error"].get("type", "")) if isinstance(data["error"], dict) else str(data["error"])
        if "stack" in data:
            record["stack_trace"] = data["stack"]
        return record
    except json.JSONDecodeError:
        return None


def _normalize_json_timestamp(ts: Any) -> Optional[str]:
    """Normalize various JSON timestamp formats to ISO-8601."""
    ts_str = str(ts)
    try:
        if ts_str.isdigit():
            ts_int = int(ts_str)
            if ts_int > 10**12:
                return datetime.fromtimestamp(ts_int / 1000).isoformat()
            return datetime.fromtimestamp(ts_int).isoformat()
        for fmt in ("%Y-%m-%dT%H:%M:%S.%fZ", "%Y-%m-%dT%H:%M:%SZ",
                    "%Y-%m-%dT%H:%M:%S.%f", "%Y-%m-%dT%H:%M:%S"):
            try:
                return datetime.strptime(ts_str, fmt).isoformat()
            except ValueError:
                continue
    except (ValueError, TypeError):
        pass
    return ts_str


def parse_excel_file(file_path: str) -> Iterator[Dict[str, Any]]:
    """Parse Excel (.xlsx) log files."""
    try:
        import openpyxl
    except ImportError:
        print(f"⚠️ 缺少 openpyxl 库，无法解析 Excel 文件。请安装: pip install openpyxl", file=sys.stderr)
        print(f"   已自动降级为文本模式解析...", file=sys.stderr)
        # Fallback: try to parse as binary/text file
        with open(file_path, 'rb') as f:
            content = f.read().decode('utf-8', errors='ignore')
            for line_no, line in enumerate(content.split('\n'), 1):
                if line.strip():
                    record = parse_line(line)
                    if record:
                        record["_format"] = "excel_fallback"
                        yield record
        return

    wb = openpyxl.load_workbook(file_path, read_only=True)
    for sheet_name in wb.sheetnames:
        ws = wb[sheet_name]
        headers = None
        for row in ws.iter_rows(values_only=True):
            if headers is None:
                headers = [str(cell) if cell is not None else f"col_{i}" for i, cell in enumerate(row)]
                continue

            record = {}
            for i, cell in enumerate(row):
                if i < len(headers):
                    record[headers[i]] = cell

            # Convert to log record format
            yield _convert_to_log_record(record, "excel")


def parse_csv_file(file_path: str) -> Iterator[Dict[str, Any]]:
    """Parse CSV log files."""
    import csv
    with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
        reader = csv.DictReader(f)
        for row in reader:
            yield _convert_to_log_record(row, "csv")


def parse_xml_file(file_path: str) -> Iterator[Dict[str, Any]]:
    """Parse XML log files."""
    import xml.etree.ElementTree as ET
    tree = ET.parse(file_path)
    root = tree.getroot()

    # Try to find log entries (common patterns)
    log_entries = root.findall('.//log') or root.findall('.//entry') or root.findall('.//record') or root.findall('.//event')

    if not log_entries:
        # If no standard log tags, treat each child element as a log entry
        log_entries = list(root)

    for entry in log_entries:
        record = {}
        for child in entry:
            record[child.tag] = child.text

        yield _convert_to_log_record(record, "xml")


def parse_pdf_file(file_path: str) -> Iterator[Dict[str, Any]]:
    """Parse PDF log files (extract text and parse as lines)."""
    try:
        import PyPDF2
    except ImportError:
        print(f"⚠️ 缺少 PyPDF2 库，无法解析 PDF 文件。请安装: pip install PyPDF2", file=sys.stderr)
        print(f"   已自动降级为文本模式解析...", file=sys.stderr)
        # Fallback: try to parse as binary/text file
        with open(file_path, 'rb') as f:
            content = f.read().decode('utf-8', errors='ignore')
            for line_no, line in enumerate(content.split('\n'), 1):
                if line.strip():
                    record = parse_line(line)
                    if record:
                        record["_format"] = "pdf_fallback"
                        yield record
        return

    with open(file_path, 'rb') as f:
        reader = PyPDF2.PdfReader(f)
        for page in reader.pages:
            text = page.extract_text()
            for line in text.split('\n'):
                if line.strip():
                    record = parse_line(line)
                    if record:
                        record["_format"] = "pdf"
                        yield record


def parse_json_file(file_path: str) -> Iterator[Dict[str, Any]]:
    """Parse JSON log files (line-delimited JSON or JSON array)."""
    with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
        content = f.read().strip()

        # Try to parse as JSON array first
        try:
            data = json.loads(content)
            if isinstance(data, list):
                for item in data:
                    if isinstance(item, dict):
                        yield _convert_to_log_record(item, "json_file")
                return
        except json.JSONDecodeError:
            pass

        # Fall back to line-delimited JSON
        for line in content.split('\n'):
            if line.strip():
                record = parse_json_log(line)
                if record:
                    yield record


def _convert_to_log_record(data: Dict[str, Any], format_type: str) -> Dict[str, Any]:
    """Convert parsed data to standard log record format."""
    record: Dict[str, Any] = {
        "_raw": str(data),
        "_format": format_type,
        "_line_no": 0,
    }

    # Map common field names
    field_mapping = {
        'level': ['level', 'severity', 'priority', 'log_level'],
        'message': ['message', 'msg', 'text', 'description', 'content'],
        'timestamp': ['timestamp', 'time', 'date', '@timestamp', 'datetime'],
        'module': ['module', 'logger', 'service', 'source', 'component', 'app'],
        'error_type': ['error_type', 'exception', 'error', 'exception_type'],
        'thread': ['thread', 'thread_name', 'threadId'],
        'trace_id': ['trace_id', 'traceId', 'request_id', 'requestId'],
    }

    for target_field, source_fields in field_mapping.items():
        for source_field in source_fields:
            if source_field in data and data[source_field]:
                value = data[source_field]
                if target_field == 'level':
                    record[target_field] = str(value).upper()
                elif target_field == 'timestamp':
                    record['timestamp_raw'] = str(value)
                    record['timestamp'] = _normalize_json_timestamp(value)
                else:
                    record[target_field] = str(value)
                break

    # If no level found, try to infer from content
    if 'level' not in record:
        message = record.get('message', '').upper()
        for level in ['CRITICAL', 'FATAL', 'ERROR', 'WARN', 'WARNING', 'INFO', 'DEBUG', 'TRACE']:
            if level in message:
                record['level'] = level
                break

    # If still no level, default to INFO
    if 'level' not in record:
        record['level'] = 'INFO'

    # Ensure message field exists
    if 'message' not in record:
        record['message'] = str(data)

    return record
