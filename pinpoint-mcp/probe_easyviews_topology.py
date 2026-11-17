#!/usr/bin/env python3
"""EasyViews 链路拓扑探针（只做测量，不修改服务端代码）

用法：
    python probe.py discover --env-file <env> --out discover.json
    python probe.py scale   --env-file <env> --node "云公司->贵州" --out scale.json
"""
import argparse
import asyncio
import json
import os
import re
import statistics
import sys
import time
from pathlib import Path

import httpx

PROXY_PATH = "/api/third/thirdAuth/getTykjProvince"
BASE_URL = os.environ.get("EASYVIEWS_BASE_URL", "https://easyviews.tech.ctseelink.cn")
TIMEOUT = 300.0


def load_env(path: str | None) -> dict:
    data = {}
    if not path:
        return data
    p = Path(path)
    try:
        text = p.read_text(encoding="utf-8-sig")
    except PermissionError:
        raise SystemExit(
            f"无权读取 {p}（mcp.env 通常为 600 且属主为 mcp）。\n"
            "解决办法（任选其一）：\n"
            "  1) 用服务用户运行：sudo -u mcp ... --env-file mcp.env\n"
            "  2) 不传 --env-file，改由当前进程环境变量提供 EASYVIEWS_APP_ID/SECRET"
        )
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        m = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$", line)
        if m:
            data[m.group(1).strip()] = m.group(2).strip()
    return data


def pick(env: dict, *names: str) -> str | None:
    for n in names:
        v = env.get(n) or os.environ.get(n)
        if v:
            return v
    return None


def make_creds(env: dict) -> dict:
    app_id = pick(env, "EASYVIEWS_APP_ID", "PROMETHEUS_MCP_SERVER_EASYVIEWS_APP_ID") or ""
    secret = pick(env, "EASYVIEWS_APP_SECRET", "PROMETHEUS_MCP_SERVER_EASYVIEWS_APP_SECRET") or ""
    return {
        "province": pick(env, "EASYVIEWS_PROVINCE", "PROMETHEUS_MCP_SERVER_EASYVIEWS_PROVINCE") or "大模型",
        "appId": app_id,
        "appSecret": secret,
        "baseUrl": pick(env, "EASYVIEWS_BASE_URL") or BASE_URL,
        "proxyPath": pick(env, "EASYVIEWS_PROXY_PATH") or PROXY_PATH,
        "product": pick(env, "ARCH_PRODUCT_NAME") or "天翼看家",
    }


def creds_summary(creds: dict) -> dict:
    """输出生效配置（凭据脱敏），便于核对是否与服务端 mcp.env 一致"""
    aid = creds["appId"]
    return {
        "baseUrl": creds["baseUrl"],
        "proxyPath": creds["proxyPath"],
        "province": creds["province"],
        "appIdPreview": (aid[:4] + "***" + aid[-2:]) if len(aid) >= 8 else "(空/过短)",
        "appIdLength": len(aid),
        "secretLength": len(creds["appSecret"]),
        "product": creds["product"],
    }


async def proxy_call(client, creds, url, types="GET", req_param=None, raw_url=False):
    payload = {
        "url": url,
        "types": types,
        "provinceDetail": {
            "province": creds["province"],
            "appId": creds["appId"],
            "appSecret": creds["appSecret"],
        },
        "reqParam": req_param or {},
    }
    url_full = url if raw_url or url.startswith("http") else creds["baseUrl"].rstrip("/") + url
    payload["url"] = url_full
    t0 = time.perf_counter()
    try:
        resp = await client.post(creds["proxyPath"], json=payload, timeout=TIMEOUT)
        elapsed = time.perf_counter() - t0
        try:
            body = resp.json()
        except ValueError:
            body = None
        ok = resp.status_code < 400 and isinstance(body, dict) and body.get("code") in (0, None)
        return {
            "ok": ok,
            "httpStatus": resp.status_code,
            "code": (body or {}).get("code") if isinstance(body, dict) else None,
            "msg": (body or {}).get("msg") if isinstance(body, dict) else None,
            "elapsed": round(elapsed, 3),
            "bytes": len(resp.content),
            "body": body,
        }
    except Exception as e:  # noqa: BLE001
        return {
            "ok": False,
            "elapsed": round(time.perf_counter() - t0, 3),
            "bytes": 0,
            "error": f"{type(e).__name__}: {str(e)[:200]}",
        }


