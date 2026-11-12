// mockmcp 本地 Mock MCP 服务器：单进程模拟四个 MCP server 的 streamable-http 端点
//
//	pinpoint    :8888   （与 config.go 默认 PINPOINT_MCP_URL 一致）
//	prometheus  :8090   （PROMETHEUS_MCP_URL）
//	cmdb        :8061   （CMDB_MCP_URL）
//	es          :9201   （ES_MCP_URL，仅本地验证 T10 用）
//
// 实现 JSON-RPC 2.0 的 initialize / notifications/* / ping / tools/list / tools/call。
// tools/call 优先走"参数感知"的动态处理器（cmdb 过滤、prometheus up 表、pinpoint 服务地图、es 日志量等），
// 其余工具直接返回 mockdata/<server>/<tool>.json 文件内容——所有数据均以本地文件为准。
package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type toolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

type serverCtx struct {
	name      string
	port      string
	dataDir   string
	tools     []toolDef
	startTime time.Time

	// pinpoint 动态数据
	apps       map[string]map[string]any
	appAlias   map[string]string
	appNames   []string
	archVer    string
	archThresh float64
	archVio    []map[string]any
	// prometheus 动态数据
	upTable      map[string]float64
	hostOverride map[string]map[string]any
	// cmdb 动态数据
	dbRecords []map[string]any
	// es 动态数据（T10 日志巡检）
	esIndices   []string
	esLogCounts map[string]int
}

func main() {
	dir := resolveDir()
	servers := []*serverCtx{
		newServer("pinpoint", getenv("PINPOINT_PORT", "8888"), dir),
		newServer("prometheus", getenv("PROMETHEUS_PORT", "8090"), dir),
		newServer("cmdb", getenv("CMDB_PORT", "8061"), dir),
		newServer("es", getenv("ES_PORT", "9201"), dir),
	}
	for _, s := range servers {
		s := s
		go func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
			mux.HandleFunc("/mcp", s.handle)
			mux.HandleFunc("/", s.handle)
			log.Printf("[mockmcp] %s listening :%s (data=%s tools=%d)", s.name, s.port, s.dataDir, len(s.tools))
			if err := http.ListenAndServe(":"+s.port, mux); err != nil {
				log.Fatalf("[mockmcp] %s: %v", s.name, err)
			}
		}()
	}
	select {}
}

// resolveDir 定位 mockdata 目录（兼容从仓库根 / backend / cmd 目录启动）。
func resolveDir() string {
	if d := os.Getenv("MOCKDATA_DIR"); d != "" {
		return d
	}
	for _, c := range []string{"mockdata", "../mockdata", "backend/mockdata", "../../mockdata"} {
		if _, err := os.Stat(filepath.Join(c, "pinpoint", "_tools.json")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	abs, _ := filepath.Abs("mockdata")
	return abs
}

func newServer(name, port, dir string) *serverCtx {
	s := &serverCtx{name: name, port: port, dataDir: filepath.Join(dir, name), startTime: time.Now()}
	loadJSONFile(filepath.Join(s.dataDir, "_tools.json"), &s.tools)
	s.appAlias = map[string]string{}
	switch name {
	case "pinpoint":
		loadJSONFile(filepath.Join(s.dataDir, "_apps.json"), &s.apps)
		for appName, info := range s.apps {
			s.appAlias[strings.ToLower(appName)] = appName
			if id, _ := info["appId"].(string); id != "" {
				s.appAlias[id] = appName
			}
			s.appNames = append(s.appNames, appName)
		}
		sort.Strings(s.appNames)
		var arch struct {
			Version    string           `json:"version"`
			Threshold  float64          `json:"threshold"`
			Violations []map[string]any `json:"violations"`
		}
		loadJSONFile(filepath.Join(s.dataDir, "_arch.json"), &arch)
		s.archVer, s.archThresh, s.archVio = arch.Version, arch.Threshold, arch.Violations
	case "prometheus":
		loadJSONFile(filepath.Join(s.dataDir, "_up_table.json"), &s.upTable)
		// _host_metrics.json 由 scripts/seed_demo.py 生成，按场景覆写主机指标
		loadJSONFile(filepath.Join(s.dataDir, "_host_metrics.json"), &s.hostOverride)
	case "cmdb":
		loadJSONFile(filepath.Join(s.dataDir, "_db_servers.json"), &s.dbRecords)
	case "es":
		loadJSONFile(filepath.Join(s.dataDir, "_indices.json"), &s.esIndices)
		loadJSONFile(filepath.Join(s.dataDir, "_log_counts.json"), &s.esLogCounts)
		if s.esLogCounts == nil {
			s.esLogCounts = map[string]int{}
		}
	}
	return s
}

// ---------- JSON-RPC 处理 ----------

func (s *serverCtx) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"jsonrpc": "2.0", "id": nil,
			"error": map[string]any{"code": -32700, "message": "parse error"}})
		return
	}
	// 通知或无 id 请求：202 无响应体（与 MCP streamable-http 约定一致）
	if strings.HasPrefix(req.Method, "notifications/") || len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", fmt.Sprintf("mock-%d", time.Now().UnixNano()))
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "mock-" + s.name, "version": "0.1.0"},
			"instructions":    "本地 Mock MCP（数据目录 backend/mockdata/" + s.name + "）",
		}})
	case "ping":
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
	case "tools/list":
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{"tools": s.tools}})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		text, found := s.callTool(p.Name, p.Arguments)
		if !found {
			text = fmt.Sprintf("mock 数据缺失: mockdata/%s/%s.json", s.name, p.Name)
			writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": true}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": false}})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"error": map[string]any{"code": -32601, "message": "method not found: " + req.Method}})
	}
}

