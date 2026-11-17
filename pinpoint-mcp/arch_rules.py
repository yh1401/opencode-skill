"""架构优化规则判定层

对应《架构优化智能体实现方案-V2》4.2：**规则引擎负责数据关联、数值计算与阈值判定，
保证同一输入得到一致结果**；大模型只负责解释风险、串联影响、组织报告。

口径约定：
- 阈值全部来自 config，可用环境变量覆盖（方案 4.1.4）
- 判定结果带规则版本，写入报告（方案 4.1.4）
- 判定状态严格对应方案第五节四态：已发现风险 / 未触发规则 / 数据不足 / 不适用
- 持续型规则要求窗口内有效采样点达到覆盖率要求**且全部超阈值**，单点不构成持续证据
"""
import logging
import math
from typing import Optional

import config

logger = logging.getLogger(__name__)

# 判定状态（方案第五节口径）
STATUS_RISK = "已发现风险"
STATUS_OK = "未触发规则"
STATUS_INSUFFICIENT = "数据不足"
STATUS_NA = "不适用"

STATUS_CODE = {
    STATUS_RISK: "RISK",
    STATUS_OK: "OK",
    STATUS_INSUFFICIENT: "INSUFFICIENT",
    STATUS_NA: "NOT_APPLICABLE",
}

# 规则类别（方案第三节分类）
CAT_AVAILABILITY = "可用性"
CAT_CAPACITY = "容量"
CAT_BALANCE = "负载"
CAT_STABILITY = "稳定性"


def rule_definitions() -> dict:
    """返回全部规则定义（阈值实时读取 config，便于运行期调整后重查）"""
    duration = f"{config.ARCH_RULE_DURATION_MINUTES}m"
    return {
        "deployment_replicas": {
            "desc": "副本不足：期望副本数未达冗余要求",
            "category": CAT_AVAILABILITY,
            "threshold": config.ARCH_RULE_MIN_REPLICAS,
            "operator": "min",
            "unit": "个",
            "duration": None,
            "scope": "名称以 -dep 结尾的 kubernetes Deployment",
            "notes": "业务已确认冗余下限为 2：spec < 2（单副本）即判风险"
            "（实测基线 2026-10-19）。spec=0 视为已下线/暂停，记不适用并单列，"
            "不计入风险。多副本仍可能位于同一故障域，需另行评估。",
        },
        "deployment_available": {
            "desc": "副本不可用：可用副本数低于期望副本数",
            "category": CAT_AVAILABILITY,
            "threshold": 0,
            "operator": "gap_le",
            "unit": "个",
            "duration": duration,
            "scope": "名称以 -dep 结尾的 kubernetes Deployment",
            "notes": "判定依据为『期望副本 - 可用副本』差值，需持续高于 0。",
        },
        "cpu_throttle_ratio": {
            "desc": "CPU 限流：限流周期比例超过 20% 并持续",
            "category": CAT_CAPACITY,
            "threshold": config.ARCH_RULE_CPU_THROTTLE_RATIO,
            "operator": "max",
            "unit": "ratio",
            "duration": duration,
            "scope": "容器维度（pod + container）",
            "notes": "总周期为零时不计算，记为不适用。需交叉验证应用侧 P99 耗时；"
            "仅比例超标而延迟无变化时优先级可降低。",
        },
        "memory_ratio": {
            "desc": "内存容量：工作集 / 有效 limit 超过 85% 并持续",
            "category": CAT_CAPACITY,
            "threshold": config.ARCH_RULE_MEMORY_RATIO,
            "operator": "max",
            "unit": "ratio",
            "duration": duration,
            "scope": "容器维度（pod + container）",
            "notes": "limit 缺失或为零时记为不适用，需单独记录，不得按无风险处理。",
        },
        "hpa_replicas": {
            "desc": "扩容容量：HPA 达到最大副本",
            "category": CAT_CAPACITY,
            "threshold": 1.0,
            "operator": "max",
            "inclusive": True,
            "unit": "ratio",
            "duration": duration,
            "scope": "具有 HPA 的工作负载",
            "notes": "达到上限仅为必要条件，须结合 cpu_throttle_ratio / memory_ratio"
            "及业务量指标确认负载持续偏高；无 HPA 的应用标记不适用。",
        },
        "pod_cpu_imbalance": {
            "desc": "Pod 负载不均衡：同规格 Pod 最大 / 平均 CPU 超过 2 并持续",
            "category": CAT_BALANCE,
            "threshold": config.ARCH_RULE_POD_CPU_IMBALANCE,
            "operator": "max",
            "unit": "ratio",
            "duration": duration,
            "scope": "按 deployment + CPU 规格分组的同规格 Pod",
            "notes": "同规格按 Pod CPU limit 分组（缺失时回退 CPU request），"
            "无法获取规格时记为数据不足，不做不同规格混算；"
            "平均值接近零时比值失真应跳过（NaN/Inf 自动剔除）。",
        },
        "oom_killed": {
            "desc": "OOM：近期出现 OOMKilled 且存在对应重启证据",
            "category": CAT_STABILITY,
            "threshold": config.ARCH_RULE_OOM_RESTART_WINDOW,
            "operator": "oom_combo",
            "unit": "组合判定",
            "duration": config.ARCH_RULE_OOM_RESTART_WINDOW,
            "scope": "容器维度（pod + container）",
            "notes": "主查询取 kube_pod_container_status_last_terminated_reason 即时值"
            "（该 gauge 持久保存最近一次终止原因，无需 7d 子查询回溯）；"
            "OOM 证据由该指标与 container_oom_events_total 增量共同提供，任一成立即为 OOM 证据；"
            "仍须与重启增长同时成立，仅有历史终止原因不能证明近期 OOM；"
            "出现 OOM 应回查 memory_ratio 是否长期贴近 limit。",
        },
        "restart_increase": {
            "desc": "重启增长：窗口内重启次数增加",
            "category": CAT_STABILITY,
            "threshold": 0,
            "operator": "max",
            "unit": "次",
            "duration": config.ARCH_RULE_OOM_RESTART_WINDOW,
            "scope": "容器维度（pod + container）",
            "notes": "仅作为 OOM 判定的配套证据，单独触发不等于 OOM 风险。",
        },
    }


