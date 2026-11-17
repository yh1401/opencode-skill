package api

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/alarm"
)

// alarmList GET /api/alarms?state=&severity=&q=&page=&pageSize=
func (s *Server) alarmList(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[alarmList PANIC] %v", r)
			fail(c, http.StatusInternalServerError, nil)
		}
	}()
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	s.alarm.SyncTickets()
	evs, total := s.alarm.Query(alarm.ListQuery{
		State:    c.Query("state"),
		Severity: c.Query("severity"),
		Q:        c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	// 直接序列整个 AlarmEvent（保证零字段遗漏），再 merge 二次关联
	type withExtra struct {
		alarm.AlarmEvent
		SessionStatus     string     `json:"sessionStatus,omitempty"`
		DiagStatus        string     `json:"diagStatus,omitempty"`
		Confidence        float64    `json:"confidence,omitempty"`
		RootCause         string     `json:"rootCause,omitempty"`
		SessionCreatedAt  *time.Time `json:"sessionCreatedAt,omitempty"`
		SessionFinishedAt *time.Time `json:"sessionFinishedAt,omitempty"`
		TicketStatus      string     `json:"ticketStatus,omitempty"`
		TicketPriority    string     `json:"ticketPriority,omitempty"`
		TicketCreatedAt   *time.Time `json:"ticketCreatedAt,omitempty"`
		TicketUpdatedAt   *time.Time `json:"ticketUpdatedAt,omitempty"`
		TicketClosedAt    *time.Time `json:"ticketClosedAt,omitempty"`
	}
	out := make([]withExtra, 0, len(evs))
	for _, ev := range evs {
		row := withExtra{AlarmEvent: ev}
		if ev.DiagSessionID != "" {
			if sess, ok := s.diag.Get(ev.DiagSessionID); ok {
				row.SessionStatus = sess.Status
				row.DiagStatus = sess.Status
				row.Confidence = sess.Confidence
				row.RootCause = sess.RootCause
				created := sess.CreatedAt
				row.SessionCreatedAt = &created
				if !sess.FinishedAt.IsZero() {
					finished := sess.FinishedAt
					row.SessionFinishedAt = &finished
				}
			}
		}
		if ev.TicketID != "" {
			if tk, ok := s.tickets.Get(ev.TicketID); ok && tk != nil {
				row.TicketStatus = string(tk.Status)
				row.TicketPriority = string(tk.Priority)
				created := tk.CreatedAt
				updated := tk.UpdatedAt
				row.TicketCreatedAt = &created
				row.TicketUpdatedAt = &updated
				row.TicketClosedAt = tk.ClosedAt
			}
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, gin.H{
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
		"items":    out,
	})
}

// alarmStats GET /api/alarms/stats
func (s *Server) alarmStats(c *gin.Context) {
	all := s.alarm.List("")
	firing := 0
	autoDiag := 0
	suppressed := 0
	now := time.Now()
	for _, ev := range all {
		if ev.State == "firing" {
			firing++
			if ev.DiagSessionID != "" {
				autoDiag++
			}
			if ev.IsSilenced(now) || ev.IsSuppressed() {
				suppressed++
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"stats":          s.alarm.Stats(),
		"total":          len(all),
		"firing":         firing,
		"autoDiag":       autoDiag,
		"suppressed":     suppressed,
		"autoTickets24h": s.alarm.AutoTickets24h(),
	})
}

// alarmTrend GET /api/alarms/trend?hours=24
func (s *Server) alarmTrend(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	c.JSON(http.StatusOK, gin.H{"points": s.alarm.Trend(hours)})
}

// alarmSilence POST /api/alarms/:fp/silence  body: {"duration":"30m|2h|forever","reason":"..."}
func (s *Server) alarmSilence(c *gin.Context) {
	var body struct {
		Duration string `json:"duration"`
		Reason   string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)

	var until time.Time
	switch strings.ToLower(strings.TrimSpace(body.Duration)) {
	case "30m":
		until = time.Now().Add(30 * time.Minute)
	case "2h":
		until = time.Now().Add(2 * time.Hour)
	case "24h":
		until = time.Now().Add(24 * time.Hour)
	default: // forever / 空 / 非法值 → 永久静默
		until = time.Time{}
	}
	ev, err := s.alarm.Silence(c.Param("fp"), until, body.Reason)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, ev)
}

// alarmUnsilence POST /api/alarms/:fp/unsilence
func (s *Server) alarmUnsilence(c *gin.Context) {
	ev, err := s.alarm.Unsilence(c.Param("fp"))
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, ev)
}

// alarmGet GET /api/alarms/:fp
func (s *Server) alarmGet(c *gin.Context) {
	fp := c.Param("fp")
	ev, ok := s.alarm.Get(fp)
	if !ok {
		fail(c, http.StatusNotFound, nil)
		return
	}
	c.JSON(http.StatusOK, ev)
}

// alarmPoll POST /api/alarms/poll
func (s *Server) alarmPoll(c *gin.Context) {
	upserted, autoDiag := s.alarm.PollOnce(context.Background())
	c.JSON(http.StatusOK, gin.H{"upserted": upserted, "autoDiag": autoDiag})
}

// alarmWebhook POST /api/alarms/webhook
// 接收 Alertmanager webhook（version 4）；firing 幂等入库，resolved 标记恢复。
func (s *Server) alarmWebhook(c *gin.Context) {
	var p alarm.WebhookPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, s.alarm.IngestWebhook(c.Request.Context(), p))
}

