package alarm

import (
	"context"
	"fmt"
	"time"

	"aiops-platform/backend/internal/domain"
)

// IngestInspectionFindings 把巡检 RISK 结论发布为告警事件（T9）。
// fingerprint 带 taskID，避免不同任务的同名风险互相覆盖；
// 同一任务下一轮未再命中的旧风险直接标记 resolved。
func (e *Engine) IngestInspectionFindings(run *domain.InspectionRun, findings []domain.Finding) int {
	if run == nil || len(findings) == 0 {
		return 0
	}
	now := time.Now()
	current := make(map[string]bool, len(findings))
	fresh := make([]AlarmEvent, 0, len(findings))

	for _, f := range findings {
		key := f.ObjectKey
		if key == "" {
			key = f.ID
		}
		fp := fmt.Sprintf("inspection|%s|%s|%s", run.TaskID, f.RuleCode, key)
		av := alertView{
			AlertName: f.Title,
			Instance:  key,
			Host:      hostOf(key),
			Severity:  severityOfFinding(f.Severity),
			Summary:   f.Detail,
			Source:    "inspection",
			Labels: map[string]string{
				"taskId":     run.TaskID,
				"taskName":   run.TaskName,
				"runId":      run.ID,
				"ruleCode":   f.RuleCode,
				"objectType": f.ObjectType,
				"severity":   f.Severity,
				"source":     "inspection",
			},
		}
		current[fp] = true
		ev, isNew := e.upsertAlertWithFP(fp, av)
		if isNew {
			fresh = append(fresh, ev)
		}
	}

	// 同一任务本轮未再出现的风险 → 已消除
	for _, ev := range e.List("firing") {
		if ev.Source != "inspection" || ev.Labels["taskId"] != run.TaskID {
			continue
		}
		if current[ev.Fingerprint] {
			continue
		}
		ev.State = "resolved"
		resolvedAt := now
		ev.ResolvedAt = &resolvedAt
		// 恢复闭环：关联工单自动流转到「已处置」，等人工确认后关闭
		e.tickets.AutoResolve(ev.TicketID, "巡检风险已消除")
		_ = e.st.Put(Kind, ev.Fingerprint, ev)
	}

	e.NotifyNew(context.Background(), fresh)
	if e.cfg.AlarmAutoDiag && len(fresh) > 0 {
		e.autoDiagnoseEvents(context.Background(), fresh)
	}
	return len(fresh)
}

// severityOfFinding 巡检 P0–P3 映射到告警 severity。
func severityOfFinding(s string) string {
	switch s {
	case "P0", "P1":
		return "critical"
	case "P2":
		return "warning"
	default:
		return "info"
	}
}
