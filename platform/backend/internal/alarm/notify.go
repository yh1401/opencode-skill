package alarm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"aiops-platform/backend/internal/store"
)

// 通知出站（T11）：通用 webhook，不绑定具体 IM，便于对接飞书 / 钉钉 / 企业微信 / 自建网关。
// 未配置 ALARM_NOTIFY_URL 时整体关闭，不产生任何网络调用。

const notifyKind = "alarm_notification"

// NotificationItem 单条告警的通知快照。
type NotificationItem struct {
	Fingerprint string `json:"fingerprint"`
	AlertName   string `json:"alertName"`
	Instance    string `json:"instance"`
	Host        string `json:"host"`
	Severity    string `json:"severity"`
	Summary     string `json:"summary"`
	Source      string `json:"source,omitempty"`
}

// NotificationLog 一次出站投递记录（批量告警共用一条）。
type NotificationLog struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`   // alarm | test
	Status     string             `json:"status"` // sent | failed | skipped
	URL        string             `json:"url"`
	HTTPStatus int                `json:"httpStatus,omitempty"`
	Error      string             `json:"error,omitempty"`
	Alarms     []NotificationItem `json:"alarms"`
	At         time.Time          `json:"at"`
}

// NotifyEnabled 是否配置了出站地址。
func (e *Engine) NotifyEnabled() bool { return strings.TrimSpace(e.cfg.AlarmNotifyURL) != "" }

// NotifyNew 推送本轮新出现的告警（未抑制）。
func (e *Engine) NotifyNew(ctx context.Context, events []AlarmEvent) {
	if !e.NotifyEnabled() || len(events) == 0 {
		return
	}
	if len(events) > 20 {
		events = events[:20]
	}
	e.send(ctx, "alarm", events)
}

// TestNotification 发送一条测试消息，返回投递记录。
func (e *Engine) TestNotification(ctx context.Context) NotificationLog {
	if !e.NotifyEnabled() {
		return NotificationLog{
			ID:     uuid.NewString(),
			Kind:   "test",
			Status: "skipped",
			Error:  "未配置 ALARM_NOTIFY_URL",
			Alarms: []NotificationItem{},
			At:     time.Now(),
		}
	}
	return e.send(ctx, "test", []AlarmEvent{{
		Fingerprint: "test|notify|info",
		AlertName:   "NotifyTest",
		Instance:    "127.0.0.1:9100",
		Host:        "127.0.0.1",
		Severity:    "info",
		Summary:     "告警通知出站连通性测试",
		Source:      "manual",
		State:       "firing",
	}})
}

func (e *Engine) send(ctx context.Context, kind string, events []AlarmEvent) NotificationLog {
	items := make([]NotificationItem, 0, len(events))
	for _, ev := range events {
		items = append(items, NotificationItem{
			Fingerprint: ev.Fingerprint,
			AlertName:   ev.AlertName,
			Instance:    ev.Instance,
			Host:        ev.Host,
			Severity:    ev.Severity,
			Summary:     ev.Summary,
			Source:      ev.Source,
		})
	}
	log := NotificationLog{
		ID:     uuid.NewString(),
		Kind:   kind,
		Status: "sent",
		URL:    e.cfg.AlarmNotifyURL,
		Alarms: items,
		At:     time.Now(),
	}

	payload := map[string]any{
		"source": "aiops-platform",
		"type":   kind,
		"count":  len(items),
		"alarms": items,
		"at":     log.At.Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)

	timeout := e.cfg.AlarmNotifyTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, e.cfg.AlarmNotifyURL, bytes.NewReader(body))
	if err != nil {
		log.Status, log.Error = "failed", err.Error()
		e.saveNotification(&log)
		return log
	}
	req.Header.Set("Content-Type", "application/json")
	if e.cfg.AlarmNotifyToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.cfg.AlarmNotifyToken)
	}
	res, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		log.Status, log.Error = "failed", err.Error()
		e.saveNotification(&log)
		return log
	}
	defer res.Body.Close()
	log.HTTPStatus = res.StatusCode
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		log.Status = "failed"
		log.Error = fmt.Sprintf("HTTP %d", res.StatusCode)
	}
	e.saveNotification(&log)
	return log
}

// saveNotification 落库并只保留最近 200 条，避免文件无限增长。
func (e *Engine) saveNotification(n *NotificationLog) {
	_ = e.st.Put(notifyKind, n.ID, *n)
	logs := store.ListInto[NotificationLog](e.st, notifyKind)
	if len(logs) <= 200 {
		return
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].At.Before(logs[j].At) })
	for i := 0; i < len(logs)-200; i++ {
		_ = e.st.Delete(notifyKind, logs[i].ID)
	}
}

// Notifications 通知记录，按时间倒序。
func (e *Engine) Notifications(limit int) []NotificationLog {
	logs := store.ListInto[NotificationLog](e.st, notifyKind)
	sort.Slice(logs, func(i, j int) bool { return logs[i].At.After(logs[j].At) })
	if limit > 0 && len(logs) > limit {
		logs = logs[:limit]
	}
	return logs
}
