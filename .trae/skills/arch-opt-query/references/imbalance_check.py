# -*- coding: utf-8 -*-
"""同规格分组复核 pod_cpu_imbalance：解析 per-pod CPU 时序 + CPU limit，按 (app, spec) 分组计算 max/avg"""
import io, os, re, json, collections

base = os.path.dirname(os.path.abspath(__file__))

def parse_prom_text(text):
    """解析 Prometheus MCP 文本格式: [metric]{labels} =>\nval @ts ..."""
    series = []
    blocks = re.split(r"\n(?=\S*\{)", text)
    for b in blocks:
        b = b.strip()
        m = re.search(r"\{([^}]*)\}\s*=>", b)
        if not m:
            continue
        labs = {}
        for kv in re.findall(r'(\w+)="([^"]*)"', m.group(1)):
            labs[kv[0]] = kv[1]
        vals = [float(x) for x in re.findall(r"=?>?\s*([0-9.eE+-]+) @\[", b)]
        if not vals:
            vals = [float(x) for x in re.findall(r"([0-9.eE+-]+) @\[", b)]
        series.append({"labels": labs, "values": vals})
    return series

# 1) per-pod CPU 时序
cpu_path = os.path.join(base, ".tool_result_spill", "range_query_20261105084925_2ec739ccaa32.json")
cpu_text = json.load(io.open(cpu_path, encoding="utf-8"))["result"]
cpu_series = parse_prom_text(cpu_text)

APP_PREFIX = {
    "ehome-geturls-232049-dep": "ehome-geturls-232049-dep",
    "ehome-public-hf-dep": "ehome-public-hf-dep",
    "sl-device-center-event-dep": "sl-device-center-event-dep",
}
pods = collections.defaultdict(list)  # app -> list of (pod, avg_cpu)
for s in cpu_series:
    pod = s["labels"].get("pod", "")
    app = next((a for a in APP_PREFIX if pod.startswith(a + "-")), None)
    if not app or not s["values"]:
        continue
    avg = sum(s["values"]) / len(s["values"])
    pods[app].append((pod, avg, s["labels"].get("cluster")))

# 2) CPU limits
lim_path = os.path.join(base, ".tool_result_spill", "query_20261105084944_a439189945dc.json")
lim_text = json.load(io.open(lim_path, encoding="utf-8"))["result"]
lim_series = parse_prom_text(lim_text)
pod_limit = {}
for s in lim_series:
    pod = s["labels"].get("pod", "")
    if s["values"]:
        pod_limit[pod] = s["values"][0]

print("== 各应用 Pod 数与规格分组（同规格 max/avg） ==")
result = {}
for app, plist in pods.items():
    groups = collections.defaultdict(list)
    for pod, avg, cl in plist:
        spec = pod_limit.get(pod)
        groups[spec].append((pod, avg))
    print(f"\n{app}: pods={len(plist)} (limit 缺失 {sum(1 for p,_,_ in plist if p not in pod_limit)} 个)")
    res = []
    for spec, gp in sorted(groups.items(), key=lambda kv: str(kv[0])):
        cpus = [c for _, c in gp]
        mx, av = max(cpus), sum(cpus) / len(cpus)
        ratio = mx / av if av > 0.001 else None
        flag = "RISK" if (ratio and ratio > 2.0) else ("跳过(均值≈0)" if (av and av <= 0.001) else "OK")
        print(f"  spec={spec} cores: pods={len(gp)} max={mx:.4f} avg={av:.4f} ratio={ratio if ratio is None else round(ratio,3)} -> {flag}")
        res.append({"spec": spec, "pods": len(gp), "max": round(mx, 4), "avg": round(av, 4), "ratio": round(ratio, 3) if ratio else None, "flag": flag})
    result[app] = res

op = os.path.join(base, "imbalance_recheck.json")
json.dump(result, io.open(op, "w", encoding="utf-8"), ensure_ascii=False, indent=1)
print("\nsaved:", op)