// callTool 动态处理器优先，未命中回落静态文件。
func (s *serverCtx) callTool(name string, args map[string]any) (string, bool) {
	if args == nil {
		args = map[string]any{}
	}
	var text string
	var ok bool
	switch s.name {
	case "pinpoint":
		switch name {
		case "get_server_map_tool":
			text, ok = s.pinServerMap(args), true
		case "drag_heatmap_tool":
			text, ok = s.pinHeatmap(args), true
		case "get_transaction_trace_tool":
			text, ok = s.pinTrace(args), true
		case "prometheus_query_tool":
			text, ok = promQuery(args, s.upTable), true
		case "arch_metric_query_tool":
			text, ok = s.archMetric(args), true
		case "arch_rule_check_tool":
			text, ok = s.archCheck(args), true
		}
	case "prometheus":
		switch name {
		case "list_alerts":
			text, ok = promListAlerts(s.startTime, s.dataDir), true
		case "healthy":
			text, ok = promHealthy(s.startTime), true
		case "targets_metadata":
			text, ok = promTargetsMetadata(s.upTable), true
		case "series":
			text, ok = promSeries(args, s.upTable), true
		case "query":
			text, ok = promQuery(args, s.upTable), true
		case "range_query":
			text, ok = promRange(args, s.upTable), true
		case "label_values":
			if v, gen := promLabelValues(args, s.upTable); gen {
				text, ok = v, true
			}
		case "host_metrics_realtime":
			text, ok = hostMetrics(args, false, s.hostOverride), true
		case "host_metrics_trend":
			text, ok = hostMetrics(args, true, s.hostOverride), true
		}
	case "cmdb":
		if name == "cmdb_server_query" {
			text, ok = cmdbServerQuery(args, s.dbRecords), true
		}
	case "es":
		switch name {
		case "list_indices":
			text, ok = s.esListIndices(args), true
		case "search":
			text, ok = s.esSearch(args), true
		case "get_shards":
			text, ok = s.esShards(args), true
		}
	}
	if ok {
		return text, true
	}
	b, err := os.ReadFile(filepath.Join(s.dataDir, name+".json"))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// ---------- cmdb 动态处理器 ----------

// cmdbServerQuery 按参数过滤 _db_servers.json：ip 精确/逗号成员匹配，state 等值，分页。
// 响应体形状与 cmdb-mcp-server 一致：{code, message, fail, data:{records, total}}。
func cmdbServerQuery(args map[string]any, db []map[string]any) string {
	ip, _ := args["ip"].(string)
	ip = strings.TrimSpace(ip)
	state := -1.0
	switch v := args["state"].(type) {
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			state = f
		}
	case float64:
		state = v
	}
	page := numArg(args, "currentPage", 1)
	size := numArg(args, "pageSize", 10)

	filtered := []map[string]any{}
	for _, rec := range db {
		if ip != "" && !recMatchIP(rec, ip) {
			continue
		}
		if state >= 0 {
			if st, _ := rec["state"].(float64); st != state {
				continue
			}
		}
		filtered = append(filtered, rec)
	}
	start := int((page - 1) * size)
	if start > len(filtered) || start < 0 {
		start = 0
	}
	end := start + int(size)
	if end > len(filtered) {
		end = len(filtered)
	}
	return mustJSON(map[string]any{
		"code": 200, "message": "success", "fail": false,
		"data": map[string]any{"records": filtered[start:end], "total": len(filtered)},
	})
}

