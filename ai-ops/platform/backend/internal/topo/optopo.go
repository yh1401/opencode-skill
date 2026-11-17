package topo

import (
	"fmt"
	"sort"
	"strings"

	"aiops-platform/backend/internal/domain"
)

// 运维关系拓扑能力面（tprel 契约的 file 快照本地实现）。
// 口径对齐 topo 服务 /operationTopology：
//   - 行级条件：env / middlewareType / apolloHostName(模糊) / connectAddr(模糊)
//   - appId 属性条件（depProject 模糊；engProject / node / ppApplication 精确；
//     apolloAppId 精确；apolloAppName / appDept / appOwner）多值逗号 OR、维度间 AND，
//     命中即保留该 appId 的全部关系行（不因属性拆行/拆节点）
//   - expandDepNode 缺省展开：应用节点粒度 = appId+部署项目+节点（一个部署点一个节点）；
//     折叠 = appId 一个节点，属性多值聚合为列表

// OpFilters 运维关系拓扑查询条件（/graph /detail /list 同口径）。
type OpFilters struct {
	Env            string
	MiddlewareType string
	DepProject     string
	EngProject     string
	Node           string
	PpApplication  string
	AppID          string
	AppName        string
	HostName       string
	AppDept        string
	AppOwner       string
	ConnectAddr    string
}

