#!/usr/bin/env python3
"""按 5 条演示链路生成跨 MCP 一致的数据。

数据来源全部是仓库内已有样本：
  - mockdata/pinpoint/_apps.json       应用 ↔ 中间件依赖
  - mockdata/prometheus/_up_table.json 主机 up 表
  - mockdata/cmdb/_db_servers.json     CMDB 资产
  - mockdata/_meta.json                已埋的异常样本说明

本脚本只做三件事：
  1. 把 5 条链路固化成 mockdata/_scenarios.json（唯一事实源，前后端可读）；
  2. 按链路写 prometheus/list_alerts.json、es/_log_counts.json、prometheus/_host_metrics.json；
  3. 为场景主机补齐 CMDB 记录（缺失时），保证稽核 C1 与排障 CMDB 步骤有数据。

用法：python3 scripts/seed_demo.py
"""
import json
import os
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MOCK = os.path.join(ROOT, "mockdata")


def load(rel, default):
    path = os.path.join(MOCK, rel)
    if not os.path.exists(path):
        return default
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def dump(rel, data):
    path = os.path.join(MOCK, rel)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print("写入", os.path.relpath(path, ROOT))


APPS = load("pinpoint/_apps.json", {})
UP = load("prometheus/_up_table.json", {})
CMDB = load("cmdb/_db_servers.json", [])

# ---- 5 条示例链路：实体全部取自现有数据，appId / host / addr 三处一致 ----
SCENARIOS = [
    {
        "id": "D1",
        "name": "应用错误率飙升",
        "chain": ["业务监控", "告警中心", "自动排障", "工单"],
        "appId": "apollo-ehome-pro",
        "appName": "ehome-pro-server",
        "host": "ehome-pro.mysql.ehomeinner.cn",
        "addr": "ehome-pro.mysql.ehomeinner.cn:13303",
        "mwType": "mysql",
        "alert": {
            "labels": {
                "alertname": "HighErrorRate",
                "appId": "apollo-ehome-pro",
                "application": "ehome-pro-server",
                "severity": "critical",
            },
            "annotations": {
                "summary": "应用错误率过高",
                "description": "5 分钟错误率 8.2%，超过阈值 1%；tp99 1.7s",
            },
            "value": "8.2",
        },
        "logs": {"ehome-pro.mysql.ehomeinner.cn": 120},
        "cmdbRecord": True,
        "expect": {"alarm": "HighErrorRate", "diag": "应用视角 11 步", "ticket": "自动建单"},
    },
    {
        "id": "D2",
        "name": "MySQL 主从延迟",
        "chain": ["告警中心", "中间件排障", "资源稽核", "工单"],
        "appId": "ehome",
        "appName": "ehome线上版本",
        "host": "ehome.mysql.ehomeinner.cn",
        "addr": "ehome.mysql.ehomeinner.cn:3306",
        "mwType": "mysql",
        "alert": {
            "labels": {
                "alertname": "MySQLReplicationDelay",
                "instance": "ehome.mysql.ehomeinner.cn:3306",
                "type": "mysql",
                "severity": "warning",
            },
            "annotations": {
                "summary": "MySQL 主从延迟",
                "description": "主从延迟 38s，超过阈值 30s",
            },
            "value": "38",
        },
        "logs": {"ehome.mysql.ehomeinner.cn": 86},
        "expect": {"alarm": "MySQLReplicationDelay", "diag": "中间件视角 9 步", "audit": "C1/C4"},
    },
    {
        "id": "D3",
        "name": "待下线主机失联",
        "chain": ["巡检 T7", "告警中心", "主机排障", "CMDB 交叉验证"],
        "appId": "ehome",
        "appName": "ehome线上版本",
        "host": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn",
        "addr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9100",
        "mwType": "rocketmq",
        "alert": {
            "labels": {
                "alertname": "TargetDown",
                "instance": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9100",
                "job": "node_exporter",
                "severity": "critical",
            },
            "annotations": {
                "summary": "主机失联",
                "description": "目标已超过 5 分钟未上报指标",
            },
            "value": "0",
        },
        "logs": {"sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn": 240},
        "up": 0,
        "expect": {"inspect": "T7 down", "alarm": "TargetDown", "diag": "host up=0"},
    },
    {
        "id": "D4",
        "name": "报废主机仍上报指标",
        "chain": ["资源稽核 C2", "指标巡检 T11", "告警中心", "工单"],
        "appId": "ehome-order",
        "appName": "ehome-order",
        "host": "public-push3.rocketmq.ehome.dlifeinner.cn",
        "addr": "public-push3.rocketmq.ehome.dlifeinner.cn:9100",
        "mwType": "rocketmq",
        "alert": {
            "labels": {
                "alertname": "HostHighCPU",
                "instance": "public-push3.rocketmq.ehome.dlifeinner.cn:9100",
                "job": "node_exporter",
                "severity": "warning",
            },
            "annotations": {
                "summary": "主机 CPU 过高",
                "description": "CPU 使用率 15 分钟均值 91.2%",
            },
            "value": "91.2",
        },
        "logs": {"public-push3.rocketmq.ehome.dlifeinner.cn": 45},
        "hostMetrics": {
            "cpuUsage": 94.6,
            "memUsage": 88.3,
            "load": 14.2,
            "tcpOverflow": 3,
            "tcpTotal": 11200,
            "diskUsage": 82.4,
        },
        "expect": {"audit": "C2 报废仍 up=1", "inspect": "T11 CPU 阈值", "diag": "host 指标"},
    },
    {
        "id": "D5",
        "name": "配置引用主机缺失 CMDB",
        "chain": ["拓扑影响面", "资源稽核 C1", "中间件排障", "工单"],
        "appId": "ehome",
        "appName": "ehome线上版本",
        "host": "public-push1.rocketmq.ehome.dlifeinner.cn",
        "addr": "public-push1.rocketmq.ehome.dlifeinner.cn:9876",
        "mwType": "rocketmq",
        "alert": None,
        "logs": {"public-push1.rocketmq.ehome.dlifeinner.cn": 30},
        "expect": {"audit": "C1 CMDB 缺失", "topo": "影响 11 个应用", "diag": "中间件视角"},
    },
]

