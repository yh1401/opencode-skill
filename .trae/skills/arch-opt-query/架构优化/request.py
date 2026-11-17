import os
import requests
import json
import time
from typing import List, Dict, Any
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

# ===================== 配置区 =====================
# 拓扑接口地址
TOPO_API_URL = "https://easyviews.tech.ctseelink.cn/api/third/thirdAuth/getTykjProvince"
# Prometheus 即时查询接口（只能用这个）
PROM_HOST = "http://183.56.210.90:8082"
PROM_QUERY_API = f"{PROM_HOST}/api/v1/query"

# 两个待查询node
NODE_LIST = [
    {
        "node": "云公司->贵州",
        "province": "大模型",
        "appId": os.getenv("EASYVIEWS_APP_ID", ""),
        "appSecret": os.getenv("EASYVIEWS_APP_SECRET", "")
    },
    {
        "node": "云公司->广州4d",
        "province": "大模型",
        "appId": os.getenv("EASYVIEWS_APP_ID", ""),
        "appSecret": os.getenv("EASYVIEWS_APP_SECRET", "")
    }
]
PRODUCT_NAME = "天翼看家"
TIME_DURATION = 15 * 60
STEP = 60
# 【重要】替换成你实际业务的PromQL模板
PROMQL_TPL = 'sum(your_metric{app="$APP_NAME"})'

# Prometheus查询配置
PROM_RETRY_MAX = 3
PROM_QUERY_INTERVAL = 1  # 每个时间点查询间隔，避免触发10053断连
# =================================================

def create_retry_session():
    retry_strategy = Retry(
        total=PROM_RETRY_MAX,
        backoff_factor=1,
        status_forcelist=[429,500,502,503,504]
    )
    adapter = HTTPAdapter(max_retries=retry_strategy)
    session = requests.Session()
    session.mount("https://", adapter)
    session.mount("http://", adapter)
    return session

session = create_retry_session()

def fetch_topology(node_conf: Dict[str, str]) -> List[Dict[str, Any]]:
    payload = {
        "url": "https://easyviews.tech.ctseelink.cn/api/autoOperation/topology/getTopologyByProductName",
        "types": "GET",
        "provinceDetail": {
            "province": node_conf["province"],
            "appId": node_conf["appId"],
            "appSecret": node_conf["appSecret"]
        },
        "reqParam": {
            "center": "true",
            "node": node_conf["node"],
            "productName": PRODUCT_NAME
        }
    }
    resp = session.post(TOPO_API_URL, json=payload, timeout=30)
    resp.raise_for_status()
    ret = resp.json()
    all_affiliations = []
    data_list = ret.get("data", [])
    for data_item in data_list:
        topology_data = data_item.get("topologyData", {})
        nodes = topology_data.get("nodes", [])
        for n in nodes:
            pn_raw = n.get("projectName")
            if isinstance(pn_raw, list):
                if len(pn_raw) > 0:
                    pn = str(pn_raw[0])
                else:
                    pn = ""
            elif pn_raw is None:
                pn = ""
            else:
                pn = str(pn_raw)

            aff_item = {
                "source_node": node_conf["node"],
                "module_id": n.get("模块标识"),
                "module_cn": n.get("中文名称"),
                "obj_type": n.get("对象类型"),
                "projectName": pn_raw,
                "app_unique_key": pn
            }
            all_affiliations.append(aff_item)
    return all_affiliations


def query_prom_instant(app_name: str, query_time: int) -> Dict[str, Any]:
    """
    即时查询接口 /api/v1/query，可指定历史时间戳
    :param app_name: 应用名
    :param query_time: unix时间戳（秒）
    """
    promql = PROMQL_TPL.replace("$APP_NAME", app_name)
    params = {
        "query": promql,
        "time": query_time
    }
    headers = {"Connection": "close"}
    resp = session.get(PROM_QUERY_API, params=params, timeout=25, headers=headers)
    resp.raise_for_status()
    return resp.json()


