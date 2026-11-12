package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/store"
)

// Gateway 统一 MCP 网关：pinpoint / prometheus / cmdb / es 四个 MCP 服务不直连 Agent，全部经由网关做
// 白名单/黑名单、超时、探针与调用审计（落 tool_call_log）。
type Gateway struct {
	enabled bool
	timeout time.Duration
	clients map[string]*Client
	order   []string
	deny    map[string]struct{}
	allow   map[string]struct{}
	store   store.Store
}

// 工具调用审计日志。
type CallLog struct {
	Server    string         `json:"server"`
	Tool      string         `json:"tool"`
	Args      map[string]any `json:"args,omitempty"`
	OK        bool           `json:"ok"`
	Error     string         `json:"error,omitempty"`
	LatencyMs int64          `json:"latencyMs"`
	At        time.Time      `json:"at"`
}

func NewGateway(cfg config.Config, st store.Store) *Gateway {
	g := &Gateway{
		enabled: cfg.MCPEnabled,
		timeout: cfg.MCPTimeout,
		clients: map[string]*Client{},
		deny:    map[string]struct{}{},
		allow:   map[string]struct{}{},
		store:   st,
	}
	for _, t := range cfg.MCPDenyTools {
		g.deny[strings.ToLower(t)] = struct{}{}
	}
	for _, t := range cfg.MCPAllowTools {
		g.allow[strings.ToLower(t)] = struct{}{}
	}
	g.add("pinpoint", cfg.PinpointURL)
	g.add("prometheus", cfg.PrometheusMCPURL)
	g.add("cmdb", cfg.CMDBMCPURL)
	// ES 日志服务按需接入：ES_MCP_URL 为空时不注册，巡检 T10 会返回 NO_DATA。
	g.add("es", cfg.ESMCPURL)
	return g
}

func (g *Gateway) add(name, url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	g.clients[name] = NewClient(name, url, g.timeout)
	g.order = append(g.order, name)
}

func (g *Gateway) Enabled() bool { return g.enabled }

func (g *Gateway) Servers() []string { return append([]string{}, g.order...) }

// Allowed 判断工具是否放行：黑名单永远优先；白名单非空时必须是白名单命中。
func (g *Gateway) Allowed(server, tool string) bool {
	tool = strings.ToLower(tool)
	if _, ok := g.deny[tool]; ok {
		return false
	}
	if len(g.allow) == 0 {
		return true
	}
	candidates := []string{tool, strings.ToLower(server) + "/" + tool, strings.ToLower(server) + "/*"}
	for _, c := range candidates {
		if _, ok := g.allow[c]; ok {
			return true
		}
	}
	return false
}

// Probe 探测全部 MCP 服务可用性。
func (g *Gateway) Probe() []domain.MCPServerStatus {
	out := make([]domain.MCPServerStatus, 0, len(g.order))
	for _, name := range g.order {
		c := g.clients[name]
		st := domain.MCPServerStatus{Name: name, URL: c.URL, Enabled: g.enabled}
		if !g.enabled {
			st.Error = "上游服务未启用（MCP_ENABLED=false）"
			out = append(out, st)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		start := time.Now()
		tools, err := c.ListTools(ctx)
		st.LatencyMs = time.Since(start).Milliseconds()
		cancel()
		if err != nil {
			st.Error = err.Error()
		} else {
			st.Reachable = true
			st.Tools = len(tools)
		}
		out = append(out, st)
	}
	return out
}

// Tools 汇总两端工具清单并标注是否放行。
func (g *Gateway) Tools(ctx context.Context) []domain.MCPTool {
	out := []domain.MCPTool{}
	if !g.enabled {
		return out
	}
	for _, name := range g.order {
		c := g.clients[name]
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		tools, err := c.ListTools(cctx)
		cancel()
		if err != nil {
			continue
		}
		for _, t := range tools {
			out = append(out, domain.MCPTool{
				Server:      name,
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
				Allowed:     g.Allowed(name, t.Name),
			})
		}
	}
	return out
}

// Call 调用指定服务上的工具，并落审计。
func (g *Gateway) Call(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, error) {
	return g.callInner(ctx, server, tool, args, true)
}

// CallSilent 与 Call 同逻辑但不落审计，用于高频轮询（如告警采集）。
func (g *Gateway) CallSilent(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, error) {
	return g.callInner(ctx, server, tool, args, false)
}

func (g *Gateway) callInner(ctx context.Context, server, tool string, args map[string]any, audit bool) (json.RawMessage, error) {
	if !g.enabled {
		return nil, fmt.Errorf("上游服务未启用")
	}
	c, ok := g.clients[server]
	if !ok {
		return nil, fmt.Errorf("未知上游服务: %s", server)
	}
	if !g.Allowed(server, tool) {
		return nil, fmt.Errorf("工具 %s/%s 未在放行名单内", server, tool)
	}
	cctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	start := time.Now()
	res, err := c.CallTool(cctx, tool, args)
	if audit {
		g.audit(CallLog{
			Server: server, Tool: tool, Args: args,
			OK: err == nil, Error: errString(err),
			LatencyMs: time.Since(start).Milliseconds(), At: time.Now(),
		})
	}
	return res, err
}

func (g *Gateway) audit(l CallLog) {
	if g.store == nil {
		return
	}
	id := fmt.Sprintf("%s-%d", l.Server, l.At.UnixNano())
	_ = g.store.Put("tool_call_log", id, l)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