func splitMulti(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchList 多值条件匹配：conds 逗号分隔（OR），vals 中任一命中即 true；fuzzy 为子串匹配。
func matchList(conds string, vals []string, fuzzy bool) bool {
	cs := splitMulti(conds)
	if len(cs) == 0 {
		return true
	}
	for _, c := range cs {
		c = strings.ToLower(c)
		for _, v := range vals {
			v = strings.ToLower(strings.TrimSpace(v))
			if fuzzy {
				if v != "" && strings.Contains(v, c) {
					return true
				}
			} else if v == c {
				return true
			}
		}
	}
	return false
}

// appAttr 单行 appId 属性视图（多值列表优先，缺省回退单值列）。
type appAttr struct {
	deps, nodes, engs, pps []string
}

func attrOf(r domain.TopoRow) appAttr {
	a := appAttr{
		deps: r.DepProjectList, nodes: r.NodeList,
		engs: r.EngProjectList, pps: r.PpApplicationList,
	}
	if len(a.deps) == 0 && r.DepProject != "" {
		a.deps = []string{r.DepProject}
	}
	if len(a.nodes) == 0 && r.Node != "" {
		a.nodes = []string{r.Node}
	}
	if len(a.engs) == 0 && r.EngProject != "" {
		a.engs = []string{r.EngProject}
	}
	if len(a.pps) == 0 && r.PPApplication != "" {
		a.pps = []string{r.PPApplication}
	}
	return a
}

func (a appAttr) hit(r domain.TopoRow, f OpFilters) bool {
	if !matchList(f.AppID, []string{r.ApolloAppID}, false) {
		return false
	}
	if !matchList(f.AppName, []string{r.ApolloAppName}, true) {
		return false
	}
	if !matchList(f.AppDept, []string{r.AppDept}, false) {
		return false
	}
	if !matchList(f.AppOwner, []string{r.AppOwner}, false) {
		return false
	}
	if !matchList(f.DepProject, a.deps, true) {
		return false
	}
	if !matchList(f.EngProject, a.engs, false) {
		return false
	}
	if !matchList(f.Node, a.nodes, false) {
		return false
	}
	return matchList(f.PpApplication, a.pps, false)
}

func (f OpFilters) appLevel() bool {
	return f.DepProject != "" || f.EngProject != "" || f.Node != "" || f.PpApplication != "" ||
		f.AppID != "" || f.AppName != "" || f.AppDept != "" || f.AppOwner != ""
}

// OpFilterRows 按运维关系拓扑口径过滤行集。
func OpFilterRows(rows []domain.TopoRow, f OpFilters) []domain.TopoRow {
	pass1 := make([]domain.TopoRow, 0, len(rows))
	for _, r := range rows {
		if !matchList(f.Env, []string{r.Env}, false) {
			continue
		}
		if !matchList(f.MiddlewareType, []string{r.MiddlewareType}, false) {
			continue
		}
		if !matchList(f.HostName, []string{r.ApolloHostName}, true) {
			continue
		}
		if !matchList(f.ConnectAddr, []string{r.ConnectAddr}, true) {
			continue
		}
		pass1 = append(pass1, r)
	}
	if !f.appLevel() {
		return pass1
	}
	hit := map[string]bool{}
	for _, r := range pass1 {
		if attrOf(r).hit(r, f) {
			hit[r.ApolloAppID] = true
		}
	}
	out := make([]domain.TopoRow, 0, len(pass1))
	for _, r := range pass1 {
		if hit[r.ApolloAppID] {
			out = append(out, r)
		}
	}
	return out
}

// opPairs 部署点 (dep, node) 组合（笛卡尔，空值以 "" 占位）。
func opPairs(a appAttr) [][2]string {
	deps, nodes := a.deps, a.nodes
	if len(deps) == 0 {
		deps = []string{""}
	}
	if len(nodes) == 0 {
		nodes = []string{""}
	}
	out := make([][2]string, 0, len(deps)*len(nodes))
	for _, d := range deps {
		for _, n := range nodes {
			out = append(out, [2]string{d, n})
		}
	}
	return out
}

func firstOf(vals []string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// opHosts 聚合行集的 Apollo 实例归属（appCount 按去重 appId 计）。
func opHosts(rows []domain.TopoRow) []domain.OpGraphHost {
	hosts := map[int64]*domain.OpGraphHost{}
	apps := map[int64]map[string]bool{}
	for _, r := range rows {
		h, ok := hosts[r.ApolloHostID]
		if !ok {
			h = &domain.OpGraphHost{ID: r.ApolloHostID, Host: r.ApolloHostAddr}
			hosts[r.ApolloHostID] = h
			apps[r.ApolloHostID] = map[string]bool{}
		}
		if h.Name == "" {
			h.Name = r.ApolloHostName
		}
		if h.Host == "" {
			h.Host = r.ApolloHostAddr
		}
		if r.ApolloAppID != "" {
			apps[r.ApolloHostID][r.ApolloAppID] = true
		}
	}
	out := make([]domain.OpGraphHost, 0, len(hosts))
	for id, h := range hosts {
		h.AppCount = len(apps[id])
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// OpGraphFromRows 由过滤后的行集构建拓扑图。
func OpGraphFromRows(rows []domain.TopoRow, expand bool, date, env, dataSource string) *domain.OpGraph {
	g := &domain.OpGraph{
		Nodes: []domain.OpGraphNode{}, Edges: []domain.OpGraphEdge{},
		SyncDate: date, Env: env, DataSource: dataSource,
		ApolloHosts: []domain.OpGraphHost{},
	}
	appNodes := map[string]*domain.OpGraphNode{}
	hosts := map[int64]*domain.OpGraphHost{}
	hostApps := map[int64]map[string]bool{}
	mwNodes := map[string]*domain.OpGraphNode{}
	mwApps := map[string]map[string]bool{}
	edgeSeen := map[string]bool{}

	touchHost := func(r domain.TopoRow) {
		h, ok := hosts[r.ApolloHostID]
		if !ok {
			h = &domain.OpGraphHost{ID: r.ApolloHostID, Host: r.ApolloHostAddr}
			hosts[r.ApolloHostID] = h
			hostApps[r.ApolloHostID] = map[string]bool{}
		}
		if h.Name == "" {
			h.Name = r.ApolloHostName
		}
		if h.Host == "" {
			h.Host = r.ApolloHostAddr
		}
		if r.ApolloAppID != "" {
			hostApps[r.ApolloHostID][r.ApolloAppID] = true
		}
	}

	linkMW := func(r domain.TopoRow, appNodeID string) {
		addr := strings.TrimSpace(r.ConnectAddr)
		if r.MiddlewareType == "" || addr == "" {
			return
		}
		mwID := "mw:" + r.MiddlewareType + ":" + addr
		m, ok := mwNodes[mwID]
		if !ok {
			m = &domain.OpGraphNode{
				ID: mwID, Category: "middleware",
				Name: r.MiddlewareType + ":" + addr, Label: addr,
				MwType: r.MiddlewareType, Addr: addr,
			}
			mwNodes[mwID] = m
			mwApps[mwID] = map[string]bool{}
		}
		if m.MwProject == "" {
			m.MwProject = r.MiddlewareProject
		}
		mwApps[mwID][appNodeID] = true
		ek := appNodeID + "->" + mwID
		if !edgeSeen[ek] {
			edgeSeen[ek] = true
			g.Edges = append(g.Edges, domain.OpGraphEdge{Source: appNodeID, Target: mwID, Kind: "depend"})
		}
	}

	for _, r := range rows {
		touchHost(r)
		a := attrOf(r)
		if expand {
			for _, p := range opPairs(a) {
				id := "app:" + r.ApolloAppID + "|" + p[0] + "|" + p[1]
				n, ok := appNodes[id]
				if !ok {
					n = &domain.OpGraphNode{ID: id, Category: "application", ApolloHosts: []domain.OpGraphHost{}}
					appNodes[id] = n
				}
				n.Name = firstNonEmpty(r.ApolloAppName, r.ApolloAppID)
				n.Label = r.ApolloAppID
				n.ApolloAppID, n.ApolloAppName = r.ApolloAppID, r.ApolloAppName
				n.AppDept, n.AppOwner = r.AppDept, r.AppOwner
				n.EngProject, n.PpApp = firstOf(a.engs), firstOf(a.pps)
				n.DepProject, n.DeployProjectName, n.Node = p[0], p[0], p[1]
				linkMW(r, id)
			}
			continue
		}
		id := "app:" + r.ApolloAppID
		n, ok := appNodes[id]
		if !ok {
			n = &domain.OpGraphNode{
				ID: id, Category: "application",
				Name: firstNonEmpty(r.ApolloAppName, r.ApolloAppID), Label: r.ApolloAppID,
				ApolloAppID: r.ApolloAppID, ApolloAppName: r.ApolloAppName,
				AppDept: r.AppDept, AppOwner: r.AppOwner,
				ApolloHosts: []domain.OpGraphHost{},
			}
			appNodes[id] = n
		}
		if n.AppDept == "" {
			n.AppDept = r.AppDept
		}
		if n.AppOwner == "" {
			n.AppOwner = r.AppOwner
		}
		if n.EngProject == "" {
			n.EngProject = firstOf(a.engs)
		}
		if n.PpApp == "" {
			n.PpApp = firstOf(a.pps)
		}
		n.EngProjects = mergeUnique(n.EngProjects, a.engs)
		n.DepProjects = mergeUnique(n.DepProjects, a.deps)
		n.PpApps = mergeUnique(n.PpApps, a.pps)
		n.Nodes = mergeUnique(n.Nodes, a.nodes)
		n.DepProject, n.DeployProjectName, n.Node = firstOf(n.DepProjects), firstOf(n.DepProjects), firstOf(n.Nodes)
		linkMW(r, id)
	}

	for id, m := range mwNodes {
		ids := make([]string, 0, len(mwApps[id]))
		for k := range mwApps[id] {
			ids = append(ids, k)
		}
		sort.Strings(ids)
		m.AppIDs = ids
		m.DepCount = len(ids)
	}

	g.ApolloHosts = opHosts(rows)
	for _, n := range appNodes {
		g.Nodes = append(g.Nodes, *n)
	}
	for _, n := range mwNodes {
		g.Nodes = append(g.Nodes, *n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool {
		if g.Nodes[i].Category != g.Nodes[j].Category {
			return g.Nodes[i].Category < g.Nodes[j].Category
		}
		return g.Nodes[i].ID < g.Nodes[j].ID
	})
	g.Counts = domain.OpGraphCounts{
		ApolloHosts: len(g.ApolloHosts), Application: len(appNodes),
		Middleware: len(mwNodes), Edges: len(g.Edges),
		Nodes: len(appNodes) + len(mwNodes),
	}
	return g
}

func mergeUnique(base, add []string) []string {
	seen := map[string]bool{}
	for _, v := range base {
		seen[v] = true
	}
	for _, v := range add {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		base = append(base, v)
	}
	sort.Strings(base)
	return base
}

// appKeyOf 与图节点粒度一致的代表性 key（详情/明细引用用）。
func appKeyOf(r domain.TopoRow, expand bool) string {
	if !expand {
		return r.ApolloAppID
	}
	a := attrOf(r)
	dep, node := firstOf(a.deps), firstOf(a.nodes)
	if dep == "" && node == "" {
		return r.ApolloAppID
	}
	return r.ApolloAppID + "|" + dep + "|" + node
}

// OpDetailFromRows 节点详情（id: app:{appId} / app:{appId|dep|node} / mw:{type}:{addr} / host:{id}）。
func OpDetailFromRows(all []domain.TopoRow, id string, expand bool, date, env string) (*domain.OpNodeDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("缺少节点 id")
	}
	d := &domain.OpNodeDetail{
		ID: id, SyncDate: date, Env: env,
		ApolloHosts: []domain.OpGraphHost{}, Middlewares: []domain.OpNodeDetailMw{}, Apps: []domain.OpNodeDetailApp{},
	}
	switch {
	case strings.HasPrefix(id, "app:"):
		d.Category = "application"
		appID := strings.TrimPrefix(id, "app:")
		var dep, node string
		if parts := strings.SplitN(appID, "|", 3); len(parts) == 3 {
			appID, dep, node = parts[0], parts[1], parts[2]
		}
		var rows []domain.TopoRow
		for _, r := range all {
			if r.ApolloAppID != appID {
				continue
			}
			if dep != "" || node != "" {
				a := attrOf(r)
				if dep != "" && !containsStr(a.deps, dep) {
					continue
				}
				if node != "" && !containsStr(a.nodes, node) {
					continue
				}
			}
			rows = append(rows, r)
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("未找到应用节点: %s", id)
		}
		r0 := rows[0]
		a := attrOf(r0)
		d.Name = firstNonEmpty(r0.ApolloAppName, r0.ApolloAppID)
		d.ApolloAppID, d.ApolloAppName = r0.ApolloAppID, r0.ApolloAppName
		d.AppDept, d.AppOwner = r0.AppDept, r0.AppOwner
		d.EngProject, d.PpApp, d.Node = firstOf(a.engs), firstOf(a.pps), firstOf(a.nodes)
		d.DepProject, d.DeployProjectName = firstOf(a.deps), firstOf(a.deps)
		if !expand {
			d.EngProjects, d.DepProjects, d.PpApps, d.Nodes = a.engs, a.deps, a.pps, a.nodes
		}
		d.ApolloHosts = opHosts(rows)
		// 该应用依赖的中间件（depCount 按当前行集全量口径）
		mwDep := map[string]int{}
		for _, r := range all {
			addr := strings.TrimSpace(r.ConnectAddr)
			if r.MiddlewareType == "" || addr == "" {
				continue
			}
			mwDep[r.MiddlewareType+"|"+addr]++
		}
		seen := map[string]bool{}
		for _, r := range rows {
			addr := strings.TrimSpace(r.ConnectAddr)
			if r.MiddlewareType == "" || addr == "" {
				continue
			}
			k := r.MiddlewareType + "|" + addr
			if seen[k] {
				continue
			}
			seen[k] = true
			d.Middlewares = append(d.Middlewares, domain.OpNodeDetailMw{
				MwType: r.MiddlewareType, Addr: addr, MwDb: r.MiddlewareDb,
				MwProject: r.MiddlewareProject, DepCount: mwDep[k],
			})
		}
		sort.Slice(d.Middlewares, func(i, j int) bool { return d.Middlewares[i].DepCount > d.Middlewares[j].DepCount })
	case strings.HasPrefix(id, "mw:"):
		d.Category = "middleware"
		rest := strings.TrimPrefix(id, "mw:")
		idx := strings.Index(rest, ":")
		if idx <= 0 {
			return nil, fmt.Errorf("非法中间件节点 id: %s", id)
		}
		mwType, addr := rest[:idx], rest[idx+1:]
		seenApp := map[string]bool{}
		for _, r := range all {
			if !strings.EqualFold(r.MiddlewareType, mwType) || strings.TrimSpace(r.ConnectAddr) != addr {
				continue
			}
			if d.MwType == "" {
				d.MwType, d.Addr, d.MwDb, d.MwProject = r.MiddlewareType, addr, r.MiddlewareDb, r.MiddlewareProject
				d.Name = r.MiddlewareType + ":" + addr
			}
			key := appKeyOf(r, expand)
			if seenApp[key] {
				continue
			}
			seenApp[key] = true
			ra := attrOf(r)
			d.Apps = append(d.Apps, domain.OpNodeDetailApp{
				NodeId: "app:" + key, ApolloAppID: r.ApolloAppID,
				ApolloAppName: r.ApolloAppName, AppDept: r.AppDept, AppOwner: r.AppOwner,
				EngProject: firstOf(ra.engs), DepProject: firstOf(ra.deps),
				Node: firstOf(ra.nodes), PpApp: firstOf(ra.pps),
			})
		}
		if d.MwType == "" {
			return nil, fmt.Errorf("未找到中间件节点: %s", id)
		}
		d.DepCount = len(seenApp)
		sort.Slice(d.Apps, func(i, j int) bool { return d.Apps[i].ApolloAppID < d.Apps[j].ApolloAppID })
	case strings.HasPrefix(id, "host:"):
		d.Category = "host"
		var hid int64
		if _, err := fmt.Sscanf(strings.TrimPrefix(id, "host:"), "%d", &hid); err != nil {
			return nil, fmt.Errorf("非法实例节点 id: %s", id)
		}
		seenApp := map[string]bool{}
		for _, r := range all {
			if r.ApolloHostID != hid {
				continue
			}
			if d.HostName == "" {
				d.HostID, d.HostName, d.HostAddr = r.ApolloHostID, r.ApolloHostName, r.ApolloHostAddr
				d.Name = firstNonEmpty(r.ApolloHostName, id)
			}
			key := appKeyOf(r, expand)
			if seenApp[key] {
				continue
			}
			seenApp[key] = true
			ra := attrOf(r)
			d.Apps = append(d.Apps, domain.OpNodeDetailApp{
				NodeId: "app:" + key, ApolloAppID: r.ApolloAppID,
				ApolloAppName: r.ApolloAppName, AppDept: r.AppDept, AppOwner: r.AppOwner,
				EngProject: firstOf(ra.engs), DepProject: firstOf(ra.deps),
				Node: firstOf(ra.nodes), PpApp: firstOf(ra.pps),
			})
		}
		if d.HostName == "" {
			return nil, fmt.Errorf("未找到实例节点: %s", id)
		}
		d.HostAppCount = len(seenApp)
		sort.Slice(d.Apps, func(i, j int) bool { return d.Apps[i].ApolloAppID < d.Apps[j].ApolloAppID })
	default:
		return nil, fmt.Errorf("未知节点类型: %s", id)
	}
	return d, nil
}

// OpListRows 生成明细行：折叠 = appId×中间件 去重；展开 = 按部署点扩行（属性单值）。
func OpListRows(rows []domain.TopoRow, expand bool) []domain.TopoRow {
	out := []domain.TopoRow{}
	if expand {
		for _, r := range rows {
			a := attrOf(r)
			for _, p := range opPairs(a) {
				c := r
				c.DepProject, c.Node = p[0], p[1]
				c.EngProject, c.PPApplication = firstOf(a.engs), firstOf(a.pps)
				c.DepProjectList, c.NodeList, c.EngProjectList, c.PpApplicationList = nil, nil, nil, nil
				out = append(out, c)
			}
		}
	} else {
		seen := map[string]bool{}
		for _, r := range rows {
			k := r.ApolloAppID + "|" + r.MiddlewareType + "|" + strings.TrimSpace(r.ConnectAddr) + "|" + r.Env
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, r)
		}
	}
	// 对齐 topo /list 的 id 倒序口径（快照行无稳定自增 id 时按 appId 倒序兜底）
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID > out[j].ID
		}
		if out[i].ApolloAppID != out[j].ApolloAppID {
			return out[i].ApolloAppID > out[j].ApolloAppID
		}
		return out[i].ConnectAddr < out[j].ConnectAddr
	})
	return out
}

