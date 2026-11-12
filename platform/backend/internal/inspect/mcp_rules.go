package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/mcp"
)

// 巡检是批量任务：一次可能覆盖几十个应用和主机，MCP 调用必须限量、限时，
// 且同一主机在同一批次内只查询一次（hostMCPCache 负责）。

// scopeHost 巡检范围内的中间件主机（已按身份归一去重）。
type scopeHost struct {
	Host  string
	Key   string
	Apps  map[string]struct{}
	Types map[string]struct{}
}

// hostMCPCache 批次级主机查询缓存。execute 为单协程顺序执行，无需加锁。
type hostMCPCache struct {
	e       *Engine
	up      map[string]upHostResult
	cmdb    map[string]cmdbHostResult
	metrics map[string]hostMetricsResult
}

type upHostResult struct {
	samples []mcp.PromSample
	err     error
}

type cmdbHostResult struct {
	records []map[string]any
	err     error
}

type hostMetricsResult struct {
	metrics mcp.PromHostMetrics
	err     error
}

func newHostMCPCache(e *Engine) *hostMCPCache {
	return &hostMCPCache{
		e:       e,
		up:      map[string]upHostResult{},
		cmdb:    map[string]cmdbHostResult{},
		metrics: map[string]hostMetricsResult{},
	}
}

func (c *hostMCPCache) mcpReady() bool {
	return c.e.cfg.InspectMCPEnabled && c.e.mcp != nil && c.e.mcp.Enabled()
}

// upSamples 查询单台主机的 up 序列；命中缓存直接返回，保证同一主机只查一次。
func (c *hostMCPCache) upSamples(ctx context.Context, h scopeHost) ([]mcp.PromSample, error) {
	if r, ok := c.up[h.Key]; ok {
		return r.samples, r.err
	}
	// 单主机最多尝试 2 个标识（主标识 + 1 个备用别名），避免批量任务里调用数失控。
	aliases := c.e.hostAliases(h)
	if len(aliases) > 2 {
		aliases = aliases[:2]
	}
	samples := []mcp.PromSample{}
	var err error
	if len(aliases) == 0 {
		err = fmt.Errorf("主机 %s 无可用标识", h.Host)
	}
	for _, a := range aliases {
		// 不做正则转义：与现有 mock / 演示数据的 `host.*` 约定一致，
		// 返回后仍会按别名严格归属，前缀误匹配不会影响结论。
		key := identity.NormalizeHost(a)
		if key == "" {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, c.e.mcpTimeout)
		raw, callErr := c.e.mcp.Call(cctx, "prometheus", "query", map[string]any{
			"query": fmt.Sprintf(`up{instance=~"%s.*"}`, key),
		})
		cancel()
		if callErr != nil {
			err = callErr
			if ctx.Err() != nil { // 批次预算耗尽，不再重试其它别名
				break
			}
			continue
		}
		got, parseErr := mcp.PrometheusSamples(raw)
		if parseErr != nil {
			err = parseErr
			continue
		}
		if len(got) > 0 { // 主标识命中即停止，保证同一主机只产生必要调用
			samples, err = got, nil
			break
		}
	}
	c.up[h.Key] = upHostResult{samples: samples, err: err}
	return samples, err
}

// cmdbRecords 查询单台主机的资产记录；命中缓存直接返回。
func (c *hostMCPCache) cmdbRecords(ctx context.Context, h scopeHost) ([]map[string]any, error) {
	if r, ok := c.cmdb[h.Key]; ok {
		return r.records, r.err
	}
	size := c.e.cfg.InspectCMDBPageSize
	if size <= 0 {
		size = 5
	}
	cctx, cancel := context.WithTimeout(ctx, c.e.mcpTimeout)
	raw, err := c.e.mcp.Call(cctx, "cmdb", "cmdb_server_query", map[string]any{
		"ip": h.Host, "currentPage": 1, "pageSize": size,
	})
	cancel()
	records := []map[string]any{}
	if err == nil {
		var page mcp.CMDBPage
		if page, err = mcp.CMDBRecords(raw); err == nil {
			records = page.Records
		}
	}
	c.cmdb[h.Key] = cmdbHostResult{records: records, err: err}
	return records, err
}

