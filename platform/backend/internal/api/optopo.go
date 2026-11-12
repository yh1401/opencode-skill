package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/topo"
)

// ---------- 运维关系拓扑（tprel 能力面）----------
// file 模式：平台本地从快照计算；http 模式：透传 topo 服务原生响应。

func (s *Server) opProxy() (topo.OpProxy, bool) {
	p, ok := s.topo.(topo.OpProxy)
	return p, ok
}

// opDateEnv 统一处理快照日期（syncDate/date 均可，缺省最新）与环境（空 = 不限）。
func (s *Server) opDateEnv(c *gin.Context) (string, string) {
	date := c.Query("syncDate")
	if date == "" {
		date = c.Query("date")
	}
	if date == "" {
		if ds := s.topo.Dates(); len(ds) > 0 {
			date = ds[len(ds)-1]
		}
	}
	return date, c.Query("env")
}

func (s *Server) opFilters(c *gin.Context) topo.OpFilters {
	return topo.OpFilters{
		Env:            c.Query("env"),
		MiddlewareType: c.Query("middlewareType"),
		DepProject:     c.Query("depProject"),
		EngProject:     c.Query("engProject"),
		Node:           c.Query("node"),
		PpApplication:  c.Query("ppApplication"),
		AppID:          c.Query("apolloAppId"),
		AppName:        c.Query("apolloAppName"),
		HostName:       c.Query("apolloHostName"),
		AppDept:        c.Query("appDept"),
		AppOwner:       c.Query("appOwner"),
		ConnectAddr:    c.Query("connectAddr"),
	}
}