def _parse_float(raw) -> tuple[Optional[float], Optional[str]]:
    """解析 Prometheus 字符串值，返回 (数值, 非有限值类型)

    NaN / Inf 数值置为 None 并保留类型，供上层区分"分母为零"与"无数据"。
    """
    try:
        v = float(raw)
    except (TypeError, ValueError):
        return None, None
    if math.isnan(v):
        return None, "NaN"
    if math.isinf(v):
        return None, "Inf"
    return v, None


def _is_aggr(samples) -> bool:
    """判断 samples 是按 series 聚合的新格式还是按时间点展开的旧格式

    新格式：  {"series": [{"metric": {...}, "values": [[ts, "val"], ...]}], "timePoints": [...]}
    旧格式：  [{"ts": int, "result": [{"metric": {...}, "value": [...]}]}]
    """
    return isinstance(samples, dict) and "series" in samples


def group_series(samples) -> dict:
    """把多点采样结果按标签组合分组为时间序列

    兼容两种输入格式（见 _is_aggr）。返回统一结构：
        {labels_key: {"labels": {...}, "points": [{"ts":, "value":, "nonfinite":}]}}
    value 为 None 表示该点无有效观测；nonfinite 记录 NaN/Inf 类型以便精细化判定。
    """
    grouped: dict[tuple, dict] = {}
    if _is_aggr(samples):
        for s in samples.get("series", []):
            labels = s.get("metric") or {}
            key = tuple(sorted(labels.items()))
            item = grouped.setdefault(key, {"labels": labels, "points": []})
            for ts, raw_val in s.get("values") or []:
                val, nonfinite = _parse_float(raw_val)
                item["points"].append({"ts": ts, "value": val, "nonfinite": nonfinite})
    else:
        for point in samples or []:
            ts = point.get("ts")
            for r in point.get("result") or []:
                labels = r.get("metric") or {}
                key = tuple(sorted(labels.items()))
                val, nonfinite = _parse_float((r.get("value") or [None, None])[1])
                item = grouped.setdefault(key, {"labels": labels, "points": []})
                item["points"].append({"ts": ts, "value": val, "nonfinite": nonfinite})
    return grouped