func recMatchIP(rec map[string]any, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	for _, k := range []string{"ip", "vip", "publicIp"} {
		for _, p := range strings.Split(str(rec[k]), ",") {
			if strings.ToLower(strings.TrimSpace(p)) == target {
				return true
			}
		}
	}
	return false
}

// ---------- es 动态处理器 ----------

// esListIndices 按 index_pattern（支持尾部 *）过滤 _indices.json，返回 JSON 数组。
func (s *serverCtx) esListIndices(args map[string]any) string {
	pattern, _ := args["index_pattern"].(string)
	pattern = strings.TrimSpace(pattern)
	prefix := strings.TrimSuffix(pattern, "*")
	out := []map[string]any{}
	for _, idx := range s.esIndices {
		if pattern == "" || pattern == "*" || strings.HasPrefix(idx, prefix) {
			out = append(out, map[string]any{"index": idx, "status": "open", "docs.count": "1200"})
		}
	}
	return mustJSON(out)
}

// esSearch 从 query_body 中识别主机，返回 _log_counts.json 中配置的错误日志条数；
// 文案格式与 elasticsearch-mcp 的 search 工具一致（"Total results: N, showing 0."）。
func (s *serverCtx) esSearch(args map[string]any) string {
	body := mustJSON(args["query_body"])
	host := ""
	for name := range s.esLogCounts {
		if name != "default" && strings.Contains(body, name) {
			host = name
			break
		}
	}
	count := s.esLogCounts["default"]
	if host != "" {
		count = s.esLogCounts[host]
	}
	return fmt.Sprintf("Total results: %d, showing 0.", count)
}

// esShards 按索引返回分片信息（ES _cat/shards 的简化形态），供排障验真确认日志源可用。
func (s *serverCtx) esShards(args map[string]any) string {
	index, _ := args["index"].(string)
	index = strings.TrimSpace(index)
	prefix := strings.TrimSuffix(index, "*")
	out := []map[string]any{}
	for _, idx := range s.esIndices {
		if index != "" && index != "*" && !strings.HasPrefix(idx, prefix) {
			continue
		}
		for shard := 0; shard < 2; shard++ {
			out = append(out, map[string]any{
				"index": idx, "shard": strconv.Itoa(shard), "prirep": "p",
				"state": "STARTED", "docs": "1200", "store": "1.2mb",
				"node": fmt.Sprintf("mock-node-%d", shard+1),
			})
		}
	}
	if len(out) == 0 {
		out = append(out, map[string]any{
			"index": "logs-mock", "shard": "0", "prirep": "p",
			"state": "STARTED", "docs": "1200", "store": "1.2mb", "node": "mock-node-1",
		})
	}
	return mustJSON(out)
}

// ---------- prometheus 动态处理器 ----------