// opExpand 部署点展开开关：缺省展开（tprel 口径），显式 0/false 折叠。
func (s *Server) opExpand(c *gin.Context) bool {
	v := strings.TrimSpace(c.Query("expandDepNode"))
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// unwrapTprel 解 tprel 标准信封 {code, data, msg}。
func unwrapTprel(body []byte) (json.RawMessage, error) {
	var resp struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
		Msg  string          `json:"msg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 && resp.Code != 200 {
		return nil, fmt.Errorf("topo 服务返回 code=%d: %s", resp.Code, resp.Msg)
	}
	return resp.Data, nil
}

// opForward http 模式透传：GET topo 服务并解包信封。
func (s *Server) opForward(c *gin.Context, path string, q url.Values) bool {
	p, ok := s.opProxy()
	if !ok {
		return false
	}
	body, err := p.OpGet(path, q)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return true
	}
	data, err := unwrapTprel(body)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return true
	}
	c.Data(http.StatusOK, "application/json", data)
	return true
}

func (s *Server) opGraph(c *gin.Context) {
	if s.opForward(c, topo.OpBasePath+"/graph", opQuery(c)) {
		return
	}
	date, env := s.opDateEnv(c)
	rows, err := s.topo.Rows(date, "")
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	rows = topo.OpFilterRows(rows, s.opFilters(c))
	g := topo.OpGraphFromRows(rows, s.opExpand(c), date, env, "file")
	s.enrichOpGraphRefs(g)
	c.JSON(http.StatusOK, g)
}

// enrichOpGraphRefs 给图节点附带排障会话 / 工单状态摘要，供前端按状态着色。
// 仅 file 模式生效（http 模式透传 topo 服务响应，不做二次组装）。
func (s *Server) enrichOpGraphRefs(g *domain.OpGraph) {
	if g == nil || len(g.Nodes) == 0 {
		return
	}
	sessions := s.diag.List(0)
	tickets := s.tickets.List()

	// 中间件地址归一：去掉端口，避免排障 / 工单用 host 不带端口而拓扑节点带端口导致匹配失败。
	normAddr := func(a string) string {
		if i := strings.Index(a, ":"); i > 0 {
			return a[:i]
		}
		return a
	}

	// 应用索引：appId -> *OpGraphNode
	// 中间件索引：mwType|addr(host) -> *OpGraphNode
	appIdx := make(map[string][]*domain.OpGraphNode)
	mwIdx := make(map[string][]*domain.OpGraphNode)
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Category == "application" && n.ApolloAppID != "" {
			appIdx[n.ApolloAppID] = append(appIdx[n.ApolloAppID], n)
		}
		if n.Category == "middleware" && n.MwType != "" && n.Addr != "" {
			key := n.MwType + "|" + normAddr(n.Addr)
			mwIdx[key] = append(mwIdx[key], n)
		}
	}

	for _, sess := range sessions {
		ref := domain.OpNodeDiagRef{SessionID: sess.ID, Status: sess.Status, Confidence: sess.Confidence}
		if sess.AppID != "" {
			for _, n := range appIdx[sess.AppID] {
				n.DiagRefs = append(n.DiagRefs, ref)
			}
		}
		if sess.MiddlewareType != "" && sess.Addr != "" {
			for _, n := range mwIdx[sess.MiddlewareType+"|"+normAddr(sess.Addr)] {
				n.DiagRefs = append(n.DiagRefs, ref)
			}
		}
	}

	for _, tk := range tickets {
		ref := domain.OpNodeTicketRef{ID: tk.ID, Status: string(tk.Status)}
		if tk.AppID != "" {
			for _, n := range appIdx[tk.AppID] {
				n.TicketRefs = append(n.TicketRefs, ref)
			}
		}
		if tk.MwType != "" && tk.Addr != "" {
			for _, n := range mwIdx[tk.MwType+"|"+normAddr(tk.Addr)] {
				n.TicketRefs = append(n.TicketRefs, ref)
			}
		}
	}
}

func (s *Server) opDetail(c *gin.Context) {
	if s.opForward(c, topo.OpBasePath+"/detail", opQuery(c)) {
		return
	}
	date, env := s.opDateEnv(c)
	rows, err := s.topo.Rows(date, "")
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	rows = topo.OpFilterRows(rows, s.opFilters(c))
	d, err := topo.OpDetailFromRows(rows, c.Query("id"), s.opExpand(c), date, env)
	if err != nil {
		fail(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (s *Server) opList(c *gin.Context) {
	if s.opForward(c, topo.OpBasePath+"/list", opQuery(c)) {
		return
	}
	date, env := s.opDateEnv(c)
	rows, err := s.topo.Rows(date, "")
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	rows = topo.OpFilterRows(rows, s.opFilters(c))
	rows = topo.OpListRows(rows, s.opExpand(c))

	page := atoiDefault(c.Query("page"), 1)
	pageSize := atoiDefault(c.Query("pageSize"), 20)
	total := len(rows)
	if pageSize > 0 {
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		rows = rows[start:end]
	}
	c.JSON(http.StatusOK, gin.H{
		"page": page, "pageSize": pageSize, "pageTotal": total,
		"list": rows, "date": date, "env": env,
	})
}

func (s *Server) opMwTypes(c *gin.Context) {
	if s.opForward(c, topo.OpBasePath+"/middlewareTypes", opQuery(c)) {
		return
	}
	date, env := s.opDateEnv(c)
	rows, err := s.topo.Rows(date, "")
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	rows = topo.OpFilterRows(rows, s.opFilters(c))
	c.JSON(http.StatusOK, gin.H{"date": date, "env": env, "types": topo.OpMwTypeList(rows)})
}

func (s *Server) opMwTargets(c *gin.Context) {
	// 排障发起表单的中间件候选（去重 mwType+addr，附带依赖应用数与示例）。
	// file 模式：本地计算；http 模式：不代理（topo 服务无此接口），仍本地计算。
	date, env := s.opDateEnv(c)
	rows, err := s.topo.Rows(date, env)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	rows = topo.OpFilterRows(rows, s.opFilters(c))
	c.JSON(http.StatusOK, gin.H{"date": date, "env": env, "targets": topo.OpMwTargets(rows)})
}

func (s *Server) opSync(c *gin.Context) {
	syncer, ok := s.topo.(topo.OpSyncer)
	if !ok {
		fail(c, http.StatusNotImplemented, errors.New("当前数据源不支持同步"))
		return
	}
	started, msg, state, err := syncer.OpTriggerSync("平台菜单手动同步")
	if errors.Is(err, topo.ErrOpSyncNotSupported) {
		c.JSON(http.StatusOK, gin.H{"started": false, "supported": false, "message": err.Error(), "state": state})
		return
	}
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"started": started, "supported": true, "message": msg, "state": state})
}

func (s *Server) opSyncStatus(c *gin.Context) {
	syncer, ok := s.topo.(topo.OpSyncer)
	if !ok {
		c.JSON(http.StatusOK, domain.OpSyncState{})
		return
	}
	c.JSON(http.StatusOK, syncer.OpSyncStatus())
}

func (s *Server) opSyncLogs(c *gin.Context) {
	syncer, ok := s.topo.(topo.OpSyncer)
	if !ok {
		fail(c, http.StatusNotImplemented, errors.New("当前数据源不支持同步日志"))
		return
	}
	dl := c.Query("download")
	ct, body, err := syncer.OpSyncLogs(c.Query("date"), dl)
	if errors.Is(err, topo.ErrOpSyncNotSupported) {
		c.JSON(http.StatusOK, gin.H{"supported": false, "message": err.Error(), "files": []any{}})
		return
	}
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	if dl == "1" || strings.EqualFold(dl, "true") {
		c.Data(http.StatusOK, ct, body)
		return
	}
	if s.cfg.TopoMode == "http" {
		data, uerr := unwrapTprel(body)
		if uerr != nil {
			fail(c, http.StatusBadGateway, uerr)
			return
		}
		c.Data(http.StatusOK, "application/json", data)
		return
	}
	c.Data(http.StatusOK, ct, body)
}

// opQuery 透传 topo 服务的原始查询参数（保持与 tprel 参数名一致）。
func opQuery(c *gin.Context) url.Values {
	q := url.Values{}
	for _, k := range []string{
		"env", "middlewareType", "depProject", "engProject", "node", "ppApplication",
		"apolloAppId", "apolloAppName", "apolloHostName", "appDept", "appOwner", "connectAddr",
		"syncDate", "expandDepNode", "page", "pageSize", "source",
	} {
		if v := c.Query(k); v != "" {
			q.Set(k, v)
		}
	}
	return q
}
