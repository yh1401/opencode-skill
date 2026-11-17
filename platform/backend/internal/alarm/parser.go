package alarm

import (
	"encoding/json"
	"fmt"
	"strings"

	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
)

// alertView 解析后的 Prometheus 告警（内部统一形态）。
type alertView struct {
	AlertName string
	Instance  string
	Host      string // instance 去掉端口
	Severity  string
	Labels    map[string]string
	Summary   string
	State     string // firing | pending | resolved
	ActiveAt  string // RFC3339
	Value     string
	// Source 采集来源：prometheus（list_alerts）| alertmanager（/api/v2/alerts 或 webhook）
	Source string
	// Suppressed 上游抑制原因：silenced | inhibited（空表示正常）
	Suppressed string
	// Group Alertmanager 分组标签（写入 labels.group）
	Group string
}

// parseAlerts 解析 MCP tools/call 的 result，拿到 Prometheus 告警列表。
// 统一走 mcp.PrometheusAlerts，兼容 envelope / 业务 JSON / 双重编码字符串。
func parseAlerts(raw json.RawMessage) ([]alertView, error) {
	alerts, err := mcp.PrometheusAlerts(raw)
	if err != nil {
		return nil, err
	}

	out := make([]alertView, 0, len(alerts))
	for _, a := range alerts {
		name := a.Labels["alertname"]
		if name == "" {
			name = "(unnamed)"
		}
		inst := a.Labels["instance"]
		v := alertView{
			AlertName: name,
			Instance:  inst,
			Host:      hostOf(inst),
			Severity:  firstLabel(a.Labels, "severity", "level"),
			Labels:    a.Labels,
			Summary:   a.Annotations["summary"],
			State:     a.State,
			ActiveAt:  a.ActiveAt,
			Value:     a.Value,
		}
		if v.Severity == "" {
			v.Severity = "info"
		}
		out = append(out, v)
	}
	return out, nil
}

// fingerprint 幂等键：alertname + instance + severity。
// 同一机器同一规则 → 永远同一条 alarm_event，状态机在这行内部走 firing→resolved→firing。
func fingerprint(a alertView) string {
	return strings.ToLower(fmt.Sprintf("%s|%s|%s", a.AlertName, a.Instance, a.Severity))
}

// hostOf 从 instance（host:port / ip:port）提取主机，统一走身份映射归一。
func hostOf(addr string) string { return identity.NormalizeHost(addr) }

func firstLabel(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return ""
}