def projects_of(body) -> list[str]:
    data = (body or {}).get("data") or []
    out = []
    deploy = None
    for item in data:
        if deploy is None:
            deploy = item.get("deployNode")
        for n in (item.get("topologyData") or {}).get("nodes") or []:
            for p in n.get("projectName") or []:
                if p and p not in out:
                    out.append(p)
    return out, deploy


def _resolve_arch_dir() -> str:
    """定位 arch_tools.py 所在目录，用于复用 _extract_graph 统计调用边"""
    cand = os.environ.get("ARCH_TOOLS_DIR")
    if cand:
        return cand
    here = Path(__file__).resolve()
    for item in (
        here.parent,
        Path("/home/fortress/pinpoint-mcp/pinpoint-mcp-server"),
    ):
        if (item / "arch_tools.py").exists():
            return str(item)
    return ""


ARCH_DIR = _resolve_arch_dir()
_EXTRACT = None
_ARCH_TRIED = False


def _ensure_arch_tools() -> None:
    """延迟导入 arch_tools：确保 os.environ 已注入 mcp.env 配置后再加载 config"""
    global _EXTRACT, _ARCH_TRIED
    if _ARCH_TRIED:
        return
    _ARCH_TRIED = True
    if not ARCH_DIR:
        return
    sys.path.insert(0, ARCH_DIR)
    try:
        from arch_tools import _extract_graph as extract  # type: ignore

        _EXTRACT = extract
        print(f"[probe] 复用 {ARCH_DIR}/arch_tools.py 解析调用边", flush=True)
    except Exception as e:  # noqa: BLE001
        print(f"[probe] arch_tools 载入失败，边数将记为 -1: {type(e).__name__}: {e}", flush=True)


def edge_count(body) -> int:
    _ensure_arch_tools()
    if _EXTRACT is None:
        return -1
    try:
        return len(_EXTRACT((body or {}).get("data"))["edges"])
    except Exception:
        return -1


def stats(samples: list[dict]) -> dict:
    if not samples:
        return {}
    el = [s["elapsed"] for s in samples]
    return {
        "n": len(samples),
        "min": round(min(el), 3),
        "median": round(statistics.median(el), 3),
        "max": round(max(el), 3),
        "okCount": sum(1 for s in samples if s["ok"]),
        "avgBytes": int(statistics.mean([s["bytes"] for s in samples])),
    }


async def discover(args, client, creds):
    nodes_resp = await proxy_call(
        client, creds, "/api/external/monitorData/getNode", "GET", {"center": "true"}
    )
    raw = (nodes_resp.get("body") or {}).get("data") or []
    if args.nodes:
        nodes = [x.strip() for x in args.nodes.split(",") if x.strip()]
    else:
        nodes = [n.get("node") for n in raw if str((n or {}).get("node", "")).startswith(args.prefix)]
    result = {
        "nodeListElapsed": nodes_resp["elapsed"],
        "nodeListOk": nodes_resp["ok"],
        "nodeListCode": nodes_resp.get("code"),
        "nodeListMsg": nodes_resp.get("msg"),
        "nodeListError": nodes_resp.get("error"),
        "nodeListBytes": nodes_resp["bytes"],
        "nodeListRawCount": len(raw),
        "nodeListSample": raw[:3],
        "nodeCount": len(nodes),
        "items": [],
    }
    for node in nodes:
        info = await topology_meta(client, creds, node, args.product)
        info["node"] = node
        result["items"].append(info)
        print(f"[discover] {node}: projects={info.get('projectCount')} "
              f"elapsed={info.get('elapsed')}s ok={info.get('ok')}", flush=True)
    return result


