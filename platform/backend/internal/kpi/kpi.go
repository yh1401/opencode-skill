// Package kpi 汇总告警 / 排障 / 工单 / 巡检的运行数据，输出可考核的时延与自动化指标。
//
// 统计口径（与现场考核项对齐）：
//   - 告警→排障启动：alarm.firstSeenAt → session.createdAt（“1 分钟发现”在此口径下为发现后 60s 内启动定位）
//   - 排障耗时：session.createdAt → finishedAt（“5 分钟定位”）
//   - 定位→建单：session.finishedAt → ticket.createdAt
//   - 告警→建单：firstSeenAt → ticket.createdAt（“10 分钟闭环”）
package kpi

import (
	"math"
	"sort"
	"time"

	"aiops-platform/backend/internal/alarm"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// 目标值（秒）：对应 1 分钟发现 / 5 分钟定位 / 5 分钟建单 / 10 分钟闭环。
const (
	targetAlertToSession = 60
	targetSessionToRoot  = 300
	targetRootToTicket   = 300
	targetAlertToTicket  = 600
)

// Latency 一组时延的分布与达标率。
type Latency struct {
	P50     float64 `json:"p50"`     // 秒
	P90     float64 `json:"p90"`     // 秒
	Avg     float64 `json:"avg"`     // 秒
	Samples int     `json:"samples"` // 有效样本数
	Target  float64 `json:"target"`  // 目标（秒）
	Rate    float64 `json:"rate"`    // 达标率 0–1
}

// Counts 统计区间内的实体数量。
type Counts struct {
	Alarms     int `json:"alarms"`
	Firing     int `json:"firing"`
	Resolved   int `json:"resolved"`
	WithDiag   int `json:"withDiag"`
	WithTicket int `json:"withTicket"`
	Sessions   int `json:"sessions"`
	Tickets    int `json:"tickets"`
	Runs       int `json:"runs"`
}

// Automation 自动化执行率。
type Automation struct {
	AutoDiagRate    float64 `json:"autoDiagRate"`    // 告警自动开排障 / 告警总数
	AutoTicketRate  float64 `json:"autoTicketRate"`  // 自动建单 / 已开排障
	InspectCronRate float64 `json:"inspectCronRate"` // cron 批次 / 全部巡检批次
	DiagSuccessRate float64 `json:"diagSuccessRate"` // 定位出根因的会话 / 会话总数
}

// Accuracy 根因准确率（T10）：依赖人工回填，未标注时 Rate 为 0 且 Labeled 为 0。
type Accuracy struct {
	Labeled  int     `json:"labeled"`  // 已标注会话数
	Accurate int     `json:"accurate"` // 判定正确的会话数
	Rate     float64 `json:"rate"`     // 准确率 = accurate / labeled
	Coverage float64 `json:"coverage"` // 标注覆盖率 = labeled / sessions
}

// DailyPoint 单日趋势。
type DailyPoint struct {
	Date     string `json:"date"` // yyyy-mm-dd
	Alarms   int    `json:"alarms"`
	Sessions int    `json:"sessions"`
	Tickets  int    `json:"tickets"`
	Runs     int    `json:"runs"`
}

// Result KPI 汇总结果。
type Result struct {
	StartAt        time.Time      `json:"startAt"`
	EndAt          time.Time      `json:"endAt"`
	Days           int            `json:"days"`
	Counts         Counts         `json:"counts"`
	AlertToSession Latency        `json:"alertToSession"`
	SessionToRoot  Latency        `json:"sessionToRootCause"`
	RootToTicket   Latency        `json:"rootCauseToTicket"`
	AlertToTicket  Latency        `json:"alertToTicket"`
	Automation     Automation     `json:"automation"`
	Accuracy       Accuracy       `json:"accuracy"`
	BySource       map[string]int `json:"bySource"`
	Daily          []DailyPoint   `json:"daily"`
	Note           string         `json:"note"`
}

// Compute 汇总最近 days 天的 KPI（days 会被夹到 1–90）。
func Compute(st store.Store, days int) Result {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, -(days - 1))

	alarms := store.ListInto[alarm.AlarmEvent](st, alarm.Kind)
	sessions := store.ListInto[domain.DiagnosisSession](st, "diagnosis_session")
	tickets := store.ListInto[domain.Ticket](st, "ticket")
	runs := store.ListInto[domain.InspectionRun](st, "inspection_run")

	inWindow := func(t time.Time) bool { return !t.IsZero() && !t.Before(start) }

	sessByID := make(map[string]domain.DiagnosisSession, len(sessions))
	for _, s := range sessions {
		sessByID[s.ID] = s
	}
	// 一个会话最多对应一张诊断工单（sync 幂等以 sourceID 去重）。
	ticketBySession := make(map[string]domain.Ticket)
	for _, t := range tickets {
		if t.SourceType == "diagnosis" {
			ticketBySession[t.SourceID] = t
		}
	}

	res := Result{
		StartAt:  start,
		EndAt:    now,
		Days:     days,
		BySource: map[string]int{},
		Note:     "时延以平台首次采集到告警的时间为起点，不含“真实故障发生→告警入库”的外部采集延迟；根因准确率来自排障会话的人工回填（T10），未标注时 Labeled=0。",
	}

	var d1, d2, d3, d4 []float64
	sessionInWindow := 0
	ticketInWindow := 0
	runInWindow, cronRuns := 0, 0
	successSessions := 0
	labeledSessions, accurateSessions := 0, 0

	for _, ev := range alarms {
		if !inWindow(ev.FirstSeenAt) {
			continue
		}
		res.Counts.Alarms++
		src := ev.Source
		if src == "" {
			src = "prometheus"
		}
		res.BySource[src]++
		if ev.State == "firing" {
			res.Counts.Firing++
		} else if ev.State == "resolved" {
			res.Counts.Resolved++
		}
		if ev.DiagSessionID == "" {
			continue
		}
		res.Counts.WithDiag++
		sess, ok := sessByID[ev.DiagSessionID]
		if !ok {
			continue
		}
		if !sess.CreatedAt.IsZero() {
			if v := sess.CreatedAt.Sub(ev.FirstSeenAt).Seconds(); v >= 0 {
				d1 = append(d1, v)
			}
		}
		if !sess.CreatedAt.IsZero() && !sess.FinishedAt.IsZero() {
			if v := sess.FinishedAt.Sub(sess.CreatedAt).Seconds(); v >= 0 {
				d2 = append(d2, v)
			}
		}
		tk, hasTicket := ticketBySession[sess.ID]
		if !hasTicket {
			continue
		}
		res.Counts.WithTicket++
		if !sess.FinishedAt.IsZero() {
			if v := tk.CreatedAt.Sub(sess.FinishedAt).Seconds(); v >= 0 {
				d3 = append(d3, v)
			}
		}
		if v := tk.CreatedAt.Sub(ev.FirstSeenAt).Seconds(); v >= 0 {
			d4 = append(d4, v)
		}
	}

	for _, s := range sessions {
		if !inWindow(s.CreatedAt) {
			continue
		}
		sessionInWindow++
		res.Counts.Sessions++
		if s.Status == "root_caused" || s.RootCause != "" {
			successSessions++
		}
		if s.Feedback != nil {
			labeledSessions++
			if s.Feedback.Accurate {
				accurateSessions++
			}
		}
	}
	for _, t := range tickets {
		if inWindow(t.CreatedAt) {
			ticketInWindow++
			res.Counts.Tickets++
		}
	}
	for _, r := range runs {
		if !inWindow(r.StartedAt) {
			continue
		}
		runInWindow++
		res.Counts.Runs++
		if r.Trigger == "cron" {
			cronRuns++
		}
	}

	res.AlertToSession = summarize(d1, targetAlertToSession)
	res.SessionToRoot = summarize(d2, targetSessionToRoot)
	res.RootToTicket = summarize(d3, targetRootToTicket)
	res.AlertToTicket = summarize(d4, targetAlertToTicket)

	res.Automation = Automation{
		AutoDiagRate:    ratio(res.Counts.WithDiag, res.Counts.Alarms),
		AutoTicketRate:  ratio(res.Counts.WithTicket, res.Counts.WithDiag),
		InspectCronRate: ratio(cronRuns, runInWindow),
		DiagSuccessRate: ratio(successSessions, sessionInWindow),
	}

	res.Accuracy = Accuracy{
		Labeled:  labeledSessions,
		Accurate: accurateSessions,
		Rate:     ratio(accurateSessions, labeledSessions),
		Coverage: ratio(labeledSessions, sessionInWindow),
	}

	res.Daily = daily(alarms, sessions, tickets, runs, start, days)
	return res
}