// OpMwTarget 去重中间件候选（排障发起表单下拉用）。
type OpMwTarget struct {
	MwType     string   `json:"mwType"`
	Addr       string   `json:"addr"`
	DepApps    int      `json:"depApps"`
	SampleApps []string `json:"sampleApps,omitempty"`
}

// OpMwTargets 从过滤后的行集生成中间件下拉候选（mwType+addr 去重）。
func OpMwTargets(rows []domain.TopoRow) []OpMwTarget {
	type agg struct {
		apps   map[string]struct{}
		sample []string
		mwType string
		addr   string
	}
	hit := map[string]*agg{}
	for _, r := range rows {
		if r.MiddlewareType == "" || strings.TrimSpace(r.ConnectAddr) == "" {
			continue
		}
		key := strings.ToLower(r.MiddlewareType) + "|" + strings.TrimSpace(r.ConnectAddr)
		a, ok := hit[key]
		if !ok {
			a = &agg{apps: map[string]struct{}{}, mwType: r.MiddlewareType, addr: strings.TrimSpace(r.ConnectAddr)}
			hit[key] = a
		}
		a.apps[r.ApolloAppID] = struct{}{}
		if len(a.sample) < 3 {
			a.sample = append(a.sample, firstNonEmpty(r.ApolloAppName, r.ApolloAppID))
		}
	}
	out := make([]OpMwTarget, 0, len(hit))
	for _, a := range hit {
		out = append(out, OpMwTarget{
			MwType: a.mwType, Addr: a.addr, DepApps: len(a.apps), SampleApps: a.sample,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].MwType != out[j].MwType {
			return out[i].MwType < out[j].MwType
		}
		return out[i].Addr < out[j].Addr
	})
	return out
}

// OpMwTypeList 中间件类型枚举（去重升序）。
func OpMwTypeList(rows []domain.TopoRow) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, r := range rows {
		if r.MiddlewareType == "" || seen[r.MiddlewareType] {
			continue
		}
		seen[r.MiddlewareType] = true
		out = append(out, r.MiddlewareType)
	}
	sort.Strings(out)
	return out
}