def collect_15min_samples(app_name: str, end_ts: int) -> tuple[bool, list, list]:
    """
    循环调用即时接口，采集15分钟历史采样点
    end_ts: 结束时间戳（当前）
    返回 (success_flag, samples, error_list)
    """
    samples = []
    errors = []
    # 生成15个时间点，向前倒推
    for offset in range(0, TIME_DURATION+1, STEP):
        ts = end_ts - offset
        try:
            resp = query_prom_instant(app_name, ts)
            res_data = resp.get("data", {}).get("result", [])
            val = None
            if res_data:
                # value格式 [timestamp, "value_str"]
                _, val_str = res_data[0]["value"]
                val = float(val_str)
            samples.append({"timestamp": ts, "value": val})
        except Exception as e:
            err_msg = f"时间点{ts}查询失败: {str(e)}"
            errors.append(err_msg)
        time.sleep(PROM_QUERY_INTERVAL)
    if len(errors) > 0:
        return False, samples, errors
    return True, samples, errors


def check_continuous_15min(samples: list, threshold: float) -> bool:
    """
    判断全部有效样本点是否都满足阈值条件
    samples: collect_15min_samples返回的采样列表
    threshold: 判断阈值
    """
    valid_points = [p for p in samples if p["value"] is not None]
    if len(valid_points) == 0:
        return False
    all_meet = True
    for p in valid_points:
        if p["value"] <= threshold:
            all_meet = False
    return all_meet


def main():
    total_affiliations = []
    for node_cfg in NODE_LIST:
        affs = fetch_topology(node_cfg)
        total_affiliations.extend(affs)
        print(f"【{node_cfg['node']}】获取拓扑节点数量：{len(affs)}")

    app_map: Dict[str, Dict[str, Any]] = {}
    not_cover_list: List[Dict[str, Any]] = []
    for aff in total_affiliations:
        app_name_raw = aff.get("projectName")
        if isinstance(app_name_raw, list):
            if len(app_name_raw) > 0:
                app_name = str(app_name_raw[0])
            else:
                app_name = ""
        elif app_name_raw is None:
            app_name = ""
        else:
            app_name = str(app_name_raw)

        if not app_name:
            not_cover_list.append(aff)
            continue
        if app_name.endswith("-dep"):
            unique_key = aff["app_unique_key"]
            if unique_key not in app_map:
                app_map[unique_key] = {
                    "app_name": app_name,
                    "aff_list": [],
                    "metric_result": None
                }
            app_map[unique_key]["aff_list"].append(aff)
        else:
            not_cover_list.append(aff)

    print(f"\n全量归属记录总数：{len(total_affiliations)}")
    print(f"去重后待Prometheus查询应用数量：{len(app_map)}")
    print(f"本阶段未覆盖应用数量：{len(not_cover_list)}")

    end_ts = int(time.time())
    # 遍历每个应用
    for unique_key, app_info in app_map.items():
        app_nm = app_info["app_name"]
        print(f"\n==== 开始采集应用：{app_nm} 的15分钟指标 ====")
        try:
            success, samples, errors = collect_15min_samples(app_nm, end_ts)
            # 阈值按需修改
            threshold = 0.8
            continuous_ok = check_continuous_15min(samples, threshold)
            metric_result = {
                "samples": samples,
                "continuous_15min_check": continuous_ok,
                "query_success": success,
                "errors": errors
            }
            app_info["metric_result"] = metric_result
            print(f"✅ {app_nm} 采集完成，持续15分钟达标？ {continuous_ok}")
        except Exception as e:
            app_info["metric_result"] = {"error": str(e)}
            print(f"❌ 应用整体采集失败：{app_nm}, err:{e}")

    output_result = {
        "checked_app_affiliations": [],
        "not_cover": not_cover_list
    }
    for unique_key, app_info in app_map.items():
        for aff in app_info["aff_list"]:
            aff["metric"] = app_info["metric_result"]
            output_result["checked_app_affiliations"].append(aff)

    print("\n===== 输出结果概览 =====")
    print(f"完成指标回填的归属记录条数：{len(output_result['checked_app_affiliations'])}")
    with open("output_result.json", "w", encoding="utf-8") as f:
        json.dump(output_result, f, ensure_ascii=False, indent=2)
    print("结果已保存至 output_result.json")


if __name__ == "__main__":
    main()