async def topology_meta(client, creds, node, product):
    resp = await proxy_call(
        client, creds,
        "/api/autoOperation/topology/getTopologyByProductName",
        "GET",
        {"center": "true", "node": node, "productName": product},
    )
    projects, deploy = projects_of(resp.get("body"))
    preview = json.dumps(resp.get("body"), ensure_ascii=False)[:300] if not resp["ok"] else None
    return {
        "ok": resp["ok"],
        "bodyPreview": preview,
        "httpStatus": resp.get("httpStatus"),
        "code": resp.get("code"),
        "msg": resp.get("msg"),
        "error": resp.get("error"),
        "elapsed": resp["elapsed"],
        "bytes": resp["bytes"],
        "projectCount": len(projects),
        "deployNode": deploy,
        "deployNodeType": type(deploy).__name__,
        "projects": projects,
    }


async def topo_call(client, creds, node, projects, minute, ranges=(1, 1)):
    return await proxy_call(
        client, creds,
        "/api/autoOperation/topology/getTopologyByNodeProject/",
        "POST",
        {
            "center": "true",
            "calleeRange": ranges[1],
            "callerRange": ranges[0],
            "filterNode": False,
            "minute": minute,
            "nodeType": "",
            "node": node,
            "projects": projects,
        },
    )


def summarize_case(resp: dict, projects_count: int, label: str) -> dict:
    return {
        "case": label,
        "projects": projects_count,
        "ok": resp["ok"],
        "httpStatus": resp.get("httpStatus"),
        "code": resp.get("code"),
        "msg": resp.get("msg"),
        "error": resp.get("error"),
        "elapsed": resp["elapsed"],
        "bytes": resp["bytes"],
        "edgeCount": edge_count(resp.get("body")) if resp["ok"] else -1,
    }


async def scale(args, client, creds, projects, node, minute):
    if args.sizes:
        sizes = sorted({int(x) for x in args.sizes.split(",") if x.strip()} & set(range(1, len(projects) + 1)))
    else:
        sizes = sorted({1, 8, 20, 40, len(projects)} & set(range(1, len(projects) + 1)))
    results = []
    for size in sizes:
        samples = []
        for i in range(args.repeat):
            r = await topo_call(client, creds, node, projects[:size], minute)
            item = summarize_case(r, size, f"scale-{size}-r{i}")
            samples.append(item)
            results.append(item)
            print(f"[scale] projects={size} minute={minute} elapsed={item['elapsed']}s "
                  f"bytes={item['bytes']} edges={item['edgeCount']} ok={item['ok']}", flush=True)
        print(f"[scale] projects={size} summary={stats(samples)}", flush=True)
    return {"node": node, "minute": minute, "totalProjects": len(projects),
            "cases": results, "summary": {
                str(s): stats([r for r in results if r["case"].startswith(f"scale-{s}-")])
                for s in sizes
            }}


async def window(args, client, creds, projects, node):
    minutes = [5, 10, 15, 30]
    results = []
    for m in minutes:
        samples = []
        for i in range(args.repeat):
            r = await topo_call(client, creds, node, projects, m)
            item = summarize_case(r, len(projects), f"window-{m}-r{i}")
            samples.append(item)
            results.append(item)
            print(f"[window] minute={m} elapsed={item['elapsed']}s bytes={item['bytes']} "
                  f"edges={item['edgeCount']} ok={item['ok']}", flush=True)
        print(f"[window] minute={m} summary={stats(samples)}", flush=True)
    return {"node": node, "cases": results,
            "summary": {str(m): stats([r for r in results if r["case"].startswith(f"window-{m}-")])
                        for m in minutes}}


