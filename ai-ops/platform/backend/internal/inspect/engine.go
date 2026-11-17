package inspect

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/topo"
)

// Engine 巡检引擎：基于 topo 快照执行 TOPO_T1~T11，输出四态结论。
// 结论口径：RISK / NOT_TRIGGERED / NO_DATA / NOT_APPLICABLE，
// NO_DATA 与 NOT_APPLICABLE 都必须显式给出原因，禁止用“无风险”掩盖数据缺失。
type Engine struct {
	topo  topo.Provider
	store store.Store
	cfg   config.Config
	// mcp / reg 供 T7（存活）、T9（CMDB）、T8（调用链）、T10（日志）、T11（指标）使用，均可为 nil。
	mcp        *mcp.Gateway
	reg        *identity.Registry
	mcpTimeout time.Duration
	// onRisk 巡检风险发布钩子（T9）：批次成功后把 RISK 结论推给告警中心。
	onRisk func(task domain.InspectionTask, run *domain.InspectionRun, findings []domain.Finding)
}

func New(tp topo.Provider, st store.Store, cfg config.Config, gw *mcp.Gateway, reg *identity.Registry) *Engine {
	timeout := cfg.InspectMCPTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Engine{topo: tp, store: st, cfg: cfg, mcp: gw, reg: reg, mcpTimeout: timeout}
}

// SetRiskPublisher 注册风险发布钩子，由启动装配层注入，避免 inspect 反向依赖 alarm。
func (e *Engine) SetRiskPublisher(fn func(task domain.InspectionTask, run *domain.InspectionRun, findings []domain.Finding)) {
	e.onRisk = fn
}

const (
	VerdictRisk          = "RISK"
	VerdictNotTriggered  = "NOT_TRIGGERED"
	VerdictNoData        = "NO_DATA"
	VerdictNotApplicable = "NOT_APPLICABLE"
)

// Start 创建批次并异步执行，返回批次记录供前端轮询。
func (e *Engine) Start(task domain.InspectionTask, trigger string) *domain.InspectionRun {
	run := &domain.InspectionRun{
		ID:           uuid.NewString(),
		TaskID:       task.ID,
		TaskName:     task.Name,
		Trigger:      trigger,
		TopoSyncDate: task.Scope.Date,
		ViewMode:     topo.ViewFolded,
		Status:       "running",
		StartedAt:    time.Now(),
	}
	e.persistRun(run)
	go e.execute(task, run)
	return run
}

func (e *Engine) persistRun(run *domain.InspectionRun) {
	_ = e.store.Put("inspection_run", run.ID, run)
}

