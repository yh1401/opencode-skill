package changes

import "aiops-platform/backend/internal/domain"

// 5 个内置割接模板：按 targetType 区分，每个 4 步标准流水线。

var registry = map[string]domain.ChangeTemplate{
	"middleware-upgrade": {
		ID:          "middleware-upgrade",
		Name:        "中间件升级",
		TargetType:  "middleware",
		DefaultEnv:  "PRO",
		Description: "升级指定中间件实例（MySQL/Redis/Kafka 等），先校验影响面再灰度升级，最后回比对基线指标",
		Steps: []domain.ChangeStep{
			{No: 1, Title: "影响面校验", CheckTool: "topo-impact", ActionHint: "检查该中间件被多少应用依赖，爆炸半径超过阈值需人工审批", Rollback: "直接终止，不进入升级步骤"},
			{No: 2, Title: "灰度升级", CheckTool: "manual", ActionHint: "先升级 10% 实例，观察 15 分钟；再升级 50%；最后全量升级", Rollback: "灰度期间若出现告警立即回滚旧版本"},
			{No: 3, Title: "Prometheus 基线比对", CheckTool: "prometheus-baseline", ActionHint: "升级后采集吞吐量/错误率/CPU/内存，与升级前基线比对", Rollback: "指标异常度 ≥ 20% 时触发自动回滚"},
			{No: 4, Title: "回退预案", CheckTool: "manual", ActionHint: "保留旧版本 binary + 配置备份 7 天，便于快速回退", Rollback: "停掉新版本进程，拉起旧版本 binary，恢复旧配置"},
		},
	},

	"app-scale": {
		ID:          "app-scale",
		Name:        "应用扩缩容",
		TargetType:  "app",
		DefaultEnv:  "PRO",
		Description: "对指定应用执行扩容或缩容，先确认爆炸半径再做实例调整",
		Steps: []domain.ChangeStep{
			{No: 1, Title: "爆炸半径确认", CheckTool: "topo-impact", ActionHint: "查看该应用依赖的中间件清单，确认扩缩容不影响共享中间件", Rollback: "退出扩缩容，先排查依赖冲突"},
			{No: 2, Title: "新实例健康检查", CheckTool: "prometheus-baseline", ActionHint: "扩容后检查新实例 CPU/内存/存活探针，缩容前检查目标实例无活跃连接", Rollback: "不健康实例不接入流量，直接下线"},
			{No: 3, Title: "Prometheus 指标观测", CheckTool: "prometheus-baseline", ActionHint: "观测 15 分钟，对比扩缩容前后的吞吐量/错误率/TP99", Rollback: "指标异常时立即反向执行扩/缩操作"},
			{No: 4, Title: "收尾与验证", CheckTool: "topo-impact", ActionHint: "确认拓扑关系更新正确，新实例/下线实例在依赖图中一致", Rollback: "手动刷新拓扑同步"},
		},
	},

	"config-change": {
		ID:          "config-change",
		Name:        "配置变更（Apollo）",
		TargetType:  "apollo",
		DefaultEnv:  "PRO",
		Description: "修改 Apollo namespace/key 的配置值，先检查关联应用影响面再灰度发布",
		Steps: []domain.ChangeStep{
			{No: 1, Title: "关联应用影响面", CheckTool: "topo-impact", ActionHint: "拓扑中查询使用该 Apollo 配置的应用清单，列出依赖范围", Rollback: "范围过大时分批次变更"},
			{No: 2, Title: "Apollo 灰度发布", CheckTool: "manual", ActionHint: "先推送到灰度分组（10% 应用），确认无误后全量推送", Rollback: "灰度期间应用异常立即回滚旧值"},
			{No: 3, Title: "Prometheus 存活探针", CheckTool: "prometheus-baseline", ActionHint: "观测关联应用 up + 错误率 + TP99，确认配置生效后无负向影响", Rollback: "指标劣化立即回滚配置"},
			{No: 4, Title: "变更留痕", CheckTool: "manual", ActionHint: "记录变更前后值、灰度范围、生效时间到工单系统", Rollback: "回滚后同样留痕"},
		},
	},

	"app-offline": {
		ID:          "app-offline",
		Name:        "应用下线",
		TargetType:  "app",
		DefaultEnv:  "PRO",
		Description: "安全下线一个应用实例，先解耦依赖再做下线操作，最后清理拓扑",
		Steps: []domain.ChangeStep{
			{No: 1, Title: "爆炸半径确认", CheckTool: "topo-impact", ActionHint: "确认没有其他应用依赖此应用的输出（被依赖应用清单），若有需先迁移", Rollback: "退出下线，先处理依赖方"},
			{No: 2, Title: "依赖中间件解耦", CheckTool: "topo-impact", ActionHint: "解除该应用对共享中间件的引用（DNS/注册中心/配置）", Rollback: "停止解耦，恢复正常运行"},
			{No: 3, Title: "Prometheus 告警静默", CheckTool: "manual", ActionHint: "下线期间对该应用的 Prometheus 告警做静默/抑制", Rollback: "恢复告警规则"},
			{No: 4, Title: "下线执行", CheckTool: "manual", ActionHint: "停进程 → 释放端口 → 清理注册中心 → 刷新拓扑同步", Rollback: "重启应用进程，恢复注册信息"},
		},
	},

	"datacenter-migrate": {
		ID:          "datacenter-migrate",
		Name:        "机房迁移",
		TargetType:  "datacenter",
		DefaultEnv:  "PRO",
		Description: "将一个机房内所有应用整体迁移到目标机房，按应用分组逐步迁移",
		Steps: []domain.ChangeStep{
			{No: 1, Title: "机房内应用影响面", CheckTool: "topo-impact", ActionHint: "列出源机房内所有应用及其依赖，按拓扑分组决定迁移顺序", Rollback: "拆分迁移批次，优先孤立应用"},
			{No: 2, Title: "目标机房健康度", CheckTool: "prometheus-baseline", ActionHint: "目标机房 Prometheus 存活探针 + 资源水位 + 中间件就绪状态", Rollback: "等待目标机房资源充足后再启动迁移"},
			{No: 3, Title: "批量迁移 + 每步回退", CheckTool: "manual", ActionHint: "按应用分组逐批迁移，每批迁移后观测 10 分钟，异常即回退该批", Rollback: "单批失败立即切回源机房"},
			{No: 4, Title: "拓扑刷新 + 验收", CheckTool: "topo-impact", ActionHint: "迁移完成后触发拓扑同步，确认所有应用新机房节点正确落位", Rollback: "手动触发 /api/topo/op/sync 强制刷新"},
		},
	},
}

// Templates 返回所有内置模板。
func Templates() []domain.ChangeTemplate {
	out := make([]domain.ChangeTemplate, 0, len(registry))
	for _, t := range registry {
		out = append(out, t)
	}
	return out
}

// GetTemplate 按 ID 取模板，找不到返回 ok=false。
func GetTemplate(id string) (domain.ChangeTemplate, bool) {
	t, ok := registry[id]
	return t, ok
}
