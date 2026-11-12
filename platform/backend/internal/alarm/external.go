package alarm

import (
	"context"
	"fmt"
	"time"
)

// ExternalItem 外部监控源（业务接口 / 质差分析等）写入告警中心的统一输入。
type ExternalItem struct {
	Fingerprint string // 必填，源内唯一
	AlertName   string
	Instance    string
	Host        string
	Severity    string // critical|warning|info
	Summary     string
	Labels      map[string]string
}

// IngestExternalEvents 把外部源命中项写入告警中心，复用去重 / 自动排障 / 通知 / 工单联动。
// 返回本轮新触发（首次出现或死灰复燃）的事件。
func (e *Engine) IngestExternalEvents(ctx context.Context, source string, items []ExternalItem) (created []AlarmEvent, resolved []AlarmEvent, err error) {
	created = make([]AlarmEvent, 0, len(items))
	for _, it := range items {
		if it.Fingerprint == "" {
			err = fmt.Errorf("外部告警项 fingerprint 不能为空")
			continue
		}
		labels := make(map[string]string, len(it.Labels)+1)
		for k, v := range it.Labels {
			labels[k] = v
		}
		labels["source"] = source
		severity := it.Severity
		if severity == "" {
			severity = "warning"
		}
		av := alertView{
			AlertName: it.AlertName,
			Instance:  it.Instance,
			Host:      it.Host,
			Severity:  severity,
			Labels:    labels,
			Summary:   it.Summary,
			Source:    source,
			Group:     source,
		}
		ev, isNew := e.upsertAlertWithFP(it.Fingerprint, av)
		if isNew {
			created = append(created, ev)
		}
	}

	if e.cfg.AlarmAutoDiag && len(created) > 0 {
		e.autoDiagnoseEvents(ctx, created)
	}
	e.NotifyNew(ctx, created)
	return created, resolved, err
}

// ResolveExternal 把某外部源下本轮未再命中（不在 keep 中）的 firing 事件置为 resolved。
// missCount 语义与 degradation 保持一致：连续未命中达到阈值后才恢复；恢复不自动关工单。
func (e *Engine) ResolveExternal(ctx context.Context, source string, keep map[string]bool) []AlarmEvent {
	resolved := make([]AlarmEvent, 0)
	now := time.Now()
	for _, ev := range e.List("firing") {
		if ev.Source != source || keep[ev.Fingerprint] {
			continue
		}
		ev.MissCount++
		if ev.MissCount >= e.cfg.AlarmMissTolerance {
			t := now
			ev.State = "resolved"
			ev.ResolvedAt = &t
			resolved = append(resolved, ev)
			if tk, ok := e.tickets.Get(ev.TicketID); ok && tk != nil {
				tk.Findings = append(tk.Findings, fmt.Sprintf("外部告警已恢复（%s），建议人工确认处置", now.Format("2006-01-02 15:04:05")))
				_ = e.st.Put("ticket", tk.ID, tk)
			}
		}
		_ = e.st.Put(Kind, ev.Fingerprint, ev)
	}
	return resolved
}