func (e *Engine) execute(task domain.InspectionTask, run *domain.InspectionRun) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	rows, date, err := e.loadRows(task.Scope)
	if err != nil {
		run.Status = "failed"
		run.Message = err.Error()
		run.FinishedAt = time.Now()
		e.persistRun(run)
		return
	}
	run.TopoSyncDate = date

	var findings []domain.Finding
	add := func(f domain.Finding) {
		f.ID = fmt.Sprintf("%s-%s", run.ID[:8], findingKey(f.RuleCode, f.ObjectKey))
		f.RunID = run.ID
		f.CreatedAt = time.Now()
		findings = append(findings, f)
	}

	// 规则执行：根据任务配置的启用状态动态执行
	if IsRuleEnabled(task, "TOPO_T1") {
		add(e.ruleDrift(ctx, task, date))
	} else {
		add(domain.Finding{RuleCode: "TOPO_T1", Title: "依赖漂移", ObjectType: "rule", ObjectKey: "disabled", Severity: "P3", Verdict: VerdictNotApplicable, Detail: "该规则在当前任务中已禁用"})
	}
	if IsRuleEnabled(task, "TOPO_T2") {
		for _, f := range e.ruleExplosion(rows, date, EffectiveParams(task, "TOPO_T2")) {
			add(f)
		}
	}
	if IsRuleEnabled(task, "TOPO_T3") {
		for _, f := range e.ruleSinglePoint(rows, date, EffectiveParams(task, "TOPO_T3")) {
			add(f)
		}
	}
	if IsRuleEnabled(task, "TOPO_T4") {
		add(e.ruleCycle())
	}
	if IsRuleEnabled(task, "TOPO_T5") {
		add(e.ruleOrphan(rows))
	}
	if IsRuleEnabled(task, "TOPO_T6") {
		add(e.ruleEnvMismatch(date, task.Scope.Env))
	}

	// T7 / T9 依赖 MCP：主机集合先在本批次内去重并限量，再交给带超时的查询缓存。
	hosts := e.collectScopeHosts(rows)
	cache := newHostMCPCache(e)
	if IsRuleEnabled(task, "TOPO_T7") {
		for _, f := range e.ruleHostAlive(ctx, hosts, cache) {
			add(f)
		}
	} else {
		add(domain.Finding{RuleCode: "TOPO_T7", Title: "主机存活（Prometheus up）", ObjectType: "rule", ObjectKey: "disabled", Severity: "P1", Verdict: VerdictNotApplicable, Detail: "该规则在当前任务中已禁用"})
	}
	if IsRuleEnabled(task, "TOPO_T9") {
		for _, f := range e.ruleCMDB(ctx, hosts, cache) {
			add(f)
		}
	} else {
		add(domain.Finding{RuleCode: "TOPO_T9", Title: "CMDB 资产一致性", ObjectType: "rule", ObjectKey: "disabled", Severity: "P2", Verdict: VerdictNotApplicable, Detail: "该规则在当前任务中已禁用"})
	}
	if IsRuleEnabled(task, "TOPO_T8") {
		for _, f := range e.ruleTrace(ctx, rows, EffectiveParams(task, "TOPO_T8")) {
			add(f)
		}
	} else {
		add(domain.Finding{RuleCode: "TOPO_T8", Title: "调用链异常（Pinpoint）", ObjectType: "rule", ObjectKey: "disabled", Severity: "P2", Verdict: VerdictNotApplicable, Detail: "该规则在当前任务中已禁用"})
	}
	if IsRuleEnabled(task, "TOPO_T10") {
		for _, f := range e.ruleLogErrors(ctx, hosts, cache, EffectiveParams(task, "TOPO_T10")) {
			add(f)
		}
	} else {
		add(domain.Finding{RuleCode: "TOPO_T10", Title: "日志错误（ES）", ObjectType: "rule", ObjectKey: "disabled", Severity: "P2", Verdict: VerdictNotApplicable, Detail: "该规则在当前任务中已禁用"})
	}
	if IsRuleEnabled(task, "TOPO_T11") {
		for _, f := range e.ruleMetricThreshold(ctx, hosts, cache, EffectiveParams(task, "TOPO_T11")) {
			add(f)
		}
	} else {
		add(domain.Finding{RuleCode: "TOPO_T11", Title: "主机指标阈值", ObjectType: "rule", ObjectKey: "disabled", Severity: "P2", Verdict: VerdictNotApplicable, Detail: "该规则在当前任务中已禁用"})
	}

	for _, f := range findings {
		_ = e.store.Put("finding", f.ID, f)
	}

	run.Stats = domain.RunStats{Items: len(findings)}
	for _, f := range findings {
		switch f.Verdict {
		case VerdictRisk:
			run.Stats.Risk++
		case VerdictNotTriggered:
			run.Stats.NotTriggered++
		case VerdictNoData:
			run.Stats.NoData++
		case VerdictNotApplicable:
			run.Stats.NotApplicable++
		}
	}
	run.Status = "succeeded"
	if run.Stats.Risk > 0 {
		run.Message = fmt.Sprintf("发现 %d 项风险", run.Stats.Risk)
	} else {
		run.Message = "未发现风险（注意 NO_DATA 项的含义）"
	}
	run.FinishedAt = time.Now()
	e.persistRun(run)
	if err := e.store.Put("inspection_report", run.ID, e.report(run, findings)); err != nil {
		run.Message += "；报告写入失败: " + err.Error()
	}
	// T9：RISK 结论进入告警中心，复用告警去重 / 自动排障 / 通知能力。
	if e.onRisk != nil {
		risk := make([]domain.Finding, 0, run.Stats.Risk)
		for _, f := range findings {
			if f.Verdict == VerdictRisk {
				risk = append(risk, f)
			}
		}
		if len(risk) > 0 {
			e.onRisk(task, run, risk)
		}
	}
}

// loadRows 锁定快照日期并按范围过滤。
func (e *Engine) loadRows(scope domain.Scope) ([]domain.TopoRow, string, error) {
	env := scope.Env
	if env == "" {
		env = e.cfg.DefaultEnv
	}
	date := scope.Date
	if date == "" {
		dates := e.topo.Dates()
		if len(dates) == 0 {
			return nil, "", fmt.Errorf("拓扑数据源无可用快照")
		}
		date = dates[len(dates)-1]
	}
	rows, err := e.topo.Rows(date, env)
	if err != nil {
		return nil, date, err
	}
	rows = filterRows(rows, scope)
	if len(rows) == 0 {
		return nil, date, fmt.Errorf("快照 %s/%s 在所选范围内无数据", date, env)
	}
	return rows, date, nil
}

