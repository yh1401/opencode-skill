# -*- coding: utf-8 -*-
"""从第二次 top_n=200 全量结果提取：各规则完整 items、按应用聚合、namespace 映射"""
import io, os, json, collections

base = os.path.dirname(os.path.abspath(__file__))
p = os.path.join(base, ".tool_result_spill", "analyze_node_runtime_tool_20261105083839_4b11f01947c4.json")
d = json.load(io.open(p, encoding="utf-8"))

out = {"summary": d["summary"], "rules": {}, "moduleSummary": [m for m in d["moduleSummary"] if m.get("appCount")],
       "dataLimitations": d["dataLimitations"]}

ns_map = {}
for r in d.get("rules", []):
    items = []
    for it in r.get("items", []):
        t = it.get("target", {})
        dep = t.get("deployment")
        if dep:
            ns_map[dep] = t.get("namespace")
        items.append({
            "deployment": dep, "namespace": t.get("namespace"), "pod": t.get("pod"),
            "status": it.get("statusCode"), "obs": it.get("observedValue"),
            "max": it.get("maxValue"), "min": it.get("minValue"), "reason": it.get("reason", "")[:100]
        })
    out["rules"][r["rule"]] = {
        "status": r["status"], "targetCount": r.get("targetCount"), "riskCount": r.get("riskCount"),
        "insufficientCount": r.get("insufficientCount"), "naCount": r.get("naCount"),
        "okCount": r.get("okCount"), "offlineCount": r.get("offlineCount"),
        "itemsTruncated": r.get("itemsTruncated"), "items": items
    }

out["nsMap"] = ns_map

# 按应用聚合稳定性风险
agg = collections.defaultdict(lambda: collections.defaultdict(int))
for f in d.get("findings", []):
    a = f.get("app")
    if a:
        agg[a][f.get("rule")] += 1
# 从 rules items 聚合（findings 只有 top30）
for rule in ("oom_killed", "restart_increase"):
    for it in out["rules"][rule]["items"]:
        dep = it["deployment"]
        if dep:
            agg[dep][rule] = agg[dep].get(rule, 0)
out["appAgg"] = {k: dict(v) for k, v in sorted(agg.items())}

# 完整写出
op = os.path.join(base, "full_findings.json")
json.dump(out, io.open(op, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
print("saved:", op)

# 打印关键摘要
print("\n== 各规则状态 ==")
for rule, r in out["rules"].items():
    print(f'{rule}: status={r["status"]} target={r["targetCount"]} risk={r["riskCount"]} insuf={r["insufficientCount"]} na={r["naCount"]} ok={r["okCount"]} offline={r["offlineCount"]} items={len(r["items"])} truncated={r["itemsTruncated"]}')

print("\n== deployment_available 风险项（如有） ==")
for it in out["rules"]["deployment_available"]["items"]:
    if it["status"] == "RISK":
        print(f'  {it["deployment"]} ns={it["namespace"]} obs={it["obs"]} | {it["reason"]}')

print("\n== memory_ratio 全部风险项 ==")
for it in out["rules"]["memory_ratio"]["items"]:
    if it["status"] == "RISK":
        print(f'  {it["deployment"]} ns={it["namespace"]} pod={it["pod"]} obs={it["obs"]}')

print("\n== pod_cpu_imbalance 全部风险项 ==")
for it in out["rules"]["pod_cpu_imbalance"]["items"]:
    if it["status"] == "RISK":
        print(f'  {it["deployment"]} ns={it["namespace"]} obs={it["obs"]} | {it["reason"]}')

print("\n== oom_killed 按应用聚合 ==")
oom = collections.defaultdict(int)
for it in out["rules"]["oom_killed"]["items"]:
    if it["status"] == "RISK":
        oom[(it["deployment"], it["namespace"])] += 1
for (dep, ns), c in sorted(oom.items(), key=lambda kv: -kv[1]):
    print(f"  {dep} ns={ns} pods={c}")

print("\n== restart_increase 按应用聚合（top 20） ==")
rst = collections.defaultdict(lambda: [0, 0])
for it in out["rules"]["restart_increase"]["items"]:
    if it["status"] == "RISK":
        k = (it["deployment"], it["namespace"])
        rst[k][0] += 1
        rst[k][1] = max(rst[k][1], int(it["obs"] or 0))
for (dep, ns), (c, mx) in sorted(rst.items(), key=lambda kv: (-kv[1][0], -kv[1][1]))[:20]:
    print(f"  {dep} ns={ns} pods={c} maxRestart={mx}")
print(f"  ...共 {len(rst)} 个应用")

print("\n== nsMap（供复核用） ==")
print(json.dumps(ns_map, ensure_ascii=False))
