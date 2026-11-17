package alarm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// amAlert Alertmanager API v2 单条告警。
type amAlert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	Status       struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
	} `json:"status"`
}

// WebhookPayload Alertmanager webhook（version 4）入站载荷。
type WebhookPayload struct {
	Version           string            `json:"version"`
	Status            string            `json:"status"` // firing | resolved
	Receiver          string            `json:"receiver"`
	GroupKey          string            `json:"groupKey"`
	TruncatedAlerts   int               `json:"truncatedAlerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []WebhookAlert    `json:"alerts"`
}

// WebhookAlert webhook 中的单条告警。
type WebhookAlert struct {
	Status       string            `json:"status"` // firing | resolved
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// WebhookResult webhook 入站结果。
type WebhookResult struct {
	Ingested   int `json:"ingested"`
	Resolved   int `json:"resolved"`
	AutoDiag   int `json:"autoDiag"`
	Suppressed int `json:"suppressed"`
	Ignored    int `json:"ignored"`
}

// fetchAlertmanager 主动拉取 Alertmanager /api/v2/alerts。
// 同时请求 active / silenced / inhibited，抑制态交由上层过滤（不入自动排障）。
func (e *Engine) fetchAlertmanager(ctx context.Context) ([]alertView, error) {
	if !e.cfg.AlertmanagerEnabled || strings.TrimSpace(e.cfg.AlertmanagerURL) == "" {
		return nil, nil
	}
	url := strings.TrimRight(e.cfg.AlertmanagerURL, "/") +
		"/api/v2/alerts?active=true&silenced=true&inhibited=true&unprocessed=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	timeout := e.cfg.AlertmanagerTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alertmanager 返回 %d", resp.StatusCode)
	}

	var list []amAlert
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("alertmanager JSON 解析失败: %w", err)
	}

	out := make([]alertView, 0, len(list))
	for _, a := range list {
		name := a.Labels["alertname"]
		if name == "" {
			name = "(unnamed)"
		}
		inst := a.Labels["instance"]
		severity := firstLabel(a.Labels, "severity", "level")
		if severity == "" {
			severity = "info"
		}
		out = append(out, alertView{
			AlertName:  name,
			Instance:   inst,
			Host:       hostOf(inst),
			Severity:   severity,
			Labels:     a.Labels,
			Summary:    a.Annotations["summary"],
			State:      "firing",
			ActiveAt:   a.StartsAt,
			Source:     "alertmanager",
			Suppressed: suppressedReason(a),
			Group:      a.Labels[e.groupLabel()],
		})
	}
	return out, nil
}

// suppressedReason 判定 Alertmanager 抑制原因。
func suppressedReason(a amAlert) string {
	if len(a.Status.SilencedBy) > 0 {
		return "silenced"
	}
	if len(a.Status.InhibitedBy) > 0 {
		return "inhibited"
	}
	return ""
}

// IngestWebhook 接收 Alertmanager webhook：firing 幂等入库 + resolved 标记恢复。
func (e *Engine) IngestWebhook(ctx context.Context, p WebhookPayload) WebhookResult {
	var res WebhookResult
	group := groupOf(p.GroupLabels)
	diagnosable := make([]alertView, 0, len(p.Alerts))
	fresh := make([]AlarmEvent, 0, 4)

	for _, a := range p.Alerts {
		name := a.Labels["alertname"]
		if name == "" {
			res.Ignored++
			continue
		}
		inst := a.Labels["instance"]
		severity := firstLabel(a.Labels, "severity", "level")
		if severity == "" {
			severity = "info"
		}
		v := alertView{
			AlertName: name,
			Instance:  inst,
			Host:      hostOf(inst),
			Severity:  severity,
			Labels:    a.Labels,
			Summary:   a.Annotations["summary"],
			State:     "firing",
			ActiveAt:  a.StartsAt,
			Source:    "alertmanager",
			Group:     group,
		}
		fp := fingerprint(v)

		if a.Status == "resolved" || (a.Status == "" && p.Status == "resolved") {
			ev, ok := e.Get(fp)
			if !ok {
				res.Ignored++
				continue
			}
			if ev.State != "resolved" {
				now := time.Now()
				ev.State = "resolved"
				ev.ResolvedAt = &now
				ev.MissCount = 0
				ev.SuppressedBy = ""
				_ = e.st.Put(Kind, fp, ev)
				res.Resolved++
			} else {
				res.Ignored++
			}
			continue
		}

		// webhook 一般由 Alertmanager 过滤掉静默告警；若上游显式带上抑制态则兼容。
		switch strings.ToLower(a.Status) {
		case "suppressed", "silenced":
			v.Suppressed = "silenced"
		case "inhibited":
			v.Suppressed = "inhibited"
		}
		ev, isNew := e.upsertAlert(v)
		if isNew && v.Suppressed == "" {
			fresh = append(fresh, ev)
		}
		res.Ingested++
		if v.Suppressed != "" {
			res.Suppressed++
		} else {
			diagnosable = append(diagnosable, v)
		}
	}

	if e.cfg.AlarmAutoDiag && len(diagnosable) > 0 {
		res.AutoDiag = e.runAutoDiagnose(ctx, diagnosable)
	}
	e.NotifyNew(ctx, fresh)
	return res
}

// mergeAlerts 以 Prometheus list_alerts 为底，用 Alertmanager 数据补全抑制态与分组。
// 两者同一 fingerprint 视为同一条告警，保证不重复入库。
func mergeAlerts(base, extra []alertView) []alertView {
	out := make([]alertView, len(base))
	copy(out, base)
	index := make(map[string]int, len(out))
	for i, a := range out {
		index[fingerprint(a)] = i
	}
	for _, a := range extra {
		fp := fingerprint(a)
		if i, ok := index[fp]; ok {
			out[i].Suppressed = a.Suppressed
			if a.Group != "" {
				out[i].Group = a.Group
			}
			if out[i].Summary == "" {
				out[i].Summary = a.Summary
			}
			continue
		}
		index[fp] = len(out)
		out = append(out, a)
	}
	return out
}

// groupOf 把 Alertmanager groupLabels 序列化为稳定字符串，写入 labels.group。
func groupOf(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+labels[k])
	}
	return strings.Join(parts, ",")
}

func (e *Engine) groupLabel() string {
	if e.cfg.AlertmanagerGroupLabel == "" {
		return "group"
	}
	return e.cfg.AlertmanagerGroupLabel
}
