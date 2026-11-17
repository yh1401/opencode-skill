#!/usr/bin/env python3
"""Mock EasyOps Apollo 代理接口（本地开发/联调，数据全部来自生产真实抓包）

数据来源：生产环境实测抓包（/graph 返回 + 同步日志，原始转储已移出仓库）
  · 5 个 Apollo 实例（id/名称/地址取自生产 apolloHosts）
  · 108 个应用（名称/部门/负责人取自生产应用节点，按实例归属为一对多）
  · 341 个中间件实例 / 756 条依赖（配置 key、namespace、连接地址取自生产同步日志）
  · 同一应用在不同实例下的配置按日志分别还原（生产即如此），不含任何虚构数据
  · 接口4（appId ↔ 部署项目 关系，真实路径 /thirdApi/apolloApp/list）数据 = 真实全量导出：
    - 首选 docs/业务数据/apolloApp_list.json（1426 行 / 260 个 appId，上游全量导出，2026-09-17）
    - 退回「真实抓包样本 upstream-appId-dp-relation-sample.json（100 行）＋ 造数 relation_rows_generated.json（183 行）」
    契约与上游一致：paginator=false（或 pageSize<=0）时不分页、直接返回全表，pageTotal = 总记录数
"""
import http.server
import json
import os

# ---------------------------------------------------------
# 接口4 占位数据：appId ↔ 部署项目 关系
#   ① 真实全量导出（docs/业务数据/apolloApp_list.json，上游 /thirdApi/apolloApp/list 全量）
#   ② 退回：真实抓包样本（docs/业务数据/upstream-appId-dp-relation-sample.json）
#            ＋ Mock 造数补充（脚本同级 relation_rows_generated.json，非真实数据）
# ---------------------------------------------------------
RELATION_FULL_NAME = os.path.join("业务数据", "apolloApp_list.json")
RELATION_SAMPLE_NAME = os.path.join("业务数据", "upstream-appId-dp-relation-sample.json")
RELATION_GENERATED_NAME = "relation_rows_generated.json"
_RELATION_ROWS = None


def _load_relation_rows():
    """读取关系接口数据并缓存；返回 None 表示各路数据源均不可用。

    ① 真实全量导出 `业务数据/apolloApp_list.json` 优先，命中即整表使用（不再叠加样本/造数）；
    ② 未命中则退回「真实抓包样本 + 造数补充」的老口径。
    查找顺序：/docs（docker-compose 挂载）→ 脚本同级 ../docs（本地直跑）。
    样本额外支持环境变量 RELATION_SAMPLE_JSON 指定；删除 relation_rows_generated.json 即关闭造数补充。
    """
    global _RELATION_ROWS
    if _RELATION_ROWS is not None:
        return _RELATION_ROWS
    here = os.path.dirname(os.path.abspath(__file__))
    docs_dirs = ["/docs", os.path.join(here, "..", "docs")]

    for d in docs_dirs:
        full = os.path.join(d, RELATION_FULL_NAME)
        if os.path.isfile(full):
            with open(full, encoding="utf-8") as f:
                rows = list(json.load(f).get("list", []))
            if rows:
                print(f"[MOCK] 关系接口真实全量已加载: {full}（{len(rows)} 行）")
                _RELATION_ROWS = rows
                return _RELATION_ROWS

    candidates = [os.environ.get("RELATION_SAMPLE_JSON", "")]
    candidates += [os.path.join(d, RELATION_SAMPLE_NAME) for d in docs_dirs]
    rows = []
    for p in candidates:
        if p and os.path.isfile(p):
            with open(p, encoding="utf-8") as f:
                data = json.load(f)
            rows = list(data.get("list", []))
            print(f"[MOCK] 关系接口真实样本已加载: {p}（{len(rows)} 行）")
            break
    else:
        print("[MOCK] 关系接口真实样本缺失：请挂载 docs 目录（docker-compose）或设置 RELATION_SAMPLE_JSON")

    gen = os.path.join(here, RELATION_GENERATED_NAME)
    if os.path.isfile(gen):
        with open(gen, encoding="utf-8") as f:
            groot = json.load(f)
        g = list(groot.get("list", []))
        rows.extend(g)
        print(f"[MOCK] 关系接口造数补充已加载: {gen}（{len(g)} 行，待运维替换为真实全量）")

    if not rows:
        return None
    _RELATION_ROWS = rows
    return _RELATION_ROWS

# 生产 Apollo 实例（套）：id / 名称 / 地址与生产一致（token 为空，Mock 不校验）
# 顺序同生产 hosts 接口实测返回（按 id 降序）：
# 同步按该顺序组装任务，应用节点的 apolloHosts 列表顺序随之与生产一致。
HOSTS = [
    {"id": 14, "apolloHostId": 14, "name": "天翼云眼广州4多AZ生产Apollo", "host": "https://tyyy-guangzhou4d-apollo-prod.ctseelink.cn:8158", "token": "", "secondProductId": [], "appCount": 0, "available": True, "availableDetail": ""},
    {"id": 13, "apolloHostId": 13, "name": "天翼看家广州4多AZ生产Apollo", "host": "https://tykj-guangzhou4d-apollo-prod.ctseelink.cn:8109", "token": "", "secondProductId": [], "appCount": 0, "available": True, "availableDetail": ""},
    {"id": 8, "apolloHostId": 8, "name": "视联百川", "host": "https://apollo-bc.ctseelink.cn:8156", "token": "", "secondProductId": [], "appCount": 0, "available": True, "availableDetail": ""},
    {"id": 3, "apolloHostId": 3, "name": "天翼云眼Apollo", "host": "https://apollo-vcp.21cn.com", "token": "", "secondProductId": [], "appCount": 0, "available": True, "availableDetail": ""},
    {"id": 2, "apolloHostId": 2, "name": "天翼看家Apollo", "host": "https://apollo-ehome.21cn.com:8071", "token": "", "secondProductId": [], "appCount": 0, "available": True, "availableDetail": ""},
]