def _labels_summary(labels: dict) -> dict:
    """提取用于报告的可读标签"""
    keys = (
        "deployment", "pod", "container", "namespace", "hpa", "endpoint",
        "cpuSpec", "specMissing", "lowAvg",
    )
    out = {k: labels[k] for k in keys if k in labels}
    return out or dict(labels)


def _build_item(
    labels: dict,
    status: str,
    observed: Optional[float],
    values: list[Optional[float]],
    total_points: int,
    reason: str,
    threshold: Optional[float] = None,
    exceed_count: int = 0,
    window_seconds: int = 0,
) -> dict:
    """构造单条判定明细（方案第五节证据字段）"""
    valid = [v for v in values if v is not None]
    return {
        "target": _labels_summary(labels),
        "status": status,
        "statusCode": STATUS_CODE[status],
        "observedValue": None if observed is None else round(observed, 6),
        "threshold": threshold,
        "maxValue": round(max(valid), 6) if valid else None,
        "minValue": round(min(valid), 6) if valid else None,
        "validPoints": len(valid),
        "totalPoints": total_points,
        "exceedPoints": exceed_count,
        "windowSeconds": window_seconds,
        "reason": reason,
    }


def _judge(
    rule: str,
    values: list[Optional[float]],
    threshold: float,
    require_min: bool,
    duration_required: bool,
    total_points: int,
    min_coverage: float,
    inclusive: bool = False,
) -> tuple[str, Optional[float], int, str]:
    """通用阈值判定，返回 (status, observed, exceed_count, reason)

    inclusive=True 时按 >= 判定（如 HPA 达到最大副本），否则严格 >/< 。
    """
    valid = [v for v in values if v is not None]
    if not valid:
        return (
            STATUS_NA,
            None,
            0,
            "窗口内无有效数值（可能为分母为零或指标不适用）",
        )
    coverage = len(valid) / total_points if total_points else 0.0
    if duration_required and coverage < min_coverage:
        return (
            STATUS_INSUFFICIENT,
            max(valid) if not require_min else min(valid),
            0,
            f"有效采样点 {len(valid)}/{total_points}（覆盖率 {coverage:.0%}）"
            f"低于 {min_coverage:.0%}，无法证明持续状态",
        )

    if require_min:
        exceed = [v for v in valid if v < threshold]
        observed = min(valid)
        cmp_desc = f"低于 {threshold}"
    else:
        exceed = (
            [v for v in valid if v >= threshold]
            if inclusive
            else [v for v in valid if v > threshold]
        )
        observed = max(valid)
        cmp_desc = f"达到或超过 {threshold}" if inclusive else f"高于 {threshold}"

    if not exceed:
        return STATUS_OK, observed, 0, f"窗口内实测 {observed:.6g}，未{cmp_desc}"

    if duration_required:
        if len(exceed) == len(valid):
            return (
                STATUS_RISK,
                observed,
                len(exceed),
                f"{len(valid)} 个采样点全部{cmp_desc}，最大值 {max(valid):.6g}",
            )
        return (
            STATUS_OK,
            observed,
            len(exceed),
            f"仅 {len(exceed)}/{len(valid)} 个采样点{cmp_desc}，未形成持续触发",
        )

    return (
        STATUS_RISK,
        observed,
        len(exceed),
        f"实测 {observed:.6g}，{cmp_desc}",
    )


def _rule_duration_required(rule_def: dict) -> bool:
    """判断该规则是否需要持续窗口证据（方案 1.2 要求）"""
    return rule_def.get("duration") is not None and rule_def.get("operator") not in (
        "oom_combo",
    )


