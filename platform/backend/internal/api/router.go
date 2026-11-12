package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/alarm"
	"aiops-platform/backend/internal/audit"
	"aiops-platform/backend/internal/changes"
	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/inspect"
	"aiops-platform/backend/internal/knowledge"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/relations"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/tickets"
	"aiops-platform/backend/internal/topo"
)

// Server HTTP 控制面：拓扑 / 巡检 / 排障 / MCP / 变更 / 工单 / 稽核 / 告警 八组能力。
type Server struct {
	cfg      config.Config
	store    store.Store
	topo     topo.Provider
	mcp      *mcp.Gateway
	inspect  *inspect.Engine
	diag     *diagnose.Engine
	changes  *changes.Engine
	tickets  *tickets.Engine
	audit    *audit.Engine
	alarm    *alarm.Engine
	rel      *relations.Engine
	kb       *knowledge.Engine
	identity *identity.Registry
	started  time.Time
}

func New(cfg config.Config, st store.Store, tp topo.Provider, gw *mcp.Gateway, ie *inspect.Engine, de *diagnose.Engine, ce *changes.Engine, te *tickets.Engine, ae *audit.Engine, al *alarm.Engine, rel *relations.Engine, kb *knowledge.Engine, reg *identity.Registry) *Server {
	return &Server{cfg: cfg, store: st, topo: tp, mcp: gw, inspect: ie, diag: de, changes: ce, tickets: te, audit: ae, alarm: al, rel: rel, kb: kb, identity: reg, started: time.Now()}
}