// upHostsIn 从 up{instance=~"..."} 查询里解析所有候选主机。
// 兼容两种写法：identity.UpQuery 的 ^host(:[0-9]+)?$ 精确形式与旧版 host.* 前缀形式。
// 返回 nil 表示不是 up 查询。
func upHostsIn(q string, up map[string]float64) []string {
	if !strings.Contains(q, "up{instance=~") {
		return nil
	}
	hosts := make([]string, 0, len(up))
	for k := range up {
		hosts = append(hosts, k)
	}
	sort.Strings(hosts)
	out := []string{}
	for _, h := range hosts {
		lit := strings.ReplaceAll(regexp.QuoteMeta(h), `\`, `\\`)
		if strings.Contains(q, lit) || strings.Contains(q, h) {
			out = append(out, h)
		}
	}
	return out
}

// promQuery 解析 up{instance=~"host.*"} 查 up 表；业务接口指标返回演示序列；
// 其余 promql 返回通用示例序列。
func promQuery(args map[string]any, up map[string]float64) string {
	q, _ := args["query"].(string)
	now := float64(time.Now().Unix())
	if hosts := upHostsIn(q, up); hosts != nil {
		res := []any{}
		for _, host := range hosts {
			// up=0 也要返回序列：平台据此区分「采集异常 / 宕机」与「未纳管」。
			v := "0"
			if up[host] >= 1 {
				v = "1"
			}
			res = append(res, map[string]any{
				"metric": map[string]any{"__name__": "up", "instance": host + ":9100", "job": "node_exporter"},
				"value":  []any{now, v},
			})
		}
		return mustJSON(map[string]any{"status": "success",
			"data": map[string]any{"resultType": "vector", "result": res}})
	}
	if strings.Contains(q, "http_requests_total") || strings.Contains(q, "http_request_duration_seconds_bucket") {
		name, v := bizQueryPoint(q)
		metric := map[string]any{"__name__": name}
		if app := bizAppOf(q); app != "" {
			metric["app_id"] = app
		}
		return mustJSON(map[string]any{"status": "success", "data": map[string]any{
			"resultType": "vector",
			"result": []any{map[string]any{
				"metric": metric,
				"value":  []any{now, fmt.Sprintf("%.4f", v)},
			}}}})
	}
	res := []any{map[string]any{
		"metric": map[string]any{"__name__": "node_load1", "instance": "10.214.0.9:9100", "job": "node_exporter"},
		"value":  []any{now, "0.42"},
	}}
	return mustJSON(map[string]any{"status": "success",
		"data": map[string]any{"resultType": "vector", "result": res}})
}

// appIDRe 从 PromQL 的 app_id="{app}" 中提取应用。
var appIDRe = regexp.MustCompile(`app_id="([^"]+)"`)

func bizAppOf(q string) string {
	if m := appIDRe.FindStringSubmatch(q); m != nil {
		return m[1]
	}
	return ""
}

// bizQueryPoint 业务接口监控三类查询的确定性取值：
// apollo-ehome-pro 故意越界（error_rate≈0.08、tp99≈1.6、请求量偏低），其他应用正常。
func bizQueryPoint(q string) (string, float64) {
	h := fnv32(q)
	app := bizAppOf(q)
	apollo := app == "apollo-ehome-pro"
	switch {
	case strings.Contains(q, "histogram_quantile"):
		if apollo {
			return "http_request_duration_seconds", 1.6
		}
		return "http_request_duration_seconds", 0.2 + float64(h%80)/1000
	case strings.Contains(q, "by (app_id)"), strings.Contains(q, "by(app_id)"):
		if apollo {
			return "http_requests_total", 0.08
		}
		return "http_requests_total", 0.01 + float64(h%30)/10000
	default:
		if apollo {
			return "http_requests_total", 1200
		}
		return "http_requests_total", 800 + float64(h%300)
	}
}

func promRange(args map[string]any, up map[string]float64) string {
	q, _ := args["query"].(string)
	now := time.Now().Unix()
	if hosts := upHostsIn(q, up); hosts != nil {
		res := []any{}
		for _, host := range hosts {
			val := "1"
			if up[host] < 1 {
				val = "0"
			}
			values := make([][]any, 0, 24)
			for i := 0; i < 24; i++ {
				values = append(values, []any{float64(now - int64((23-i)*300)), val})
			}
			res = append(res, map[string]any{
				"metric": map[string]any{"__name__": "up", "instance": host + ":9100", "job": "node_exporter"},
				"values": values,
			})
		}
		return mustJSON(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix",
			"result": res}})
	}
	if strings.Contains(q, "http_requests_total") || strings.Contains(q, "http_request_duration_seconds_bucket") {
		name, vals := bizRangeSeries(q)
		metric := map[string]any{"__name__": name}
		if app := bizAppOf(q); app != "" {
			metric["app_id"] = app
		}
		values := make([][]any, 0, len(vals))
		for i, v := range vals {
			values = append(values, []any{float64(now - int64((len(vals)-1-i)*300)), fmt.Sprintf("%.4f", v)})
		}
		return mustJSON(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix",
			"result": []any{map[string]any{"metric": metric, "values": values}}}})
	}
	host := "10.214.0.9"
	if m := hostRe.FindStringSubmatch(q); m != nil {
		host = m[1]
	}
	// 演示用：按 host 哈希一半目标呈上升趋势（触发劣化扫描），另一半平稳
	rising := fnv32(host)%2 == 0
	values := make([][]any, 0, 24)
	for i := 0; i < 24; i++ {
		v := 0.3 + 0.1*float64(i%5)
		if rising {
			v = 0.30 + 0.05*float64(i)
		}
		values = append(values, []any{float64(now - int64((23-i)*300)), fmt.Sprintf("%.2f", v)})
	}
	return mustJSON(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix",
		"result": []any{map[string]any{
			"metric": map[string]any{"__name__": "node_load1", "instance": host + ":9100", "job": "node_exporter"},
			"values": values}}}})
}

