# -*- coding: utf-8 -*-
"""在原始链路 JSON 中检索 gb-stream 节点与 fusion/device-control 边"""
import io, os, json

base = os.path.dirname(os.path.abspath(__file__))
link = json.load(io.open(os.path.join(base, ".tool_result_spill", "get_link_topology_tool_20261105084426_d2d7dbe97844.json"), encoding="utf-8"))

print("== 节点中含 gb-stream 的 ==")
for n in link["data"]["nodes"]:
    nid = n.get("id", "") + n.get("name", "")
    if "gb-stream" in nid:
        print(json.dumps(n, ensure_ascii=False)[:600])

print("\n== 边中含 fusion 或 device-control 的 ==")
for e in link["data"]["edges"]:
    if "fusion" in e["source"] or "fusion" in e["target"] or "device-control" in e["source"] or "device-control" in e["target"]:
        print(json.dumps(e, ensure_ascii=False))