// Router 注册全部路由，前端通过 /api 前缀调用。
func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), cors())

	api := r.Group("/api")
	{
		api.GET("/health", s.health)
		api.GET("/meta", s.meta)

		t := api.Group("/topo")
		{
			t.GET("/dates", s.topoDates)
			t.GET("/apps", s.topoApps)
			t.GET("/graph", s.topoGraph)
			t.GET("/impact", s.topoImpact)
			t.GET("/diff", s.topoDiff)
			t.GET("/mwtypes", s.topoMwTypes)
			t.GET("/rows", s.topoRows)
		}

		// 运维关系拓扑（tprel 能力面）：file 本地计算 / http 透传 topo 服务
		o := api.Group("/topo/op")
		{
			o.GET("/graph", s.opGraph)
			o.GET("/detail", s.opDetail)
			o.GET("/list", s.opList)
			o.GET("/middlewareTypes", s.opMwTypes)
			o.GET("/mwTargets", s.opMwTargets)
			o.POST("/sync", s.opSync)
			o.GET("/sync/status", s.opSyncStatus)
			o.GET("/sync/logs", s.opSyncLogs)
		}

		// 变更方案 / 风险评估（复用 topo impact + diff + diagnose 历史故障）
		c := api.Group("/changes")
		{
			c.GET("/templates", s.changeTemplates)
			c.POST("/generate", s.changeGenerate)
			c.GET("", s.changeList)
			c.GET("/:id", s.changeGet)
		}

		// 隐患与工单（自动从排障会话 root_caused 生成）
		tk := api.Group("/tickets")
		{
			tk.GET("", s.ticketList)
			tk.GET("/:id", s.ticketGet)
			tk.PATCH("/:id/status", s.ticketUpdateStatus)
			tk.POST("/:id/resolve", s.ticketResolve)
		}

		// 资源稽核：topo 关系 ↔ CMDB 资产对账 + Prometheus 指标佐证
		au := api.Group("/audit")
		{
			au.POST("/run", s.auditRun)
			au.GET("/runs", s.auditRuns)
			au.GET("/runs/:id", s.auditRunGet)
			au.GET("/runs/:id/findings", s.auditFindings)
		}

		i := api.Group("/inspect")
		{
			i.GET("/rules", s.inspectRules)
			i.GET("/tasks", s.listTasks)
			i.POST("/tasks", s.createTask)
			i.PUT("/tasks/:id", s.inspectUpdateTask)
			i.DELETE("/tasks/:id", s.deleteTask)
			i.POST("/tasks/:id/run", s.runTask)
			i.POST("/run", s.runAdhoc)
			i.GET("/runs", s.listRuns)
			i.GET("/runs/:id", s.getRun)
			i.GET("/runs/:id/findings", s.listFindings)
			i.GET("/runs/:id/report", s.getReport)
		}

		d := api.Group("/diagnose")
		{
			d.POST("/sessions", s.createDiagnosis)
			d.GET("/sessions", s.listDiagnosis)
			d.GET("/sessions/:id", s.getDiagnosis)
			d.POST("/sessions/:id/feedback", s.diagnosisFeedback)
			d.GET("/sessions/:id/stream", s.streamDiagnosis)
		}

		m := api.Group("/mcp")
		{
			m.GET("/probe", s.mcpProbe)
			m.GET("/tools", s.mcpTools)
			m.POST("/call", s.mcpCall)
			m.GET("/logs", s.mcpLogs)
		}

		// 告警中心（断点⑤）：采集 / 幂等入库 / 自动排障
		al := api.Group("/alarms")
		{
			al.GET("", s.alarmList)
			al.GET("/stats", s.alarmStats)
			al.GET("/trend", s.alarmTrend)
			al.GET("/degradation", s.alarmDegradationOverview)
			al.GET("/notifications", s.alarmNotifications)
			al.GET("/:fp", s.alarmGet)
			al.POST("/poll", s.alarmPoll)
			al.POST("/webhook", s.alarmWebhook)
			al.POST("/degradation/scan", s.alarmDegradationScan)
			al.POST("/notifications/test", s.alarmNotifyTest)
			al.POST("/:fp/diagnose", s.alarmDiagnose)
			al.POST("/:fp/silence", s.alarmSilence)
			al.POST("/:fp/unsilence", s.alarmUnsilence)
		}

		// 外部业务接口监控（T12）
		bm := api.Group("/biz-monitor")
		{
			bm.GET("/overview", s.bizOverview)
			bm.GET("/rules", s.bizRules)
			bm.POST("/rules", s.bizRuleCreate)
			bm.DELETE("/rules/:id", s.bizRuleDelete)
			bm.POST("/scan", s.bizScan)
		}

		// 质差分析（T13）
		q := api.Group("/quality")
		{
			q.GET("/overview", s.qualityOverview)
			q.POST("/scan", s.qualityScan)
			q.GET("/reports", s.qualityReports)
			q.GET("/reports/:id", s.qualityReportGet)
		}

		// 考核指标（S3）：时延 P50/P90、达标率、自动化执行率
		mt := api.Group("/metrics")
		{
			mt.GET("/kpi", s.metricsKPI)
		}

		// 关系修正（T16）：拓扑 Diff + 稽核孤儿 → 建议 → 审批 → 执行留痕
		rl := api.Group("/relations")
		{
			rl.POST("/scan", s.relationScan)
			rl.GET("", s.relationList)
			rl.GET("/stats", s.relationStats)
			rl.GET("/overrides", s.relationOverrides)
			rl.PATCH("/:id", s.relationUpdate)
			rl.POST("/:id/apply", s.relationApply)
		}

		// 历史故障知识库（T17）：会话归档 + 关键词检索 + 相似案例
		kb := api.Group("/knowledge")
		{
			kb.GET("", s.knowledgeList)
			kb.GET("/stats", s.knowledgeStats)
			kb.GET("/similar", s.knowledgeSimilar)
			kb.GET("/:id", s.knowledgeGet)
			kb.POST("/reindex", s.knowledgeReindex)
		}

		// 外部日志分析服务（log_analyzer）健康探测：供 /log-analysis 页决定嵌入还是降级提示
		lg := api.Group("/logs")
		{
			lg.GET("/health", s.logsHealth)
		}

		// 身份映射：拓扑 / Pinpoint / Prometheus / CMDB 字段贯通后的统一应用与主机视图
		id := api.Group("/identity")
		{
			id.GET("/stats", s.identityStats)
			id.GET("/apps", s.identityApps)
			id.GET("/hosts", s.identityHosts)
			id.POST("/rebuild", s.identityRebuild)
			id.GET("/resolve", s.identityResolve)
		}
	}
	return r
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func fail(c *gin.Context, code int, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg == "" {
		switch code {
		case http.StatusNotFound:
			msg = "not found"
		case http.StatusBadRequest:
			msg = "bad request"
		case http.StatusInternalServerError:
			msg = "internal server error"
		default:
			msg = http.StatusText(code)
		}
	}
	c.JSON(code, gin.H{"error": msg})
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"uptime":  time.Since(s.started).Round(time.Second).String(),
		"storage": s.store.Info(),
		"topo":    s.topo.Source(),
		"mcp":     s.mcp.Enabled(),
		"now":     time.Now(),
	})
}