async def concurrency(args, client, creds, projects, node, minute):
    chunk = args.chunk
    chunks = [projects[i:i + chunk] for i in range(0, len(projects), chunk)]
    results = []
    for conc in args.concurrencies:
        for rep in range(args.repeat):
            sem = asyncio.Semaphore(conc)
            t0 = time.perf_counter()
            per_batch = []

            async def one(idx, batch):
                async with sem:
                    r = await topo_call(client, creds, node, batch, minute)
                    per_batch.append(summarize_case(r, len(batch), f"batch-{conc}-{rep}-{idx}"))
                    return r

            await asyncio.gather(*(one(i, b) for i, b in enumerate(chunks)))
            wall = round(time.perf_counter() - t0, 3)
            ok = sum(1 for b in per_batch if b["ok"])
            rec = {
                "concurrency": conc,
                "chunk": chunk,
                "batchCount": len(chunks),
                "repeat": rep,
                "wall": wall,
                "okBatches": ok,
                "failedBatches": len(chunks) - ok,
                "batchElapsed": [b["elapsed"] for b in per_batch],
                "batchMedian": round(statistics.median([b["elapsed"] for b in per_batch]), 3),
                "batchMax": round(max(b["elapsed"] for b in per_batch), 3),
            }
            results.append(rec)
            print(f"[concurrency] conc={conc} chunk={chunk} rep={rep} wall={wall}s "
                  f"ok={ok}/{len(chunks)} batchMed={rec['batchMedian']}s", flush=True)
    return {"node": node, "minute": minute, "totalProjects": len(projects), "runs": results}


async def matrix(args, client, creds):
    """对比 url 形态与接口，定位 code=7 是鉴权还是请求结构问题"""
    base = creds["baseUrl"].rstrip("/")
    node = args.node or "云公司->贵州"
    cases = []
    for name, path, kind in (
        ("getNode-absolute", "/api/external/monitorData/getNode", "node"),
        ("getNode-relative", "/api/external/monitorData/getNode", "node"),
        ("topology-absolute", "/api/autoOperation/topology/getTopologyByProductName", "topo"),
        ("topology-relative", "/api/autoOperation/topology/getTopologyByProductName", "topo"),
    ):
        url = base + path if name.endswith("absolute") else path
        req = {"center": "true"}
        if kind == "topo":
            req.update({"node": node, "productName": args.product})
        r = await proxy_call(client, creds, url, "GET", req, raw_url=name.endswith("relative"))
        item = {
            "case": name,
            "urlSent": (base + path) if name.endswith("absolute") else path,
            "ok": r["ok"],
            "code": r.get("code"),
            "msg": r.get("msg"),
            "error": r.get("error"),
            "elapsed": r["elapsed"],
            "bytes": r["bytes"],
            "bodyPreview": None if r["ok"] else json.dumps(r.get("body"), ensure_ascii=False)[:300],
        }
        cases.append(item)
        print(f"[matrix] {name}: ok={item['ok']} code={item['code']} "
              f"msg={item['msg']} elapsed={item['elapsed']}s", flush=True)
    return {"node": node, "cases": cases}


async def nodes_summary(args, client, creds):
    """只输出各节点 projectCount，便于定位目标节点"""
    out = await discover(args, client, creds)
    for item in out["items"]:
        item.pop("projects", None)
    out["items"].sort(key=lambda x: x.get("projectCount") or 0, reverse=True)
    for item in out["items"]:
        print(f"[nodes] {item['node']}: projects={item.get('projectCount')} "
              f"ok={item['ok']} elapsed={item['elapsed']}s", flush=True)
    return out


async def e2e(args, creds, node, projects, minute):
    """直接调用 arch_tools.analyze_link_topology，测端到端耗时（取数 + 图计算 + 序列化）"""
    _ensure_arch_tools()
    if not ARCH_DIR:
        raise SystemExit("未找到 arch_tools.py，无法端到端计时")
    import arch_tools  # type: ignore

    # 阶段 1：单独测 EasyViews 取数
    t_fetch = time.perf_counter()
    raw = await arch_tools.get_link_topology(
        node=node,
        projects=projects,
        minute=minute,
        caller_range=args.caller_range,
        callee_range=args.callee_range,
    )
    fetch_elapsed = time.perf_counter() - t_fetch
    raw_bytes = len(json.dumps(raw, ensure_ascii=False, default=str).encode())

    # 阶段 2：完整工具（含图计算 + 序列化）
    t0 = time.perf_counter()
    result = await arch_tools.analyze_link_topology(
        node=node,
        projects=projects,
        minute=minute,
        caller_range=args.caller_range,
        callee_range=args.callee_range,
    )
    elapsed = time.perf_counter() - t0
    payload = json.dumps(result, ensure_ascii=False, default=str)
    graph = result.get("graph") or {}
    out = {
        "node": node,
        "minute": minute,
        "projectCount": len(projects),
        "callerRange": args.caller_range,
        "calleeRange": args.callee_range,
        "elapsed": round(elapsed, 3),
        "fetchElapsed": round(fetch_elapsed, 3),
        "fetchBytes": raw_bytes,
        "graphElapsed": round(max(0.0, elapsed - fetch_elapsed), 3),
        "resultBytes": len(payload.encode()),
        "status": result.get("status"),
        "graph": {k: graph.get(k) for k in ("nodeCount", "edgeCount", "parseMode")},
        "findingsCount": len(result.get("findings") or []),
        "dataLimitations": result.get("dataLimitations"),
    }
    print(f"[e2e] projects={len(projects)} minute={minute} "
          f"callerRange={args.caller_range} calleeRange={args.callee_range} "
          f"fetch={out['fetchElapsed']}s ({out['fetchBytes']}B) "
          f"graph={out['graphElapsed']}s total={out['elapsed']}s "
          f"result={out['resultBytes']}B status={out['status']} "
          f"findings={out['findingsCount']}", flush=True)
    return out