def evaluate(
    rule: str,
    samples: list[dict],
    total_points: int,
    step_seconds: int,
    context: dict,
    aux: Optional[dict] = None,
) -> dict:
    """执行规则判定

    rule: 规则名，须在 rule_definitions() 中
    samples: arch_metric_query 的采样结果（主指标）
    aux: 辅助判定数据
        - spec_series: deployment_available 需要期望副本序列
        - restart_series: oom_killed 需要重启增长序列
        - limit_exists: memory_ratio 为空时用于区分 limit 缺失
    """
    defs = rule_definitions()
    if rule not in defs:
        raise ValueError(
            f"未知规则: {rule}，可选值: {', '.join(defs.keys())}"
        )
    rule_def = defs[rule]
    aux = aux or {}
    threshold = rule_def["threshold"]
    duration_required = _rule_duration_required(rule_def)
    window_seconds = total_points * step_seconds
    grouped = group_series(samples)

    items: list[dict] = []
    notes: list[str] = []

    # ---------- 组合规则：OOM 需与重启增长同时成立 ----------
    if rule_def["operator"] == "oom_combo":
        restart_grouped = group_series(aux.get("restart_series") or [])
        restart_by_target = _index_by_target(restart_grouped)
        events_grouped = group_series(aux.get("oom_events_series") or [])
        events_by_target = _index_by_target(events_grouped)
        main_by_target = _index_by_target(grouped)

        # 主指标（终止原因）与 OOM 事件计数任一有数据即纳入判定
        targets: dict[str, dict] = {}
        for series in list(grouped.values()) + list(events_grouped.values()):
            targets.setdefault(_target_key(series["labels"]), series["labels"])

        for tkey, labels in targets.items():
            main_series = main_by_target.get(tkey)
            values = [p["value"] for p in main_series["points"]] if main_series else []
            valid = [v for v in values if v is not None]
            # 证据一：最近终止原因为 OOMKilled
            last_reason_oom = any(v >= 1 for v in valid)
            # 证据二：container_oom_events_total 在窗口内有增长
            ev_series = events_by_target.get(tkey)
            ev_values = (
                [p["value"] for p in ev_series["points"] if p["value"] is not None]
                if ev_series
                else []
            )
            events_oom = any(v > 0 for v in ev_values)
            if not valid and not ev_values:
                items.append(
                    _build_item(
                        labels,
                        STATUS_NA,
                        None,
                        values,
                        total_points,
                        "窗口内无 OOMKilled 相关数值",
                    )
                )
                continue

            restart_series = restart_by_target.get(tkey)
            restart_valid = (
                [p["value"] for p in restart_series["points"] if p["value"] is not None]
                if restart_series
                else []
            )
            has_restart = any(v > 0 for v in restart_valid)
            evidence_values = valid + ev_values

            if last_reason_oom or events_oom:
                sources = []
                if last_reason_oom:
                    sources.append("OOMKilled 终止记录")
                if events_oom:
                    sources.append(
                        f"container_oom_events_total 增长 {max(ev_values):.0f} 次"
                    )
                source_desc = "与".join(sources)
                if has_restart:
                    items.append(
                        _build_item(
                            labels,
                            STATUS_RISK,
                            max(restart_valid),
                            evidence_values,
                            total_points,
                            f"存在{source_desc}，且 {rule_def['threshold']} 窗口内"
                            f"重启增长 {max(restart_valid):.0f} 次，OOM 风险确认",
                            threshold=threshold,
                        )
                    )
                else:
                    items.append(
                        _build_item(
                            labels,
                            STATUS_INSUFFICIENT,
                            None,
                            evidence_values,
                            total_points,
                            f"存在{source_desc}，但缺少对应重启增长证据，"
                            "不能确认近期 OOM",
                            threshold=threshold,
                        )
                    )
            else:
                items.append(
                    _build_item(
                        labels,
                        STATUS_OK,
                        max(valid) if valid else max(ev_values),
                        evidence_values,
                        total_points,
                        "窗口内未发现 OOMKilled 终止记录，OOM 事件计数亦无增长",
                        threshold=threshold,
                    )
                )
        # 仅有重启增长、无 OOM 证据的目标：不构成 OOM 风险
        for key, series in restart_grouped.items():
            if _target_key(series["labels"]) in targets:
                continue
            restart_valid = [
                p["value"] for p in series["points"] if p["value"] is not None
            ]
            if restart_valid and max(restart_valid) > 0:
                items.append(
                    _build_item(
                        series["labels"],
                        STATUS_OK,
                        max(restart_valid),
                        restart_valid,
                        total_points,
                        "存在重启增长但无 OOMKilled 记录，不构成本规则 OOM 风险"
                        "（重启原因需另行排查）",
                        threshold=threshold,
                    )
                )
        if not items:
            return _finalize(
                rule, rule_def, context, [], total_points, step_seconds, samples
            )
        return _finalize(
            rule, rule_def, context, items, total_points, step_seconds, samples
        )

    # ---------- 常规规则：逐 series 判定 ----------
    if not grouped:
        # 空结果需区分指标缺失与指标不适用
        if rule == "hpa_replicas":
            items.append(
                _build_item(
                    {},
                    STATUS_NA,
                    None,
                    [],
                    total_points,
                    "未查询到 HPA 序列，判定为不适用（应用未配置 HPA 或指标未接入）",
                    threshold=float(threshold),
                )
            )
            return _finalize(
                rule, rule_def, context, items, total_points, step_seconds, samples
            )
        if rule == "memory_ratio" and aux.get("limit_exists") is False:
            items.append(
                _build_item(
                    {},
                    STATUS_NA,
                    None,
                    [],
                    total_points,
                    "未查询到容器内存 limit，比值不成立，记为不适用（需单独记录 limit 配置缺失）",
                    threshold=threshold,
                )
            )
            return _finalize(
                rule, rule_def, context, items, total_points, step_seconds, samples
            )
        return _finalize(
            rule, rule_def, context, [], total_points, step_seconds, samples
        )

    spec_by_target = _index_by_target(group_series(aux.get("spec_series") or []))

    for key, series in grouped.items():
        values = [p["value"] for p in series["points"]]

        # 全点无有效值：区分「分母为零不适用」与「指标无数据」
        if values and all(v is None for v in values):
            nonfinite = {
                p.get("nonfinite") for p in series["points"] if p.get("nonfinite")
            }
            if nonfinite:
                kind = "Inf" if "Inf" in nonfinite else "NaN"
                if rule in ("cpu_throttle_ratio", "memory_ratio"):
                    reason = (
                        f"比值分母为零或缺失（{kind}），比值不成立，记为不适用；"
                        "需确认 CPU 总周期 / 内存 limit 配置"
                    )
                else:
                    reason = f"指标值为 {kind}，无法参与判定，记为不适用"
            else:
                reason = "窗口内无有效数值，无法判定，需补数据后复核"
            items.append(
                _build_item(
                    series["labels"],
                    STATUS_NA if nonfinite else STATUS_INSUFFICIENT,
                    None,
                    values,
                    total_points,
                    reason,
                    threshold=float(threshold),
                )
            )
            continue

        # Pod 级规则的 deployment 由 label_replace 从 Pod 名派生，派生失败时不能静默丢弃
        if rule in ("cpu_throttle_ratio", "memory_ratio", "pod_cpu_imbalance") and not series[
            "labels"
        ].get("deployment"):
            items.append(
                _build_item(
                    series["labels"],
                    STATUS_INSUFFICIENT,
                    None,
                    values,
                    total_points,
                    "未能从 Pod 名派生 deployment（可能为 StatefulSet 或自定义命名），"
                    "无法归属到应用，判定为数据不足",
                    threshold=float(threshold),
                )
            )
            continue

        # 副本不可用：按 期望副本 - 可用副本 的差值判定
        if rule_def["operator"] == "gap_le":
            spec_series = spec_by_target.get(_target_key(series["labels"]))
            # 按时间戳配对，避免任一序列缺点导致逐点错位
            spec_by_ts = (
                {p["ts"]: p["value"] for p in spec_series["points"] if p["value"] is not None}
                if spec_series
                else {}
            )
            avail_by_ts = {
                p["ts"]: p["value"] for p in series["points"] if p["value"] is not None
            }
            pairs = [
                (spec_by_ts[ts], avail_by_ts[ts])
                for ts in sorted(set(spec_by_ts) & set(avail_by_ts))
            ]
            if not pairs:
                items.append(
                    _build_item(
                        series["labels"],
                        STATUS_INSUFFICIENT,
                        None,
                        values,
                        total_points,
                        "缺少期望副本数或可用副本数，无法计算差值",
                        threshold=threshold,
                    )
                )
                continue
            gaps = [s - a for s, a in pairs]
            gap_values: list[Optional[float]] = gaps
            status, observed, exceed, reason = _judge(
                rule,
                gap_values,
                threshold=0.0,
                require_min=False,
                duration_required=True,
                total_points=total_points,
                min_coverage=config.ARCH_RULE_MIN_COVERAGE,
            )
            items.append(
                _build_item(
                    series["labels"],
                    status,
                    observed,
                    gap_values,
                    total_points,
                    f"期望副本 {pairs[-1][0]:.0f}，可用副本 {pairs[-1][1]:.0f}；{reason}",
                    threshold=0.0,
                    exceed_count=exceed,
                    window_seconds=window_seconds,
                )
            )
            continue

        # 副本不足：期望副本数低于冗余要求
        if rule == "deployment_replicas":
            valid = [v for v in values if v is not None]
            # spec=0 表示应用已下线 / 暂停（KSM 保留 Deployment 对象但副本归零），
            # 按冗余不足判 RISK 会把正常下线应用误报为风险
            if valid and max(valid) <= 0:
                item = _build_item(
                    series["labels"],
                    STATUS_NA,
                    None,
                    values,
                    total_points,
                    "期望副本数为 0，视为已下线/暂停，不纳入冗余判定；"
                    "若为异常缩容需人工确认",
                    threshold=float(threshold),
                )
                item["offline"] = True
                items.append(item)
                continue
            status, observed, exceed, reason = _judge(
                rule,
                values,
                threshold=float(threshold),
                require_min=True,
                duration_required=False,
                total_points=total_points,
                min_coverage=config.ARCH_RULE_MIN_COVERAGE,
            )
            items.append(
                _build_item(
                    series["labels"],
                    status,
                    observed,
                    values,
                    total_points,
                    f"期望副本数 {observed:.0f} 个，冗余要求至少 {threshold} 个；{reason}"
                    if observed is not None
                    else reason,
                    threshold=float(threshold),
                    exceed_count=exceed,
                )
            )
            continue

        # Pod 负载不均衡：同规格分组标记由工具层给出
        if rule == "pod_cpu_imbalance":
            if series["labels"].get("batchMode") == "true":
                # 批量模式已在 PromQL 内按 deployment 聚合，规格分组需单应用复核
                pass
            else:
                spec_key = series["labels"].get("cpuSpec") or "unknown"
                if spec_key == "unknown" or series["labels"].get("specMissing") == "true":
                    items.append(
                        _build_item(
                            series["labels"],
                            STATUS_INSUFFICIENT,
                            None,
                            values,
                            total_points,
                            "缺少 Pod CPU 规格（limit/request），无法同规格比较，"
                            "不同规格混算会导致比值失真",
                            threshold=float(threshold),
                        )
                    )
                    continue
                if series["labels"].get("lowAvg") == "true":
                    items.append(
                        _build_item(
                            series["labels"],
                            STATUS_NA,
                            None,
                            values,
                            total_points,
                            f"规格 {spec_key} 组平均 CPU 低于 "
                            f"{config.ARCH_RULE_IMBALANCE_MIN_AVG_CORES} 核，"
                            "比值失真，不参与判定",
                            threshold=float(threshold),
                        )
                    )
                    continue

        # 其余指标型规则
        status, observed, exceed, reason = _judge(
            rule,
            values,
            threshold=float(threshold),
            require_min=False,
            duration_required=duration_required,
            total_points=total_points,
            min_coverage=config.ARCH_RULE_MIN_COVERAGE,
            inclusive=bool(rule_def.get("inclusive")),
        )
        items.append(
            _build_item(
                series["labels"],
                status,
                observed,
                values,
                total_points,
                reason,
                threshold=float(threshold),
                exceed_count=exceed,
                window_seconds=window_seconds,
            )
        )

    return _finalize(
        rule, rule_def, context, items, total_points, step_seconds, samples
    )