// collectScopeHosts 从拓扑行抽取主机：按身份归一去重，按依赖应用数排序，并限制批量上限。
func (e *Engine) collectScopeHosts(rows []domain.TopoRow) []scopeHost {
	byKey := map[string]*scopeHost{}
	for _, r := range rows {
		host := identity.NormalizeHost(r.ConnectAddr)
		if host == "" {
			continue
		}
		key := host
		if e.reg != nil && e.reg.Enabled() {
			if h, ok := e.reg.ResolveHost(host); ok && h.Key != "" {
				key = h.Key
			}
		}
		sh := byKey[key]
		if sh == nil {
			sh = &scopeHost{Host: host, Key: key, Apps: map[string]struct{}{}, Types: map[string]struct{}{}}
			byKey[key] = sh
		}
		if r.ApolloAppID != "" {
			sh.Apps[r.ApolloAppID] = struct{}{}
		}
		if t := strings.TrimSpace(r.MiddlewareType); t != "" {
			sh.Types[strings.ToLower(t)] = struct{}{}
		}
	}
	out := make([]scopeHost, 0, len(byKey))
	for _, h := range byKey {
		out = append(out, *h)
	}
	// 先按被依赖应用数降序（优先覆盖爆炸半径大的主机），再按主机名保证确定性。
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Apps) != len(out[j].Apps) {
			return len(out[i].Apps) > len(out[j].Apps)
		}
		return out[i].Host < out[j].Host
	})
	max := e.cfg.InspectMCPMaxHosts
	if max <= 0 {
		max = 20
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// hostAliases 返回主机的全部候选标识，首个为主标识（保证 mock / 真实环境都能命中）。
func (e *Engine) hostAliases(h scopeHost) []string {
	primary := identity.NormalizeHost(h.Host)
	out := []string{h.Host}
	rest := []string{}
	for a := range e.reg.HostAliases(h.Host) {
		n := identity.NormalizeHost(a)
		if n == "" || n == primary {
			continue
		}
		rest = append(rest, a)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// T7 主机存活：up=0 判 RISK；无序列 / 查询失败判 NO_DATA（不可当作健康）。
func (e *Engine) ruleHostAlive(ctx context.Context, hosts []scopeHost, cache *hostMCPCache) []domain.Finding {
	if !cache.mcpReady() {
		return []domain.Finding{mcpNoData("TOPO_T7", "主机存活（Prometheus up）",
			"上游服务未启用（INSPECT_MCP_ENABLED / MCP_ENABLED=false），无法判断主机存活")}
	}
	if len(hosts) == 0 {
		return []domain.Finding{mcpNoData("TOPO_T7", "主机存活（Prometheus up）",
			"巡检范围内没有可识别的中间件主机地址")}
	}

	// instance → 身份 Key，用于把返回序列严格归属到主机（避免前缀误匹配）。
	aliasToHost := map[string]string{}
	for _, h := range hosts {
		aliasToHost[identity.NormalizeHost(h.Host)] = h.Key
		for a := range e.reg.HostAliases(h.Host) {
			aliasToHost[a] = h.Key
		}
	}

	out := make([]domain.Finding, 0, len(hosts))
	for _, h := range hosts {
		f := domain.Finding{
			RuleCode:   "TOPO_T7",
			Title:      "主机存活（Prometheus up）",
			ObjectType: "host",
			ObjectKey:  h.Host,
			Severity:   "P1",
		}
		evidence := map[string]any{"host": h.Host, "middlewareTypes": sortedKeys(h.Types), "depApps": len(h.Apps)}
		samples, err := cache.upSamples(ctx, h)
		if err != nil {
			f.Verdict = VerdictNoData
			f.Detail = "Prometheus 查询失败，无法判断存活: " + err.Error()
			f.Evidence = evidence
			out = append(out, f)
			continue
		}
		values := make([]float64, 0, len(samples))
		for _, s := range samples {
			if key, ok := aliasToHost[identity.NormalizeHost(s.Metric["instance"])]; ok && key == h.Key {
				values = append(values, s.Value)
			}
		}
		if len(values) == 0 {
			f.Verdict = VerdictNoData
			f.Detail = "Prometheus 未返回该主机的 up 序列（可能未纳管 node_exporter 或实例标签不一致）"
			f.Evidence = evidence
			out = append(out, f)
			continue
		}
		down := 0
		for _, v := range values {
			if v == 0 {
				down++
			}
		}
		evidence["series"] = len(values)
		evidence["downSeries"] = down
		if down > 0 {
			f.Verdict = VerdictRisk
			f.Detail = fmt.Sprintf("Prometheus up=0（%d/%d 条序列），主机失联；被 %d 个应用依赖", down, len(values), len(h.Apps))
			f.Impact = map[string]any{"host": h.Host, "depApps": len(h.Apps), "middlewareTypes": sortedKeys(h.Types)}
		} else {
			f.Verdict = VerdictNotTriggered
			f.Detail = fmt.Sprintf("Prometheus up=1（%d 条序列），主机存活", len(values))
		}
		f.Evidence = evidence
		out = append(out, f)
	}
	return out
}

// T9 CMDB 一致性：查无资产 / 非在线 / 在线资产缺少负责人 判 RISK；查询失败判 NO_DATA。
func (e *Engine) ruleCMDB(ctx context.Context, hosts []scopeHost, cache *hostMCPCache) []domain.Finding {
	if !cache.mcpReady() {
		return []domain.Finding{mcpNoData("TOPO_T9", "CMDB 资产一致性",
			"上游服务未启用（INSPECT_MCP_ENABLED / MCP_ENABLED=false），无法核对 CMDB 资产")}
	}
	if len(hosts) == 0 {
		return []domain.Finding{mcpNoData("TOPO_T9", "CMDB 资产一致性",
			"巡检范围内没有可识别的中间件主机地址")}
	}

	out := make([]domain.Finding, 0, len(hosts))
	for _, h := range hosts {
		base := domain.Finding{
			RuleCode:   "TOPO_T9",
			Title:      "CMDB 资产一致性",
			ObjectType: "host",
			ObjectKey:  h.Host,
			Severity:   "P2",
		}
		records, err := cache.cmdbRecords(ctx, h)
		if err != nil {
			f := base
			f.Verdict = VerdictNoData
			f.Detail = "CMDB 查询失败，无法对账: " + err.Error()
			f.Evidence = map[string]any{"host": h.Host, "depApps": len(h.Apps)}
			out = append(out, f)
			continue
		}
		rec := e.reg.MatchRecord(records, h.Host)
		if rec == nil {
			f := base
			f.Severity = "P1"
			f.Verdict = VerdictRisk
			f.Detail = fmt.Sprintf("CMDB 未收录该主机（拓扑中有 %d 个应用依赖），资产账实不符或拓扑数据异常", len(h.Apps))
			f.Evidence = map[string]any{"host": h.Host, "depApps": len(h.Apps), "cmdbRecords": len(records)}
			out = append(out, f)
			continue
		}

		hostName := cmdbString(rec, "hostName", "host_name", "hostname", "name")
		node := cmdbString(rec, "node", "idc", "room", "机房")
		state, hasState := cmdbState(rec)
		evidence := map[string]any{"host": h.Host, "hostName": hostName, "node": node, "depApps": len(h.Apps)}

		switch {
		case !hasState:
			f := base
			f.Verdict = VerdictNoData
			f.Detail = "CMDB 记录缺少 state 字段，无法判断在线状态"
			evidence["cmdbId"] = cmdbString(rec, "id", "serverId", "server_id", "assetId", "asset_id")
			f.Evidence = evidence
			out = append(out, f)
			continue
		case state != 0:
			f := base
			f.Severity = "P2"
			if state == 4 || state == 6 { // 已报废 / 待清退 仍被引用
				f.Severity = "P1"
			}
			f.Verdict = VerdictRisk
			f.Detail = fmt.Sprintf("CMDB 状态=%d（非在线）但仍有 %d 个应用依赖该实例", state, len(h.Apps))
			evidence["state"] = state
			f.Evidence = evidence
			f.Impact = map[string]any{"host": h.Host, "depApps": len(h.Apps), "hostName": hostName, "node": node}
			out = append(out, f)
		default:
			f := base
			f.Verdict = VerdictNotTriggered
			f.Detail = "CMDB 状态在线，与拓扑引用一致"
			evidence["state"] = state
			f.Evidence = evidence
			out = append(out, f)
			// 在线资产才校验负责人，避免已报废资产制造噪音。
			if cmdbOwner(rec) == "" {
				of := base
				of.ObjectKey = h.Host + "@owner"
				of.Severity = "P2"
				of.Verdict = VerdictRisk
				of.Detail = "CMDB 在线资产缺少负责人（operA / usingBy 均为空），故障时无法定责"
				of.Evidence = evidence
				out = append(out, of)
			}
		}
	}
	return out
}

func mcpNoData(code, title, detail string) domain.Finding {
	return domain.Finding{
		RuleCode: code, Title: title, ObjectType: "rule", ObjectKey: "mcp",
		Severity: "P3", Verdict: VerdictNoData, Detail: detail,
	}
}

// cmdbState 读取 CMDB 在线状态（0=在线），兼容数字与字符串。
func cmdbState(rec map[string]any) (int, bool) {
	v, ok := rec["state"]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

// cmdbOwner 返回资产负责人：优先运维负责人 operA，其次 usingBy。
func cmdbOwner(rec map[string]any) string {
	return cmdbString(rec, "operA", "oper_a", "owner", "manager", "负责人", "usingBy", "using_by")
}

func cmdbString(rec map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := rec[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// paramInt / paramFloat 读取规则参数，兼容默认值（float64）与任务覆盖（JSON 数字或字符串）。
func paramInt(params map[string]any, key string, def int) int {
	if f, ok := paramNumber(params[key]); ok {
		return int(f)
	}
	return def
}

func paramFloat(params map[string]any, key string, def float64) float64 {
	if f, ok := paramNumber(params[key]); ok {
		return f
	}
	return def
}

func paramNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	case string:
		if strings.TrimSpace(n) == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
