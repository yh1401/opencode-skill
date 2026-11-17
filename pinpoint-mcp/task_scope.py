"""任务作用域注册表（跨会话工具继承）

父会话通过 arch_task_open_tool 注册本次巡检的节点白名单，之后任意会话
（包括无法继承 MCP 配置的子任务 Worker）只要携带同一 task_id 即可调用工具；
未注册的 task_id 保持原行为（不限制），保证旧调用兼容。

约束只针对"节点"，不进入 MCP session 层，因此 Worker 不需要复制父会话的
MCP 连接配置；若 Worker 完全无法使用 MCP，可走 server.py 的 REST 批量入口。
"""
import threading
import time
from collections import OrderedDict
from typing import Optional


class TaskScope:
    def __init__(self, task_id: str, nodes: list[str], ttl_minutes: int, owner: str = ""):
        now = time.time()
        self.task_id = task_id
        self.nodes = list(dict.fromkeys(n.strip() for n in nodes if n and n.strip()))
        self.owner = owner
        self.created_at = now
        self.expires_at = now + max(1, ttl_minutes) * 60
        self.used_nodes: set[str] = set()

    def alive(self) -> bool:
        return time.time() < self.expires_at

    def touch(self, minutes: int) -> None:
        self.expires_at = time.time() + max(1, minutes) * 60

    def to_dict(self) -> dict:
        return {
            "taskId": self.task_id,
            "nodes": self.nodes,
            "nodeCount": len(self.nodes),
            "usedNodeCount": len(self.used_nodes),
            "unusedNodes": [n for n in self.nodes if n not in self.used_nodes],
            "owner": self.owner,
            "createdAt": int(self.created_at * 1000),
            "expiresAt": int(self.expires_at * 1000),
        }


class TaskScopeRegistry:
    """线程安全的 task_id → 节点白名单注册表（TTL + LRU）"""

    def __init__(self, max_tasks: int = 1000):
        self._lock = threading.Lock()
        self._items: "OrderedDict[str, TaskScope]" = OrderedDict()
        self._max = max_tasks

    def register(
        self,
        task_id: str,
        nodes: list[str],
        ttl_minutes: int,
        owner: str = "",
    ) -> TaskScope:
        with self._lock:
            scope = TaskScope(task_id, nodes, ttl_minutes, owner)
            self._items[task_id] = scope
            self._items.move_to_end(task_id)
            while len(self._items) > self._max:
                self._items.popitem(last=False)
            return scope

    def resolve(self, task_id: Optional[str], ttl_minutes: int = 30) -> Optional[TaskScope]:
        if not task_id:
            return None
        with self._lock:
            scope = self._items.get(task_id)
            if scope is None:
                return None
            if not scope.alive():
                self._items.pop(task_id, None)
                return None
            scope.touch(ttl_minutes)
            self._items.move_to_end(task_id)
            return scope

    def check(self, task_id: Optional[str], node: str, ttl_minutes: int = 30) -> tuple[bool, str]:
        """返回 (是否允许, 拒绝原因)；未注册的 task_id 不限制"""
        if not task_id:
            return True, ""
        scope = self.resolve(task_id, ttl_minutes)
        if scope is None:
            return True, "TASK_UNREGISTERED"
        if node not in scope.nodes:
            return False, f"节点 {node} 不在 task {task_id} 的白名单内"
        with self._lock:
            scope.used_nodes.add(node)
        return True, ""

    def status(self, task_id: str, ttl_minutes: int = 30) -> Optional[dict]:
        scope = self.resolve(task_id, ttl_minutes)
        return scope.to_dict() if scope else None

    def sweep(self) -> int:
        with self._lock:
            dead = [k for k, v in self._items.items() if not v.alive()]
            for k in dead:
                self._items.pop(k, None)
            return len(dead)

    def count(self) -> int:
        with self._lock:
            return len([v for v in self._items.values() if v.alive()])


_registry = TaskScopeRegistry()


def get_registry() -> TaskScopeRegistry:
    return _registry
