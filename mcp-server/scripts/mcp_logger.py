import os
import json
import logging
from datetime import datetime
from typing import Dict, Optional


class MCPLogger:
    _instance = None
    MAX_TOTAL_SIZE = 1 * 1024 * 1024 * 1024
    
    def __new__(cls, *args, **kwargs):
        if cls._instance is None:
            cls._instance = super().__new__(cls)
        return cls._instance
    
    def __init__(self, log_level: str = "INFO", log_dir: str = "logs"):
        if hasattr(self, '_initialized') and self._initialized:
            return
        
        self._initialized = True
        self.log_dir = log_dir
        self.log_level = log_level.upper()
        
        os.makedirs(log_dir, exist_ok=True)
        
        self.logger = logging.getLogger('ops-data-query-mcp')
        self.logger.setLevel(getattr(logging, self.log_level))
        self.logger.propagate = False
        
        self._setup_handlers()
        self._setup_formatters()
    
    def _get_today_dir(self):
        today = datetime.now().strftime('%Y-%m-%d')
        today_dir = os.path.join(self.log_dir, today)
        os.makedirs(today_dir, exist_ok=True)
        return today_dir
    
    def _get_total_log_size(self):
        total_size = 0
        if not os.path.exists(self.log_dir):
            return 0
        for date_folder in os.listdir(self.log_dir):
            date_path = os.path.join(self.log_dir, date_folder)
            if os.path.isdir(date_path):
                for f in os.listdir(date_path):
                    f_path = os.path.join(date_path, f)
                    if os.path.isfile(f_path) and f.endswith('.log'):
                        total_size += os.path.getsize(f_path)
        return total_size
    
    def _cleanup_old_logs(self):
        total_size = self._get_total_log_size()
        if total_size <= self.MAX_TOTAL_SIZE:
            return
        
        files = []
        for date_folder in os.listdir(self.log_dir):
            date_path = os.path.join(self.log_dir, date_folder)
            if os.path.isdir(date_path):
                for f in os.listdir(date_path):
                    f_path = os.path.join(date_path, f)
                    if os.path.isfile(f_path) and f.endswith('.log'):
                        files.append((os.path.getmtime(f_path), f_path))
        
        files.sort(reverse=True)
        
        current_size = total_size
        for mtime, f_path in files[2:]:
            if current_size <= self.MAX_TOTAL_SIZE:
                break
            file_size = os.path.getsize(f_path)
            os.remove(f_path)
            current_size -= file_size
        
        for date_folder in os.listdir(self.log_dir):
            date_path = os.path.join(self.log_dir, date_folder)
            if os.path.isdir(date_path) and len(os.listdir(date_path)) == 0:
                os.rmdir(date_path)
    
    def _setup_handlers(self):
        self.logger.handlers.clear()
        
        console_handler = logging.StreamHandler()
        console_handler.setLevel(getattr(logging, self.log_level))
        
        file_handler = logging.FileHandler(
            os.path.join(self._get_today_dir(), 'mcp_server.log'),
            encoding='utf-8'
        )
        file_handler.setLevel(logging.DEBUG)
        
        error_handler = logging.FileHandler(
            os.path.join(self._get_today_dir(), 'mcp_server_error.log'),
            encoding='utf-8'
        )
        error_handler.setLevel(logging.ERROR)
        
        def create_cleanup_filter():
            class CleanupFilter(logging.Filter):
                def filter(self, record):
                    self._cleanup_old_logs()
                    return True
            
            filter_instance = CleanupFilter()
            filter_instance._cleanup_old_logs = self._cleanup_old_logs
            return filter_instance
        
        file_handler.addFilter(create_cleanup_filter())
        error_handler.addFilter(create_cleanup_filter())
        
        self.logger.addHandler(console_handler)
        self.logger.addHandler(file_handler)
        self.logger.addHandler(error_handler)
        
        self.console_handler = console_handler
        self.file_handler = file_handler
        self.error_handler = error_handler
    
    def _setup_formatters(self):
        console_formatter = logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - %(message)s',
            datefmt='%Y-%m-%d %H:%M:%S'
        )
        
        file_formatter = logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - %(module)s:%(lineno)d - %(message)s',
            datefmt='%Y-%m-%d %H:%M:%S'
        )
        
        self.console_handler.setFormatter(console_formatter)
        self.file_handler.setFormatter(file_formatter)
        self.error_handler.setFormatter(file_formatter)
    
    def _update_handlers_date(self):
        today_dir = self._get_today_dir()
        
        def create_cleanup_filter():
            class CleanupFilter(logging.Filter):
                def filter(self, record):
                    self._cleanup_old_logs()
                    return True
            
            filter_instance = CleanupFilter()
            filter_instance._cleanup_old_logs = self._cleanup_old_logs
            return filter_instance
        
        if hasattr(self, 'file_handler'):
            current_file = self.file_handler.baseFilename
            expected_file = os.path.join(today_dir, 'mcp_server.log')
            if current_file != expected_file:
                self.file_handler.close()
                self.file_handler = logging.FileHandler(expected_file, encoding='utf-8')
                self.file_handler.setLevel(logging.DEBUG)
                self.file_handler.setFormatter(self._get_file_formatter())
                self.file_handler.addFilter(create_cleanup_filter())
                self.logger.addHandler(self.file_handler)
        
        if hasattr(self, 'error_handler'):
            current_file = self.error_handler.baseFilename
            expected_file = os.path.join(today_dir, 'mcp_server_error.log')
            if current_file != expected_file:
                self.error_handler.close()
                self.error_handler = logging.FileHandler(expected_file, encoding='utf-8')
                self.error_handler.setLevel(logging.ERROR)
                self.error_handler.setFormatter(self._get_file_formatter())
                self.error_handler.addFilter(create_cleanup_filter())
                self.logger.addHandler(self.error_handler)
    
    def _get_file_formatter(self):
        return logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - %(module)s:%(lineno)d - %(message)s',
            datefmt='%Y-%m-%d %H:%M:%S'
        )
    
    def log_tool_call(self, tool_name: str, arguments: Dict, result: Dict, duration: float, success: bool = True):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "tool_call",
            "tool_name": tool_name,
            "arguments": arguments,
            "duration_ms": round(duration * 1000, 2),
            "success": success,
            "result_code": result.get("code") if isinstance(result, dict) else None,
            "result_count": result.get("data", {}).get("total", 0) if isinstance(result, dict) and "data" in result else None
        }
        
        log_message = json.dumps(log_data, ensure_ascii=False)
        
        if success:
            self.logger.info(f"[TOOL_CALL] {log_message}")
        else:
            self.logger.error(f"[TOOL_CALL_ERROR] {log_message}")
    
    def log_mcp_request(self, method: str, request_id: Optional[int] = None, params: Optional[Dict] = None):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "mcp_request",
            "method": method,
            "request_id": request_id,
            "params": params
        }
        log_message = json.dumps(log_data, ensure_ascii=False)
        self.logger.info(f"[MCP_REQUEST] {log_message}")
    
    def log_mcp_response(self, method: str, request_id: Optional[int] = None, success: bool = True, error: Optional[str] = None):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "mcp_response",
            "method": method,
            "request_id": request_id,
            "success": success,
            "error": error
        }
        log_message = json.dumps(log_data, ensure_ascii=False)
        
        if success:
            self.logger.info(f"[MCP_RESPONSE] {log_message}")
        else:
            self.logger.error(f"[MCP_RESPONSE_ERROR] {log_message}")
    
    def log_api_request(self, skill_id: str, method: str, url: str, params: Optional[Dict] = None):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "api_request",
            "skill_id": skill_id,
            "method": method,
            "url": url,
            "params": params
        }
        log_message = json.dumps(log_data, ensure_ascii=False)
        self.logger.debug(f"[API_REQUEST] {log_message}")
    
    def log_api_response(self, skill_id: str, code: Optional[int] = None, duration: float = 0.0, success: bool = True, error: Optional[str] = None):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "api_response",
            "skill_id": skill_id,
            "code": code,
            "duration_ms": round(duration * 1000, 2),
            "success": success,
            "error": error
        }
        log_message = json.dumps(log_data, ensure_ascii=False)
        
        if success:
            self.logger.debug(f"[API_RESPONSE] {log_message}")
        else:
            self.logger.warning(f"[API_RESPONSE_ERROR] {log_message}")
    
    def log_sse_connection(self, client_id: str, action: str):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "sse_connection",
            "client_id": client_id,
            "action": action
        }
        log_message = json.dumps(log_data, ensure_ascii=False)
        self.logger.info(f"[SSE_CONNECTION] {log_message}")
    
    def log_mock_usage(self, skill_id: str, reason: str = "unknown"):
        self._update_handlers_date()
        log_data = {
            "timestamp": datetime.now().isoformat(),
            "type": "mock_usage",
            "skill_id": skill_id,
            "reason": reason
        }
        log_message = json.dumps(log_data, ensure_ascii=False)
        self.logger.warning(f"[MOCK_USAGE] {log_message}")
    
    def log_error(self, message: str, exc_info: bool = False):
        self._update_handlers_date()
        self.logger.error(message, exc_info=exc_info)
    
    def log_warning(self, message: str):
        self._update_handlers_date()
        self.logger.warning(message)
    
    def log_info(self, message: str):
        self._update_handlers_date()
        self.logger.info(message)
    
    def log_debug(self, message: str):
        self._update_handlers_date()
        self.logger.debug(message)


def get_logger() -> MCPLogger:
    return MCPLogger()