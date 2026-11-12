package inspect

import "aiops-platform/backend/internal/domain"

// builtinRules 11 条内置规则的元数据。
var builtinRules = []domain.RuleDef{
	{
		Code:        "TOPO_T1",
		Name:        "依赖漂移",
		Severity:    "P2",
		Description: "相邻拓扑快照的依赖新增/删除量；反映近期变更的稳定性",
		Builtin:     true,
		Enabled:     true,
		Params: []domain.RuleParam{
			{Key: "driftThreshold", Label: "漂移阈值（条）", Type: "number", Default: 20, Min: 1},
		},
	},
	{
		Code:        "TOPO_T2",
		Name:        "爆炸半径",
		Severity:    "P2",
		Description: "单个中间件实例被过多应用共享；反映单点故障影响面",
		Builtin:     true,
		Enabled:     true,
		Params: []domain.RuleParam{
			{Key: "explosionThreshold", Label: "爆炸半径阈值（应用数）", Type: "number", Default: 10, Min: 2},
		},
	},
	{
		Code:        "TOPO_T3",
		Name:        "单点中间件",
		Severity:    "P2",
		Description: "某中间件类型在巡检范围内只有一个实例，且被多个应用依赖",
		Builtin:     true,
		Enabled:     true,
		Params: []domain.RuleParam{
			{Key: "minDepApps", Label: "最少依赖应用数才触发", Type: "number", Default: 2, Min: 1},
		},
	},
	{
		Code:        "TOPO_T4",
		Name:        "依赖环",
		Severity:    "P3",
		Description: "应用间调用环；一期拓扑为两级二部图，结构上不可能成环",
		Builtin:     true,
		Enabled:     true,
		Params:      []domain.RuleParam{},
	},
	{
		Code:        "TOPO_T5",
		Name:        "孤儿应用",
		Severity:    "P3",
		Description: "无任何中间件依赖的应用；当前需全量应用清单才能判定",
		Builtin:     true,
		Enabled:     true,
		Params:      []domain.RuleParam{},
	},
	{
		Code:        "TOPO_T6",
		Name:        "跨环境错配",
		Severity:    "P2",
		Description: "同一应用在不同环境复用同一中间件地址；易引发环境串扰",
		Builtin:     true,
		Enabled:     true,
		Params:      []domain.RuleParam{},
	},
	{
		Code:        "TOPO_T7",
		Name:        "主机存活",
		Severity:    "P1",
		Description: "对巡检范围内的中间件主机查询 Prometheus up；up=0 判失联，无序列判数据不足",
		Builtin:     true,
		Enabled:     true,
		Params:      []domain.RuleParam{},
	},
	{
		Code:        "TOPO_T8",
		Name:        "调用链异常（Pinpoint）",
		Severity:    "P2",
		Description: "Pinpoint 服务地图中上下游链路的错误数或平均耗时超阈值判风险；无应用/无调用链判数据不足",
		Builtin:     true,
		Enabled:     true,
		Params: []domain.RuleParam{
			{Key: "errorCountThreshold", Label: "链路错误数阈值（次）", Type: "number", Default: 20, Min: 1},
			{Key: "latencyThresholdMs", Label: "链路平均耗时阈值（ms）", Type: "number", Default: 1000, Min: 50},
		},
	},
	{
		Code:        "TOPO_T9",
		Name:        "CMDB 资产一致性",
		Severity:    "P2",
		Description: "拓扑引用的主机与 CMDB 对账；查无资产、非在线、在线资产缺负责人均判风险",
		Builtin:     true,
		Enabled:     true,
		Params:      []domain.RuleParam{},
	},
	{
		Code:        "TOPO_T10",
		Name:        "日志错误（ES）",
		Severity:    "P2",
		Description: "按主机统计 ES 近 N 分钟 ERROR/FATAL 日志量；索引缺失或查询失败判数据不足",
		Builtin:     true,
		Enabled:     true,
		Params: []domain.RuleParam{
			{Key: "errorThreshold", Label: "错误日志条数阈值", Type: "number", Default: 20, Min: 1},
			{Key: "lookbackMin", Label: "回溯窗口（分钟）", Type: "number", Default: 15, Min: 1},
		},
	},
	{
		Code:        "TOPO_T11",
		Name:        "主机指标阈值",
		Severity:    "P2",
		Description: "Prometheus 主机实时指标（CPU/内存/磁盘/连接数）超阈值判风险；无指标判数据不足",
		Builtin:     true,
		Enabled:     true,
		Params: []domain.RuleParam{
			{Key: "cpuThreshold", Label: "CPU 使用率阈值（%）", Type: "number", Default: 85, Min: 1},
			{Key: "memThreshold", Label: "内存使用率阈值（%）", Type: "number", Default: 85, Min: 1},
			{Key: "diskThreshold", Label: "磁盘使用率阈值（%）", Type: "number", Default: 90, Min: 1},
			{Key: "connThreshold", Label: "TCP 连接数阈值（个）", Type: "number", Default: 5000, Min: 1},
		},
	},
}

// GetRules 返回全部内置规则（含 enabled 全局状态）。
func GetRules() []domain.RuleDef {
	out := make([]domain.RuleDef, len(builtinRules))
	copy(out, builtinRules)
	return out
}

// GetRule 按 code 取内置规则。
func GetRule(code string) (domain.RuleDef, bool) {
	for _, r := range builtinRules {
		if r.Code == code {
			return r, true
		}
	}
	return domain.RuleDef{}, false
}

// EffectiveParams 合并全局默认 → 任务覆盖 → 返回可执行参数 map。
func EffectiveParams(task domain.InspectionTask, code string) map[string]any {
	out := map[string]any{}
	def, ok := GetRule(code)
	if ok {
		for _, p := range def.Params {
			out[p.Key] = p.Default
		}
	}
	if task.Rules != nil {
		if ov, ok := task.Rules[code]; ok && ov.Params != nil {
			for k, v := range ov.Params {
				out[k] = v
			}
		}
	}
	return out
}

// IsRuleEnabled 判断某任务下某规则是否启用。
func IsRuleEnabled(task domain.InspectionTask, code string) bool {
	def, ok := GetRule(code)
	if !ok || !def.Enabled {
		return false
	}
	if task.Rules != nil {
		if ov, ok := task.Rules[code]; ok {
			return ov.Enabled
		}
	}
	return def.Enabled // 默认启用
}
