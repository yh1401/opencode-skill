# -*- coding: utf-8 -*-
"""独立审计：直接解析原始 JSON，重算报告中的关键数字"""
import io, os, json, collections

base = os.path.dirname(os.path.abspath(__file__))
def load(p):
    return json.load(io.open(os.path.join(base, ".tool_result_spill", p), encoding="utf-8"))

run2 = load("analyze_node_runtime_tool_20261105083839_4b11f01947c4.json")
scope = load("resolve_analysis_scope_tool_20261105083209_bf4fe24e1be1.json")
link = load("get_link_topology_tool_20261105084426_d2d7dbe97844.json")

appModules = scope.get("appModules", {})

print("=== 1) 各规则 RISK 项重算（原始 JSON） ===")
module_risk = collections.Counter()
rule_items = {}
for r in run2["rules"]:
    items = [it for it in r["items"] if it.get("statusCode") == "RISK"]
    rule_items[r["rule"]] = items
    for it in items:
        app = it["target"]["deployment"]
        for m in appModules.get(app, ["??未映射??"]):
            module_risk[m] += 1
    print(f'{r["rule"]}: risk={len(items)} (tool riskCount={r["riskCount"]})')

print("\n=== 2) 模块风险数：工具 moduleSummary vs 独立重算 ===")
for m in run2["moduleSummary"]:
    if m.get("appCount"):
        mine = module_risk.get(m["module"], 0)
        mark = "OK" if mine == m["riskCount"] else f"!! 不一致(工具={m['riskCount']}, 重算={mine})"
        print(f'  {m["module"]}: {mark}')
print("  重算合计(含双归属重复):", sum(module_risk.values()), " 工具 summary.riskItemCount:", run2["summary"]["riskItemCount"])

print("\n=== 3) restart_increase 重算 ===")
rst = collections.defaultdict(lambda: [0, 0])
for it in rule_items["restart_increase"]:
    app = it["target"]["deployment"]
    rst[app][0] += 1
    rst[app][1] = max(rst[app][1], int(it.get("observedValue") or 0))
print("  应用数:", len(rst), " Pod 合计:", sum(v[0] for v in rst.values()))
for app, (c, mx) in sorted(rst.items(), key=lambda kv: (-kv[1][0], -kv[1][1])):
    print(f"  {app}: pods={c} maxRestart={mx} modules={appModules.get(app)}")

print("\n=== 4) oom_killed 重算 ===")
oom = collections.Counter()
for it in rule_items["oom_killed"]:
    oom[it["target"]["deployment"]] += 1
for app, c in oom.most_common():
    print(f"  {app}: pods={c} modules={appModules.get(app)}")

print("\n=== 5) memory_ratio 12 项重算 ===")
for it in rule_items["memory_ratio"]:
    t = it["target"]
    print(f'  {t["deployment"]} ns={t["namespace"]} obs={it["observedValue"]*100:.1f}% modules={appModules.get(t["deployment"])}')

print("\n=== 6) deployment_replicas 风险项 ===")
for it in rule_items["deployment_replicas"]:
    t = it["target"]
    print(f'  {t["deployment"]} ns={t["namespace"]} spec={it["observedValue"]}')

print("\n=== 7) 链路：access-prod-002254-dep 出边分析 ===")
edges = link["data"]["edges"]
def app_of(name):
    n = name
    if n.endswith("_SPRING_BOOT"):
        n = n[:-len("_SPRING_BOOT")]
    if "Product_" in n:
        n = n.rsplit("Product_", 1)[1]
    return n

ACCESS = "access-prod-002254-dep"
out_edges = [e for e in edges if app_of(e["source"]) == ACCESS and not e["source"].endswith("_SPRING_BOOT")]
targets = set(e["target"] for e in out_edges)
print(f"  出边数(不含SPRING_BOOT自环)={len(out_edges)}  去重目标数={len(targets)}")
infra = {t for t in targets if t.upper() in ("REDIS", "REDIS_LETTUCE", "REDIS_JEDIS", "MYSQL", "USER", "TOMCAT") or t.startswith(("REDIS", "MYSQL"))}
app_targets = {t for t in targets if t not in infra and app_of(t) != ACCESS}
print(f"  其中基础设施(REDIS/MYSQL等)={len(infra)}: {sorted(infra)}")
print(f"  应用类去重目标={len(app_targets)}")
prov = [e for e in out_edges if ("gb-dispatcher" in e["target"] or "gb-access-gateway" in e["target"])]
prov3 = [e for e in prov if float(e.get("errorRate") or 0) >= 3.0]
print(f"  省级边总数={len(prov)}  其中错误率>=3% 的省级边={len(prov3)}")
all3 = [e for e in out_edges if float(e.get("errorRate") or 0) >= 3.0]
print(f"  全部出边中错误率>=3% 的边={len(all3)} (省级 {len(prov3)} + 非省级 {len(all3)-len(prov3)})")
for e in sorted(all3, key=lambda x: -float(x.get("errorRate") or 0)):
    print(f'    -> {app_of(e["target"])} total={e["totalCount"]} err={e["errorRate"]}% slow={e["slowCount"]}')
in_edges = [e for e in edges if app_of(e["target"]) == ACCESS and not e["target"].endswith("_SPRING_BOOT")]
print(f"  入边数(不含SPRING_BOOT)={len(in_edges)}  去重源={len(set(e['source'] for e in in_edges))}")

print("\n=== 8) 链路：dstatus -> access 边 ===")
for e in edges:
    if app_of(e["source"]) == "dstatus-prod-195938-dep" and app_of(e["target"]) == ACCESS:
        print(f'  total={e["totalCount"]} err={e["errorRate"]}% slow={e["slowCount"]} (src={e["source"]})')

print("\n=== 9) 链路：gb-stream 节点数据 ===")
for n in link["data"]["nodes"]:
    if "edrive-prod-gb-stream-182446" in n.get("id", ""):
        print(f'  id={n["id"]} running={n["nodeData"]["running"]} totalReq={n["nodeData"]["totalRequest"]} errReq={n["nodeData"]["errorRequest"]} slow={n["nodeData"]["slowRequest"]} maxRT={n["nodeData"]["maxResponseTime"]} avgRT={n["nodeData"]["avgResponseTime"]}')

print("\n=== 10) 链路：fusion -> device-control 边 ===")
for e in edges:
    if app_of(e["source"]) == "ehome-public-fusion-dep-dep" and app_of(e["target"]) == "vnet-vpaas-device-control-dep":
        print(f'  total={e["totalCount"]} err={e["errorRate"]}% slow={e["slowCount"]}')

print("\n=== 11) 链路：access 出边中调用量 top5 ===")
for e in sorted(out_edges, key=lambda x: -int(x["totalCount"]))[:5]:
    print(f'  -> {e["target"]} total={e["totalCount"]} err={e["errorRate"]}%')