// summarize 计算 P50 / P90 / 均值 / 达标率。
func summarize(values []float64, target float64) Latency {
	out := Latency{Target: target, Samples: len(values)}
	if len(values) == 0 {
		return out
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	var sum float64
	pass := 0
	for _, v := range sorted {
		sum += v
		if v <= target {
			pass++
		}
	}
	out.P50 = percentile(sorted, 0.50)
	out.P90 = percentile(sorted, 0.90)
	out.Avg = round1(sum / float64(len(sorted)))
	out.Rate = ratio(pass, len(sorted))
	return out
}

// percentile 最近秩法取分位数（sorted 必须已升序）。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return round1(sorted[idx])
}

// daily 按自然日聚合（含区间内没有数据的空日，便于前端画连续柱）。
func daily(alarms []alarm.AlarmEvent, sessions []domain.DiagnosisSession,
	tickets []domain.Ticket, runs []domain.InspectionRun, start time.Time, days int) []DailyPoint {

	points := make([]DailyPoint, days)
	index := make(map[string]int, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		points[i] = DailyPoint{Date: key}
		index[key] = i
	}
	add := func(t time.Time, fn func(*DailyPoint)) {
		if t.IsZero() {
			return
		}
		if i, ok := index[t.Format("2006-01-02")]; ok {
			fn(&points[i])
		}
	}
	for _, a := range alarms {
		add(a.FirstSeenAt, func(p *DailyPoint) { p.Alarms++ })
	}
	for _, s := range sessions {
		add(s.CreatedAt, func(p *DailyPoint) { p.Sessions++ })
	}
	for _, t := range tickets {
		add(t.CreatedAt, func(p *DailyPoint) { p.Tickets++ })
	}
	for _, r := range runs {
		add(r.StartedAt, func(p *DailyPoint) { p.Runs++ })
	}
	return points
}

func ratio(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return round3(float64(n) / float64(d))
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