// alarmDegradationScan POST /api/alarms/degradation/scan
// 手动触发一轮指标劣化扫描，返回扫描目标数 / 命中列表 / 新建数 / 恢复数。
func (s *Server) alarmDegradationScan(c *gin.Context) {
	res := s.alarm.SweepDegradation(c.Request.Context())
	c.JSON(http.StatusOK, res)
}

// alarmDegradationOverview GET /api/alarms/degradation
// 指标劣化独立页（T8）：最近一轮扫描结果 + 当前 firing 明细 + 最近恢复记录。
func (s *Server) alarmDegradationOverview(c *gin.Context) {
	firing := make([]alarm.AlarmEvent, 0)
	for _, ev := range s.alarm.List("firing") {
		if ev.Source == "degradation" {
			firing = append(firing, ev)
		}
	}
	resolved := make([]alarm.AlarmEvent, 0)
	for _, ev := range s.alarm.List("resolved") {
		if ev.Source == "degradation" {
			resolved = append(resolved, ev)
		}
	}
	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].LastSeenAt.After(resolved[j].LastSeenAt)
	})
	if len(resolved) > 20 {
		resolved = resolved[:20]
	}
	hosts := make(map[string]bool, len(firing))
	for _, ev := range firing {
		hosts[ev.Host] = true
	}
	c.JSON(http.StatusOK, gin.H{
		"lastScan":      s.alarm.LastDegradationScan(),
		"firing":        firing,
		"resolved":      resolved,
		"affectedHosts": len(hosts),
	})
}

// alarmNotifications GET /api/alarms/notifications?limit=
// 告警通知出站记录（T11）。
func (s *Server) alarmNotifications(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": s.alarm.NotifyEnabled(),
		"items":   s.alarm.Notifications(limit),
	})
}

// alarmNotifyTest POST /api/alarms/notifications/test
func (s *Server) alarmNotifyTest(c *gin.Context) {
	c.JSON(http.StatusOK, s.alarm.TestNotification(c.Request.Context()))
}

// alarmDiagnose POST /api/alarms/:fp/diagnose
func (s *Server) alarmDiagnose(c *gin.Context) {
	fp := c.Param("fp")
	sess, err := s.alarm.ForceDiagnose(context.Background(), fp)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, sess)
}
