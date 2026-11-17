package topo

import (
	"sort"
	"strings"

	"aiops-platform/backend/internal/domain"
)

// Provider 拓扑数据源抽象：file（本地快照）/ http（topo 服务）。
type Provider interface {
	Source() string
	Dates() []string
	Rows(date, env string) ([]domain.TopoRow, error)
	Apps(date, env string) ([]domain.AppBrief, error)
	Graph(focusAppID, viewMode, date, env string) (*domain.Graph, error)
	ImpactByApp(appID, appKey, date, env, viewMode string) (*domain.Impact, error)
	ImpactByMiddleware(mwType, addr, date, env string) (*domain.Impact, error)
	Diff(from, to, env string) (*domain.TopoDiff, error)
	MwTypes(date, env string) ([]domain.MwTypeStat, error)
}

// ViewFolded / ViewExpanded 与 topo 的 expandDepNode 参数一一对应。
const (
	ViewFolded   = "folded"
	ViewExpanded = "expanded"
)

// AppKey 计算应用节点粒度 key，与 topo service.appKeyOf 保持一致。
func AppKey(r domain.TopoRow, viewMode string) string {
	if viewMode != ViewExpanded {
		return r.ApolloAppID
	}
	dep := r.DepProject
	if dep == "" && len(r.DepProjectList) > 0 {
		dep = r.DepProjectList[0]
	}
	node := r.Node
	if node == "" && len(r.NodeList) > 0 {
		node = r.NodeList[0]
	}
	if dep == "" && node == "" {
		return r.ApolloAppID
	}
	return r.ApolloAppID + "|" + dep + "|" + node
}

// MWKey 中间件节点 key。
func MWKey(mwType, addr string) string {
	return "mw:" + mwType + ":" + strings.TrimSpace(addr)
}

