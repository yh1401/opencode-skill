package domain

// RuleParam 规则可配置参数。
type RuleParam struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	Type    string  `json:"type"`           // number | boolean | enum
	Default any     `json:"default"`        // 默认值（代码硬编码）
	Min     float64 `json:"min,omitempty"`  // number 最小值
	Max     float64 `json:"max,omitempty"`  // number 最大值
	Enum    []any   `json:"enum,omitempty"` // enum 可选项
}

// RuleDef 内置巡检规则定义（只读）。
type RuleDef struct {
	Code        string      `json:"code"`        // TOPO_T1
	Name        string      `json:"name"`        // 依赖漂移
	Severity    string      `json:"severity"`    // 默认严重度
	Description string      `json:"description"` // 规则说明
	Params      []RuleParam `json:"params"`      // 可配置参数（空=无参数）
	Builtin     bool        `json:"builtin"`     // 内置还是自定义
	Enabled     bool        `json:"enabled"`     // 全局启用状态
}

// RuleOverride 任务级规则参数覆盖。
type RuleOverride struct {
	Enabled bool           `json:"enabled"`
	Params  map[string]any `json:"params,omitempty"`
}
