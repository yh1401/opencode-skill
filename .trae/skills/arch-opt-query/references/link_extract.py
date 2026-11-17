# -*- coding: utf-8 -*-
"""提取链路拓扑：异常调用边（错误率/慢调用）、出入度统计、跨集群边"""
import io, os, json, collections, re

base = os.path.dirname(os.path.abspath(__file__))
p = os.path.join(base, ".tool_result_spill", "get_link_topology_tool_20261105084426_d2d7dbe97844.json")
d = json.load(io.open(p, encoding="utf-8"))

data = d.get("data", {})
edges = data.get("edges", [])
nodes = data.get("nodes", [])
print("nodes:", len(nodes), "edges:", len(edges))

def short(name):
    # 提取可读应用名
    m = re.search(r"Product_([A-Za-z0-9_\-]+)$", name)
    if m:
        return m.group(1)
    if name in ("REDIS", "REDIS_LETTUCE", "REDIS_JEDIS", "MYSQL", "USER", "TOMCAT"):
        return name
    return name

# 只关注广州4d相关节点（tykj-guangzhou4d-az-prod / guangzhou4d-prod 前缀）+ 其直接上下游
gz_prefixes = ("tykj-guangzhou4d-az-prod", "guangzhou4d-prod")
def is_gz(n):
    return any(n.startswith(x) for x in gz_prefixes)

indeg = collections.Counter()
outdeg = collections.Counter()
anomalous = []
cross_region = []
for e in edges:
    s, t = e.get("source"), e.get("target")
    er = float(e.get("errorRate") or 0)
    sc = int(e.get("slowCount") or 0)
    tc = int(e.get("totalCount") or 0)
    if is_gz(s):
        outdeg[short(s)] += 1
    if is_gz(t):
        indeg[short(t)] += 1
    if (is_gz(s) or is_gz(t)) and (er >= 1.0 or sc >= 10):
        anomalous.append({"src": short(s), "dst": short(t), "total": tc, "errRate": er, "slow": sc})
    # 跨集群调用（广州4d 应用 ↔ 非广州4d 应用）
    if is_gz(s) and not is_gz(t) and not t.startswith(("REDIS", "MYSQL", "USER", "Tomcat", "TOMCAT")):
        if tc >= 50:
            cross_region.append({"src": short(s), "dst": short(t), "total": tc, "errRate": er, "slow": sc})
    elif is_gz(t) and not is_gz(s) and not s.startswith(("REDIS", "MYSQL", "USER", "Tomcat", "TOMCAT")):
        if tc >= 50:
            cross_region.append({"src": short(s), "dst": short(t), "total": tc, "errRate": er, "slow": sc})

print("\n== 异常调用边（错误率>=1% 或 慢调用>=10，广州4d相关） ==")
for a in sorted(anomalous, key=lambda x: (-x["errRate"], -x["slow"])):
    print(f'  {a["src"]} -> {a["dst"]} | total={a["total"]} errRate={a["errRate"]}% slow={a["slow"]}')

print("\n== 跨集群调用边（广州4d <-> 其他节点，total>=50） ==")
seen = set()
for a in sorted(cross_region, key=lambda x: -x["total"])[:30]:
    k = (a["src"], a["dst"])
    if k in seen:
        continue
    seen.add(k)
    print(f'  {a["src"]} -> {a["dst"]} | total={a["total"]} errRate={a["errRate"]}% slow={a["slow"]}')

print("\n== 出度 Top15（广州4d应用） ==")
for n, c in outdeg.most_common(15):
    print(f"  {n}: out={c}")
print("\n== 入度 Top15（广州4d应用） ==")
for n, c in indeg.most_common(15):
    print(f"  {n}: in={c}")

# 保存精简结果
out = {"edgeCount": len(edges), "nodeCount": len(nodes), "anomalous": anomalous,
       "crossRegion": cross_region[:40],
       "outTop": outdeg.most_common(20), "inTop": indeg.most_common(20)}
op = os.path.join(base, "link_summary.json")
json.dump(out, io.open(op, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
print("\nsaved:", op)