# 应用元数据（按实例归属）：共 108 个应用，应用↔实例为一对多
APPS = {
    2: [
        {"appId": "area", "name": "area", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "auth", "name": "ehome-auth", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "bellpush", "name": "ehome-bellpush-server", "ownerName": "huangyy", "orgName": "部门2"},
        {"appId": "cameraInfo", "name": "cameraInfo", "ownerName": "zn_zengxj", "orgName": "部门2"},
        {"appId": "cloud-ehome", "name": "cloud-ehome-server", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cloud-ehome-v3", "name": "cloud-ehome-v3", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cloud-prod-xxl-job-admin", "name": "xxl-job 任务调度", "ownerName": "linzs153", "orgName": "部门2"},
        {"appId": "cloud-video", "name": "cloud-video", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cp-dispatch", "name": "cp-dispatch", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "cp-mall", "name": "cp-mall", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "cp-manage", "name": "cp-manage", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "cp-manage-new", "name": "cp-manage-new", "ownerName": "apollo", "orgName": "部门1"},
        {"appId": "cp-store", "name": "cp-store", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "ddnsmgr", "name": "ddnsmgr", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "detect", "name": "门铃告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "detect-sdk", "name": "detect-sdk", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "device-access", "name": "device-access", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "device-status-service", "name": "device-status-service", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "ehome", "name": "ehome线上版本", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-account", "name": "ehome-account", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-account-new-service", "name": "天翼看家账号迁移服务", "ownerName": "liangx", "orgName": "部门2"},
        {"appId": "ehome-ai-care", "name": "公众版智能守护功能", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-api", "name": "统一SDK业务平台ehome", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "ehome-cascade", "name": "ehome-cascade", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-command", "name": "ehome-command", "ownerName": "pengzb189", "orgName": "部门2"},
        {"appId": "ehome-es-dual-active", "name": "ehome ES双写服务", "ownerName": "liangx", "orgName": "部门2"},
        {"appId": "ehome-gallery", "name": "ehome-gallery", "ownerName": "zn_caixb", "orgName": "部门2"},
        {"appId": "ehome-ims", "name": "IMS通话服务", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-mansync", "name": "ehome-mansync", "ownerName": "yangfw", "orgName": "部门2"},
        {"appId": "ehome-order", "name": "ehome-order", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-ota", "name": "ehome-ota", "ownerName": "liangyy", "orgName": "部门2"},
        {"appId": "ehome-pro-device", "name": "ehome-pro-device", "ownerName": "zhanghsh", "orgName": "部门2"},
        {"appId": "ehome-pro-open", "name": "ehome-pro-open", "ownerName": "liup", "orgName": "部门2"},
        {"appId": "ehome-pro-openmgr", "name": "ehome-pro-openmgr", "ownerName": "zn_huangt", "orgName": "部门2"},
        {"appId": "ehome-pro-viidmgr", "name": "ehome-pro-viidmg-server", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "ehome-prof-device", "name": "ehome-prof-device", "ownerName": "zn_zengyn", "orgName": "部门2"},
        {"appId": "ehome-prof-push", "name": "ehome-prof-push", "ownerName": "zn_zengyn", "orgName": "部门2"},
        {"appId": "ehome-public", "name": "ehome-public", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "ehome-public-push", "name": "ehome-public-push", "ownerName": "zn_zengyn", "orgName": "部门2"},
        {"appId": "ehome-public-transmit", "name": "ehome-public-transmit", "ownerName": "pengzb189", "orgName": "部门2"},
        {"appId": "ehome-security", "name": "ehome-security", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-security-fs", "name": "ehome-security-fs", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-sigtran-worker", "name": "ehome-sigtran-worker", "ownerName": "weigh", "orgName": "部门2"},
        {"appId": "file", "name": "file", "ownerName": "gongqf", "orgName": "部门2"},
        {"appId": "gb-cascade-gateway", "name": "gb-cascade-gateway", "ownerName": "zn_liujh", "orgName": "部门2"},
        {"appId": "message", "name": "message", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "omvs-cloud-api", "name": "omvs-cloud-api", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "push", "name": "告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "push-pro", "name": "push-pro", "ownerName": "zn_mengyl", "orgName": "部门2"},
        {"appId": "push-producer", "name": "push-producer", "ownerName": "zn_kangqzh", "orgName": "部门2"},
        {"appId": "push-sdk", "name": "push-sdk", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "submit-cloud", "name": "cloud-submit", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "submit-cloud-v2", "name": "submit-cloud", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "tree-synvcp", "name": "tree-synvcp", "ownerName": "liub1", "orgName": "部门2"},
        {"appId": "treeSevice", "name": "vcp-tree-service", "ownerName": "zn_zengxj", "orgName": "部门2"},
        {"appId": "uni-order", "name": "视联网订单中台", "ownerName": "liangx", "orgName": "部门2"},
        {"appId": "vnet-cloud-broadcast", "name": "vnet-cloud-broadcast", "ownerName": "zn_linrh", "orgName": "部门2"},
        {"appId": "vnet-icam-push", "name": "小微业务平台设备控制消费和推送服务", "ownerName": "pengzb189", "orgName": "部门2"},
        {"appId": "vnet-vpaas-manage", "name": "vnet-vpaas-manage", "ownerName": "zst_tangww", "orgName": "部门2"},
    ],
    3: [
        {"appId": "adapter-task-api", "name": "adapter-task-api", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "apollo-ehome-pro", "name": "ehome-pro-server", "ownerName": "zn_huangt", "orgName": "部门2"},
        {"appId": "area", "name": "area", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "auth", "name": "ehome-auth", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "cloud-ehome", "name": "cloud-ehome-server", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cloud-video", "name": "cloud-video", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "ddnsmgr", "name": "ddnsmgr", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "detect", "name": "门铃告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "device-access", "name": "device-access", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "device-status-service", "name": "device-status-service", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "ehome", "name": "ehome线上版本", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-account", "name": "ehome-account", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-order", "name": "ehome-order", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-pro-viidmgr", "name": "ehome-pro-viidmg-server", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "ehome-security", "name": "ehome-security", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "portal-api-dep", "name": "能力开发门户服务portal-api-dep", "ownerName": "lizj26", "orgName": "部门2"},
        {"appId": "push", "name": "告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "push-pro", "name": "push-pro", "ownerName": "zn_mengyl", "orgName": "部门2"},
        {"appId": "push-producer", "name": "push-producer", "ownerName": "zn_kangqzh", "orgName": "部门2"},
        {"appId": "seelink-batch-service", "name": "视联业务平台批处理服务", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "sl-center-auth-application", "name": "sl-center-auth-application", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-access", "name": "sl-center-device-access", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-auth", "name": "sl-center-device-auth", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-control", "name": "sl-center-device-control", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-sync", "name": "sl-center-device-sync", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-tree", "name": "sl-center-device-tree", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-goods-order", "name": "商品中心订单服务", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-goods-packages", "name": "商品中心套餐服务", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-es-dual-active", "name": "sl-es-dual-active", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "slink-biz-user", "name": "天翼视联业务平台用户模块", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "submit-cloud", "name": "cloud-submit", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "treeSevice", "name": "vcp-tree-service", "ownerName": "zn_zengxj", "orgName": "部门2"},
        {"appId": "vcp-AIManager", "name": "视频云网AI管理服务", "ownerName": "zhanghsh", "orgName": "部门2"},
        {"appId": "vcp-alert-web", "name": "视频云网行业应用WEB端告警服务", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "vcp-businesshall", "name": "vcp-businesshall", "ownerName": "zn_wangy1", "orgName": "部门2"},
        {"appId": "vcp-cascade-manage", "name": "vcp-cascade-manage", "ownerName": "yefw189", "orgName": "部门2"},
        {"appId": "vcp-ehomesync", "name": "看家目录/设备同步程序", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "vcp-gather", "name": "vcp-gather", "ownerName": "zn_zhangxl", "orgName": "部门2"},
        {"appId": "vcp-openapi", "name": "视频云网-能力开放", "ownerName": "dengzhq", "orgName": "部门2"},
        {"appId": "vcp-openmgr", "name": "视频云网-能力开放管理后台", "ownerName": "zn_liujh", "orgName": "部门2"},
        {"appId": "vcp-pro", "name": "视图云眼行业版", "ownerName": "zn_suql", "orgName": "部门2"},
        {"appId": "vcp-proapp", "name": "vcp-proapp", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "vcp-proappweb-web", "name": "平安慧眼2.0", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "vcp-saas-cmpt-scn", "name": "海拼应用-场景组件", "ownerName": "lizj26", "orgName": "部门2"},
        {"appId": "vcp-saas-grassroots-governance", "name": "海拼应用-基层治理", "ownerName": "lizj26", "orgName": "部门2"},
        {"appId": "vcp-saas-jxc", "name": "vcp-saas-jxc", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "vcp-sync-es", "name": "vcp-sync-es", "ownerName": "zn_lihb", "orgName": "部门2"},
        {"appId": "vcp-tree-service", "name": "视频云网目录树", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "vcp-viid-node", "name": "vcp-viid-node", "ownerName": "liub1", "orgName": "部门2"},
        {"appId": "vcp-waters-monitor", "name": "水域监控", "ownerName": "maoyn", "orgName": "部门2"},
    ],
    8: [
        {"appId": "bc-bp-device-control", "name": "百川白牌设备控制模块", "ownerName": "pengzb189", "orgName": "样例部门1"},
        {"appId": "bc-bp-device-manage", "name": "视联百川白牌设备管理服务", "ownerName": "liangx4", "orgName": "样例部门1"},
        {"appId": "bc-bp-manage", "name": "视联百川白牌管理服务", "ownerName": "liangx4", "orgName": "样例部门1"},
        {"appId": "bc-bp-management-platform", "name": "百川白牌管理管理后台", "ownerName": "pengzb189", "orgName": "样例部门1"},
        {"appId": "bc-bp-message", "name": "视联百川白牌消息服务", "ownerName": "liangx4", "orgName": "样例部门1"},
        {"appId": "bc-bp-order", "name": "视联百川白牌订单服务", "ownerName": "liangx4", "orgName": "样例部门1"},
        {"appId": "bc-bp-user", "name": "视联百川白牌用户服务", "ownerName": "liangx4", "orgName": "样例部门1"},
        {"appId": "bc-pa-platform", "name": "百川解决方案管理后台", "ownerName": "liangx4", "orgName": "样例部门1"},
        {"appId": "mc-centric-consumer20190129", "name": "mc-centric-consumer", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mc-centric-data-init20190129", "name": "mc-centric-data-init", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mc-centric-elink-openapi20190129", "name": "mc-centric-elink-openapi", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mc-centric-management20190129", "name": "mc-centric-management", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mc-provincial-consumer20190129", "name": "mc-provincial-consumer", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mc-provincial-management20190129", "name": "mc-provincial-management", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mqtt-centirc-login20190130", "name": "mqtt-centirc-login", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "mqtt-provincial-connector0129", "name": "mqtt-provincial-connector", "ownerName": "zhongzs189", "orgName": "样例部门1"},
        {"appId": "slink-amp-iot", "name": "slink-amp-iot", "ownerName": "zhongzs189", "orgName": "样例部门1"},
    ],
    13: [
        {"appId": "area", "name": "area", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "auth", "name": "ehome-auth", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "bellpush", "name": "ehome-bellpush-server", "ownerName": "huangyy", "orgName": "部门2"},
        {"appId": "cameraInfo", "name": "cameraInfo", "ownerName": "zn_zengxj", "orgName": "部门2"},
        {"appId": "cloud-ehome", "name": "cloud-ehome-server", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cloud-ehome-v3", "name": "cloud-ehome-v3", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cloud-prod-xxl-job-admin", "name": "xxl-job 任务调度", "ownerName": "linzs153", "orgName": "部门2"},
        {"appId": "cloud-video", "name": "cloud-video", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cp-dispatch", "name": "cp-dispatch", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "cp-mall", "name": "cp-mall", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "cp-manage", "name": "cp-manage", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "cp-manage-new", "name": "cp-manage-new", "ownerName": "apollo", "orgName": "部门1"},
        {"appId": "cp-store", "name": "cp-store", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "ddnsmgr", "name": "ddnsmgr", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "detect", "name": "门铃告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "detect-sdk", "name": "detect-sdk", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "device-access", "name": "device-access", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "device-status-service", "name": "device-status-service", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "ehome", "name": "ehome线上版本", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-account", "name": "ehome-account", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-account-new-service", "name": "天翼看家账号迁移服务", "ownerName": "liangx", "orgName": "部门2"},
        {"appId": "ehome-ai-care", "name": "公众版智能守护功能", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-api", "name": "统一SDK业务平台ehome", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "ehome-cascade", "name": "ehome-cascade", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-command", "name": "ehome-command", "ownerName": "pengzb189", "orgName": "部门2"},
        {"appId": "ehome-es-dual-active", "name": "ehome ES双写服务", "ownerName": "liangx", "orgName": "部门2"},
        {"appId": "ehome-gallery", "name": "ehome-gallery", "ownerName": "zn_caixb", "orgName": "部门2"},
        {"appId": "ehome-ims", "name": "IMS通话服务", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-mansync", "name": "ehome-mansync", "ownerName": "yangfw", "orgName": "部门2"},
        {"appId": "ehome-order", "name": "ehome-order", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-ota", "name": "ehome-ota", "ownerName": "liangyy", "orgName": "部门2"},
        {"appId": "ehome-pro-device", "name": "ehome-pro-device", "ownerName": "zhanghsh", "orgName": "部门2"},
        {"appId": "ehome-pro-open", "name": "ehome-pro-open", "ownerName": "liup", "orgName": "部门2"},
        {"appId": "ehome-pro-openmgr", "name": "ehome-pro-openmgr", "ownerName": "zn_huangt", "orgName": "部门2"},
        {"appId": "ehome-pro-viidmgr", "name": "ehome-pro-viidmg-server", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "ehome-prof-device", "name": "ehome-prof-device", "ownerName": "zn_zengyn", "orgName": "部门2"},
        {"appId": "ehome-prof-push", "name": "ehome-prof-push", "ownerName": "zn_zengyn", "orgName": "部门2"},
        {"appId": "ehome-public", "name": "ehome-public", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "ehome-public-push", "name": "ehome-public-push", "ownerName": "zn_zengyn", "orgName": "部门2"},
        {"appId": "ehome-public-transmit", "name": "ehome-public-transmit", "ownerName": "pengzb189", "orgName": "部门2"},
        {"appId": "ehome-security", "name": "ehome-security", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-security-fs", "name": "ehome-security-fs", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-sigtran-worker", "name": "ehome-sigtran-worker", "ownerName": "weigh", "orgName": "部门2"},
        {"appId": "file", "name": "file", "ownerName": "gongqf", "orgName": "部门2"},
        {"appId": "message", "name": "message", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "omvs-cloud-api", "name": "omvs-cloud-api", "ownerName": "wangw43", "orgName": "部门2"},
        {"appId": "push", "name": "告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "push-pro", "name": "push-pro", "ownerName": "zn_mengyl", "orgName": "部门2"},
        {"appId": "push-producer", "name": "push-producer", "ownerName": "zn_kangqzh", "orgName": "部门2"},
        {"appId": "push-sdk", "name": "push-sdk", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "submit-cloud", "name": "cloud-submit", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "submit-cloud-v2", "name": "submit-cloud", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "tree-synvcp", "name": "tree-synvcp", "ownerName": "liub1", "orgName": "部门2"},
        {"appId": "treeSevice", "name": "vcp-tree-service", "ownerName": "zn_zengxj", "orgName": "部门2"},
        {"appId": "uni-order", "name": "视联网订单中台", "ownerName": "liangx", "orgName": "部门2"},
        {"appId": "vnet-cloud-broadcast", "name": "vnet-cloud-broadcast", "ownerName": "zn_linrh", "orgName": "部门2"},
        {"appId": "vnet-icam-push", "name": "小微业务平台设备控制消费和推送服务", "ownerName": "pengzb189", "orgName": "部门2"},
        {"appId": "vnet-vpaas-manage", "name": "vnet-vpaas-manage", "ownerName": "zst_tangww", "orgName": "部门2"},
    ],
    14: [
        {"appId": "adapter-task-api", "name": "adapter-task-api", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "apollo-ehome-pro", "name": "ehome-pro-server", "ownerName": "zn_huangt", "orgName": "部门2"},
        {"appId": "area", "name": "area", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "auth", "name": "ehome-auth", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "cloud-ehome", "name": "cloud-ehome-server", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "cloud-video", "name": "cloud-video", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "ddnsmgr", "name": "ddnsmgr", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "detect", "name": "门铃告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "device-access", "name": "device-access", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "device-status-service", "name": "device-status-service", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "ehome", "name": "ehome线上版本", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-account", "name": "ehome-account", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "ehome-order", "name": "ehome-order", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "ehome-pro-viidmgr", "name": "ehome-pro-viidmg-server", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "ehome-security", "name": "ehome-security", "ownerName": "zn_chenshy", "orgName": "部门2"},
        {"appId": "portal-api-dep", "name": "能力开发门户服务portal-api-dep", "ownerName": "lizj26", "orgName": "部门2"},
        {"appId": "push", "name": "告警消息推送服务-视频云网", "ownerName": "chenbx", "orgName": "部门2"},
        {"appId": "push-pro", "name": "push-pro", "ownerName": "zn_mengyl", "orgName": "部门2"},
        {"appId": "push-producer", "name": "push-producer", "ownerName": "zn_kangqzh", "orgName": "部门2"},
        {"appId": "seelink-batch-service", "name": "视联业务平台批处理服务", "ownerName": "gaoshj", "orgName": "部门2"},
        {"appId": "sl-center-auth-application", "name": "sl-center-auth-application", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-access", "name": "sl-center-device-access", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-auth", "name": "sl-center-device-auth", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-control", "name": "sl-center-device-control", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-sync", "name": "sl-center-device-sync", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-device-tree", "name": "sl-center-device-tree", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-goods-order", "name": "商品中心订单服务", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-center-goods-packages", "name": "商品中心套餐服务", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "sl-es-dual-active", "name": "sl-es-dual-active", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "slink-biz-user", "name": "天翼视联业务平台用户模块", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "submit-cloud", "name": "cloud-submit", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "treeSevice", "name": "vcp-tree-service", "ownerName": "zn_zengxj", "orgName": "部门2"},
        {"appId": "vcp-AIManager", "name": "视频云网AI管理服务", "ownerName": "zhanghsh", "orgName": "部门2"},
        {"appId": "vcp-alert-web", "name": "视频云网行业应用WEB端告警服务", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "vcp-businesshall", "name": "vcp-businesshall", "ownerName": "zn_wangy1", "orgName": "部门2"},
        {"appId": "vcp-cascade-manage", "name": "vcp-cascade-manage", "ownerName": "yefw189", "orgName": "部门2"},
        {"appId": "vcp-ehomesync", "name": "看家目录/设备同步程序", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "vcp-gather", "name": "vcp-gather", "ownerName": "zn_zhangxl", "orgName": "部门2"},
        {"appId": "vcp-openapi", "name": "视频云网-能力开放", "ownerName": "dengzhq", "orgName": "部门2"},
        {"appId": "vcp-openmgr", "name": "视频云网-能力开放管理后台", "ownerName": "zn_liujh", "orgName": "部门2"},
        {"appId": "vcp-pro", "name": "视图云眼行业版", "ownerName": "zn_suql", "orgName": "部门2"},
        {"appId": "vcp-proapp", "name": "vcp-proapp", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "vcp-proappweb-web", "name": "平安慧眼2.0", "ownerName": "maoyn", "orgName": "部门2"},
        {"appId": "vcp-saas-cmpt-scn", "name": "海拼应用-场景组件", "ownerName": "lizj26", "orgName": "部门2"},
        {"appId": "vcp-saas-grassroots-governance", "name": "海拼应用-基层治理", "ownerName": "lizj26", "orgName": "部门2"},
        {"appId": "vcp-saas-jxc", "name": "vcp-saas-jxc", "ownerName": "apollo", "orgName": "部门2"},
        {"appId": "vcp-sync-es", "name": "vcp-sync-es", "ownerName": "zn_lihb", "orgName": "部门2"},
        {"appId": "vcp-tree-service", "name": "视频云网目录树", "ownerName": "zn_liangzhh", "orgName": "部门2"},
        {"appId": "vcp-viid-node", "name": "vcp-viid-node", "ownerName": "liub1", "orgName": "部门2"},
        {"appId": "vcp-waters-monitor", "name": "水域监控", "ownerName": "maoyn", "orgName": "部门2"},
    ],
}

# 各实例下每个应用的真实配置项：hostId -> appId -> namespace -> {key: value}
NAMESPACE_CONFIGS = {
    2: {
        "area": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://areacode.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "auth": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://auth.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "bellpush": {
            "application": {
                "rocketmq.address": "bellpush1.rocketmq.ehome.dlifeinner.cn,bellpush2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.shardingsphere.ehome.ctseelinkinner.cn:3306",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
                "spring.redis2.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis2.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis2.port": "19004",
            },
        },
        "cameraInfo": {
            "application": {
                "spring.datasource.ehome.backup.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.ehome.main.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
            },
        },
        "cloud-ehome": {
            "application": {
                "redis.addr": "cloudservice.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "cloud-ehome-v3": {
            "application": {
                "redis.addr": "cloudservice-v2.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "cloud-prod-xxl-job-admin": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://192.168.8.131:13301",
            },
        },
        "cloud-video": {
            "application": {
                "redis.addr": "cloudservice.redis-gw.ehome.dlifeinner.cn:19001",
            },
        },
        "cp-dispatch": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-mall": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-manage": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-manage-new": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-store": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "ddnsmgr": {
            "application": {
                "redis.addr": "ddns-mgr.redis.ehomeinner.cn:65379",
            },
        },
        "detect": {
            "application": {
                "rocketmq.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.shardingsphere.ehome.ctseelinkinner.cn:3306",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
                "spring.redis2.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis2.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis2.port": "19004",
            },
        },
        "detect-sdk": {
            "application": {
                "gb-gateway.rocketmq.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "gb-gateway.rocketmq.port": "9876",
                "redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "rocketmq.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.url": "jdbc:mysql://ehome-read.mysql.ehome.dlifeinner.cn:3302",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "device-access": {
            "application": {
                "redis.cluster.addr": "core1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "device-status-service": {
            "application": {
                "rocketmq.M.status.center.server.address": "sdk2-detectmessage1-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk2-detectmessage2-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk2-detectmessage3-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk2-detectmessage4-gz4.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.center.server.port": "9876",
                "rocketmq.M.status.gb.server.address": "common-alarmmessage1-gz4.rocketmq.ehome.ctseelinkinner.cn,common-alarmmessage2-gz4.rocketmq.ehome.ctseelinkinner.cn,common-alarmmessage3-gz4.rocketmq.ehome.ctseelinkinner.cn,common-alarmmessage4-gz4.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.gb.server.port": "9876",
                "rocketmq.M.status.province.server.address": "sdk2-onofflinemessage1-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk2-onofflinemessage2-gz4.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.province.server.port": "9876",
                "rocketmq.M.status.sdk.server.address": "sdk3a4-onofflinemessage1-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk3a4-onofflinemessage2-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk3a4-onofflinemessage3-gz4.rocketmq.ehome.ctseelinkinner.cn,sdk3a4-onofflinemessage4-gz4.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.sdk.server.port": "9876",
                "rocketmq.status.center.server.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.center.server.port": "9876",
                "rocketmq.status.gb.server.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.gb.server.port": "9876",
                "rocketmq.status.province.server.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.province.server.port": "9876",
                "rocketmq.status.sdk.server.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.sdk.server.port": "9876",
                "rocketmq.vcp.status.center.server.address": "common1.rocketmq.vcp.ctseelinkinner.cn,common2.rocketmq.vcp.ctseelinkinner.cn,common3.rocketmq.vcp.ctseelinkinner.cn,common4.rocketmq.vcp.ctseelinkinner.cn",
                "rocketmq.vcp.status.center.server.port": "9876",
                "rocketmq.vcp.status.province.server.address": "common1.rocketmq.vcp.ctseelinkinner.cn,common2.rocketmq.vcp.ctseelinkinner.cn,common3.rocketmq.vcp.ctseelinkinner.cn,common4.rocketmq.vcp.ctseelinkinner.cn",
                "rocketmq.vcp.status.province.server.port": "9876",
                "rocketmq.vcp.status.sdk.server.address": "common1.rocketmq.vcp.ctseelinkinner.cn,common2.rocketmq.vcp.ctseelinkinner.cn,common3.rocketmq.vcp.ctseelinkinner.cn,common4.rocketmq.vcp.ctseelinkinner.cn",
                "rocketmq.vcp.status.sdk.server.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.url": "jdbc:mysql://common.tidb.ehome.ctseelinkinner.cn:14001",
                "spring.redis.cluster.nodes": "other1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome": {
            "application": {
                "rocketmq.cloud.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.mqNameServer": "api1.rocketmq.ehome.dlifeinner.cn:9876,api2.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.producer.cloudOrderOperationInfo.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.cloudOrderOperationInfo.port": "9876",
                "rocketmq.producer.deviceBind.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceBind.port": "9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.ehome.ctseelinkinner.cn:3306",
            },
        },
        "ehome-account": {
            "application": {
                "redis.addr": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn:19002",
                "redis.cluster.addr": "core1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-account-new-service": {
            "application": {
                "spring.data.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.data.redis.port": "19002",
                "spring.datasource.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
            },
        },
        "ehome-ai-care": {
            "application": {
                "elasticsearch.esAddress": "common1-local.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "rocketmq.aicare-data-analysis.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.aicare-data-analysis.port": "9876",
                "rocketmq.producer.aiCareDataAnalysis.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.aiCareDataAnalysis.port": "9876",
                "rocketmq.smart-alert.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smart-alert.port": "9876",
                "spring.datasource.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3301",
                "spring.kafka.ai-care.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.ai-care.producer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.delay-sms.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.delay-sms.producer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.redis.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19004",
            },
        },
        "ehome-api": {
            "application": {
                "elasticsearch.esAddress": "common1-local.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "rocketmq.address": "api1.rocketmq.ehome.dlifeinner.cn,api2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.audio-sync-re.address": "api1.rocketmq.ehome.dlifeinner.cn,api2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.audio-sync-re.port": "9876",
                "rocketmq.audio-sync.address": "api1.rocketmq.ehome.dlifeinner.cn,api2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.audio-sync.port": "9876",
                "rocketmq.audio.namesrvAddr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.audioSync.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.audioSync.port": "9876",
                "rocketmq.common.namesrvAddr": "ehome-api3-rocketmq.ehomeinner.cn:9876,ehome-api4-rocketmq.ehomeinner.cn:9876,ehome-api5-rocketmq.ehomeinner.cn:9876,ehome-api6-rocketmq.ehomeinner.cn:9876",
                "rocketmq.consumer.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.port": "9876",
                "rocketmq.device-reset.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.device-reset.port": "9876",
                "rocketmq.deviceReset.address": "ehome-router1.rocketmq.ehomeinner.cn:9876",
                "rocketmq.hx-bms-sync.address": "api1.rocketmq.ehome.dlifeinner.cn,api2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.hx-bms-sync.port": "9876",
                "rocketmq.hxBmsSync.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.hxBmsSync.port": "9876",
                "rocketmq.port": "9876",
                "rocketmq.producer.audioSync.address": "api1.rocketmq.ehome.dlifeinner.cn,api2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.audioSync.port": "9876",
                "rocketmq.producer.device-operation.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.device-operation.port": "9876",
                "rocketmq.producer.deviceBind.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceBind.port": "9876",
                "rocketmq.producer.hxBmsSync.address": "api1.rocketmq.ehome.dlifeinner.cn,api2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.hxBmsSync.port": "9876",
                "rocketmq.smc.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smc.port": "9876",
                "rocketmq.unBindCloud.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.ehome.ctseelinkinner.cn:3306",
                "spring.datasource.url": "jdbc:mysql://ehome-api.mysql.ehomeinner.cn:3301",
            },
        },
        "ehome-cascade": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://ehome-access.mysql.ehomeinner.cn:3306",
                "spring.redis.host": "ehome-access.codis.ehomeinner.cn",
                "spring.redis.port": "16000",
            },
        },
        "ehome-command": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-es-dual-active": {
            "application": {
                "elasticsearch.master.esAddress": "common1-guizh.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "elasticsearch.slave.esAddress": "common1-guizh3.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "elasticsearch.standby.esAddress": "common1-guangzh4.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "spring.datasource.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3301",
                "spring.kafka.es.dual.write.producer.bootstrapServers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.es.write.slave.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.es.write.standby.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
            },
        },
        "ehome-gallery": {
            "application": {
                "rocketmq.addr": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
            },
        },
        "ehome-ims": {
            "application": {
                "rocketmq.consumer.bindkey.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.bindkey.port": "9876",
                "rocketmq.producer.bindkey.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://ehome-ims.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-mansync": {
            "application": {
                "data.kafka.brokers": "192.168.10.48:9092,192.168.11.228:9092,192.168.9.130:9092",
                "data.redis.addr": "ehome-sroute.codis.ehomeinner.cn:17000",
            },
        },
        "ehome-order": {
            "application": {
                "rocketmq.audio.namesrvAddr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.cloud.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.cloudCamera.areaControl.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.cloud.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.cloud.port": "9876",
                "rocketmq.consumer.deviceBind.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.deviceBind.port": "9876",
                "rocketmq.consumer.unbind.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.unbind.port": "9876",
                "rocketmq.mqNameSrvAddr": "api1.rocketmq.ehome.dlifeinner.cn:9876,api2.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.producer.aiOrderOperationInfo.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.aiOrderOperationInfo.port": "9876",
                "rocketmq.producer.cloudNotify.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.cloudNotify.port": "9876",
                "rocketmq.producer.cloudOrderOperationInfo.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.cloudOrderOperationInfo.port": "9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://order.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://order.mysql.ehomeinner.cn:3302",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-ota": {
            "application": {
                "mysql.url": "ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "ota.nacos.addr": "vpaas-nacos.vpaas-nacos",
                "ota.nacos.port": "8848",
                "redis.addr": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn:19001",
                "rocketMq.nameSrvAddr": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
            },
        },
        "ehome-pro-device": {
            "application": {
                "DruidDb1.dbUrl": "camera.tidb.ehomeinner.cn:3306",
                "DruidDb2.dbUrl": "ehome.mysql.ehomeinner.cn:3301",
                "rocketmq.name-server": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn:9876,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn:9876,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn:9876,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-pro-open": {
            "application": {
                "rocketmq.device-dis.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.device-dis.port": "9876",
                "rocketmq.smart-device-dis.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smart-device-dis.port": "9876",
                "spring.datasource.ehome.backup.url": "jdbc:mysql://ehome-read.mysql.ehome.ctseelinkinner.cn:3302",
                "spring.datasource.ehome.main.back.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.ehome.main.url": "jdbc:mysql://ehome.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "core.redis-gw.ehome.dlifeinner.cn",
                "spring.redis.port": "6379",
            },
        },
        "ehome-pro-openmgr": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.redis.cluster.nodes": "ability-open1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-pro-viidmgr": {
            "application": {
                "rocketmq.name-server": "viidmgr1.rocketmq.ehomeinner.cn:9876,viidmgr2.rocketmq.ehomeinner.cn:9876,viidmgr3.rocketmq.ehomeinner.cn:9876,viidmgr4.rocketmq.ehomeinner.cn:9876",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://192.168.5.180:3301",
            },
        },
        "ehome-prof-device": {
            "application": {
                "spring.datasource.dynamic.datasource.can_write_camera_management.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.dynamic.datasource.ehome_back_up.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.redis.cluster.nodes": "ability-open1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-prof-push": {
            "application": {
                "rocketmq.center.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.center.port": "9876",
                "rocketmq.device-dis.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.device-dis.port": "9876",
                "rocketmq.province.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.province.port": "9876",
                "rocketmq.push-meaage.address": "tree-synvcp1.rocketmq.ehome.dlifeinner.cn,tree-synvcp2.rocketmq.ehome.dlifeinner.cn,tree-synvcp3.rocketmq.ehome.dlifeinner.cn,tree-synvcp4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.push-meaage.port": "9876",
                "rocketmq.sdk-device-alarm.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.sdk-device-alarm.port": "9876",
                "rocketmq.self-support-bell.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.self-support-bell.port": "9876",
                "rocketmq.self-support-device-alarm.address": "push1.rocketmq.ehomeinner.cn,push2.rocketmq.ehomeinner.cn,push3.rocketmq.ehomeinner.cn,push4.rocketmq.ehomeinner.cn",
                "rocketmq.self-support-device-alarm.port": "9876",
                "rocketmq.unified-sdk.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.unified-sdk.port": "9876",
                "rocketmq.zhongwei-support-bell.address": "bellpush1.rocketmq.ehome.dlifeinner.cn,bellpush2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.zhongwei-support-bell.port": "9876",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.redis.cluster.nodes": "ability-open1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-public": {
            "application": {
                "rocketmq.consumer.device-operation.name-server": "public-push1.rocketmq.ehomeinner.cn:9876,public-push2.rocketmq.ehomeinner.cn:9876,public-push3.rocketmq.ehomeinner.cn:9876,public-push4.rocketmq.ehomeinner.cn:9876",
                "rocketmq.consumer.deviceBind.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceBindNotify.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceBindNotifyImsRetry.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceShareInfoHandle.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceUnBindNotifyImsRetry.nameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceUnbind.nameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceUnbindNotify.nameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.unbind.name-server": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.name-server": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.producer.device-operation.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.device-operation.port": "9876",
                "rocketmq.producer.deviceBind.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceBind.port": "9876",
                "rocketmq.producer.deviceUnbind.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceUnbind.port": "9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.druid.tidb-push.url": "jdbc:mysql://integrationapp.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.ehome.ctseelinkinner.cn:3306",
                "spring.kafka.uid-sys.producer.bootstrap-servers": "10.135.94.152:9092,10.135.94.173:9092,10.135.94.181:9092,10.135.94.206:9092,10.135.94.40:9092,10.135.95.204:9092",
                "spring.redis.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19004",
            },
        },
        "ehome-public-push": {
            "application": {
                "rocketmq.ai-analyze-alarm.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.ai-analyze-alarm.port": "9876",
                "rocketmq.center.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.center.port": "9876",
                "rocketmq.fall-alarm.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.fall-alarm.port": "9876",
                "rocketmq.j-push-highest-priority.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.j-push-highest-priority.port": "9876",
                "rocketmq.j-push-platform.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.j-push-platform.port": "9876",
                "rocketmq.producer.ai-care-event.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.ai-care-event.port": "9876",
                "rocketmq.producer.default-AI.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.default-AI.port": "9876",
                "rocketmq.producer.j-push-highest-priority.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.j-push-highest-priority.port": "9876",
                "rocketmq.producer.j-push-platform.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.j-push-platform.port": "9876",
                "rocketmq.producer.sdkCameraAlarm.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.sdkCameraAlarm.port": "9876",
                "rocketmq.producer.smartAlertElderlyCare.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.smartAlertElderlyCare.port": "9876",
                "rocketmq.producer.xygj-platform-main.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.xygj-platform-main.port": "9876",
                "rocketmq.producer.xygj-platform-push.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.xygj-platform-push.port": "9876",
                "rocketmq.producer.xygj-platform-uppermost.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.xygj-platform-uppermost.port": "9876",
                "rocketmq.province.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.province.port": "9876",
                "rocketmq.sdk-device-alarm.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.sdk-device-alarm.port": "9876",
                "rocketmq.self-support-bell.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.self-support-bell.port": "9876",
                "rocketmq.self-support-device-alarm.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.self-support-device-alarm.port": "9876",
                "rocketmq.smart-alert-elderly-care.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smart-alert-elderly-care.port": "9876",
                "rocketmq.unified-sdk.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.unified-sdk.port": "9876",
                "rocketmq.vpaas-unified-sdk.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.vpaas-unified-sdk.port": "9876",
                "rocketmq.xygj-platform-main.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.xygj-platform-main.port": "9876",
                "rocketmq.xygj-platform-push.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.xygj-platform-push.port": "9876",
                "rocketmq.xygj-platform-uppermost.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.xygj-platform-uppermost.port": "9876",
                "rocketmq.zhongwei-support-bell.address": "bellpush1.rocketmq.ehome.dlifeinner.cn,bellpush2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.zhongwei-support-bell.port": "9876",
                "spring.datasource.dynamic.datasource.ehome_back_up.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3302",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3301",
                "spring.datasource.dynamic.datasource.tidb.url": "jdbc:mysql://integrationapp.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.redis.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19004",
            },
        },
        "ehome-public-transmit": {
            "application": {
                "rocketmq.consumer.ai.default.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.ai.default.port": "9876",
            },
        },
        "ehome-security": {
            "application": {
                "rocketmq.producer.deviceShareInfoHandle.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceShareInfoHandle.port": "9876",
                "spring.datasource.url": "jdbc:mysql://security.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-security-fs": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://10.131.7.22:8301",
            },
        },
        "ehome-sigtran-worker": {
            "application": {
                "redis.addr": "codis1.ehomeinner.cn:17000",
                "rocketmq.consumer.nameSrvAddr": "192.168.3.121:9876,192.168.3.140:9876,192.168.3.198:9876,192.168.3.29:9876",
            },
        },
        "file": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
            },
        },
        "gb-cascade-gateway": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://common.mysql.ehome.ctseelinkinner.cn:13302",
                "spring.redis.host": "common.redis.ehome.ctseelinkinner.cn",
                "spring.redis.port": "6379",
            },
        },
        "message": {
            "application": {
                "rocketmq.producer.nameSrvAddr": "182.43.129.36:9876,182.43.155.189:9876,203.195.74.104:9876,203.195.74.174:9876",
            },
        },
        "omvs-cloud-api": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://192.168.233.132:3303",
            },
        },
        "push": {
            "application": {
                "rocketmq.message.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.message.port": "9876",
                "rocketmq.server.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.server.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.shardingsphere.ehome.ctseelinkinner.cn:3306",
                "spring.redis.cluster.nodes": "core1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
                "spring.redis2.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis2.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis2.port": "19004",
                "spring.redis3.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis3.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis3.port": "19001",
            },
        },
        "push-pro": {
            "application": {
                "rocketMq.consumer.message.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.consumer.message.port": "9876",
                "rocketMq.produce.ai.mq.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.produce.ai.mq.port": "9876",
                "rocketmq.motionDetection.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.motionDetection.port": "9876",
                "spring.datasource.camera.management.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.camera.url": "jdbc:mysql://camera.tidb.ehomeinner.cn:3306",
                "spring.datasource.ehome.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "push-producer": {
            "application": {
                "redis.addr": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn:19001",
                "server.rocketMq.mqNameSrv": "push-producer1.rocketmq.ehomeinner.cn:9876,push-producer2.rocketmq.ehomeinner.cn:9876,push-producer3.rocketmq.ehomeinner.cn:9876,push-producer4.rocketmq.ehomeinner.cn:9876",
            },
        },
        "push-sdk": {
            "application": {
                "aipkg.rocketmq.nameServerAddr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "elasticsearch.esAddress": "common1-local.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "rocketmq.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.ai-recognition.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.ai-recognition.port": "9876",
                "rocketmq.consumer.disposition.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.disposition.port": "9876",
                "rocketmq.message.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.message.port": "9876",
                "rocketmq.messageAddress": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.msg-push.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.msg-push.port": "9876",
                "rocketmq.port": "9876",
                "spring.datasource.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "submit-cloud": {
            "application": {
                "redis.addr": "submitcloudservice.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "submit-cloud-v2": {
            "application": {
                "redis.addr": "submitcloudservice-v2.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.guiZhouAddress": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "tree-synvcp": {
            "application": {
                "kafka.brokerList": "binlog1-guizh.kafka.ehome.dlifeinner.cn:9092,binlog2-guizh.kafka.ehome.dlifeinner.cn:9092,binlog3-guizh.kafka.ehome.dlifeinner.cn:9092",
                "rocketmq.name-server": "tree-synvcp1.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp2.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp3.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "treeSevice": {
            "application": {
                "rocketMq.consumer.message.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.consumer.message.port": "9876",
                "rocketMq.producer.apiSend.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.apiSend.server.port": "9876",
                "rocketMq.producer.group.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.group.server.port": "9876",
                "rocketMq.producer.patrolRegion.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.patrolRegion.server.port": "9876",
                "rocketMq.producer.region.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.region.server.port": "9876",
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://viid.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.third.jdbc-url": "jdbc:mysql://camera.tidb.ehomeinner.cn:3306",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "uni-order": {
            "application": {
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://order.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.kafka.ai-platform.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.producer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.third-platform.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.tykj.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.tyyy.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.uni-product-order.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.redis.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "vnet-cloud-broadcast": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://other.mysql.shardingsphere.ehome.ctseelinkinner.cn:13301",
                "spring.redis.host": "other.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19003",
            },
        },
        "vnet-icam-push": {
            "application": {
                "rocketmq.producer.vpaas-device.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.vpaas-device.port": "9876",
            },
        },
        "vnet-vpaas-manage": {
            "application": {
                "redis.pool.host": "ehome-access.codis.ehomeinner.cn",
                "redis.pool.port": "16000",
                "spring.datasource.url": "jdbc:mysql://ehome-access.mysql.ehomeinner.cn:13301",
            },
        },
    },
    3: {
        "adapter-task-api": {
            "application": {
                "spring.redis.cluster.nodes": "open-portal1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.redis.host": "open-portal.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19005",
            },
        },
        "apollo-ehome-pro": {
            "application": {
                "spring.datasource.camera.management.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13303",
                "spring.datasource.ehome.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13302",
                "spring.datasource.temp.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13303",
            },
        },
        "area": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://areacode.mysql.vcpinner.cn:13302",
            },
        },
        "auth": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
            },
        },
        "cloud-ehome": {
            "application": {
                "redis.addr": "vcp-common-local.codis.vcpinner.cn:19000",
                "rocketmq.addr": "cloudservice-v2-1.rocketmq.vcpinner.cn,cloudservice-v2-2.rocketmq.vcpinner.cn,cloudservice-v2-3.rocketmq.vcpinner.cn,cloudservice-v2-4.rocketmq.vcpinner.cn",
                "rocketmq.picAddr": "cloudservice-v2-1.rocketmq.vcpinner.cn:9876,cloudservice-v2-2.rocketmq.vcpinner.cn:9876,cloudservice-v2-3.rocketmq.vcpinner.cn:9876,cloudservice-v2-4.rocketmq.vcpinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "cloud-video": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
            },
        },
        "ddnsmgr": {
            "application": {
                "redis.addr": "ddns-mgr.redis.vcpinner.cn:65379",
            },
        },
        "detect": {
            "application": {
                "rocketmq.address": "detect1.rocketmq.vcpinner.cn,detect2.rocketmq.vcpinner.cn,detect3.rocketmq.vcpinner.cn,detect4.rocketmq.vcpinner.cn",
                "rocketmq.port": "9876",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://push.mysql.vcpinner.cn:13322",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "device-access": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
            },
        },
        "device-status-service": {
            "application": {
                "rocketmq.status.center.server.address": "status-push1.rocketmq.vcpinner.cn,status-push2.rocketmq.vcpinner.cn,status-push3.rocketmq.vcpinner.cn,status-push4.rocketmq.vcpinner.cn",
                "rocketmq.status.center.server.port": "9876",
                "rocketmq.status.province.server.address": "status-ddns1.rocketmq.vcpinner.cn,status-ddns2.rocketmq.vcpinner.cn",
                "rocketmq.status.province.server.port": "9876",
                "rocketmq.status.sdk.server.address": "status-sdk1.rocketmq.vcpinner.cn,status-sdk2.rocketmq.vcpinner.cn,status-sdk3.rocketmq.vcpinner.cn,status-sdk4.rocketmq.vcpinner.cn",
                "rocketmq.status.sdk.server.port": "9876",
                "spring.datasource.url": "jdbc:mysql://common.mysql.shardingsphere.vcp.ctseelinkinner.cn:13302",
                "spring.redis.cluster.nodes": "vcp-common.codis-gw.vcpinner.cn:19000",
                "spring.redis.host": "vcp-common.codis-gw.vcpinner.cn",
                "spring.redis.port": "19000",
            },
        },
        "ehome": {
            "application": {
                "rocketmq.cloud.NameServer": "ehome1.rocketmq.vcpinner.cn:9876,ehome2.rocketmq.vcpinner.cn:9876,ehome3.rocketmq.vcpinner.cn:9876,ehome4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://ehome.mysql.vcpinner.cn:13302",
                "spring.datasource.pro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "ehome-account": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
            },
        },
        "ehome-order": {
            "application": {
                "rocketmq.cloud.NameServer": "order1.rocketmq.vcpinner.cn:9876,order2.rocketmq.vcpinner.cn:9876,order3.rocketmq.vcpinner.cn:9876,order4.rocketmq.vcpinner.cn:9876",
                "rocketmq.mqNameSrvAddr": "cloudstore-change1.rocketmq.vcpinner.cn:9876,cloudstore-change2.rocketmq.vcpinner.cn:9876",
                "spring.datasource.ehome.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcporder.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19001",
                "spring.redis.redisson.config": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "ehome-pro-viidmgr": {
            "application": {
                "config.kafka.bootstrap-servers": "117.80.149.102:9092",
                "rocketmq.name-server": "viidmgr1.rocketmq.vcpinner.cn:9876,viidmgr2.rocketmq.vcpinner.cn:9876,viidmgr3.rocketmq.vcpinner.cn:9876,viidmgr4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://viid.mysql.vcp.dlifeinner.cn:13304",
            },
        },
        "ehome-security": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.redis.host": "security.redis.vcpinner.cn",
                "spring.redis.port": "65379",
            },
        },
        "portal-api-dep": {
            "application": {
                "spring.redis.cluster.nodes": "open-portal1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.redis.host": "open-portal.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19005",
            },
        },
        "push": {
            "application": {
                "rocketmq.message.address": "message1.rocketmq.vcpinner.cn,message2.rocketmq.vcpinner.cn",
                "rocketmq.message.port": "9876",
                "rocketmq.server.address": "push1.rocketmq.vcpinner.cn,push2.rocketmq.vcpinner.cn,push3.rocketmq.vcpinner.cn,push4.rocketmq.vcpinner.cn",
                "rocketmq.server.port": "9876",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://push.mysql.vcpinner.cn:13302",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.url": "jdbc:mysql://detect.mysql.vcpinner.cn:13322",
                "spring.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "push-pro": {
            "application": {
                "spring.datasource.camera.management.url": "jdbc:mysql://push-pro.mysql.ehomeinner.cn:13303",
                "spring.datasource.ehome.url": "jdbc:mysql://push-pro.mysql.ehomeinner.cn:13302",
            },
        },
        "push-producer": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
                "server.rocketMq.mqNameSrv": "push-producer1.rocketmq.vcpinner.cn:9876,push-producer2.rocketmq.vcpinner.cn:9876,push-producer3.rocketmq.vcpinner.cn:9876,push-producer4.rocketmq.vcpinner.cn:9876",
            },
        },
        "seelink-batch-service": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "sl-center-auth-application": {
            "application": {
                "spring.data.redis.host": "integration4.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19008",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
            },
        },
        "sl-center-device-access": {
            "application": {
                "rocketmq.consumer.access.device-change.name-server": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.name-server": "127.0.0.1:9876",
                "rocketmq.producer.device-operation.address": "127.0.0.1:9876",
                "spring.data.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "sl-center-device-auth": {
            "application": {
                "spring.data.redis.cluster.nodes": "common1-redis-cluster.vcp.dlifeinner.cn:17001",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.url": "jdbc:mysql://127.0.0.1:4000",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "sl-center-device-control": {
            "application": {
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.kafka.device-operation.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "sl-center-device-sync": {
            "application": {
                "elasticsearch.cluster": "integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.vcp-pro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.vcp.jdbc-url": "jdbc:mysql://ehome.mysql.vcpinner.cn:13302",
                "spring.kafka.binlog.properties.bootstrap-servers": "integration4-common-binlog-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-3.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.business.properties.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.event.first.bootstrap-servers": "integration4-common-binlog-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-3.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.event.sync.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.sync.tofour.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.sync.upcus.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "sl-center-device-tree": {
            "application": {
                "spring.data.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "sl-center-goods-order": {
            "application": {
                "spring.cloud.nacos.port": "8848",
                "spring.cloud.nacos.server-addr": "sl-nacos-0.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-1.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-2.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-3.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-4.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.dynamic.datasource.sl-goods.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.datasource.dynamic.datasource.vcp-pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.kafka.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
            },
        },
        "sl-center-goods-packages": {
            "application": {
                "rocketmq.name-server": "integration4-alert-1.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-2.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-3.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-4.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-5.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-6.rocketmq.vcp.ctseelinkinner.cn:9876",
                "spring.cloud.nacos.port": "8848",
                "spring.cloud.nacos.server-addr": "sl-nacos-0.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-1.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-2.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-3.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-4.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.dynamic.datasource.sl-goods.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.datasource.dynamic.datasource.vcp-pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.kafka.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.device.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.self.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
            },
        },
        "sl-es-dual-active": {
            "application": {
                "elasticsearch.master.esAddress": "gz-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "elasticsearch.slave.esAddress": "gz4-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "elasticsearch.standby.esAddress": "gz3-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.kafka.es.dual.write.producer.bootstrapServers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.es.write.slave.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.es.write.standby.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
            },
        },
        "slink-biz-user": {
            "application": {
                "rocketmq.name-server": "push1.rocketmq.vcpinner.cn:9876,push2.rocketmq.vcpinner.cn:9876,push3.rocketmq.vcpinner.cn:9876,push4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.dynamic.datasource.mysql.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.dynamic.datasource.tidb.url": "jdbc:mysql://order.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "6379",
                "spring.redis.port": "19001",
            },
        },
        "submit-cloud": {
            "application": {
                "redis.addr": "vcp-common-local.codis.vcpinner.cn:19000",
                "rocketmq.addr": "submitcloudservice1.rocketmq.vcpinner.cn,submitcloudservice2.rocketmq.vcpinner.cn,submitcloudservice3.rocketmq.vcpinner.cn,submitcloudservice4.rocketmq.vcpinner.cn",
                "rocketmq.picAddr": "submitcloudservice1.rocketmq.vcpinner.cn:9876,submitcloudservice2.rocketmq.vcpinner.cn:9876,submitcloudservice3.rocketmq.vcpinner.cn:9876,submitcloudservice4.rocketmq.vcpinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "treeSevice": {
            "application": {
                "rocketMq.consumer.message.address": "tree-service1.rocketmq.ehomeinner.cn,tree-service2.rocketmq.ehomeinner.cn,tree-service3.rocketmq.ehomeinner.cn,tree-service4.rocketmq.ehomeinner.cn",
                "rocketMq.consumer.message.port": "9876",
                "rocketMq.producer.group.server.address": "tree-service1.rocketmq.ehomeinner.cn,tree-service2.rocketmq.ehomeinner.cn,tree-service3.rocketmq.ehomeinner.cn,tree-service4.rocketmq.ehomeinner.cn",
                "rocketMq.producer.group.server.port": "9876",
                "rocketMq.producer.region.server.address": "tree-service1.rocketmq.ehomeinner.cn,tree-service2.rocketmq.ehomeinner.cn,tree-service3.rocketmq.ehomeinner.cn,tree-service4.rocketmq.ehomeinner.cn",
                "rocketMq.producer.region.server.port": "9876",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://tree-service.mysql.ehomeinner.cn:13303",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://tree-service-viid.mysql.ehomeinner.cn:13304",
            },
        },
        "vcp-AIManager": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://192.168.20.107:3301",
                "spring.kafka.bootstrap-servers": "127.0.0.1:9092",
            },
        },
        "vcp-alert-web": {
            "application": {
                "server.port": "jdbc:mysql://vcpalertweb.mysql.vcpinner.cn:13303",
            },
        },
        "vcp-businesshall": {
            "application": {
                "rocketmq.name-server": "businesshall1.rocketmq.vcpinner.cn:9876,businesshall2.rocketmq.vcpinner.cn:9876,businesshall3.rocketmq.vcpinner.cn:9876,businesshall4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://businesshall.mysql.vcpinner.cn:13301",
            },
        },
        "vcp-cascade-manage": {
            "application": {
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "rocketmq.producer.alertCenter.address.1400": "common1.rocketmq.vcp.ctseelinkinner.cn:9876,common2.rocketmq.vcp.ctseelinkinner.cn:9876,common3.rocketmq.vcp.ctseelinkinner.cn:9876,common4.rocketmq.vcp.ctseelinkinner.cn:9876",
                "spring.datasource.ehome.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "vcp-ehomesync": {
            "application": {
                "kj.rocketmq.nameServerAddr": "tree-synvcp1.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp2.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp3.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.ehome.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcp_pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-gather": {
            "application": {
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcp_pro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-openapi": {
            "application": {
                "spring.datasource.dynamic.datasource.ehome_back_up.url": "jdbc:mysql://openapi.mysql.ehomeinner.cn:13302",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13303",
                "spring.redis.host": "ehome-pro.codis.ehomeinner.cn",
                "spring.redis.port": "19000",
            },
        },
        "vcp-openmgr": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://openmgr.mysql.ehomeinner.cn:13303",
            },
        },
        "vcp-pro": {
            "application": {
                "spring.datasource.vcp.jdbc-url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcppro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-proapp": {
            "application": {
                "rocketmq.name-server": "192.168.23.167:9876",
                "spring.datasource.url": "jdbc:mysql://ehome-pro.mysql.vcpinner.cn:13303",
            },
        },
        "vcp-proappweb-web": {
            "application": {
                "rocketmq.name-server": "vcp-proapp-web1.rocketmq.vcpinner.cn:9876,vcp-proapp-web2.rocketmq.vcpinner.cn:9876,vcp-proapp-web3.rocketmq.vcpinner.cn:9876,vcp-proapp-web4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.first.url": "jdbc:mysql://proappweb.mysql.vcpinner.cn:13303",
                "spring.datasource.second.url": "jdbc:mysql://proappweb.mysql.vcpinner.cn:13303",
            },
        },
        "vcp-saas-cmpt-scn": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-saas.mysql.vcpinner.cn:13302",
                "spring.redis.cluster.nodes": "saas1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "vcp-saas-grassroots-governance": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-saas.mysql.vcpinner.cn:13302",
                "spring.redis.host": "saas.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19003",
            },
        },
        "vcp-saas-jxc": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-saas.mysql.vcpinner.cn:13302",
                "spring.redis.host": "saas.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19003",
            },
        },
        "vcp-sync-es": {
            "application": {
                "elasticsearch.host": "tree-service.elasticsearch.vcpinner.cn",
                "elasticsearch.port": "9201",
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-tree-service": {
            "application": {
                "addProRegion.rocketmq.nameServerAddr": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "addProRegionDev.rocketmq.nameServerAddr": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "elasticsearch.cluster": "vcp-common1.elasticsearch.vcpinner.cn:9201",
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "vcp-viid-node": {
            "application": {
                "rocketMq.consumer.again.message.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.consumer.again.message.port": "9876",
                "rocketMq.consumer.huiJu.message.address": "viidnode1.rocketmq.vcpinner.cn,viidnode2.rocketmq.vcpinner.cn,viidnode3.rocketmq.vcpinner.cn,viidnode4.rocketmq.vcpinner.cn",
                "rocketMq.consumer.huiJu.message.port": "9876",
                "rocketMq.consumer.picture.message.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.consumer.picture.message.port": "9876",
                "rocketMq.producer.again.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.producer.again.port": "9876",
                "rocketMq.producer.huiJu.address": "viidnode1.rocketmq.vcpinner.cn,viidnode2.rocketmq.vcpinner.cn,viidnode3.rocketmq.vcpinner.cn,viidnode4.rocketmq.vcpinner.cn",
                "rocketMq.producer.huiJu.port": "9876",
                "rocketMq.producer.picture.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.producer.picture.port": "9876",
                "spring.redis.cluster.nodes": "1400-viidnode1.redis-cluster.vcp.ctseelinkinner.cn:7000",
            },
        },
        "vcp-waters-monitor": {
            "application": {
                "ehome-pro-rocketmq.name-server": "192.168.2.28:9876",
                "rocketmq.name-server": "vcp-proapp1.rocketmq.vcpinner.cn:9876,vcp-proapp2.rocketmq.vcpinner.cn:9876,vcp-proapp3.rocketmq.vcpinner.cn:9876,vcp-proapp4.rocketmq.vcpinner.cn:9876",
                "rocketmq.producer.namesrvAddr": "vcp-proapp1.rocketmq.vcpinner.cn:9876,vcp-proapp2.rocketmq.vcpinner.cn:9876,vcp-proapp3.rocketmq.vcpinner.cn:9876,vcp-proapp4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.vcppro.jdbc-url": "jdbc:mysql://192.168.20.107:3301",
                "spring.datasource.vcpwaters.jdbc-url": "jdbc:mysql://192.168.20.107:3301",
                "spring.redis.host": "codis.vcpinner.cn",
                "spring.redis.port": "16379",
            },
        },
    },
    8: {
        "bc-bp-device-control": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-bp-device-manage": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-bp-manage": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-bp-management-platform": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-bp-message": {
            "application": {
                "rocketmq.vpaas-device-message.address": "192.168.11.119:9876",
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-bp-order": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-bp-user": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "bc-pa-platform": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://172.17.228.216:6446",
            },
        },
        "mc-centric-consumer20190129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "rocketmq.config.namesrvAddr": "172.17.228.166:9876,172.17.231.66:9876",
            },
        },
        "mc-centric-data-init20190129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "spring.datasource.url": "jdbc:mysql://172.17.228.31:6446",
            },
        },
        "mc-centric-elink-openapi20190129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "rocketmq.config.namesrvAddr": "172.17.228.166:9876,172.17.231.66:9876",
                "spring.datasource.url": "jdbc:mysql://172.17.228.31:6446",
            },
        },
        "mc-centric-management20190129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "rocketmq.config.namesrvAddr": "172.17.228.166:9876,172.17.231.66:9876",
                "spring.datasource.url": "jdbc:mysql://172.17.228.31:6446",
            },
        },
        "mc-provincial-consumer20190129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "rocketmq.config.namesrvAddr": "172.17.228.166:9876,172.17.231.66:9876",
            },
        },
        "mc-provincial-management20190129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "rocketmq.config.namesrvAddr": "172.17.228.166:9876,172.17.231.66:9876",
                "zookeeper.server.hosts": "172.17.228.68:2181,172.17.229.227:2181,172.17.230.42:2181",
            },
        },
        "mqtt-centirc-login20190130": {
            "application": {
                "centric.redis.device.envconfig.url": "192.168.17.124:16001",
                "redis.cluster.hosts": "172.17.228.214:16000",
            },
        },
        "mqtt-provincial-connector0129": {
            "application": {
                "redis.cluster.hosts": "172.17.228.214:16000",
                "rocketmq.config.namesrvAddr": "172.17.228.166:9876,172.17.231.66:9876",
                "zookeeper.server.hosts": "172.17.228.68:2181,172.17.229.227:2181,172.17.230.42:2181",
            },
        },
        "slink-amp-iot": {
            "application": {
                "spring.datasource.jdbc-url": "jdbc:mysql://172.17.230.158:3301",
            },
        },
    },
    13: {
        "area": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://areacode.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "auth": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://auth.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "bellpush": {
            "application": {
                "rocketmq.address": "bellpush1.rocketmq.ehome.dlifeinner.cn,bellpush2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.shardingsphere.ehome.ctseelinkinner.cn:3306",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
                "spring.redis2.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis2.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis2.port": "19004",
            },
        },
        "cameraInfo": {
            "application": {
                "spring.datasource.ehome.backup.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.ehome.main.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
            },
        },
        "cloud-ehome": {
            "application": {
                "redis.addr": "cloudservice.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "cloud-ehome-v3": {
            "application": {
                "redis.addr": "cloudservice-v2.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "cloud-prod-xxl-job-admin": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://192.168.8.131:13301",
            },
        },
        "cloud-video": {
            "application": {
                "redis.addr": "cloudservice.redis-gw.ehome.dlifeinner.cn:19001",
            },
        },
        "cp-dispatch": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-mall": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-manage": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-manage-new": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "cp-store": {
            "application": {
                "elasticsearch.uris": "192.168.50.187:9200",
                "spring.datasource.url": "jdbc:mysql://192.168.54.244:13301",
                "spring.redis.sentinel.nodes": "192.168.50.187:26379",
            },
        },
        "ddnsmgr": {
            "application": {
                "redis.addr": "ddns-mgr.redis.ehomeinner.cn:65379",
            },
        },
        "detect": {
            "application": {
                "rocketmq.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.shardingsphere.ehome.ctseelinkinner.cn:3306",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
                "spring.redis2.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis2.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis2.port": "19004",
            },
        },
        "detect-sdk": {
            "application": {
                "gb-gateway.rocketmq.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "gb-gateway.rocketmq.port": "9876",
                "redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "rocketmq.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.url": "jdbc:mysql://ehome-read.mysql.ehome.dlifeinner.cn:3302",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "device-access": {
            "application": {
                "redis.cluster.addr": "core1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "device-status-service": {
            "application": {
                "rocketmq.M.status.center.server.address": "sdk2-detectmessage1-gz.rocketmq.ehome.ctseelinkinner.cn,sdk2-detectmessage2-gz.rocketmq.ehome.ctseelinkinner.cn,sdk2-detectmessage3-gz.rocketmq.ehome.ctseelinkinner.cn,sdk2-detectmessage4-gz.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.center.server.port": "9876",
                "rocketmq.M.status.gb.server.address": "common-alarmmessage1-gz.rocketmq.ehome.ctseelinkinner.cn,common-alarmmessage2-gz.rocketmq.ehome.ctseelinkinner.cn,common-alarmmessage3-gz.rocketmq.ehome.ctseelinkinner.cn,common-alarmmessage4-gz.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.gb.server.port": "9876",
                "rocketmq.M.status.province.server.address": "sdk2-onofflinemessage1-gz.rocketmq.ehome.ctseelinkinner.cn,sdk2-onofflinemessage2-gz.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.province.server.port": "9876",
                "rocketmq.M.status.sdk.server.address": "sdk3a4-onofflinemessage1-gz.rocketmq.ehome.ctseelinkinner.cn,sdk3a4-onofflinemessage2-gz.rocketmq.ehome.ctseelinkinner.cn,sdk3a4-onofflinemessage3-gz.rocketmq.ehome.ctseelinkinner.cn,sdk3a4-onofflinemessage4-gz.rocketmq.ehome.ctseelinkinner.cn",
                "rocketmq.M.status.sdk.server.port": "9876",
                "rocketmq.status.center.server.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.center.server.port": "9876",
                "rocketmq.status.gb.server.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.gb.server.port": "9876",
                "rocketmq.status.province.server.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.province.server.port": "9876",
                "rocketmq.status.sdk.server.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.status.sdk.server.port": "9876",
                "rocketmq.vcp.status.center.server.address": "common1.rocketmq.vcp.ctseelinkinner.cn,common2.rocketmq.vcp.ctseelinkinner.cn,common3.rocketmq.vcp.ctseelinkinner.cn,common4.rocketmq.vcp.ctseelinkinner.cn",
                "rocketmq.vcp.status.center.server.port": "9876",
                "rocketmq.vcp.status.province.server.address": "common1.rocketmq.vcp.ctseelinkinner.cn,common2.rocketmq.vcp.ctseelinkinner.cn,common3.rocketmq.vcp.ctseelinkinner.cn,common4.rocketmq.vcp.ctseelinkinner.cn",
                "rocketmq.vcp.status.province.server.port": "9876",
                "rocketmq.vcp.status.sdk.server.address": "common1.rocketmq.vcp.ctseelinkinner.cn,common2.rocketmq.vcp.ctseelinkinner.cn,common3.rocketmq.vcp.ctseelinkinner.cn,common4.rocketmq.vcp.ctseelinkinner.cn",
                "rocketmq.vcp.status.sdk.server.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.url": "jdbc:mysql://common.tidb.ehome.ctseelinkinner.cn:14001",
                "spring.redis.cluster.nodes": "other1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome": {
            "application": {
                "rocketmq.cloud.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.mqNameServer": "api1.rocketmq.ehome.dlifeinner.cn:9876,api2.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.producer.cloudOrderOperationInfo.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.cloudOrderOperationInfo.port": "9876",
                "rocketmq.producer.deviceBind.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.deviceBind.port": "9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.ehome.ctseelinkinner.cn:3306",
            },
        },
        "ehome-account": {
            "application": {
                "redis.addr": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn:19002",
                "redis.cluster.addr": "core1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-account-new-service": {
            "application": {
                "spring.data.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.data.redis.port": "19002",
                "spring.datasource.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
            },
        },
        "ehome-ai-care": {
            "application": {
                "elasticsearch.esAddress": "common1-local.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "rocketmq.aicare-data-analysis.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.aicare-data-analysis.port": "9876",
                "rocketmq.producer.aiCareDataAnalysis.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.aiCareDataAnalysis.port": "9876",
                "rocketmq.smart-alert.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smart-alert.port": "9876",
                "spring.datasource.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3301",
                "spring.kafka.ai-care.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.ai-care.producer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.delay-sms.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.delay-sms.producer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.redis.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19004",
            },
        },
        "ehome-api": {
            "application": {
                "elasticsearch.esAddress": "ehome-api1-elasticsearch.ehomeinner.cn:9200",
                "rocketmq.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.audio.namesrvAddr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.audioSync.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.audioSync.port": "9876",
                "rocketmq.common.namesrvAddr": "ehome-api3-rocketmq.ehomeinner.cn:9876,ehome-api4-rocketmq.ehomeinner.cn:9876,ehome-api5-rocketmq.ehomeinner.cn:9876,ehome-api6-rocketmq.ehomeinner.cn:9876",
                "rocketmq.consumer.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.consumer.port": "9876",
                "rocketmq.device-reset.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.device-reset.port": "9876",
                "rocketmq.deviceReset.address": "ehome-router1.rocketmq.ehomeinner.cn:9876",
                "rocketmq.hxBmsSync.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.hxBmsSync.port": "9876",
                "rocketmq.port": "9876",
                "rocketmq.producer.audioSync.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.producer.audioSync.port": "9876",
                "rocketmq.producer.device-operation.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.device-operation.port": "9876",
                "rocketmq.producer.deviceBind.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.deviceBind.port": "9876",
                "rocketmq.producer.hxBmsSync.address": "ehome-api1.rocketmq.ehomeinner.cn,ehome-api2.rocketmq.ehomeinner.cn",
                "rocketmq.producer.hxBmsSync.port": "9876",
                "rocketmq.smc.address": "ehome1.rocketmq.ehomeinner.cn,ehome2.rocketmq.ehomeinner.cn,ehome3.rocketmq.ehomeinner.cn,ehome4.rocketmq.ehomeinner.cn",
                "rocketmq.smc.port": "9876",
                "rocketmq.unBindCloud.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://192.168.4.43:3306",
                "spring.datasource.url": "jdbc:mysql://ehome-api.mysql.ehomeinner.cn:3301",
            },
        },
        "ehome-cascade": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://ehome-access.mysql.ehomeinner.cn:3306",
                "spring.redis.host": "ehome-access.codis.ehomeinner.cn",
                "spring.redis.port": "16000",
            },
        },
        "ehome-command": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-es-dual-active": {
            "application": {
                "elasticsearch.master.esAddress": "common1-guangzh4.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "elasticsearch.slave.esAddress": "common1-guizh.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "elasticsearch.standby.esAddress": "common1-guizh3.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "spring.datasource.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3301",
                "spring.kafka.es.dual.write.producer.bootstrapServers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.es.write.slave.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.es.write.standby.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
            },
        },
        "ehome-gallery": {
            "application": {
                "rocketmq.addr": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.port": "9876",
            },
        },
        "ehome-ims": {
            "application": {
                "rocketmq.consumer.bindkey.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.bindkey.port": "9876",
                "rocketmq.producer.bindkey.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://ehome-ims.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-mansync": {
            "application": {
                "data.kafka.brokers": "192.168.10.48:9092,192.168.11.228:9092,192.168.9.130:9092",
                "data.redis.addr": "ehome-sroute.codis.ehomeinner.cn:17000",
            },
        },
        "ehome-order": {
            "application": {
                "rocketmq.audio.namesrvAddr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.cloud.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.cloudCamera.areaControl.NameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.cloud.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.cloud.port": "9876",
                "rocketmq.consumer.deviceBind.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.deviceBind.port": "9876",
                "rocketmq.consumer.unbind.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.unbind.port": "9876",
                "rocketmq.mqNameSrvAddr": "api1.rocketmq.ehome.dlifeinner.cn:9876,api2.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.producer.aiOrderOperationInfo.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.aiOrderOperationInfo.port": "9876",
                "rocketmq.producer.cloudNotify.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.cloudNotify.port": "9876",
                "rocketmq.producer.cloudOrderOperationInfo.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.cloudOrderOperationInfo.port": "9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://order.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://order.mysql.ehomeinner.cn:3302",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-ota": {
            "application": {
                "mysql.url": "ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "ota.nacos.addr": "vpaas-nacos.vpaas-nacos",
                "ota.nacos.port": "8848",
                "redis.addr": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn:19001",
                "rocketMq.nameSrvAddr": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
            },
        },
        "ehome-pro-device": {
            "application": {
                "DruidDb1.dbUrl": "camera.tidb.ehomeinner.cn:3306",
                "DruidDb2.dbUrl": "ehome.mysql.ehomeinner.cn:3301",
                "rocketmq.name-server": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn:9876,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn:9876,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn:9876,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-pro-open": {
            "application": {
                "rocketmq.device-dis.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.device-dis.port": "9876",
                "rocketmq.smart-device-dis.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smart-device-dis.port": "9876",
                "spring.datasource.ehome.backup.url": "jdbc:mysql://ehome-read.mysql.ehome.ctseelinkinner.cn:3302",
                "spring.datasource.ehome.main.back.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.ehome.main.url": "jdbc:mysql://ehome.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "core.redis-gw.ehome.dlifeinner.cn",
                "spring.redis.port": "6379",
            },
        },
        "ehome-pro-openmgr": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.redis.cluster.nodes": "ability-open1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-pro-viidmgr": {
            "application": {
                "rocketmq.name-server": "viidmgr1.rocketmq.ehomeinner.cn:9876,viidmgr2.rocketmq.ehomeinner.cn:9876,viidmgr3.rocketmq.ehomeinner.cn:9876,viidmgr4.rocketmq.ehomeinner.cn:9876",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://192.168.5.180:3301",
            },
        },
        "ehome-prof-device": {
            "application": {
                "spring.datasource.dynamic.datasource.can_write_camera_management.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.dynamic.datasource.ehome_back_up.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.redis.cluster.nodes": "ability-open1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-prof-push": {
            "application": {
                "rocketmq.center.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.center.port": "9876",
                "rocketmq.device-dis.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.device-dis.port": "9876",
                "rocketmq.province.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.province.port": "9876",
                "rocketmq.push-meaage.address": "tree-synvcp1.rocketmq.ehome.dlifeinner.cn,tree-synvcp2.rocketmq.ehome.dlifeinner.cn,tree-synvcp3.rocketmq.ehome.dlifeinner.cn,tree-synvcp4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.push-meaage.port": "9876",
                "rocketmq.sdk-device-alarm.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.sdk-device-alarm.port": "9876",
                "rocketmq.self-support-bell.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.self-support-bell.port": "9876",
                "rocketmq.self-support-device-alarm.address": "push1.rocketmq.ehomeinner.cn,push2.rocketmq.ehomeinner.cn,push3.rocketmq.ehomeinner.cn,push4.rocketmq.ehomeinner.cn",
                "rocketmq.self-support-device-alarm.port": "9876",
                "rocketmq.unified-sdk.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.unified-sdk.port": "9876",
                "rocketmq.zhongwei-support-bell.address": "bellpush1.rocketmq.ehome.dlifeinner.cn,bellpush2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.zhongwei-support-bell.port": "9876",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://camera.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.redis.cluster.nodes": "ability-open1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "ehome-public": {
            "application": {
                "rocketmq.consumer.device-operation.name-server": "public-push1.rocketmq.ehomeinner.cn:9876,public-push2.rocketmq.ehomeinner.cn:9876,public-push3.rocketmq.ehomeinner.cn:9876,public-push4.rocketmq.ehomeinner.cn:9876",
                "rocketmq.consumer.deviceBind.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceBindNotify.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceBindNotifyImsRetry.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceShareInfoHandle.nameServer": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceUnBindNotifyImsRetry.nameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceUnbind.nameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.deviceUnbindNotify.nameServer": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.consumer.unbind.name-server": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.name-server": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.producer.device-operation.address": "public-push1.rocketmq.ehomeinner.cn,public-push2.rocketmq.ehomeinner.cn,public-push3.rocketmq.ehomeinner.cn,public-push4.rocketmq.ehomeinner.cn",
                "rocketmq.producer.device-operation.port": "9876",
                "rocketmq.producer.deviceBind.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceBind.port": "9876",
                "rocketmq.producer.deviceUnbind.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceUnbind.port": "9876",
                "spring.datasource.druid.master.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.slave.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3302",
                "spring.datasource.druid.tidb-push.url": "jdbc:mysql://integrationapp.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.ehome.ctseelinkinner.cn:3306",
                "spring.kafka.uid-sys.producer.bootstrap-servers": "10.135.94.152:9092,10.135.94.173:9092,10.135.94.181:9092,10.135.94.206:9092,10.135.94.40:9092,10.135.95.204:9092",
                "spring.redis.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19004",
            },
        },
        "ehome-public-push": {
            "application": {
                "rocketmq.ai-analyze-alarm.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.ai-analyze-alarm.port": "9876",
                "rocketmq.center.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.center.port": "9876",
                "rocketmq.fall-alarm.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.fall-alarm.port": "9876",
                "rocketmq.j-push-highest-priority.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.j-push-highest-priority.port": "9876",
                "rocketmq.j-push-platform.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.j-push-platform.port": "9876",
                "rocketmq.producer.ai-care-event.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.ai-care-event.port": "9876",
                "rocketmq.producer.default-AI.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.default-AI.port": "9876",
                "rocketmq.producer.j-push-highest-priority.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.j-push-highest-priority.port": "9876",
                "rocketmq.producer.j-push-platform.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.j-push-platform.port": "9876",
                "rocketmq.producer.sdkCameraAlarm.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.sdkCameraAlarm.port": "9876",
                "rocketmq.producer.smartAlertElderlyCare.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.smartAlertElderlyCare.port": "9876",
                "rocketmq.producer.xygj-platform-main.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.xygj-platform-main.port": "9876",
                "rocketmq.producer.xygj-platform-push.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.xygj-platform-push.port": "9876",
                "rocketmq.producer.xygj-platform-uppermost.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.xygj-platform-uppermost.port": "9876",
                "rocketmq.province.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.province.port": "9876",
                "rocketmq.sdk-device-alarm.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.sdk-device-alarm.port": "9876",
                "rocketmq.self-support-bell.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.self-support-bell.port": "9876",
                "rocketmq.self-support-device-alarm.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.self-support-device-alarm.port": "9876",
                "rocketmq.smart-alert-elderly-care.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.smart-alert-elderly-care.port": "9876",
                "rocketmq.unified-sdk.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.unified-sdk.port": "9876",
                "rocketmq.vpaas-unified-sdk.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.vpaas-unified-sdk.port": "9876",
                "rocketmq.xygj-platform-main.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.xygj-platform-main.port": "9876",
                "rocketmq.xygj-platform-push.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.xygj-platform-push.port": "9876",
                "rocketmq.xygj-platform-uppermost.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.xygj-platform-uppermost.port": "9876",
                "rocketmq.zhongwei-support-bell.address": "bellpush1.rocketmq.ehome.dlifeinner.cn,bellpush2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.zhongwei-support-bell.port": "9876",
                "spring.datasource.dynamic.datasource.ehome_back_up.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3302",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://ehome-public-push.mysql.ehomeinner.cn:3301",
                "spring.datasource.dynamic.datasource.tidb.url": "jdbc:mysql://integrationapp.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.redis.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19004",
            },
        },
        "ehome-public-transmit": {
            "application": {
                "rocketmq.consumer.ai.default.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.ai.default.port": "9876",
            },
        },
        "ehome-security": {
            "application": {
                "rocketmq.producer.deviceShareInfoHandle.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.deviceShareInfoHandle.port": "9876",
                "spring.datasource.url": "jdbc:mysql://security.mysql.ehomeinner.cn:3301",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
            },
        },
        "ehome-security-fs": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://10.131.7.22:8301",
            },
        },
        "ehome-sigtran-worker": {
            "application": {
                "redis.addr": "codis1.ehomeinner.cn:17000",
                "rocketmq.consumer.nameSrvAddr": "192.168.3.121:9876,192.168.3.140:9876,192.168.3.198:9876,192.168.3.29:9876",
            },
        },
        "file": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
            },
        },
        "message": {
            "application": {
                "rocketmq.producer.nameSrvAddr": "182.43.129.36:9876,182.43.155.189:9876,203.195.74.104:9876,203.195.74.174:9876",
            },
        },
        "omvs-cloud-api": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://192.168.233.132:3303",
            },
        },
        "push": {
            "application": {
                "rocketmq.message.address": "sdk2-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk2-onofflinemessage2.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.message.port": "9876",
                "rocketmq.server.address": "sdk2-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk2-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.server.port": "9876",
                "spring.cloud.nacos.discovery.server-addr": "vpaas-nacos.vpaas-nacos:8848",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://camera.tidb.shardingsphere.ehome.ctseelinkinner.cn:3306",
                "spring.redis.cluster.nodes": "core1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.host": "core.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19002",
                "spring.redis2.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis2.host": "public-push.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis2.port": "19004",
                "spring.redis3.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis3.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis3.port": "19001",
            },
        },
        "push-pro": {
            "application": {
                "rocketMq.consumer.message.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.consumer.message.port": "9876",
                "rocketMq.produce.ai.mq.address": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.produce.ai.mq.port": "9876",
                "rocketmq.motionDetection.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.motionDetection.port": "9876",
                "spring.datasource.camera.management.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.camera.url": "jdbc:mysql://camera.tidb.ehomeinner.cn:3306",
                "spring.datasource.ehome.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "push-producer": {
            "application": {
                "redis.addr": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn:19001",
                "server.rocketMq.mqNameSrv": "push-producer1.rocketmq.ehomeinner.cn:9876,push-producer2.rocketmq.ehomeinner.cn:9876,push-producer3.rocketmq.ehomeinner.cn:9876,push-producer4.rocketmq.ehomeinner.cn:9876",
            },
        },
        "push-sdk": {
            "application": {
                "aipkg.rocketmq.nameServerAddr": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-detectmessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "elasticsearch.esAddress": "common1-local.elasticsearch.ehome.ctseelinkinner.cn:9200",
                "redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
                "rocketmq.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.ai-recognition.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.ai-recognition.port": "9876",
                "rocketmq.consumer.disposition.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.consumer.disposition.port": "9876",
                "rocketmq.message.address": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.message.port": "9876",
                "rocketmq.messageAddress": "public-push1.rocketmq.ehome.dlifeinner.cn:9876,public-push2.rocketmq.ehome.dlifeinner.cn:9876,public-push3.rocketmq.ehome.dlifeinner.cn:9876,public-push4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.msg-push.address": "common-alarmmessage1.rocketmq.ehome.dlifeinner.cn,common-alarmmessage2.rocketmq.ehome.dlifeinner.cn,common-alarmmessage3.rocketmq.ehome.dlifeinner.cn,common-alarmmessage4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.msg-push.port": "9876",
                "rocketmq.port": "9876",
                "spring.datasource.url": "jdbc:mysql://ehome.mysql.shardingsphere.ehome.ctseelinkinner.cn:3301",
                "spring.redis.host": "common.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "submit-cloud": {
            "application": {
                "redis.addr": "submitcloudservice.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "submit-cloud-v2": {
            "application": {
                "redis.addr": "submitcloudservice-v2.codis.ehomeinner.cn:19000",
                "rocketmq.addr": "cloudservice1.rocketmq.ehome.dlifeinner.cn,cloudservice2.rocketmq.ehome.dlifeinner.cn,cloudservice3.rocketmq.ehome.dlifeinner.cn,cloudservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.guiZhouAddress": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.picAddr": "cloudservice1.rocketmq.ehome.dlifeinner.cn:9876,cloudservice2.rocketmq.ehome.dlifeinner.cn:9876,cloudservice3.rocketmq.ehome.dlifeinner.cn:9876,cloudservice4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "tree-synvcp": {
            "application": {
                "kafka.brokerList": "binlog1-guizh.kafka.ehome.dlifeinner.cn:9092,binlog2-guizh.kafka.ehome.dlifeinner.cn:9092,binlog3-guizh.kafka.ehome.dlifeinner.cn:9092",
                "rocketmq.name-server": "tree-synvcp1.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp2.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp3.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp4.rocketmq.ehome.dlifeinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "treeSevice": {
            "application": {
                "rocketMq.consumer.message.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.consumer.message.port": "9876",
                "rocketMq.producer.apiSend.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.apiSend.server.port": "9876",
                "rocketMq.producer.group.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.group.server.port": "9876",
                "rocketMq.producer.patrolRegion.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.patrolRegion.server.port": "9876",
                "rocketMq.producer.region.server.address": "treeservice1.rocketmq.ehome.dlifeinner.cn,treeservice2.rocketmq.ehome.dlifeinner.cn,treeservice3.rocketmq.ehome.dlifeinner.cn,treeservice4.rocketmq.ehome.dlifeinner.cn",
                "rocketMq.producer.region.server.port": "9876",
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://camera.mysql.ehomeinner.cn:3301",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://viid.mysql.ehome.ctseelinkinner.cn:3301",
                "spring.datasource.third.jdbc-url": "jdbc:mysql://camera.tidb.ehomeinner.cn:3306",
                "spring.redis.cluster.nodes": "common1.redis-cluster.ehome.dlifeinner.cn:16000",
            },
        },
        "uni-order": {
            "application": {
                "spring.datasource.druid.mysql.url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://order.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.kafka.ai-platform.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.producer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.third-platform.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.tykj.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.tyyy.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.kafka.uni-product-order.consumer.bootstrap-servers": "common1.kafka-mm.ehome.dlifeinner.cn:9092,common2.kafka-mm.ehome.dlifeinner.cn:9092,common3.kafka-mm.ehome.dlifeinner.cn:9092,common4.kafka-mm.ehome.dlifeinner.cn:9092,common5.kafka-mm.ehome.dlifeinner.cn:9092",
                "spring.redis.cluster.nodes": "public-push1.redis-cluster.ehome.dlifeinner.cn:16000",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "vnet-cloud-broadcast": {
            "application": {
                "spring.datasource.druid.url": "jdbc:mysql://other.mysql.shardingsphere.ehome.ctseelinkinner.cn:13301",
                "spring.redis.host": "other.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.port": "19003",
            },
        },
        "vnet-icam-push": {
            "application": {
                "rocketmq.producer.vpaas-device.address": "public-push1.rocketmq.ehome.dlifeinner.cn,public-push2.rocketmq.ehome.dlifeinner.cn,public-push3.rocketmq.ehome.dlifeinner.cn,public-push4.rocketmq.ehome.dlifeinner.cn",
                "rocketmq.producer.vpaas-device.port": "9876",
            },
        },
        "vnet-vpaas-manage": {
            "application": {
                "redis.pool.host": "ehome-access.codis.ehomeinner.cn",
                "redis.pool.port": "16000",
                "spring.datasource.url": "jdbc:mysql://ehome-access.mysql.ehomeinner.cn:13301",
            },
        },
    },
    14: {
        "adapter-task-api": {
            "application": {
                "spring.redis.cluster.nodes": "open-portal1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.redis.host": "open-portal.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19005",
            },
        },
        "apollo-ehome-pro": {
            "application": {
                "spring.datasource.camera.management.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13303",
                "spring.datasource.ehome.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13302",
                "spring.datasource.temp.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13303",
            },
        },
        "area": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://areacode.mysql.vcpinner.cn:13302",
            },
        },
        "auth": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
            },
        },
        "cloud-ehome": {
            "application": {
                "redis.addr": "vcp-common-local.codis.vcpinner.cn:19000",
                "rocketmq.addr": "cloudservice-v2-1.rocketmq.vcpinner.cn,cloudservice-v2-2.rocketmq.vcpinner.cn,cloudservice-v2-3.rocketmq.vcpinner.cn,cloudservice-v2-4.rocketmq.vcpinner.cn",
                "rocketmq.picAddr": "cloudservice-v2-1.rocketmq.vcpinner.cn:9876,cloudservice-v2-2.rocketmq.vcpinner.cn:9876,cloudservice-v2-3.rocketmq.vcpinner.cn:9876,cloudservice-v2-4.rocketmq.vcpinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "cloud-video": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
            },
        },
        "ddnsmgr": {
            "application": {
                "redis.addr": "ddns-mgr.redis.vcpinner.cn:65379",
            },
        },
        "detect": {
            "application": {
                "rocketmq.address": "detect1.rocketmq.vcpinner.cn,detect2.rocketmq.vcpinner.cn,detect3.rocketmq.vcpinner.cn,detect4.rocketmq.vcpinner.cn",
                "rocketmq.port": "9876",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://push.mysql.vcpinner.cn:13322",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "device-access": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
            },
        },
        "device-status-service": {
            "application": {
                "rocketmq.status.center.server.address": "status-push1.rocketmq.vcpinner.cn,status-push2.rocketmq.vcpinner.cn,status-push3.rocketmq.vcpinner.cn,status-push4.rocketmq.vcpinner.cn",
                "rocketmq.status.center.server.port": "9876",
                "rocketmq.status.province.server.address": "status-ddns1.rocketmq.vcpinner.cn,status-ddns2.rocketmq.vcpinner.cn",
                "rocketmq.status.province.server.port": "9876",
                "rocketmq.status.sdk.server.address": "status-sdk1.rocketmq.vcpinner.cn,status-sdk2.rocketmq.vcpinner.cn,status-sdk3.rocketmq.vcpinner.cn,status-sdk4.rocketmq.vcpinner.cn",
                "rocketmq.status.sdk.server.port": "9876",
                "spring.datasource.url": "jdbc:mysql://common.mysql.shardingsphere.vcp.ctseelinkinner.cn:13302",
                "spring.redis.cluster.nodes": "vcp-common.codis-gw.vcpinner.cn:19000",
                "spring.redis.host": "vcp-common.codis-gw.vcpinner.cn",
                "spring.redis.port": "19000",
            },
        },
        "ehome": {
            "application": {
                "rocketmq.cloud.NameServer": "ehome1.rocketmq.vcpinner.cn:9876,ehome2.rocketmq.vcpinner.cn:9876,ehome3.rocketmq.vcpinner.cn:9876,ehome4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://ehome.mysql.vcpinner.cn:13302",
                "spring.datasource.pro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "ehome-account": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
            },
        },
        "ehome-order": {
            "application": {
                "rocketmq.cloud.NameServer": "order1.rocketmq.vcpinner.cn:9876,order2.rocketmq.vcpinner.cn:9876,order3.rocketmq.vcpinner.cn:9876,order4.rocketmq.vcpinner.cn:9876",
                "rocketmq.mqNameSrvAddr": "cloudstore-change1.rocketmq.vcpinner.cn:9876,cloudstore-change2.rocketmq.vcpinner.cn:9876",
                "spring.datasource.ehome.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcporder.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19001",
                "spring.redis.redisson.config": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "ehome-pro-viidmgr": {
            "application": {
                "config.kafka.bootstrap-servers": "117.80.149.102:9092",
                "rocketmq.name-server": "viidmgr1.rocketmq.vcpinner.cn:9876,viidmgr2.rocketmq.vcpinner.cn:9876,viidmgr3.rocketmq.vcpinner.cn:9876,viidmgr4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://viid.mysql.vcp.dlifeinner.cn:13304",
            },
        },
        "ehome-security": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.redis.host": "security.redis.vcpinner.cn",
                "spring.redis.port": "65379",
            },
        },
        "portal-api-dep": {
            "application": {
                "spring.redis.cluster.nodes": "open-portal1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.redis.host": "open-portal.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19005",
            },
        },
        "push": {
            "application": {
                "rocketmq.message.address": "message1.rocketmq.vcpinner.cn,message2.rocketmq.vcpinner.cn",
                "rocketmq.message.port": "9876",
                "rocketmq.server.address": "push1.rocketmq.vcpinner.cn,push2.rocketmq.vcpinner.cn,push3.rocketmq.vcpinner.cn,push4.rocketmq.vcpinner.cn",
                "rocketmq.server.port": "9876",
                "spring.datasource.druid.mysql.url": "jdbc:mysql://push.mysql.vcpinner.cn:13302",
                "spring.datasource.druid.tidb.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.url": "jdbc:mysql://detect.mysql.vcpinner.cn:13322",
                "spring.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "push-pro": {
            "application": {
                "spring.datasource.camera.management.url": "jdbc:mysql://push-pro.mysql.ehomeinner.cn:13303",
                "spring.datasource.ehome.url": "jdbc:mysql://push-pro.mysql.ehomeinner.cn:13302",
            },
        },
        "push-producer": {
            "application": {
                "redis.addr": "vcp-common.codis-gw.vcpinner.cn:19000",
                "server.rocketMq.mqNameSrv": "push-producer1.rocketmq.vcpinner.cn:9876,push-producer2.rocketmq.vcpinner.cn:9876,push-producer3.rocketmq.vcpinner.cn:9876,push-producer4.rocketmq.vcpinner.cn:9876",
            },
        },
        "seelink-batch-service": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "sl-center-auth-application": {
            "application": {
                "spring.data.redis.host": "integration4.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19008",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
            },
        },
        "sl-center-device-access": {
            "application": {
                "rocketmq.consumer.access.device-change.name-server": "sdk3a4-onofflinemessage1.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-onofflinemessage2.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-onofflinemessage3.rocketmq.ehome.dlifeinner.cn:9876,sdk3a4-onofflinemessage4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.name-server": "127.0.0.1:9876",
                "rocketmq.producer.device-operation.address": "127.0.0.1:9876",
                "spring.data.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "sl-center-device-auth": {
            "application": {
                "spring.data.redis.cluster.nodes": "common1-redis-cluster.vcp.dlifeinner.cn:17001",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.url": "jdbc:mysql://127.0.0.1:4000",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "sl-center-device-control": {
            "application": {
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.kafka.device-operation.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "sl-center-device-sync": {
            "application": {
                "elasticsearch.cluster": "gz4-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://ehome.mysql.ehomeinner.cn:3301",
                "spring.datasource.vcp-pro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.vcp.jdbc-url": "jdbc:mysql://ehome.mysql.vcpinner.cn:13302",
                "spring.kafka.binlog.properties.bootstrap-servers": "integration4-common-binlog-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-3.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.business.properties.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.event.first.bootstrap-servers": "integration4-common-binlog-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-binlog-3.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.event.sync.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.sync.tofour.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.sync.upcus.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "19012",
            },
        },
        "sl-center-device-tree": {
            "application": {
                "spring.data.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "sl-center-goods-order": {
            "application": {
                "spring.cloud.nacos.port": "8848",
                "spring.cloud.nacos.server-addr": "sl-nacos-0.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-1.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-2.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-3.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-4.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.dynamic.datasource.sl-goods.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.datasource.dynamic.datasource.vcp-pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.kafka.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
            },
        },
        "sl-center-goods-packages": {
            "application": {
                "rocketmq.name-server": "integration4-alert-1.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-2.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-3.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-4.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-5.rocketmq.vcp.ctseelinkinner.cn:9876,integration4-alert-6.rocketmq.vcp.ctseelinkinner.cn:9876",
                "spring.cloud.nacos.port": "8848",
                "spring.cloud.nacos.server-addr": "sl-nacos-0.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-1.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-2.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-3.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.,sl-nacos-4.sl-nacos.middleware-sl-platform-nacos.svc.cluster.local.",
                "spring.data.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.data.redis.port": "19001",
                "spring.datasource.dynamic.datasource.sl-goods.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.datasource.dynamic.datasource.vcp-pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.kafka.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.device.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.self.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
            },
        },
        "sl-es-dual-active": {
            "application": {
                "elasticsearch.master.esAddress": "gz4-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "elasticsearch.slave.esAddress": "gz-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "elasticsearch.standby.esAddress": "gz3-integration4-1.elasticsearch.vcp.ctseelinkinner.cn:9200",
                "spring.datasource.url": "jdbc:mysql://integration4.tidb.vcp.ctseelinkinner.cn:13303",
                "spring.kafka.es.dual.write.producer.bootstrapServers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.es.write.slave.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
                "spring.kafka.es.write.standby.consumer.bootstrap-servers": "integration4-common-1.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-2.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-3.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-4.kafka.vcp.ctseelinkinner.cn:9092,integration4-common-5.kafka.vcp.ctseelinkinner.cn:9092",
            },
        },
        "slink-biz-user": {
            "application": {
                "rocketmq.name-server": "push1.rocketmq.vcpinner.cn:9876,push2.rocketmq.vcpinner.cn:9876,push3.rocketmq.vcpinner.cn:9876,push4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.datasource.dynamic.datasource.mysql.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.dynamic.datasource.tidb.url": "jdbc:mysql://order.tidb.ehome.ctseelinkinner.cn:13301",
                "spring.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.lock.host": "distributedlock.redis-cluster.camellia.ehome.ctseelinkinner.cn",
                "spring.redis.lock.port": "6379",
                "spring.redis.port": "19001",
            },
        },
        "submit-cloud": {
            "application": {
                "redis.addr": "vcp-common-local.codis.vcpinner.cn:19000",
                "rocketmq.addr": "submitcloudservice1.rocketmq.vcpinner.cn,submitcloudservice2.rocketmq.vcpinner.cn,submitcloudservice3.rocketmq.vcpinner.cn,submitcloudservice4.rocketmq.vcpinner.cn",
                "rocketmq.picAddr": "submitcloudservice1.rocketmq.vcpinner.cn:9876,submitcloudservice2.rocketmq.vcpinner.cn:9876,submitcloudservice3.rocketmq.vcpinner.cn:9876,submitcloudservice4.rocketmq.vcpinner.cn:9876",
                "rocketmq.port": "9876",
            },
        },
        "treeSevice": {
            "application": {
                "rocketMq.consumer.message.address": "tree-service1.rocketmq.ehomeinner.cn,tree-service2.rocketmq.ehomeinner.cn,tree-service3.rocketmq.ehomeinner.cn,tree-service4.rocketmq.ehomeinner.cn",
                "rocketMq.consumer.message.port": "9876",
                "rocketMq.producer.group.server.address": "tree-service1.rocketmq.ehomeinner.cn,tree-service2.rocketmq.ehomeinner.cn,tree-service3.rocketmq.ehomeinner.cn,tree-service4.rocketmq.ehomeinner.cn",
                "rocketMq.producer.group.server.port": "9876",
                "rocketMq.producer.region.server.address": "tree-service1.rocketmq.ehomeinner.cn,tree-service2.rocketmq.ehomeinner.cn,tree-service3.rocketmq.ehomeinner.cn,tree-service4.rocketmq.ehomeinner.cn",
                "rocketMq.producer.region.server.port": "9876",
                "spring.datasource.master.jdbc-url": "jdbc:mysql://tree-service.mysql.ehomeinner.cn:13303",
                "spring.datasource.second.jdbc-url": "jdbc:mysql://tree-service-viid.mysql.ehomeinner.cn:13304",
            },
        },
        "vcp-AIManager": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://192.168.20.107:3301",
                "spring.kafka.bootstrap-servers": "127.0.0.1:9092",
            },
        },
        "vcp-alert-web": {
            "application": {
                "server.port": "jdbc:mysql://vcpalertweb.mysql.vcpinner.cn:13303",
            },
        },
        "vcp-businesshall": {
            "application": {
                "rocketmq.name-server": "businesshall1.rocketmq.vcpinner.cn:9876,businesshall2.rocketmq.vcpinner.cn:9876,businesshall3.rocketmq.vcpinner.cn:9876,businesshall4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://businesshall.mysql.vcpinner.cn:13301",
            },
        },
        "vcp-cascade-manage": {
            "application": {
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "rocketmq.producer.alertCenter.address.1400": "common1.rocketmq.vcp.ctseelinkinner.cn:9876,common2.rocketmq.vcp.ctseelinkinner.cn:9876,common3.rocketmq.vcp.ctseelinkinner.cn:9876,common4.rocketmq.vcp.ctseelinkinner.cn:9876",
                "spring.datasource.ehome.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.redis.host": "common.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19001",
            },
        },
        "vcp-ehomesync": {
            "application": {
                "kj.rocketmq.nameServerAddr": "tree-synvcp1.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp2.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp3.rocketmq.ehome.dlifeinner.cn:9876,tree-synvcp4.rocketmq.ehome.dlifeinner.cn:9876",
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.ehome.url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcp_pro.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-gather": {
            "application": {
                "spring.datasource.ehome.jdbc-url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcp_pro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-openapi": {
            "application": {
                "spring.datasource.dynamic.datasource.ehome_back_up.url": "jdbc:mysql://openapi.mysql.ehomeinner.cn:13302",
                "spring.datasource.dynamic.datasource.master.url": "jdbc:mysql://ehome-pro.mysql.ehomeinner.cn:13303",
                "spring.redis.host": "ehome-pro.codis.ehomeinner.cn",
                "spring.redis.port": "19000",
            },
        },
        "vcp-openmgr": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://openmgr.mysql.ehomeinner.cn:13303",
            },
        },
        "vcp-pro": {
            "application": {
                "spring.datasource.vcp.jdbc-url": "jdbc:mysql://vcp-common.mysql.vcpinner.cn:13302",
                "spring.datasource.vcppro.jdbc-url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-proapp": {
            "application": {
                "rocketmq.name-server": "192.168.23.167:9876",
                "spring.datasource.url": "jdbc:mysql://ehome-pro.mysql.vcpinner.cn:13303",
            },
        },
        "vcp-proappweb-web": {
            "application": {
                "rocketmq.name-server": "vcp-proapp-web1.rocketmq.vcpinner.cn:9876,vcp-proapp-web2.rocketmq.vcpinner.cn:9876,vcp-proapp-web3.rocketmq.vcpinner.cn:9876,vcp-proapp-web4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.first.url": "jdbc:mysql://proappweb.mysql.vcpinner.cn:13303",
                "spring.datasource.second.url": "jdbc:mysql://proappweb.mysql.vcpinner.cn:13303",
            },
        },
        "vcp-saas-cmpt-scn": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-saas.mysql.vcpinner.cn:13302",
                "spring.redis.cluster.nodes": "saas1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "vcp-saas-grassroots-governance": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-saas.mysql.vcpinner.cn:13302",
                "spring.redis.host": "saas.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19003",
            },
        },
        "vcp-saas-jxc": {
            "application": {
                "spring.datasource.url": "jdbc:mysql://vcp-saas.mysql.vcpinner.cn:13302",
                "spring.redis.host": "saas.redis-cluster.camellia.vcp.ctseelinkinner.cn",
                "spring.redis.port": "19003",
            },
        },
        "vcp-sync-es": {
            "application": {
                "elasticsearch.host": "tree-service.elasticsearch.vcpinner.cn",
                "elasticsearch.port": "9201",
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
            },
        },
        "vcp-tree-service": {
            "application": {
                "addProRegion.rocketmq.nameServerAddr": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "addProRegionDev.rocketmq.nameServerAddr": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "elasticsearch.cluster": "vcp-common1.elasticsearch.vcpinner.cn:9201",
                "rocketmq.name-server": "tree-service1.rocketmq.vcpinner.cn:9876,tree-service2.rocketmq.vcpinner.cn:9876,tree-service3.rocketmq.vcpinner.cn:9876,tree-service4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.url": "jdbc:mysql://vcp-pro.tidb.vcpinner.cn:13303",
                "spring.redis.cluster.nodes": "common1.redis-cluster.vcp.ctseelinkinner.cn:16000",
            },
        },
        "vcp-viid-node": {
            "application": {
                "rocketMq.consumer.again.message.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.consumer.again.message.port": "9876",
                "rocketMq.consumer.huiJu.message.address": "viidnode1.rocketmq.vcpinner.cn,viidnode2.rocketmq.vcpinner.cn,viidnode3.rocketmq.vcpinner.cn,viidnode4.rocketmq.vcpinner.cn",
                "rocketMq.consumer.huiJu.message.port": "9876",
                "rocketMq.consumer.picture.message.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.consumer.picture.message.port": "9876",
                "rocketMq.producer.again.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.producer.again.port": "9876",
                "rocketMq.producer.huiJu.address": "viidnode1.rocketmq.vcpinner.cn,viidnode2.rocketmq.vcpinner.cn,viidnode3.rocketmq.vcpinner.cn,viidnode4.rocketmq.vcpinner.cn",
                "rocketMq.producer.huiJu.port": "9876",
                "rocketMq.producer.picture.address": "viid1.rocketmq.vcpinner.cn,viid2.rocketmq.vcpinner.cn,viid3.rocketmq.vcpinner.cn,viid4.rocketmq.vcpinner.cn",
                "rocketMq.producer.picture.port": "9876",
                "spring.redis.cluster.nodes": "1400-viidnode1.redis-cluster.vcp.ctseelinkinner.cn:7000",
            },
        },
        "vcp-waters-monitor": {
            "application": {
                "ehome-pro-rocketmq.name-server": "192.168.2.28:9876",
                "rocketmq.name-server": "vcp-proapp1.rocketmq.vcpinner.cn:9876,vcp-proapp2.rocketmq.vcpinner.cn:9876,vcp-proapp3.rocketmq.vcpinner.cn:9876,vcp-proapp4.rocketmq.vcpinner.cn:9876",
                "rocketmq.producer.namesrvAddr": "vcp-proapp1.rocketmq.vcpinner.cn:9876,vcp-proapp2.rocketmq.vcpinner.cn:9876,vcp-proapp3.rocketmq.vcpinner.cn:9876,vcp-proapp4.rocketmq.vcpinner.cn:9876",
                "spring.datasource.vcppro.jdbc-url": "jdbc:mysql://192.168.20.107:3301",
                "spring.datasource.vcpwaters.jdbc-url": "jdbc:mysql://192.168.20.107:3301",
                "spring.redis.host": "codis.vcpinner.cn",
                "spring.redis.port": "16379",
            },
        },
    },
}


class MockHandler(http.server.BaseHTTPRequestHandler):

    def do_GET(self):
        paths = self.path.split("?")
        path = paths[0]

        # 接口1: GET /thirdApi/getApolloHostInfo
        if "/thirdApi/getApolloHostInfo" in path:
            self._json({"code": "success", "list": HOSTS, "pageTotal": len(HOSTS)})
            return

        # 接口2: GET /thirdApi/apollo/apps
        if "/thirdApi/apollo/apps" in path:
            params = self._parse_params()
            host_id = int(params.get("apolloHostId", str(HOSTS[0]["id"])))
            self._json({"code": 200, "message": "success", "data": APPS.get(host_id, [])})
            return

        # 接口3: GET /thirdApi/apollo/namespace（按实例 + 应用返回，生产同应用在不同实例下配置不同）
        if "/thirdApi/apollo/namespace" in path:
            params = self._parse_params()
            host_id = int(params.get("apolloHostId", str(HOSTS[0]["id"])))
            app_id = params.get("appId", "")
            ns_name = params.get("namespaceName", "application")
            config = self._get_config(host_id, app_id, ns_name)
            items = [{"key": k, "value": v, "dataChangeCreatedTime": "2026-08-26T15:04:11.000+0800"}
                     for k, v in config.items()]
            self._json({
                "code": 200, "message": "success",
                "data": {"appId": app_id, "clusterName": "default", "namespaceName": ns_name, "items": items}
            })
            return

        # 接口4: GET /thirdApi/apolloApp/list（跳① appId ↔ 部署项目 关系，生产真实路径）
        # 契约对齐 service/apollo_client.go GetAppDpRelations：请求 paginator/pageIndex/pageSize，
        # 返回 {"code":"success","message":...,"list":[...],"pageTotal":N}，code 必须为字符串 "success"。
        # 与上游一致：paginator=false（或 pageSize<=0）时不分页、直接返回全表；pageTotal = 总记录数。
        if "/thirdApi/apolloApp/list" in path:
            params = self._parse_params()
            rows = _load_relation_rows()
            if rows is None:
                self._json({"code": "error", "message": "关系接口数据缺失（docs 未挂载）"}, 500)
                return
            paginator = str(params.get("paginator", "false")).lower() == "true"
            page_size = self._int_param(params, "pageSize", -1)
            page_index = self._int_param(params, "pageIndex", 1)
            if page_index <= 0:
                page_index = 1
            if not paginator or page_size <= 0:
                page = rows  # 不分页：一次返回全表
            else:
                start = (page_index - 1) * page_size
                page = rows[start:start + page_size]
            self._json({"code": "success", "message": "success", "list": page, "pageTotal": len(rows)})
            return

        self._json({"code": 404, "message": "not found"}, 404)

    def _int_param(self, params, key, default):
        try:
            return int(params.get(key, default))
        except (TypeError, ValueError):
            return default

    def _get_config(self, host_id, app_id, ns_name):
        # 结构：hostId -> appId -> namespace -> {key: value}（未配置的 namespace 返回空，等价于该实例下无此空间）
        return dict(NAMESPACE_CONFIGS.get(host_id, {}).get(app_id, {}).get(ns_name, {}))

    def _parse_params(self):
        if "?" not in self.path:
            return {}
        qs = self.path.split("?", 1)[1]
        params = {}
        for pair in qs.split("&"):
            if "=" in pair:
                k, v = pair.split("=", 1)
                from urllib.parse import unquote
                params[k] = unquote(v)
        return params

    def _json(self, data, code=200):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False).encode("utf-8"))

    def log_message(self, fmt, *args):
        print(f"[MOCK] {args[0]}")


if __name__ == "__main__":
    server = http.server.HTTPServer(("0.0.0.0", 18080), MockHandler)
    print("Mock Apollo Server running on http://0.0.0.0:18080")
    print("Endpoints:")
    print("  GET /thirdApi/getApolloHostInfo  -> host list")
    print("  GET /thirdApi/apollo/apps        -> apps list")
    print("  GET /thirdApi/apollo/namespace   -> namespace config")
    server.serve_forever()