# ---- 1. 巡检：确保场景主机的 up 值符合预期 ----
for s in SCENARIOS:
    want = s.get("up", 1)
    if s["host"] not in UP:
        print("补充 up 表主机", s["host"])
    UP[s["host"]] = want
dump("prometheus/_up_table.json", UP)

# ---- 2. 告警：由 _scenarios.json 驱动 alertmanager/prometheus 告警 ----
alerts = []
now = time.time()
for i, s in enumerate(SCENARIOS):
    if not s.get("alert"):
        continue
    alerts.append(
        {
            "labels": s["alert"]["labels"],
            "annotations": s["alert"]["annotations"],
            "state": "firing",
            "activeAt": time.strftime(
                "%Y-%m-%dT%H:%M:%S+08:00", time.localtime(now - (12 + i * 3) * 60)
            ),
            "value": s["alert"]["value"],
        }
    )
dump("prometheus/list_alerts.json", {"status": "success", "data": {"alerts": alerts}})

# ---- 3. 日志：按主机补错误日志量，供排障第 10 步 / 巡检 T10 使用 ----
log_counts = load("es/_log_counts.json", {"default": 5})
for s in SCENARIOS:
    log_counts.update(s.get("logs", {}))
dump("es/_log_counts.json", log_counts)

# ---- 4. 指标：场景主机指标覆写（mockmcp 优先读该文件）----
overrides = {
    s["host"]: s["hostMetrics"] for s in SCENARIOS if s.get("hostMetrics")
}
dump("prometheus/_host_metrics.json", overrides)

# ---- 5. CMDB：D1 主机补资产（ip 字段就是中间件域名，与现有样本一致）
# D5 故意不补，用来演示 C1「配置引用了 CMDB 不存在的主机」。
CMDB = [
    rec for rec in CMDB
    if not str(rec.get("id", "")).startswith("demo-")
]
existing = set()
for rec in CMDB:
    for key in ("hostName", "ip", "vip"):
        val = (rec.get(key) or "").strip().lower()
        if val:
            existing.add(val)
added = 0
for s in SCENARIOS:
    if not s.get("cmdbRecord"):
        continue
    host = s["host"]
    if host.lower() in existing:
        continue
    idx = len(CMDB) + 1
    CMDB.append(
        {
            "id": "demo-%s-%02d" % (s["id"].lower(), idx),
            "hostName": "ecs-demo-%02d" % idx,
            "ip": host,
            "publicIp": None,
            "vip": "",
            "node": "云公司->贵州4c",
            "state": 0,
            "type": 2,
            "belong": 2,
            "environment": "生产",
            "specification": "8C32GSAS(600G)",
            "cpuCores": 8,
            "memory": 32,
            "osType": "linux",
            "os": "CentOS Linux 7.9.2009 Core",
            "usingBy": s["appName"],
            "operA": "演示数据",
            "assetNo": "DEMO%s" % s["id"],
            "rack": "R01-A01",
            "_middlewareType": s["mwType"],
        }
    )
    existing.add(host.lower())
    added += 1
dump("cmdb/_db_servers.json", CMDB)
print("CMDB 新增场景主机 %d 条，共 %d 条" % (added, len(CMDB)))

# ---- 6. 场景清单：唯一事实源 ----
dump(
    "_scenarios.json",
    {
        "generatedAt": time.strftime("%Y-%m-%d %H:%M:%S"),
        "source": "mockdata/_meta.json 已埋异常样本 + pinpoint/_apps.json 应用依赖",
        "scenarios": SCENARIOS,
    },
)

print("\n五条链路：")
for s in SCENARIOS:
    print("  %s %-16s %-28s -> %s" % (s["id"], s["name"], s["host"], " / ".join(s["chain"])))