func (s *Server) meta(c *gin.Context) {
	info := s.store.Info()
	c.JSON(http.StatusOK, gin.H{
		"defaultEnv":         s.cfg.DefaultEnv,
		"defaultViewMode":    s.cfg.DefaultViewMode,
		"explosionThreshold": s.cfg.ExplosionThreshold,
		"topo": gin.H{
			"mode":   s.cfg.TopoMode,
			"source": s.topo.Source(),
			"dir":    s.cfg.TopoDataDir,
			"dates":  s.topo.Dates(),
		},
		"storage": info,
		"mcp": gin.H{
			"enabled": s.mcp.Enabled(),
			"servers": s.mcp.Servers(),
			"urls": gin.H{
				"pinpoint":   s.cfg.PinpointURL,
				"prometheus": s.cfg.PrometheusMCPURL,
			},
		},
	})
}

// ---------- 拓扑 ----------

func (s *Server) topoDates(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"source": s.topo.Source(), "dates": s.topo.Dates()})
}

func (s *Server) topoApps(c *gin.Context) {
	date, env := s.dateEnv(c)
	apps, err := s.topo.Apps(date, env)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "env": env, "total": len(apps), "apps": apps})
}

func (s *Server) topoGraph(c *gin.Context) {
	date, env := s.dateEnv(c)
	viewMode := c.DefaultQuery("viewMode", s.cfg.DefaultViewMode)
	g, err := s.topo.Graph(c.Query("focusAppId"), viewMode, date, env)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, g)
}

func (s *Server) topoImpact(c *gin.Context) {
	date, env := s.dateEnv(c)
	viewMode := c.DefaultQuery("viewMode", s.cfg.DefaultViewMode)
	appID := strings.TrimSpace(c.Query("appId"))
	mwType := strings.TrimSpace(c.Query("mwType"))
	addr := strings.TrimSpace(c.Query("addr"))
	if appID == "" && (mwType == "" || addr == "") {
		fail(c, http.StatusBadRequest, fmt.Errorf("必须提供 appId，或同时提供 mwType 与 addr"))
		return
	}
	var (
		im  *domain.Impact
		err error
	)
	if appID != "" {
		im, err = s.topo.ImpactByApp(appID, appID, date, env, viewMode)
	} else {
		im, err = s.topo.ImpactByMiddleware(mwType, addr, date, env)
	}
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, im)
}

func (s *Server) topoDiff(c *gin.Context) {
	dates := s.topo.Dates()
	from, to := c.Query("from"), c.Query("to")
	if to == "" && len(dates) > 0 {
		to = dates[len(dates)-1]
	}
	if from == "" && len(dates) > 1 {
		from = dates[len(dates)-2]
	}
	if from == "" || to == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("需要至少两个快照，或显式传 from 与 to"))
		return
	}
	env := c.DefaultQuery("env", s.cfg.DefaultEnv)
	d, err := s.topo.Diff(from, to, env)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (s *Server) topoMwTypes(c *gin.Context) {
	date, env := s.dateEnv(c)
	stats, err := s.topo.MwTypes(date, env)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "env": env, "types": stats})
}

func (s *Server) topoRows(c *gin.Context) {
	date, env := s.dateEnv(c)
	limit := atoiDefault(c.Query("limit"), 200)
	rows, err := s.topo.Rows(date, env)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	total := len(rows)
	if limit > 0 && total > limit {
		rows = rows[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "env": env, "total": total, "rows": rows})
}

// dateEnv 统一处理快照日期与环境默认值。
func (s *Server) dateEnv(c *gin.Context) (string, string) {
	date := c.Query("date")
	if date == "" {
		if dates := s.topo.Dates(); len(dates) > 0 {
			date = dates[len(dates)-1]
		}
	}
	env := c.DefaultQuery("env", s.cfg.DefaultEnv)
	return date, env
}

// ---------- 巡检 ----------

func (s *Server) listTasks(c *gin.Context) {
	tasks := store.ListInto[domain.InspectionTask](s.store, "inspection_task")
	c.JSON(http.StatusOK, gin.H{"total": len(tasks), "tasks": tasks})
}

func (s *Server) createTask(c *gin.Context) {
	var task domain.InspectionTask
	if err := c.ShouldBindJSON(&task); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(task.Name) == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("任务名不能为空"))
		return
	}
	if err := inspect.ValidateCron(task.Cron); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if task.ID == "" {
		task.ID = store.NewID("task")
	}
	if task.Scene == "" {
		task.Scene = "topo"
	}
	if task.RuleSetVersion == "" {
		task.RuleSetVersion = "topo-t1-t11@v1"
	}
	if task.Scope.Env == "" {
		task.Scope.Env = s.cfg.DefaultEnv
	}
	task.Enabled = true
	task.CreatedAt = time.Now()
	if err := s.store.Put("inspection_task", task.ID, task); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