// bizRangeSeries 生成 24 点业务指标矩阵：apollo-ehome-pro 后 1/3 明显恶化，其他应用平稳。
func bizRangeSeries(q string) (string, []float64) {
	h := fnv32(q)
	apollo := bizAppOf(q) == "apollo-ehome-pro"
	vals := make([]float64, 24)
	switch {
	case strings.Contains(q, "histogram_quantile"):
		for i := range vals {
			vals[i] = 0.18 + float64(h%40)/1000
			if apollo {
				vals[i] = 0.2
				if i >= 16 {
					vals[i] = 1.7
				}
			}
		}
		return "http_request_duration_seconds", vals
	case strings.Contains(q, "by (app_id)"), strings.Contains(q, "by(app_id)"):
		for i := range vals {
			vals[i] = 0.008 + float64(h%30)/10000
			if apollo {
				vals[i] = 0.01
				if i >= 16 {
					vals[i] = 0.09
				}
			}
		}
		return "http_requests_total", vals
	default:
		for i := range vals {
			vals[i] = 800 + float64(h%300)
			if apollo {
				vals[i] = 1200
				if i >= 16 {
					vals[i] = 720
				}
			}
		}
		return "http_requests_total", vals
	}
}

// hostRe 从任意 PromQL 的 instance=~"host.*" 中提取 host。
var hostRe = regexp.MustCompile(`instance=~"([^".*]+)`)

// promListAlerts 按启动时间模拟告警生命周期：
//
//	0–90s   全 6 条 firing
//	90–150s TargetDown 两条消失（模拟恢复）
//	150s+   全 6 条 firing（死灰复燃）
func promListAlerts(startTime time.Time, dataDir string) string {
	// 优先读 mockdata/prometheus/list_alerts.json（由 scripts/seed_demo.py 按 5 条演示链路生成）
	if b, err := os.ReadFile(filepath.Join(dataDir, "list_alerts.json")); err == nil {
		var probe struct {
			Data struct {
				Alerts []any `json:"alerts"`
			} `json:"data"`
		}
		if json.Unmarshal(b, &probe) == nil && len(probe.Data.Alerts) > 0 {
			return string(b)
		}
	}
	elapsed := time.Since(startTime).Seconds()
	alerts := []any{}
	now := time.Now()
	// 基准模板：6 条告警（TargetDown×2 critical，其余 warning）
	type tpl struct {
		Labels      map[string]any `json:"labels"`
		Annotations map[string]any `json:"annotations"`
	}
	base := []tpl{
		{
			Labels:      map[string]any{"alertname": "TargetDown", "instance": "sdk3a4-detectmessage1.rocketmq.ehome.dlifeinner.cn:9100", "job": "node_exporter", "severity": "critical", "node": "云公司->贵州4c"},
			Annotations: map[string]any{"description": "目标已超过 5 分钟未上报指标", "summary": "主机失联"},
		},
		{
			Labels:      map[string]any{"alertname": "TargetDown", "instance": "172.17.228.214:9100", "job": "node_exporter", "severity": "critical", "node": "云公司->贵州4c"},
			Annotations: map[string]any{"description": "目标已超过 5 分钟未上报指标", "summary": "主机失联"},
		},
		{
			Labels:      map[string]any{"alertname": "HostHighCPU", "instance": "public-push3.rocketmq.ehome.dlifeinner.cn:9100", "job": "node_exporter", "severity": "warning", "node": "云公司->贵州4c"},
			Annotations: map[string]any{"description": "CPU 使用率 15 分钟均值 91.2%", "summary": "主机 CPU 过高"},
		},
		{
			Labels:      map[string]any{"alertname": "HighErrorRate", "application": "IMS通话服务", "appId": "apollo-ehome-pro", "severity": "warning"},
			Annotations: map[string]any{"description": "5 分钟错误率 2.4%，超过阈值 1%", "summary": "应用错误率过高"},
		},
		{
			Labels:      map[string]any{"alertname": "MySQLReplicationDelay", "instance": "ehome-pro.mysql.ehomeinner.cn:3306", "severity": "warning", "type": "mysql"},
			Annotations: map[string]any{"description": "主从延迟 38s，超过阈值 30s", "summary": "MySQL 主从延迟"},
		},
		{
			Labels:      map[string]any{"alertname": "RocketMQConsumerLag", "topic": "tyyy-ehome-event", "consumerGroup": "ehome-pro-consumer", "severity": "warning", "type": "rocketmq"},
			Annotations: map[string]any{"description": "消费积压 128000 条", "summary": "RocketMQ 消费积压"},
		},
	}
	// value 模拟值
	values := []string{"0", "0", "91.2", "2.4", "38", "128000"}

	for i, t := range base {
		// 90–150s 期间，两条 TargetDown（i=0,1）不加入 firing 列表
		if elapsed >= 90 && elapsed < 150 && t.Labels["alertname"] == "TargetDown" {
			continue
		}
		// activeAt 用现在往前 10~20 分钟的随机时间（确定性：按索引）
		activeMinAgo := 10 + (i*7)%10
		alerts = append(alerts, map[string]any{
			"labels":      t.Labels,
			"annotations": t.Annotations,
			"state":       "firing",
			"activeAt":    now.Add(-time.Duration(activeMinAgo) * time.Minute).Format(time.RFC3339),
			"value":       values[i],
		})
	}
	return mustJSON(map[string]any{
		"status": "success",
		"data":   map[string]any{"alerts": alerts},
	})
}

