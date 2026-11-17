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

# ---- 2.5 背景告警：从 CMDB 真实记录/拓扑主机派生，让告警、日志、指标更接近真实平台 ----
# 设计约束：
#   - 主机全部取自 CMDB 已有记录，避免稽核 C1/C4 意外新增孤儿；
#   - severity 分布 2 critical / 3 warning / 3 info，firing 的 critical+warning 才进自动排障，
#     不会淹没 D1–D4 演示链路（自动排障每轮上限 5 会话 + 冷却）；
#   - 5 条 resolved 提供历史恢复数据：告警趋势图有 created/resolved 两组柱，统计页有恢复口径；
#   - activeAt 分散在过去 1–23 小时，配合告警入库取 activeAt 作为 FirstSeenAt，趋势按小时展开。
BACKGROUNDS = [
    # firing：持续中的背景告警
    {"alertname": "HostHighMemory", "host": "public-push2.rocketmq.ehome.dlifeinner.cn", "port": 9100,
     "severity": "warning", "hoursAgo": 3, "summary": "主机内存过高",
     "description": "内存使用率 15 分钟均值 92.4%，超过阈值 90%", "value": "92.4",
     "logs": 88, "metrics": {"memUsage": 92.4, "diskUsage": 81.0}},
    {"alertname": "RedisMemoryUsageHigh", "host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn", "port": 6379,
     "severity": "critical", "hoursAgo": 5, "summary": "Redis 内存接近上限",
     "description": "used_memory 达 maxmemory 的 96%，存在淘汰风险", "value": "96",
     "logs": 64},
    {"alertname": "RocketMQBrokerHighDisk", "host": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn", "port": 9876,
     "severity": "warning", "hoursAgo": 7, "summary": "Broker 磁盘使用率过高",
     "description": "commitlog 所在分区使用率 87%，超过阈值 85%", "value": "87",
     "logs": 52, "metrics": {"diskUsage": 87.3}},
    {"alertname": "DiskUsageHigh", "host": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn", "port": 9100,
     "severity": "info", "hoursAgo": 9, "summary": "磁盘使用率偏高",
     "description": "数据盘使用率 89%，建议清理过期数据", "value": "89", "logs": 41},
    {"alertname": "NacosInstanceOffline", "host": "vpaas-nacos.vpaas-nacos", "port": 8848,
     "severity": "info", "hoursAgo": 2, "summary": "Nacos 实例下线",
     "description": "健康检查连续 3 次失败，实例已摘除", "value": "1", "logs": 36},
    {"alertname": "HighErrorRate", "host": "cloud-ehome", "port": "", "appId": "cloud-ehome",
     "application": "cloud-ehome-server",
     "severity": "warning", "hoursAgo": 4, "summary": "应用错误率过高",
     "description": "5 分钟错误率 3.6%，超过阈值 1%（未达 critical 级别）", "value": "3.6", "logs": 73},
    # resolved：历史已恢复告警，入库即 resolved，只贡献趋势/统计，不进排障
    {"alertname": "HostHighCPU", "host": "sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn", "port": 9100,
     "severity": "warning", "hoursAgo": 9, "state": "resolved", "summary": "主机 CPU 过高（已恢复）",
     "description": "CPU 使用率曾达 93%，当前已回落", "value": "93", "logs": 47},
    {"alertname": "RedisMemoryUsageHigh", "host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn", "port": 6379,
     "severity": "warning", "hoursAgo": 17, "state": "resolved", "summary": "Redis 内存偏高（已恢复）",
     "description": "used_memory 曾达 maxmemory 的 94%，已扩容", "value": "94", "logs": 33},
    {"alertname": "TargetDown", "host": "sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn", "port": 9100,
     "severity": "critical", "hoursAgo": 21, "state": "resolved", "summary": "主机失联（已恢复）",
     "description": "目标曾超过 5 分钟未上报指标，现已恢复上报", "value": "0", "logs": 58},
    {"alertname": "HostHighMemory", "host": "common-alarmmessage2.rocketmq.ehome.dlifeinner.cn", "port": 9100,
     "severity": "info", "hoursAgo": 13, "state": "resolved", "summary": "主机内存偏高（已恢复）",
     "description": "内存使用率曾达 91%，已回收", "value": "91", "logs": 24},
    {"alertname": "MySQLSlowQueries", "host": "order.mysql.ehomeinner.cn", "port": 3306,
     "severity": "warning", "hoursAgo": 23, "state": "resolved", "summary": "慢查询激增（已恢复）",
     "description": "慢查询曾达 42 条/分钟，索引优化后恢复", "value": "42", "logs": 61},
]

# ---- 1. 巡检：确保场景主机的 up 值符合预期 ----
for s in SCENARIOS:
    want = s.get("up", 1)
    if s["host"] not in UP:
        print("补充 up 表主机", s["host"])
    UP[s["host"]] = want
# 背景主机全部在线（含已恢复的 TargetDown，语义上已恢复上报）
for b in BACKGROUNDS:
    UP[b["host"]] = 1
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
# 背景告警追加在场景告警之后（场景排前，自动排障优先处理 D1–D4）
for b in BACKGROUNDS:
    labels = {"alertname": b["alertname"], "severity": b["severity"]}
    instance = ""
    if b.get("port"):
        instance = "%s:%s" % (b["host"], b["port"])
    elif not b.get("appId"):
        instance = b["host"]
    if instance:
        labels["instance"] = instance
    if b.get("appId"):
        labels["appId"] = b["appId"]
        labels["application"] = b["application"]
    alerts.append(
        {
            "labels": labels,
            "annotations": {"summary": b["summary"], "description": b["description"]},
            "state": b.get("state", "firing"),
            "activeAt": time.strftime(
                "%Y-%m-%dT%H:%M:%S+08:00", time.localtime(now - b["hoursAgo"] * 3600)
            ),
            "value": b["value"],
        }
    )
dump("prometheus/list_alerts.json", {"status": "success", "data": {"alerts": alerts}})

# ---- 3. 日志：按主机补错误日志量，供排障第 10 步 / 巡检 T10 使用 ----
log_counts = load("es/_log_counts.json", {"default": 5})
for s in SCENARIOS:
    log_counts.update(s.get("logs", {}))
for b in BACKGROUNDS:
    log_counts[b["host"]] = b.get("logs", 30)
dump("es/_log_counts.json", log_counts)

# ---- 4. 指标：场景 + 背景主机指标覆写（mockmcp 优先读该文件）----
# D3 失联主机覆写为全 0（失联不上报），与 up=0 语义一致。
overrides = {
    s["host"]: s["hostMetrics"] for s in SCENARIOS if s.get("hostMetrics")
}
overrides["sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn"] = {"cpuUsage": 0, "memUsage": 0, "load": 0}
for b in BACKGROUNDS:
    if b.get("metrics"):
        overrides[b["host"]] = b["metrics"]
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