func (s *Server) deleteTask(c *gin.Context) {
	if err := s.store.Delete("inspection_task", c.Param("id")); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("id")})
}

func (s *Server) runTask(c *gin.Context) {
	var task domain.InspectionTask
	if !store.GetInto(s.store, "inspection_task", c.Param("id"), &task) {
		fail(c, http.StatusNotFound, fmt.Errorf("任务不存在: %s", c.Param("id")))
		return
	}
	run := s.inspect.Start(task, "manual")
	c.JSON(http.StatusOK, run)
}

func (s *Server) runAdhoc(c *gin.Context) {
	var req struct {
		Name  string       `json:"name"`
		Scope domain.Scope `json:"scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		req.Name = "临时全量巡检"
	}
	if req.Scope.Env == "" {
		req.Scope.Env = s.cfg.DefaultEnv
	}
	task := domain.InspectionTask{
		ID:             "adhoc",
		Name:           req.Name,
		Scene:          "topo",
		Scope:          req.Scope,
		RuleSetVersion: "topo-t1-t11@v1",
		CreatedAt:      time.Now(),
	}
	run := s.inspect.Start(task, "api")
	c.JSON(http.StatusOK, run)
}

func (s *Server) listRuns(c *gin.Context) {
	runs := store.ListInto[domain.InspectionRun](s.store, "inspection_run")
	for i := 1; i < len(runs); i++ {
		for j := i; j > 0 && runs[j].StartedAt.After(runs[j-1].StartedAt); j-- {
			runs[j], runs[j-1] = runs[j-1], runs[j]
		}
	}
	limit := atoiDefault(c.Query("limit"), 50)
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"total": len(runs), "runs": runs})
}

func (s *Server) getRun(c *gin.Context) {
	var run domain.InspectionRun
	if !store.GetInto(s.store, "inspection_run", c.Param("id"), &run) {
		fail(c, http.StatusNotFound, fmt.Errorf("批次不存在: %s", c.Param("id")))
		return
	}
	c.JSON(http.StatusOK, run)
}

func (s *Server) listFindings(c *gin.Context) {
	runID := c.Param("id")
	all := store.ListInto[domain.Finding](s.store, "finding")
	out := make([]domain.Finding, 0, len(all))
	for _, f := range all {
		if f.RunID == runID {
			out = append(out, f)
		}
	}
	// 风险优先，其次按规则码
	rank := map[string]int{inspect.VerdictRisk: 0, inspect.VerdictNoData: 1, inspect.VerdictNotApplicable: 2, inspect.VerdictNotTriggered: 3}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			if rank[out[j].Verdict] < rank[out[j-1].Verdict] {
				out[j], out[j-1] = out[j-1], out[j]
				continue
			}
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"total": len(out), "findings": out})
}

func (s *Server) getReport(c *gin.Context) {
	raw, ok := s.store.Get("inspection_report", c.Param("id"))
	if !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("报告不存在: %s", c.Param("id")))
		return
	}
	var md string
	if err := json.Unmarshal(raw, &md); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(md))
}

// ---------- 排障 ----------

func (s *Server) createDiagnosis(c *gin.Context) {
	var req diagnose.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	sess, err := s.diag.Start(req)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (s *Server) listDiagnosis(c *gin.Context) {
	sessions := s.diag.List(atoiDefault(c.Query("limit"), 50))
	c.JSON(http.StatusOK, gin.H{"total": len(sessions), "sessions": sessions})
}

func (s *Server) getDiagnosis(c *gin.Context) {
	sess, ok := s.diag.Get(c.Param("id"))
	if !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("会话不存在: %s", c.Param("id")))
		return
	}
	c.JSON(http.StatusOK, sess)
}

// diagnosisFeedback POST /api/diagnose/sessions/:id/feedback
// 人工回填根因准确率（T10）：{"accurate":true,"actualRootCause":"...","note":"...","operator":"..."}
func (s *Server) diagnosisFeedback(c *gin.Context) {
	var body struct {
		Accurate        *bool  `json:"accurate"`
		ActualRootCause string `json:"actualRootCause"`
		Note            string `json:"note"`
		Operator        string `json:"operator"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Accurate == nil {
		fail(c, http.StatusBadRequest, fmt.Errorf("accurate 必填"))
		return
	}
	sess, ok := s.diag.SubmitFeedback(c.Param("id"), domain.DiagnosisFeedback{
		Accurate:        *body.Accurate,
		ActualRootCause: body.ActualRootCause,
		Note:            body.Note,
		Operator:        body.Operator,
	})
	if !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("会话不存在: %s", c.Param("id")))
		return
	}
	c.JSON(http.StatusOK, sess)
}