// promHealthy Prometheus 自证健康，供排障验真判断「数据源本身是否可信」。
func promHealthy(start time.Time) string {
	return mustJSON(map[string]any{
		"status": "success",
		"data": map[string]any{
			"health": "ok",
			"uptime": int(time.Since(start).Seconds()),
		},
	})
}

// promTargetsMetadata 按 up 表生成指标元数据，保证 up 指标一定被纳管。
func promTargetsMetadata(up map[string]float64) string {
	hosts := sortedHosts(up)
	out := make([]any, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, map[string]any{
			"metric": "up",
			"type":   "gauge",
			"help":   "1 表示目标可达，0 表示不可达",
			"target": h + ":9100",
			"job":    "node_exporter",
		})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"metric": "up", "type": "gauge", "help": "1 表示目标可达"})
	}
	return mustJSON(map[string]any{"status": "success", "data": out})
}

// promSeries 按 match[] 过滤 up 表，返回可用于区分「未纳管」与「采集异常」的序列清单。
func promSeries(args map[string]any, up map[string]float64) string {
	hosts := sortedHosts(up)
	if match := matchArg(args); match != "" && !strings.Contains(match, "up") {
		hosts = nil
	}
	out := make([]any, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, map[string]any{"__name__": "up", "instance": h + ":9100", "job": "node_exporter"})
	}
	return mustJSON(map[string]any{"status": "success", "data": out})
}

