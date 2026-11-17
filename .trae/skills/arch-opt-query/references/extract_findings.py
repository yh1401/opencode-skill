# -*- coding: utf-8 -*-
import io, os, json

base = os.path.dirname(os.path.abspath(__file__))
p = os.path.join(base, ".tool_result_spill", "analyze_node_runtime_tool_20261105083225_fb0fb344976e.json")
d = json.load(io.open(p, encoding="utf-8"))

print("top keys:", list(d.keys()))
fs = d.get("findings", [])
print("findings count:", len(fs))
if fs:
    print("first finding keys:", list(fs[0].keys()))
    print(json.dumps(fs[0], ensure_ascii=False, indent=1)[:1500])
    print("---second---")
    print(json.dumps(fs[1], ensure_ascii=False, indent=1)[:1500])
# 检查 rules 里各规则 items 数量
for r in d.get("rules", []):
    print(r["rule"], "items:", len(r.get("items", [])), "truncated:", r.get("itemsTruncated"))