// streamDiagnosis 以 SSE 增量下发证据链：先补发已有步骤，再轮询新增步骤直到会话结束。
func (s *Server) streamDiagnosis(c *gin.Context) {
	id := c.Param("id")
	if _, ok := s.diag.Get(id); !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("会话不存在: %s", id))
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		fail(c, http.StatusInternalServerError, fmt.Errorf("当前连接不支持流式输出"))
		return
	}

	sent := 0
	lastStatus := ""
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(3 * time.Minute)

	for {
		sess, exists := s.diag.Get(id)
		if !exists {
			fmt.Fprintf(c.Writer, "event: error\ndata: {\"error\":\"会话已不存在\"}\n\n")
			flusher.Flush()
			return
		}
		for ; sent < len(sess.Steps); sent++ {
			b, _ := json.Marshal(sess.Steps[sent])
			fmt.Fprintf(c.Writer, "event: step\ndata: %s\n\n", b)
		}
		if sess.Status != lastStatus {
			lastStatus = sess.Status
			meta, _ := json.Marshal(gin.H{
				"id": sess.ID, "status": sess.Status, "confidence": sess.Confidence,
				"rootCause": sess.RootCause, "totalSteps": len(sess.Steps),
			})
			fmt.Fprintf(c.Writer, "event: session\ndata: %s\n\n", meta)
		}
		flusher.Flush()

		if !sess.FinishedAt.IsZero() {
			b, _ := json.Marshal(sess)
			fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", b)
			flusher.Flush()
			return
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-deadline:
			fmt.Fprintf(c.Writer, "event: timeout\ndata: {\"error\":\"等待超时，请重新查询会话\"}\n\n")
			flusher.Flush()
			return
		case <-ticker.C:
		}
	}
}

// ---------- MCP ----------

func (s *Server) mcpProbe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"servers": s.mcp.Probe()})
}

func (s *Server) mcpTools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"tools": s.mcp.Tools(c.Request.Context())})
}

func (s *Server) mcpCall(c *gin.Context) {
	var req struct {
		Server string         `json:"server"`
		Tool   string         `json:"tool"`
		Args   map[string]any `json:"args"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if req.Server == "" || req.Tool == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("server 与 tool 均为必填"))
		return
	}
	start := time.Now()
	res, err := s.mcp.Call(c.Request.Context(), req.Server, req.Tool, req.Args)
	cost := time.Since(start).Milliseconds()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "latencyMs": cost})
		return
	}
	c.JSON(http.StatusOK, gin.H{"latencyMs": cost, "result": json.RawMessage(res)})
}

func (s *Server) mcpLogs(c *gin.Context) {
	logs := store.ListInto[mcp.CallLog](s.store, "tool_call_log")
	for i := 1; i < len(logs); i++ {
		for j := i; j > 0 && logs[j].At.After(logs[j-1].At); j-- {
			logs[j], logs[j-1] = logs[j-1], logs[j]
		}
	}
	limit := atoiDefault(c.Query("limit"), 100)
	if limit > 0 && len(logs) > limit {
		logs = logs[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"total": len(logs), "logs": logs})
}

// EnsureDefaultTask 首次启动时写入一个全量拓扑巡检任务，保证前端有可点入口。
func EnsureDefaultTask(st store.Store, cfg config.Config) domain.InspectionTask {
	tasks := store.ListInto[domain.InspectionTask](st, "inspection_task")
	if len(tasks) > 0 {
		return tasks[0]
	}
	task := domain.InspectionTask{
		ID:             "task-default-topo",
		Name:           "全量拓扑巡检（默认）",
		Scene:          "topo",
		Scope:          domain.Scope{Env: cfg.DefaultEnv},
		RuleSetVersion: "topo-t1-t11@v1",
		Enabled:        true,
		CreatedAt:      time.Now(),
	}
	_ = st.Put("inspection_task", task.ID, task)
	return task
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