func sortedHosts(up map[string]float64) []string {
	out := make([]string, 0, len(up))
	for k := range up {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func matchArg(args map[string]any) string {
	switch v := args["match"].(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, it := range v {
			parts = append(parts, fmt.Sprint(it))
		}
		return strings.Join(parts, ",")
	}
	return ""
}

func promLabelValues(args map[string]any, up map[string]float64) (string, bool) {
	label := firstString(args, "label", "name")
	switch label {
	case "instance":
		hosts := make([]string, 0, len(up))
		for h := range up {
			hosts = append(hosts, h+":9100")
		}
		sort.Strings(hosts)
		return mustJSON(map[string]any{"status": "success", "data": hosts}), true
	case "job":
		return mustJSON(map[string]any{"status": "success", "data": []string{
			"node_exporter", "mysqld_exporter", "redis_exporter", "rocketmq_exporter", "process-exporter"}}), true
	}
	return "", false
}

// hostMetrics 生成 12 项主机指标（实时/趋势），值由主机名哈希 + 分钟桶确定性导出。
func hostMetrics(args map[string]any, trend bool, overrides map[string]map[string]any) string {
	host := firstString(args, "node", "ip", "host", "target", "instance", "node_code")
	if host == "" {
		host = "10.214.0.9"
	}
	ov := overrides[host]
	h := fnv32(host)
	bucket := time.Now().Unix() / 60
	wob := func(k string, lo, hi float64) float64 {
		span := hi - lo
		v := lo + span*float64(fnv32(host+":"+k+strconv.FormatInt(bucket, 10))%1000)/1000
		return math.Round(v*100) / 100
	}
	cores := []float64{4, 8, 16, 32}[h%4]
	if trend {
		now := time.Now().Unix()
		pts := func(k string, lo, hi float64) []map[string]any {
			out := make([]map[string]any, 0, 24)
			for i := 0; i < 24; i++ {
				out = append(out, map[string]any{
					"t": now - int64((23-i)*3600),
					"v": lo + (hi-lo)*float64(fnv32(host+":"+k+strconv.Itoa(i))%1000)/1000,
				})
			}
			return out
		}
		cpuLo, cpuHi := 18.0, 92.0
		if ov != nil {
			if v := numArg(ov, "cpuUsage", 0); v > 0 {
				cpuLo, cpuHi = v-4, v+1
			}
		}
		return mustJSON(map[string]any{
			"node": host, "from": now - 86400, "to": now, "step": 3600,
			"series": map[string]any{
				"cpuUsage": pts("cpu", cpuLo, cpuHi), "memUsage": pts("mem", 35, 85), "load": pts("load", 0.4, 6.5),
			}})
	}
	metrics := map[string]any{
		"cpuCores": cores, "cpuUsage": wob("cpu", 5, 95),
		"memTotal": cores * 4, "memUsage": wob("mem", 30, 88),
		"diskTotal": []float64{300, 600, 900}[h%3], "diskUsage": wob("disk", 20, 90),
		"netConnections": int(wob("net", 200, 8000)), "netIn": wob("netin", 1, 400),
		"netOut": wob("netout", 1, 320), "diskIO": int(wob("io", 20, 2000)),
		"load": wob("load", 0.2, 8), "tcpOverflow": int(wob("ovf", 0, 3)), "tcpTotal": int(wob("tcp", 500, 12000)),
	}
	// 场景覆写优先（scripts/seed_demo.py 生成的 _host_metrics.json）
	for k, v := range ov {
		metrics[k] = v
	}
	return mustJSON(map[string]any{
		"node": host, "ts": time.Now().Unix(),
		"metrics": metrics})
}

// ---------- pinpoint 动态处理器 ----------

func (s *serverCtx) pinServerMap(args map[string]any) string {
	name := firstString(args, "application_name", "application", "appName", "name")
	if name == "" {
		if len(s.appNames) == 0 {
			return mustJSON(map[string]any{"applicationName": "", "nodeData": []any{}, "linkData": []any{}})
		}
		name = s.appNames[int(fnv32("default"))%len(s.appNames)]
	}
	key := name
	if k, ok := s.appAlias[strings.ToLower(name)]; ok {
		key = k
	} else if k, ok := s.appAlias[name]; ok {
		key = k
	}
	info := s.apps[key]
	if info == nil {
		return mustJSON(map[string]any{"applicationName": name,
			"note": "mock 应用清单中未找到该应用（服务地图无数据）", "nodeData": []any{}, "linkData": []any{}})
	}
	appId, _ := info["appId"].(string)
	mw, _ := info["mw"].([]any)
	callers, callees := toSlice(info["callers"]), toSlice(info["callees"])
	nodes := []any{map[string]any{"id": key, "applicationName": key, "applicationId": appId,
		"serviceType": "DEFAULT", "servCount": len(mw) + len(callees), "isSelf": true}}
	links := []any{}
	for _, c := range callers {
		nodes = append(nodes, map[string]any{"id": c, "applicationName": c,
			"applicationId": s.appIDOf(c), "serviceType": "DEFAULT", "isSelf": false})
		links = append(links, map[string]any{"from": c, "to": key,
			"count": 800 + int(fnv32(c+"->"+key)%9000), "errorCount": int(fnv32(key+"<-"+c) % 30),
			"responseTime": 4 + int(fnv32("rt:"+c+key)%160)})
	}
	for _, c := range callees {
		nodes = append(nodes, map[string]any{"id": c, "applicationName": c,
			"applicationId": s.appIDOf(c), "serviceType": "DEFAULT", "isSelf": false})
		links = append(links, map[string]any{"from": key, "to": c,
			"count": 800 + int(fnv32(key+"->"+c)%9000), "errorCount": int(fnv32(c+"<-"+key) % 30),
			"responseTime": 4 + int(fnv32("rt:"+key+c)%160)})
	}
	return mustJSON(map[string]any{"applicationName": key, "applicationId": appId,
		"nodeData": nodes, "linkData": links, "middleware": mw})
}

func (s *serverCtx) pinHeatmap(args map[string]any) string {
	app := firstString(args, "application", "application_name", "applicationName")
	if app == "" {
		app = "unknown"
	}
	now := float64(time.Now().UnixMilli())
	x1 := numArg(args, "x1", now-3600_000)
	x2 := numArg(args, "x2", now)
	n := 60
	points := make([][]any, 0, n)
	for i := 0; i < n; i++ {
		ts := x1 + (x2-x1)*float64(i)/float64(n-1)
		base := int(fnv32(app+strconv.Itoa(i/10)) % 180)
		spike := 0
		if i%17 == 0 {
			spike = 240
		}
		points = append(points, []any{ts, base + spike + i%9})
	}
	return mustJSON(map[string]any{"application": app, "from": x1, "to": x2,
		"interval": 60000, "points": points,
		"traceIds": []string{fmt.Sprintf("mock-trace-%08x", fnv32(app))}})
}

func (s *serverCtx) pinTrace(args map[string]any) string {
	tid := firstString(args, "trace_id", "traceId", "traceid")
	if tid == "" {
		tid = "mock-trace-0001"
	}
	h := fnv32(tid)
	app := "unknown"
	if len(s.appNames) > 0 {
		app = s.appNames[int(h)%len(s.appNames)]
	}
	var callees []string
	if info := s.apps[app]; info != nil {
		callees = toSlice(info["callees"])
	}
	var mw []any
	if info := s.apps[app]; info != nil {
		mw, _ = info["mw"].([]any)
	}
	elapsed := 80 + int(h%900)
	start := float64(time.Now().Add(-time.Duration(int(h)%900) * time.Second).UnixMilli())
	spans := []any{map[string]any{"spanId": 0, "parentId": -1, "application": app,
		"serviceType": 1200, "rpc": "GET /api/v1/entry", "destination": "tomcat-" + strconv.Itoa(int(h)%3+1),
		"agentId": fmt.Sprintf("agent-%d", int(h)%4), "begin": 0, "end": elapsed,
		"executionMilliseconds": elapsed, "exception": 0}}
	spanID := 1
	for i, c := range callees {
		if i >= 3 {
			break
		}
		b := 10 + i*18
		exec := 40 + int(fnv32(c+tid)%80)
		if i == 0 && h%3 == 0 {
			// 每 3 个事务中有 1 个把首个下游 span 放大到 60%，用于演示「单 span 瓶颈」判据。
			exec = elapsed * 6 / 10
		}
		spans = append(spans, map[string]any{"spanId": spanID, "parentId": 0, "application": c,
			"serviceType": 1200, "rpc": "POST /api/v1/call", "destination": "tomcat-1",
			"agentId": "agent-1", "begin": b, "end": b + exec,
			"executionMilliseconds": exec, "exception": boolToInt(h%4 == 0 && i == 1)})
		spanID++
	}
	for i, m := range mw {
		if i >= 2 {
			break
		}
		mm, _ := m.(map[string]any)
		addr, _ := mm["addr"].(string)
		typ, _ := mm["type"].(string)
		spans = append(spans, map[string]any{"spanId": spanID, "parentId": 0, "application": strings.ToUpper(typ) + "-cluster",
			"serviceType": 2100, "rpc": typ + " query", "destination": addr, "agentId": "agent-1",
			"begin": 20 + i*30, "end": 30 + i*30 + int(fnv32(tid+addr)%50),
			"executionMilliseconds": 3 + int(fnv32(addr+tid)%40), "exception": 0})
		spanID++
	}
	return mustJSON(map[string]any{"traceId": tid, "startTime": start, "elapsed": elapsed,
		"callStackStart": start, "callStack": spans})
}

func (s *serverCtx) archMetric(args map[string]any) string {
	rule := firstString(args, "rule", "rule_name")
	if rule == "" {
		rule = "dep_concentration"
	}
	node := firstString(args, "node", "node_code")
	vio := filterVio(s.archVio, rule)
	return mustJSON(map[string]any{"rule": rule, "node": node,
		"ruleVersion": s.archVer, "threshold": s.archThresh,
		"metrics": vio})
}

func (s *serverCtx) archCheck(args map[string]any) string {
	rule := firstString(args, "rule", "rule_name")
	if rule == "" {
		rule = "dep_concentration"
	}
	node := firstString(args, "node", "node_code")
	vio := filterVio(s.archVio, rule)
	if len(vio) > 5 {
		vio = vio[:5]
	}
	return mustJSON(map[string]any{"rule": rule, "node": node, "env": firstString(args, "env"),
		"ruleVersion": s.archVer, "passed": len(vio) == 0, "violations": vio})
}

func filterVio(vio []map[string]any, rule string) []map[string]any {
	out := []map[string]any{}
	for _, v := range vio {
		if r, _ := v["rule"].(string); r == rule {
			out = append(out, v)
		}
	}
	return out
}

func (s *serverCtx) appIDOf(name string) string {
	if info := s.apps[name]; info != nil {
		id, _ := info["appId"].(string)
		return id
	}
	return ""
}

// ---------- 小工具 ----------

func firstString(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := args[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func numArg(args map[string]any, key string, def float64) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func toSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fnv32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func loadJSONFile(path string, target any) {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[mockmcp] 跳过 %s: %v", path, err)
		return
	}
	if err := json.Unmarshal(b, target); err != nil {
		log.Printf("[mockmcp] 解析失败 %s: %v", path, err)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}