def _target_key(labels: dict) -> str:
    """用于跨查询配对的稳定键（优先最具体的容器粒度，避免同 Deployment 下串号）"""
    ns = f"namespace={labels['namespace']}|" if labels.get("namespace") else ""
    if labels.get("pod"):
        if labels.get("container"):
            return f"{ns}pod={labels['pod']}|container={labels['container']}"
        return f"{ns}pod={labels['pod']}"
    for k in ("deployment", "hpa"):
        if k in labels:
            return f"{ns}{k}={labels[k]}"
    return str(sorted(labels.items()))


def _index_by_target(grouped: dict) -> dict:
    """把分组序列按目标键索引"""
    out: dict = {}
    for series in grouped.values():
        out.setdefault(_target_key(series["labels"]), series)
    return out


def _finalize(
    rule: str,
    rule_def: dict,
    context: dict,
    items: list[dict],
    total_points: int,
    step_seconds: int,
    samples: list[dict],
) -> dict:
    """汇总判定结果，输出方案第五节要求的证据结构"""
    statuses = [i["statusCode"] for i in items]
    if not items:
        overall = STATUS_INSUFFICIENT
        summary = "窗口内未采集到该指标的可用数据，记为数据不足，不得据此判定无风险"
    elif "RISK" in statuses:
        overall = STATUS_RISK
        risk_items = [i for i in items if i["statusCode"] == "RISK"]
        summary = f"{len(risk_items)} 个目标触发「{rule_def['desc']}」"
    elif "INSUFFICIENT" in statuses:
        overall = STATUS_INSUFFICIENT
        summary = (
            f"{statuses.count('INSUFFICIENT')} 个目标证据不足，"
            f"其余未触发，需补数据后复核"
        )
    elif all(s == "NOT_APPLICABLE" for s in statuses):
        overall = STATUS_NA
        summary = "全部目标对本规则不适用"
    else:
        overall = STATUS_OK
        summary = f"全部目标未触发「{rule_def['desc']}」"

    ts_values = samples.get("timePoints", []) if _is_aggr(samples) else [
        s.get("ts") for s in samples or [] if s.get("ts") is not None
    ]
    return {
        "ruleVersion": config.ARCH_RULE_VERSION,
        "rule": rule,
        "ruleDesc": rule_def["desc"],
        "category": rule_def["category"],
        "status": overall,
        "statusCode": STATUS_CODE[overall],
        "summary": summary,
        "threshold": {
            "value": rule_def["threshold"],
            "operator": rule_def["operator"],
            "unit": rule_def["unit"],
            "duration": rule_def["duration"],
        },
        "context": context,
        "items": items,
        "dataQuality": {
            "samplePoints": total_points,
            "stepSeconds": step_seconds,
            "windowSeconds": total_points * step_seconds,
            "minCoverage": config.ARCH_RULE_MIN_COVERAGE,
            "collectTime": max(ts_values) if ts_values else None,
            "oldestSampleTs": min(ts_values) if ts_values else None,
        },
        "scope": rule_def["scope"],
        "notes": rule_def["notes"],
    }