async def main():
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "cmd", choices=["matrix", "nodes", "discover", "scale", "window", "concurrency", "e2e"]
    )
    ap.add_argument("--env-file")
    ap.add_argument("--node")
    ap.add_argument("--product", default="", help="产品名，默认取 mcp.env 的 ARCH_PRODUCT_NAME 或 天翼看家")
    ap.add_argument("--prefix", default="云公司->")
    ap.add_argument("--nodes", default="", help="逗号分隔的节点名，指定后跳过节点列表接口")
    ap.add_argument("--minute", type=int, default=15)
    ap.add_argument("--repeat", type=int, default=3)
    ap.add_argument("--sizes", default="", help="scale 子命令的项目数档位，逗号分隔")
    ap.add_argument("--chunk", type=int, default=12)
    ap.add_argument("--concurrencies", type=str, default="1,2,3,4")
    ap.add_argument("--caller-range", type=int, default=1)
    ap.add_argument("--callee-range", type=int, default=1)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    env = load_env(args.env_file)
    for k, v in env.items():
        os.environ.setdefault(k, v)
    creds = make_creds(env)
    if not (creds["appId"] and creds["appSecret"]):
        raise SystemExit("缺少 EasyViews 凭据")
    args.concurrencies = [int(x) for x in args.concurrencies.split(",") if x.strip()]
    args.product = args.product or creds["product"]
    print(f"[config] {json.dumps(creds_summary(creds), ensure_ascii=False)}", flush=True)

    async with httpx.AsyncClient(base_url=creds["baseUrl"], timeout=TIMEOUT) as client:
        if args.cmd == "discover":
            out = await discover(args, client, creds)
        elif args.cmd == "matrix":
            out = await matrix(args, client, creds)
        elif args.cmd == "nodes":
            out = await nodes_summary(args, client, creds)
        else:
            if not args.node:
                raise SystemExit("--node 必填")
            meta = await topology_meta(client, creds, args.node, args.product)
            projects = meta["projects"]
            print(f"[meta] node={args.node} projects={len(projects)} "
                  f"deployNode={meta['deployNode']!r} elapsed={meta['elapsed']}s", flush=True)
            if not projects:
                raise SystemExit("模块拓扑未返回 projectName")
            if meta["deployNode"]:
                node = meta["deployNode"]
            else:
                node = args.node
            if isinstance(node, list):
                node = node[0]
            if args.cmd == "scale":
                out = await scale(args, client, creds, projects, node, args.minute)
            elif args.cmd == "window":
                out = await window(args, client, creds, projects, node)
            elif args.cmd == "e2e":
                out = await e2e(args, creds, node, projects, args.minute)
            else:
                out = await concurrency(args, client, creds, projects, node, args.minute)
            out["meta"] = {k: v for k, v in meta.items() if k != "projects"}
            out["projects"] = projects

    out["config"] = creds_summary(creds)
    Path(args.out).write_text(json.dumps(out, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"written: {args.out}", flush=True)


if __name__ == "__main__":
    asyncio.run(main())