func filterRows(rows []domain.TopoRow, scope domain.Scope) []domain.TopoRow {
	if len(scope.AppIDs) == 0 && len(scope.MwTypes) == 0 {
		return rows
	}
	apps := map[string]struct{}{}
	for _, a := range scope.AppIDs {
		apps[a] = struct{}{}
	}
	mws := map[string]struct{}{}
	for _, m := range scope.MwTypes {
		mws[strings.ToLower(m)] = struct{}{}
	}
	out := make([]domain.TopoRow, 0, len(rows))
	for _, r := range rows {
		if len(apps) > 0 {
			if _, ok := apps[r.ApolloAppID]; !ok {
				continue
			}
		}
		if len(mws) > 0 {
			if _, ok := mws[strings.ToLower(r.MiddlewareType)]; !ok {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// T1 依赖漂移：相邻快照的依赖新增/删除。
func (e *Engine) ruleDrift(ctx context.Context, task domain.InspectionTask, date string) domain.Finding {
	f := domain.Finding{
		RuleCode:   "TOPO_T1",
		Title:      "依赖漂移（相邻快照差异）",
		ObjectType: "snapshot",
		ObjectKey:  date,
		Severity:   "P2",
		Verdict:    VerdictNoData,
	}
	dates := e.topo.Dates()
	if len(dates) < 2 {
		f.Detail = fmt.Sprintf("快照数不足（当前 %d 个），无法计算依赖漂移", len(dates))
		return f
	}
	from, to := dates[len(dates)-2], dates[len(dates)-1]
	diff, err := e.topo.Diff(from, to, task.Scope.Env)
	if err != nil {
		f.Detail = "差异计算失败: " + err.Error()
		return f
	}
	if len(diff.Added) == 0 && len(diff.Removed) == 0 {
		f.Verdict = VerdictNotTriggered
		f.Detail = fmt.Sprintf("%s → %s 依赖关系无变化", from, to)
		return f
	}
	f.Verdict = VerdictRisk
	f.Detail = fmt.Sprintf("%s → %s 新增 %d 条依赖，删除 %d 条依赖", from, to, len(diff.Added), len(diff.Removed))
	f.Evidence = map[string]any{
		"from": from, "to": to,
		"added": diff.Added, "removed": diff.Removed,
		"addedCount": len(diff.Added), "removedCount": len(diff.Removed),
	}
	return f
}

// T2 爆炸半径：单个中间件实例被过多应用共享。
func (e *Engine) ruleExplosion(rows []domain.TopoRow, date string, params map[string]any) []domain.Finding {
	threshold := e.cfg.ExplosionThreshold
	if v, ok := params["explosionThreshold"]; ok {
		if f, ok := v.(float64); ok && f > 0 {
			threshold = int(f)
		}
	}
	if threshold <= 0 {
		threshold = 10
	}
	inst := map[string]map[string]struct{}{}
	meta := map[string][2]string{}
	for _, r := range rows {
		if r.MiddlewareType == "" || strings.TrimSpace(r.ConnectAddr) == "" {
			continue
		}
		addr := strings.TrimSpace(r.ConnectAddr)
		k := strings.ToLower(r.MiddlewareType) + "|" + addr
		if inst[k] == nil {
			inst[k] = map[string]struct{}{}
		}
		inst[k][r.ApolloAppID] = struct{}{}
		meta[k] = [2]string{r.MiddlewareType, addr}
	}
	type hit struct {
		key  string
		apps int
	}
	hits := []hit{}
	for k, apps := range inst {
		if len(apps) >= threshold {
			hits = append(hits, hit{k, len(apps)})
		}
	}
	if len(hits) == 0 {
		return []domain.Finding{{
			RuleCode: "TOPO_T2", Title: "爆炸半径（共享中间件）", ObjectType: "snapshot", ObjectKey: date,
			Severity: "P3", Verdict: VerdictNotTriggered,
			Detail: fmt.Sprintf("没有中间件实例被 ≥%d 个应用共享", threshold),
		}}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].apps > hits[j].apps })
	if len(hits) > 30 {
		hits = hits[:30]
	}
	out := make([]domain.Finding, 0, len(hits))
	for _, h := range hits {
		m := meta[h.key]
		severity := "P2"
		if h.apps >= threshold*3 {
			severity = "P0"
		} else if h.apps >= threshold*2 {
			severity = "P1"
		}
		out = append(out, domain.Finding{
			RuleCode:   "TOPO_T2",
			Title:      fmt.Sprintf("中间件 %s 被 %d 个应用共享", m[1], h.apps),
			ObjectType: "middleware",
			ObjectKey:  h.key,
			Severity:   severity,
			Verdict:    VerdictRisk,
			Detail:     fmt.Sprintf("%s 实例 %s 被 %d 个应用依赖，超过阈值 %d，故障时爆炸半径大", m[0], m[1], h.apps, threshold),
			Evidence: map[string]any{
				"middlewareType": m[0], "addr": m[1], "depApps": h.apps, "threshold": threshold,
			},
			Impact: map[string]any{"addr": m[1], "middlewareType": m[0], "depApps": h.apps},
		})
	}
	return out
}

// T3 单点中间件：某类型在范围内只有一个实例，且被多个应用共享。
func (e *Engine) ruleSinglePoint(rows []domain.TopoRow, date string, params map[string]any) []domain.Finding {
	minDepApps := 2
	if v, ok := params["minDepApps"]; ok {
		if f, ok := v.(float64); ok && f >= 1 {
			minDepApps = int(f)
		}
	}
	type agg struct {
		addrs map[string]map[string]struct{}
	}
	byType := map[string]*agg{}
	for _, r := range rows {
		if r.MiddlewareType == "" || strings.TrimSpace(r.ConnectAddr) == "" {
			continue
		}
		t := strings.ToLower(r.MiddlewareType)
		a := byType[t]
		if a == nil {
			a = &agg{addrs: map[string]map[string]struct{}{}}
			byType[t] = a
		}
		addr := strings.TrimSpace(r.ConnectAddr)
		if a.addrs[addr] == nil {
			a.addrs[addr] = map[string]struct{}{}
		}
		a.addrs[addr][r.ApolloAppID] = struct{}{}
	}
	out := []domain.Finding{}
	for t, a := range byType {
		if len(a.addrs) != 1 {
			continue
		}
		var addr string
		var apps map[string]struct{}
		for k, v := range a.addrs {
			addr, apps = k, v
		}
		if len(apps) < minDepApps {
			continue
		}
		out = append(out, domain.Finding{
			RuleCode:   "TOPO_T3",
			Title:      fmt.Sprintf("%s 仅有一个实例 %s", t, addr),
			ObjectType: "middleware",
			ObjectKey:  t + "|" + addr,
			Severity:   "P2",
			Verdict:    VerdictRisk,
			Detail:     fmt.Sprintf("%s 在该范围内只有 1 个实例，被 %d 个应用依赖，存在单点风险", t, len(apps)),
			Evidence:   map[string]any{"middlewareType": t, "addr": addr, "instances": 1, "depApps": len(apps)},
		})
	}
	if len(out) == 0 {
		out = append(out, domain.Finding{
			RuleCode: "TOPO_T3", Title: "单点中间件", ObjectType: "snapshot", ObjectKey: date,
			Severity: "P3", Verdict: VerdictNotTriggered,
			Detail: "范围内各中间件类型均有多个实例",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObjectKey < out[j].ObjectKey })
	return out
}

// T4 依赖环：平台一期只建「应用 → 中间件」两级图，天然无环。
func (e *Engine) ruleCycle() domain.Finding {
	return domain.Finding{
		RuleCode: "TOPO_T4", Title: "依赖环检测", ObjectType: "snapshot", ObjectKey: "two-level-graph",
		Severity: "P3", Verdict: VerdictNotApplicable,
		Detail: "一期拓扑为应用→中间件两级二部图，结构上不可能成环；应用间调用环需接入 Pinpoint 调用链后再启用",
	}
}

// T5 孤儿应用：关系表只能表达“有依赖”，无法表达“无依赖”。
func (e *Engine) ruleOrphan(rows []domain.TopoRow) domain.Finding {
	return domain.Finding{
		RuleCode: "TOPO_T5", Title: "孤儿应用（无任何中间件依赖）", ObjectType: "snapshot", ObjectKey: "app-set",
		Severity: "P3", Verdict: VerdictNoData,
		Detail: fmt.Sprintf("当前快照 %d 行关系均代表“存在依赖”；无依赖的应用不会出现在关系表中，需接入 Apollo 应用全量清单后才能判定", len(rows)),
	}
}

// T6 跨环境错配：同一应用在不同环境复用同一中间件地址。
func (e *Engine) ruleEnvMismatch(date, env string) domain.Finding {
	f := domain.Finding{
		RuleCode: "TOPO_T6", Title: "跨环境中间件错配", ObjectType: "snapshot", ObjectKey: date,
		Severity: "P2", Verdict: VerdictNoData,
	}
	all, err := e.topo.Rows(date, "")
	if err != nil {
		f.Detail = "读取全环境数据失败: " + err.Error()
		return f
	}
	envApps := map[string]map[string]map[string]struct{}{} // env -> app -> addrs
	for _, r := range all {
		if r.MiddlewareType == "" || strings.TrimSpace(r.ConnectAddr) == "" {
			continue
		}
		if envApps[r.Env] == nil {
			envApps[r.Env] = map[string]map[string]struct{}{}
		}
		if envApps[r.Env][r.ApolloAppID] == nil {
			envApps[r.Env][r.ApolloAppID] = map[string]struct{}{}
		}
		envApps[r.Env][r.ApolloAppID][strings.ToLower(r.MiddlewareType)+"|"+strings.TrimSpace(r.ConnectAddr)] = struct{}{}
	}
	if len(envApps) < 2 {
		f.Detail = fmt.Sprintf("当前快照只有 %d 个环境的数据，无法判定跨环境错配", len(envApps))
		return f
	}
	target := env
	if target == "" {
		target = e.cfg.DefaultEnv
	}
	otherAddrs := map[string]struct{}{}
	for e2, apps := range envApps {
		if strings.EqualFold(e2, target) {
			continue
		}
		for _, addrs := range apps {
			for a := range addrs {
				otherAddrs[a] = struct{}{}
			}
		}
	}
	var hits []map[string]any
	for app, addrs := range envApps[target] {
		for a := range addrs {
			if _, ok := otherAddrs[a]; ok {
				hits = append(hits, map[string]any{"appId": app, "relation": a})
			}
		}
	}
	if len(hits) == 0 {
		f.Verdict = VerdictNotTriggered
		f.Detail = fmt.Sprintf("%s 与其它环境没有复用同一中间件地址", target)
		return f
	}
	f.Verdict = VerdictRisk
	f.Detail = fmt.Sprintf("%s 有 %d 条依赖与其它环境复用同一中间件地址", target, len(hits))
	f.Evidence = map[string]any{"hits": hits, "env": target}
	return f
}

// report 生成 markdown 报告（锁定 sync_date 与规则版本，可复现）。
func (e *Engine) report(run *domain.InspectionRun, findings []domain.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 智能巡检报告\n\n")
	fmt.Fprintf(&b, "- 批次: `%s`\n- 任务: %s\n- 触发: %s\n- 拓扑快照: `%s`（固定，可复现）\n- 开始: %s\n- 结束: %s\n\n",
		run.ID, run.TaskName, run.Trigger, run.TopoSyncDate,
		run.StartedAt.Format("2006-01-02 15:04:05"), run.FinishedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "## 结论统计\n\n| 项 | 数量 |\n|---|---|\n")
	fmt.Fprintf(&b, "| 总项 | %d |\n| 🟥 RISK | %d |\n| 🟩 NOT_TRIGGERED | %d |\n| 🟨 NO_DATA | %d |\n| ⚪ NOT_APPLICABLE | %d |\n\n",
		run.Stats.Items, run.Stats.Risk, run.Stats.NotTriggered, run.Stats.NoData, run.Stats.NotApplicable)

	fmt.Fprintf(&b, "## 风险明细\n\n")
	risk := 0
	for _, f := range findings {
		if f.Verdict != VerdictRisk {
			continue
		}
		risk++
		fmt.Fprintf(&b, "%d. **[%s] %s**（%s / %s）\n   - %s\n", risk, f.Severity, f.Title, f.RuleCode, f.ObjectType, f.Detail)
	}
	if risk == 0 {
		fmt.Fprintf(&b, "无。\n")
	}
	fmt.Fprintf(&b, "\n## 数据不足与不适用项（不可当作“无风险”）\n\n")
	for _, f := range findings {
		if f.Verdict == VerdictNoData || f.Verdict == VerdictNotApplicable {
			fmt.Fprintf(&b, "- **%s** %s：%s\n", f.RuleCode, f.Title, f.Detail)
		}
	}
	fmt.Fprintf(&b, "\n## 未触发项\n\n")
	for _, f := range findings {
		if f.Verdict == VerdictNotTriggered {
			fmt.Fprintf(&b, "- **%s** %s：%s\n", f.RuleCode, f.Title, f.Detail)
		}
	}
	return b.String()
}

func findingKey(rule, key string) string {
	key = strings.NewReplacer("/", "_", "|", "_", ":", "_", " ", "_").Replace(key)
	if len(key) > 60 {
		key = key[:60]
	}
	return strings.ToLower(rule) + "-" + key
}