// ComputeApps 汇总应用摘要。
func ComputeApps(rows []domain.TopoRow, viewMode string) []domain.AppBrief {
	type agg struct {
		name  string
		deps  map[string]struct{}
		nodes map[string]struct{}
		mws   map[string]struct{}
	}
	m := map[string]*agg{}
	for _, r := range rows {
		key := AppKey(r, viewMode)
		a, ok := m[key]
		if !ok {
			a = &agg{name: r.ApolloAppName, deps: map[string]struct{}{}, nodes: map[string]struct{}{}, mws: map[string]struct{}{}}
			m[key] = a
		}
		if a.name == "" {
			a.name = r.ApolloAppName
		}
		if len(r.DepProjectList) > 0 {
			for _, d := range r.DepProjectList {
				if d != "" {
					a.deps[d] = struct{}{}
				}
			}
		} else if r.DepProject != "" {
			a.deps[r.DepProject] = struct{}{}
		}
		if len(r.NodeList) > 0 {
			for _, n := range r.NodeList {
				if n != "" {
					a.nodes[n] = struct{}{}
				}
			}
		} else if r.Node != "" {
			a.nodes[r.Node] = struct{}{}
		}
		if r.MiddlewareType != "" {
			a.mws[r.MiddlewareType] = struct{}{}
		}
	}
	out := make([]domain.AppBrief, 0, len(m))
	for key, a := range m {
		out = append(out, domain.AppBrief{
			AppID:       key,
			AppName:     a.name,
			DepProjects: sortedKeys(a.deps),
			Nodes:       sortedKeys(a.nodes),
			MwTypes:     sortedKeys(a.mws),
			DepApps:     len(a.mws),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AppID < out[j].AppID })
	return out
}

// ComputeGraph 构建应用→中间件二部图。focusAppID 非空时只返回该应用的 ego 网络。
func ComputeGraph(rows []domain.TopoRow, viewMode, date, env, focusAppID string) *domain.Graph {
	g := &domain.Graph{SyncDate: date, ViewMode: viewMode, Env: env, Nodes: []domain.GraphNode{}, Edges: []domain.GraphEdge{}}
	nodeIndex := map[string]*domain.GraphNode{}
	edgeSeen := map[string]struct{}{}
	appName := map[string]string{}
	mwApps := map[string]map[string]struct{}{}

	for _, r := range rows {
		key := AppKey(r, viewMode)
		if focusAppID != "" && r.ApolloAppID != focusAppID {
			continue
		}
		appID := "app:" + key
		if _, ok := nodeIndex[appID]; !ok {
			nodeIndex[appID] = &domain.GraphNode{ID: appID, Label: firstNonEmpty(r.ApolloAppName, key), Kind: "app", AppID: key, Env: r.Env}
		}
		appName[key] = firstNonEmpty(r.ApolloAppName, key)

		addr := strings.TrimSpace(r.ConnectAddr)
		if r.MiddlewareType == "" || addr == "" {
			continue
		}
		mwID := MWKey(r.MiddlewareType, addr)
		if _, ok := nodeIndex[mwID]; !ok {
			nodeIndex[mwID] = &domain.GraphNode{ID: mwID, Label: addr, Kind: "middleware", Type: r.MiddlewareType, Addr: addr}
		}
		if mwApps[mwID] == nil {
			mwApps[mwID] = map[string]struct{}{}
		}
		mwApps[mwID][key] = struct{}{}

		ek := appID + "->" + mwID
		if _, ok := edgeSeen[ek]; ok {
			continue
		}
		edgeSeen[ek] = struct{}{}
		g.Edges = append(g.Edges, domain.GraphEdge{Source: appID, Target: mwID, Kind: "depend"})
	}

	for id, n := range nodeIndex {
		if n.Kind == "middleware" {
			n.DepApps = len(mwApps[id])
			n.Degree = n.DepApps
		}
	}
	for id := range nodeIndex {
		for _, e := range g.Edges {
			if e.Source == id || e.Target == id {
				nodeIndex[id].Degree++
			}
		}
	}

	if focusAppID != "" {
		// ego：目标应用 + 其依赖中间件 + 共享同一中间件的其它应用（用于看清爆炸半径）
		keep := map[string]struct{}{}
		keep["app:"+focusAppID] = struct{}{}
		for _, e := range g.Edges {
			if e.Source == "app:"+focusAppID {
				keep[e.Target] = struct{}{}
			}
		}
		for id := range keep {
			if !strings.HasPrefix(id, "mw:") {
				continue
			}
			for appID := range mwApps[id] {
				keep["app:"+appID] = struct{}{}
			}
		}
		filtered := g.Edges[:0]
		for _, e := range g.Edges {
			if _, ok := keep[e.Source]; !ok {
				continue
			}
			if _, ok := keep[e.Target]; !ok {
				continue
			}
			filtered = append(filtered, e)
		}
		g.Edges = filtered
		for id := range nodeIndex {
			if _, ok := keep[id]; !ok {
				delete(nodeIndex, id)
			}
		}
	}

	for _, n := range nodeIndex {
		g.Nodes = append(g.Nodes, *n)
	}
	sort.Slice(g.Nodes, func(i, j int) bool {
		if g.Nodes[i].Kind != g.Nodes[j].Kind {
			return g.Nodes[i].Kind < g.Nodes[j].Kind
		}
		return g.Nodes[i].ID < g.Nodes[j].ID
	})
	return g
}

// ComputeImpactByApp 应用视角影响面：自身依赖的中间件 + 共享这些中间件的兄弟应用。
func ComputeImpactByApp(rows []domain.TopoRow, appID, appKey, viewMode, date string) *domain.Impact {
	im := &domain.Impact{SyncDate: date, Level: "app", Apps: []domain.ImpactApp{}, Middlewares: []domain.ImpactMW{}}
	if appKey == "" {
		appKey = appID
	}
	im.Target = appKey

	mwSet := map[string]struct{}{}
	peerSet := map[string]domain.ImpactApp{}
	nameOf := map[string]string{}
	mwApps := map[string]map[string]struct{}{}

	for _, r := range rows {
		key := AppKey(r, viewMode)
		nameOf[key] = firstNonEmpty(r.ApolloAppName, key)
		if r.MiddlewareType == "" || strings.TrimSpace(r.ConnectAddr) == "" {
			continue
		}
		mwID := strings.ToLower(r.MiddlewareType) + "|" + strings.TrimSpace(r.ConnectAddr)
		if mwApps[mwID] == nil {
			mwApps[mwID] = map[string]struct{}{}
		}
		mwApps[mwID][key] = struct{}{}
		if key == appKey || r.ApolloAppID == appID {
			mwSet[mwID] = struct{}{}
		}
	}
	if len(mwSet) == 0 {
		im.Note = "该应用在当前快照中无中间件依赖记录（可能是采集缺失，不代表无依赖）"
		return im
	}
	for mwID := range mwSet {
		parts := strings.SplitN(mwID, "|", 2)
		im.Middlewares = append(im.Middlewares, domain.ImpactMW{Type: parts[0], Addr: parts[1], DepApps: len(mwApps[mwID]), IsTarget: true})
		for p := range mwApps[mwID] {
			if p == appKey || p == appID {
				continue
			}
			peerSet[p] = domain.ImpactApp{AppKey: p, AppID: strings.SplitN(p, "|", 2)[0], AppName: nameOf[p]}
		}
	}
	for _, p := range peerSet {
		im.Apps = append(im.Apps, p)
	}
	sort.Slice(im.Apps, func(i, j int) bool { return im.Apps[i].AppKey < im.Apps[j].AppKey })
	sort.Slice(im.Middlewares, func(i, j int) bool { return im.Middlewares[i].DepApps > im.Middlewares[j].DepApps })
	im.TotalApps = len(im.Apps) + 1
	return im
}

// ComputeImpactByMW 中间件视角影响面：依赖该地址的全部应用。
func ComputeImpactByMW(rows []domain.TopoRow, mwType, addr, viewMode, date string) *domain.Impact {
	im := &domain.Impact{SyncDate: date, Level: "middleware", Target: strings.ToLower(mwType) + ":" + addr, Apps: []domain.ImpactApp{}, Middlewares: []domain.ImpactMW{}}
	seen := map[string]domain.ImpactApp{}
	for _, r := range rows {
		if !strings.EqualFold(r.MiddlewareType, mwType) {
			continue
		}
		if !addrEqual(r.ConnectAddr, addr) {
			continue
		}
		key := AppKey(r, viewMode)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = domain.ImpactApp{AppKey: key, AppID: r.ApolloAppID, AppName: firstNonEmpty(r.ApolloAppName, r.ApolloAppID), DepProject: r.DepProject}
	}
	for _, a := range seen {
		im.Apps = append(im.Apps, a)
	}
	sort.Slice(im.Apps, func(i, j int) bool { return im.Apps[i].AppKey < im.Apps[j].AppKey })
	im.TotalApps = len(im.Apps)
	im.Middlewares = append(im.Middlewares, domain.ImpactMW{Type: strings.ToLower(mwType), Addr: addr, DepApps: len(im.Apps), IsTarget: true})
	if len(im.Apps) == 0 {
		im.Note = "当前快照中未找到该中间件地址的依赖记录"
	}
	return im
}

// ComputeDiff 计算两个快照的依赖差异（折叠粒度，避免 node 噪声）。
func ComputeDiff(fromRows, toRows []domain.TopoRow, from, to string) *domain.TopoDiff {
	d := &domain.TopoDiff{From: from, To: to, Added: []domain.DiffItem{}, Removed: []domain.DiffItem{}}
	fromSet := relationSet(fromRows)
	toSet := relationSet(toRows)
	for k, item := range toSet {
		if _, ok := fromSet[k]; !ok {
			d.Added = append(d.Added, item)
		}
	}
	for k, item := range fromSet {
		if _, ok := toSet[k]; !ok {
			d.Removed = append(d.Removed, item)
		}
	}
	sortDiff(d.Added)
	sortDiff(d.Removed)
	return d
}

// ComputeMwTypes 中间件类型统计。
func ComputeMwTypes(rows []domain.TopoRow) []domain.MwTypeStat {
	type agg struct {
		rows      int
		instances map[string]struct{}
		apps      map[string]struct{}
	}
	m := map[string]*agg{}
	for _, r := range rows {
		if r.MiddlewareType == "" {
			continue
		}
		a, ok := m[r.MiddlewareType]
		if !ok {
			a = &agg{instances: map[string]struct{}{}, apps: map[string]struct{}{}}
			m[r.MiddlewareType] = a
		}
		a.rows++
		if r.ConnectAddr != "" {
			a.instances[strings.TrimSpace(r.ConnectAddr)] = struct{}{}
		}
		a.apps[r.ApolloAppID] = struct{}{}
	}
	out := make([]domain.MwTypeStat, 0, len(m))
	for t, a := range m {
		out = append(out, domain.MwTypeStat{Type: t, Rows: a.rows, Instances: len(a.instances), Apps: len(a.apps)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rows > out[j].Rows })
	return out
}

func relationSet(rows []domain.TopoRow) map[string]domain.DiffItem {
	m := map[string]domain.DiffItem{}
	for _, r := range rows {
		if r.MiddlewareType == "" || r.ConnectAddr == "" {
			continue
		}
		k := r.ApolloAppID + "|" + strings.ToLower(r.MiddlewareType) + "|" + strings.TrimSpace(r.ConnectAddr)
		if _, ok := m[k]; ok {
			continue
		}
		m[k] = domain.DiffItem{
			AppKey:         r.ApolloAppID,
			AppID:          r.ApolloAppID,
			AppName:        r.ApolloAppName,
			MiddlewareType: r.MiddlewareType,
			ConnectAddr:    r.ConnectAddr,
			DepProject:     r.DepProject,
		}
	}
	return m
}

func sortDiff(items []domain.DiffItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].AppID != items[j].AppID {
			return items[i].AppID < items[j].AppID
		}
		return items[i].ConnectAddr < items[j].ConnectAddr
	})
}

func addrEqual(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return true
	}
	// 允许只填 host 的模糊匹配
	if !strings.Contains(b, ":") {
		return strings.SplitN(a, ":", 2)[0] == b
	}
	return false
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
