// Package identity 维护「应用 / 主机」跨数据源的统一身份映射。
//
// 存储上完全复用现有 store.Store：只新增两个 docType（identity_app / identity_host），
// 文件主存储 + PG 镜像自动生效，无需新增 DDL。
package identity

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/topo"
)

// DocType 常量：与 store 的 (docType, docID) 二元组配合，docID 即 App.Key / Host.Key。
const (
	DocTypeApp  = "identity_app"
	DocTypeHost = "identity_host"
)

// App 应用统一身份：把拓扑、Pinpoint、Prometheus 三套应用标识收敛到一行。
type App struct {
	Key             string    `json:"key"`
	ApolloAppID     string    `json:"apolloAppId,omitempty"`
	ApolloAppName   string    `json:"apolloAppName,omitempty"`
	PPApplication   string    `json:"ppApplication,omitempty"`
	PinpointAppID   string    `json:"pinpointAppId,omitempty"`
	PinpointAppName string    `json:"pinpointAppName,omitempty"`
	PromAppID       string    `json:"promAppId,omitempty"`
	Env             string    `json:"env,omitempty"`
	Dept            string    `json:"dept,omitempty"`
	Owner           string    `json:"owner,omitempty"`
	Nodes           []string  `json:"nodes,omitempty"`
	DepProjects     []string  `json:"depProjects,omitempty"`
	Aliases         []string  `json:"aliases,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Host 主机统一身份：把 ConnectAddr / ApolloHostAddr / CMDB 记录收敛到一行。
type Host struct {
	Key             string    `json:"key"`
	Host            string    `json:"host"`
	Addrs           []string  `json:"addrs,omitempty"`
	IPs             []string  `json:"ips,omitempty"`
	HostNames       []string  `json:"hostNames,omitempty"`
	CMDBIDs         []string  `json:"cmdbIds,omitempty"`
	Node            string    `json:"node,omitempty"`
	MiddlewareTypes []string  `json:"middlewareTypes,omitempty"`
	Apps            []string  `json:"apps,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// Stats 最近一次构建结果 + 当前规模。
type Stats struct {
	Enabled         bool      `json:"enabled"`
	Apps            int       `json:"apps"`
	Hosts           int       `json:"hosts"`
	PinpointTotal   int       `json:"pinpointTotal"`
	PinpointMatched int       `json:"pinpointMatched"`
	CMDBRecords     int       `json:"cmdbRecords"`
	Date            string    `json:"date,omitempty"`
	Env             string    `json:"env,omitempty"`
	RebuiltAt       time.Time `json:"rebuiltAt,omitempty"`
	DurationMs      int64     `json:"durationMs"`
	Error           string    `json:"error,omitempty"`
}

// Registry 内存索引 + 持久化。读多写少，Rebuild 时整体替换。
type Registry struct {
	enabled bool
	tp      topo.Provider
	st      store.Store
	gw      *mcp.Gateway
	cfg     config.Config

	mu        sync.RWMutex
	apps      map[string]App
	hosts     map[string]Host
	appAlias  map[string]string
	hostAlias map[string]string
	stats     Stats
}

// New 构造并加载已有映射（store 为空时返回空索引，等待 Rebuild）。
func New(tp topo.Provider, st store.Store, gw *mcp.Gateway, cfg config.Config) *Registry {
	r := &Registry{
		enabled:   cfg.IdentityEnabled,
		tp:        tp,
		st:        st,
		gw:        gw,
		cfg:       cfg,
		apps:      map[string]App{},
		hosts:     map[string]Host{},
		appAlias:  map[string]string{},
		hostAlias: map[string]string{},
		stats:     Stats{Enabled: cfg.IdentityEnabled},
	}
	r.load()
	return r
}

// Enabled 身份映射是否启用。
func (r *Registry) Enabled() bool { return r != nil && r.enabled }

func (r *Registry) load() {
	apps := store.ListInto[App](r.st, DocTypeApp)
	hosts := store.ListInto[Host](r.st, DocTypeHost)
	var latest time.Time

	appAlias := map[string]string{}
	for _, a := range apps {
		if a.Key == "" {
			continue
		}
		r.apps[a.Key] = a
		for _, alias := range append([]string{a.ApolloAppID, a.ApolloAppName, a.PPApplication, a.PinpointAppID, a.PinpointAppName}, a.Aliases...) {
			if k := appAliasKey(alias); k != "" {
				appAlias[k] = a.Key
			}
		}
		if a.UpdatedAt.After(latest) {
			latest = a.UpdatedAt
		}
	}

	hostAlias := map[string]string{}
	for _, h := range hosts {
		if h.Key == "" {
			continue
		}
		r.hosts[h.Key] = h
		hostAlias[h.Key] = h.Key
		for _, alias := range append(append(append([]string{h.Host}, h.Addrs...), h.IPs...), h.HostNames...) {
			if k := NormalizeHost(alias); k != "" {
				hostAlias[k] = h.Key
			}
		}
		if h.UpdatedAt.After(latest) {
			latest = h.UpdatedAt
		}
	}

	r.appAlias = appAlias
	r.hostAlias = hostAlias
	r.stats.Apps = len(r.apps)
	r.stats.Hosts = len(r.hosts)
	r.stats.RebuiltAt = latest
}

// Rebuild 从拓扑快照重建应用 / 主机映射，并可选回填 Pinpoint、CMDB、DNS。
// date / env 为空时取最新快照与默认环境。
func (r *Registry) Rebuild(ctx context.Context, date, env string) (Stats, error) {
	start := time.Now()
	if !r.Enabled() {
		return r.Stats(), fmt.Errorf("身份映射未启用（IDENTITY_ENABLED=false）")
	}
	if r.tp == nil {
		return r.Stats(), fmt.Errorf("拓扑数据源不可用")
	}
	if env == "" {
		env = r.cfg.DefaultEnv
	}
	if date == "" {
		if ds := r.tp.Dates(); len(ds) > 0 {
			date = ds[len(ds)-1]
		}
	}

	rows, err := r.tp.Rows(date, env)
	if err != nil {
		stats := Stats{Enabled: true, Date: date, Env: env, Error: err.Error()}
		stats.DurationMs = time.Since(start).Milliseconds()
		r.setStats(stats)
		return stats, err
	}

	now := time.Now()
	apps := map[string]*App{}
	hosts := map[string]*Host{}
	appAlias := map[string]string{}
	hostAlias := map[string]string{}
	stats := Stats{Enabled: true, Date: date, Env: env}

	for _, row := range rows {
		appID := strings.TrimSpace(row.ApolloAppID)
		appName := strings.TrimSpace(row.ApolloAppName)
		pp := strings.TrimSpace(row.PPApplication)
		key := appKey(appID, appName, pp)
		if key != "" {
			a := apps[key]
			if a == nil {
				a = &App{Key: key, UpdatedAt: now}
				apps[key] = a
			}
			if a.ApolloAppID == "" {
				a.ApolloAppID = appID
			}
			if a.ApolloAppName == "" {
				a.ApolloAppName = appName
			}
			if a.PPApplication == "" {
				a.PPApplication = pp
			}
			if a.PromAppID == "" {
				a.PromAppID = appID
			}
			if a.Env == "" {
				a.Env = env
			}
			if a.Dept == "" {
				a.Dept = row.AppDept
			}
			if a.Owner == "" {
				a.Owner = row.AppOwner
			}
			a.Nodes = appendUnique(a.Nodes, row.Node)
			a.DepProjects = appendUnique(a.DepProjects, row.DepProject)
			for _, alias := range []string{appID, appName, pp} {
				if k := appAliasKey(alias); k != "" {
					appAlias[k] = key
				}
			}
		}

		addr := strings.TrimSpace(row.ConnectAddr)
		if addr == "" {
			continue
		}
		hk := NormalizeHost(addr)
		if hk == "" {
			continue
		}
		h := hosts[hk]
		if h == nil {
			h = &Host{Key: hk, Host: hk, UpdatedAt: now}
			hosts[hk] = h
		}
		h.Addrs = appendUnique(h.Addrs, addr)
		// ApolloHostAddr 可能是 IP 列表，也可能是域名（vip / publicIp），按形态分流。
		for _, part := range strings.Split(row.ApolloHostAddr, ",") {
			n := NormalizeHost(part)
			if n == "" {
				continue
			}
			if IsIP(n) {
				h.IPs = appendUnique(h.IPs, n)
			} else {
				h.HostNames = appendUnique(h.HostNames, n)
			}
		}
		h.HostNames = appendUnique(h.HostNames, strings.TrimSpace(row.ApolloHostName))
		h.MiddlewareTypes = appendUnique(h.MiddlewareTypes, strings.TrimSpace(row.MiddlewareType))
		if key != "" {
			h.Apps = appendUnique(h.Apps, key)
		}
		hostAlias[hk] = hk
		for _, alias := range append([]string{addr, row.ApolloHostAddr, row.ApolloHostName}, h.IPs...) {
			if k := NormalizeHost(alias); k != "" {
				hostAlias[k] = hk
			}
		}
	}

	// Pinpoint：应用清单回填 pinpointId / pinpointName（best-effort，不阻断构建）。
	if r.gw != nil && r.gw.Enabled() {
		if raw, err := r.gw.CallSilent(ctx, "pinpoint", "list_applications_tool", map[string]any{"service_name": "DEFAULT"}); err == nil {
			if ppApps, err := mcp.PinpointApplications(raw); err == nil {
				stats.PinpointTotal = len(ppApps)
				for _, pp := range ppApps {
					key := ""
					if k, ok := appAlias[appAliasKey(pp.ID)]; ok {
						key = k
					}
					if key == "" {
						if k, ok := appAlias[appAliasKey(pp.Name)]; ok {
							key = k
						}
					}
					if key == "" {
						for ak, a := range apps {
							if appMatches(pp, a) {
								key = ak
								break
							}
						}
					}
					if key == "" {
						continue
					}
					a := apps[key]
					if a.PinpointAppID == "" {
						a.PinpointAppID = pp.ID
					}
					if a.PinpointAppName == "" {
						a.PinpointAppName = pp.Name
					}
					if k := appAliasKey(pp.Name); k != "" {
						appAlias[k] = key
					}
					if k := appAliasKey(pp.ID); k != "" {
						appAlias[k] = key
					}
					stats.PinpointMatched++
				}
			}
		}
	}

	// CMDB：分页拉取资产回填 IP / hostName / CMDB ID（best-effort）。
	if r.cfg.IdentityCMDBPages > 0 && r.gw != nil && r.gw.Enabled() && len(hosts) > 0 {
		pageSize := r.cfg.IdentityCMDBPageSize
		if pageSize <= 0 {
			pageSize = 200
		}
		ipIndex := map[string]string{}
		for k, h := range hosts {
			ipIndex[k] = k
			for _, ip := range append(append([]string{}, h.IPs...), h.Addrs...) {
				if n := NormalizeHost(ip); n != "" {
					ipIndex[n] = k
				}
			}
		}
		for page := 1; page <= r.cfg.IdentityCMDBPages; page++ {
			raw, err := r.gw.CallSilent(ctx, "cmdb", "cmdb_server_query", map[string]any{
				"currentPage": page,
				"pageSize":    pageSize,
			})
			if err != nil {
				break
			}
			pg, err := mcp.CMDBRecords(raw)
			if err != nil || len(pg.Records) == 0 {
				break
			}
			stats.CMDBRecords += len(pg.Records)
			for _, rec := range pg.Records {
				mergeCMDBRecord(rec, hosts, ipIndex, hostAlias)
			}
			if pg.Total > 0 && page*pageSize >= pg.Total {
				break
			}
		}
	}

	// DNS：可选解析，默认关闭（IDENITY_DNS_RESOLVE=false）。
	if r.cfg.IdentityDNSResolve {
		resolver := net.DefaultResolver
		for _, h := range hosts {
			for _, ip := range append([]string{}, h.IPs...) {
				c, cancel := context.WithTimeout(ctx, 2*time.Second)
				if names, err := resolver.LookupAddr(c, ip); err == nil {
					for _, n := range names {
						h.HostNames = appendUnique(h.HostNames, strings.TrimSuffix(n, "."))
					}
				}
				cancel()
			}
			for _, name := range append([]string{}, h.HostNames...) {
				c, cancel := context.WithTimeout(ctx, 2*time.Second)
				if ips, err := resolver.LookupHost(c, name); err == nil {
					h.IPs = appendUnique(h.IPs, ips...)
				}
				cancel()
			}
		}
	}

	if err := r.persist(apps, hosts); err != nil {
		stats.Error = err.Error()
	}

	finalApps := make(map[string]App, len(apps))
	for k, a := range apps {
		finalApps[k] = *a
	}
	finalHosts := make(map[string]Host, len(hosts))
	for k, h := range hosts {
		finalHosts[k] = *h
	}

	r.mu.Lock()
	oldApps := r.apps
	oldHosts := r.hosts
	r.apps = finalApps
	r.hosts = finalHosts
	r.appAlias = appAlias
	r.hostAlias = hostAlias
	r.mu.Unlock()

	// 清理本次构建后不再存在的旧 key（先落盘再换内存，避免半态）。
	cleanupKeys(r.st, oldApps, finalApps, DocTypeApp)
	cleanupKeys(r.st, oldHosts, finalHosts, DocTypeHost)

	stats.Apps = len(finalApps)
	stats.Hosts = len(finalHosts)
	stats.RebuiltAt = now
	stats.DurationMs = time.Since(start).Milliseconds()
	r.setStats(stats)
	return stats, nil
}

func (r *Registry) persist(apps map[string]*App, hosts map[string]*Host) error {
	var firstErr error
	for k, a := range apps {
		if err := r.st.Put(DocTypeApp, k, *a); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for k, h := range hosts {
		if err := r.st.Put(DocTypeHost, k, *h); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func cleanupKeys[T any](st store.Store, old map[string]T, cur map[string]T, docType string) {
	for k := range old {
		if _, ok := cur[k]; ok {
			continue
		}
		_ = st.Delete(docType, k)
	}
}

func (r *Registry) setStats(s Stats) {
	r.mu.Lock()
	r.stats = s
	r.mu.Unlock()
}

// Stats 返回当前统计。
func (r *Registry) Stats() Stats {
	if r == nil {
		return Stats{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.stats
	s.Apps = len(r.apps)
	s.Hosts = len(r.hosts)
	return s
}

// Apps 返回应用列表（按 Key 排序）。
func (r *Registry) Apps() []App {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]App, 0, len(r.apps))
	for _, a := range r.apps {
		out = append(out, a)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Hosts 返回主机列表（按 Key 排序）。
func (r *Registry) Hosts() []Host {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Host, 0, len(r.hosts))
	for _, h := range r.hosts {
		out = append(out, h)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ResolveApp 输入 appId / 应用名 / PP 应用名 / Pinpoint 名都能命中同一条身份。
func (r *Registry) ResolveApp(input string) (App, bool) {
	if r == nil {
		return App{}, false
	}
	in := appAliasKey(input)
	if in == "" {
		return App{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if a, ok := r.apps[in]; ok {
		return a, true
	}
	if k, ok := r.appAlias[in]; ok {
		if a, ok := r.apps[k]; ok {
			return a, true
		}
	}
	// 模糊兜底：包含匹配，结果按 Key 最短优先，保证确定性。
	bestKey := ""
	for k, a := range r.apps {
		if strings.Contains(k, in) ||
			strings.Contains(appAliasKey(a.ApolloAppName), in) ||
			strings.Contains(appAliasKey(a.PPApplication), in) ||
			strings.Contains(appAliasKey(a.PinpointAppName), in) {
			if bestKey == "" || len(k) < len(bestKey) {
				bestKey = k
			}
		}
	}
	if bestKey != "" {
		return r.apps[bestKey], true
	}
	return App{}, false
}

// ResolveHost 输入 host / addr / ip / hostName 都能命中同一条身份。
func (r *Registry) ResolveHost(input string) (Host, bool) {
	if r == nil {
		return Host{}, false
	}
	in := NormalizeHost(input)
	if in == "" {
		return Host{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if h, ok := r.hosts[in]; ok {
		return h, true
	}
	if k, ok := r.hostAlias[in]; ok {
		if h, ok := r.hosts[k]; ok {
			return h, true
		}
	}
	bestKey := ""
	for k := range r.hosts {
		if strings.Contains(k, in) {
			if bestKey == "" || len(k) < len(bestKey) {
				bestKey = k
			}
		}
	}
	if bestKey != "" {
		return r.hosts[bestKey], true
	}
	return Host{}, false
}

// HostAliases 返回该主机在所有数据源里可能出现的归一标识（host / addr / IP / hostName）。
func (r *Registry) HostAliases(host string) map[string]struct{} {
	aliases := map[string]struct{}{}
	if k := NormalizeHost(host); k != "" {
		aliases[k] = struct{}{}
	}
	x, ok := r.ResolveHost(host)
	if !ok {
		return aliases
	}
	for _, v := range append(append(append([]string{x.Host}, x.Addrs...), x.IPs...), x.HostNames...) {
		if k := NormalizeHost(v); k != "" {
			aliases[k] = struct{}{}
		}
	}
	return aliases
}

// MatchRecord 在 CMDB 记录中按 ip/vip/publicIp/hostName 任一别名命中目标主机。
// 用于把 Prometheus / 拓扑里的主机标识传导到 CMDB 资产。
func (r *Registry) MatchRecord(recs []map[string]any, host string) map[string]any {
	aliases := r.HostAliases(host)
	if len(aliases) == 0 {
		return nil
	}
	hit := func(v string) bool {
		_, ok := aliases[NormalizeHost(v)]
		return ok
	}
	for _, rec := range recs {
		if v, ok := rec["ip"].(string); ok {
			for _, part := range strings.Split(v, ",") {
				if hit(part) {
					return rec
				}
			}
		}
		for _, key := range []string{"vip", "publicIp", "public_ip", "hostName", "host_name"} {
			if v, ok := rec[key].(string); ok && hit(v) {
				return rec
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 归一化与 PromQL 构造
// ---------------------------------------------------------------------------

// NormalizeHost 去 scheme / 路径 / userinfo / 端口，统一小写。IPv6 保留地址本体。
func NormalizeHost(in string) string {
	s := strings.TrimSpace(strings.ToLower(in))
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i > 0 {
			return s[1:i]
		}
		return strings.Trim(s, "[]")
	}
	if strings.Count(s, ":") == 1 {
		if i := strings.LastIndex(s, ":"); i > 0 {
			s = s[:i]
		}
	}
	return strings.Trim(s, " .")
}

// IsIP 判断是否为 IP 字面量。
func IsIP(s string) bool { return net.ParseIP(strings.TrimSpace(s)) != nil }

// PromQLInstanceRegex 生成匹配 instance 的完整正则（含端口可选）。
func PromQLInstanceRegex(host string) string {
	h := PromQLLiteral(host)
	if h == "" {
		return ""
	}
	return "^" + h + "(:[0-9]+)?$"
}

// PromQLLiteral 返回可安全嵌入 PromQL 正则字符串的主机字面量（反斜杠双写，不含锚点）。
// 适合模板形如 instance=~"{host}.*" 的场景。
func PromQLLiteral(host string) string {
	h := NormalizeHost(host)
	if h == "" {
		return ""
	}
	return strings.ReplaceAll(regexp.QuoteMeta(h), `\`, `\\`)
}

// UpQuery 用一组 host 生成 up 查询；去重后以 | 连接，兼容多候选主机。
func UpQuery(hosts []string) string {
	seen := map[string]struct{}{}
	parts := make([]string, 0, len(hosts))
	for _, h := range hosts {
		re := PromQLInstanceRegex(h)
		if re == "" {
			continue
		}
		if _, ok := seen[re]; ok {
			continue
		}
		seen[re] = struct{}{}
		parts = append(parts, re)
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf(`up{instance=~"%s"}`, strings.Join(parts, "|"))
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func appKey(id, name, pp string) string {
	return appAliasKey(firstNonEmpty(id, name, pp))
}

func appAliasKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func appendUnique(list []string, vals ...string) []string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		exists := false
		for _, cur := range list {
			if strings.EqualFold(cur, v) {
				exists = true
				break
			}
		}
		if !exists {
			list = append(list, v)
		}
	}
	return list
}

// appMatches Pinpoint 名称与拓扑应用信息的弱匹配（名称包含或去分隔符后相等）。
func appMatches(pp mcp.PinpointApplication, a *App) bool {
	name := appAliasKey(pp.Name)
	if name == "" {
		return false
	}
	for _, cand := range []string{a.ApolloAppID, a.ApolloAppName, a.PPApplication, a.Key} {
		c := appAliasKey(cand)
		if c == "" {
			continue
		}
		if c == name || strings.Contains(c, name) || strings.Contains(name, c) {
			return true
		}
		if normalizeToken(c) == normalizeToken(name) {
			return true
		}
	}
	return false
}

func normalizeToken(s string) string {
	replacer := strings.NewReplacer("-", "", "_", "", ".", "", " ", "")
	return replacer.Replace(strings.ToLower(s))
}

func mergeCMDBRecord(rec map[string]any, hosts map[string]*Host, ipIndex map[string]string, hostAlias map[string]string) {
	var ips []string
	var extraNames []string
	for _, key := range []string{"ip", "vip", "publicIp", "public_ip", "privateIp", "private_ip"} {
		if v, ok := rec[key].(string); ok {
			for _, part := range strings.Split(v, ",") {
				n := NormalizeHost(part)
				if n == "" {
					continue
				}
				// CMDB 的 vip/publicIp 等字段可能存域名而非 IP，按形态分流到别名。
				if IsIP(n) {
					ips = appendUnique(ips, n)
				} else {
					extraNames = appendUnique(extraNames, n)
				}
			}
		}
	}
	hostName := ""
	for _, key := range []string{"hostName", "host_name", "hostname", "name"} {
		if v, ok := rec[key].(string); ok && strings.TrimSpace(v) != "" {
			hostName = strings.TrimSpace(v)
			break
		}
	}
	cmdbID := ""
	for _, key := range []string{"id", "serverId", "server_id", "assetId", "asset_id"} {
		if v, ok := rec[key]; ok {
			cmdbID = fmt.Sprintf("%v", v)
			break
		}
	}
	node := ""
	for _, key := range []string{"node", "idc", "机房", "room"} {
		if v, ok := rec[key].(string); ok && strings.TrimSpace(v) != "" {
			node = strings.TrimSpace(v)
			break
		}
	}

	nodeKey := ""
	for _, ip := range ips {
		if k, ok := ipIndex[ip]; ok {
			nodeKey = k
			break
		}
	}
	if nodeKey == "" && hostName != "" {
		if k, ok := hostAlias[NormalizeHost(hostName)]; ok {
			nodeKey = k
		}
	}
	if nodeKey == "" {
		return
	}
	h := hosts[nodeKey]
	if h == nil {
		return
	}
	h.IPs = appendUnique(h.IPs, ips...)
	if hostName != "" {
		h.HostNames = appendUnique(h.HostNames, hostName)
		hostAlias[NormalizeHost(hostName)] = nodeKey
	}
	if len(extraNames) > 0 {
		h.HostNames = appendUnique(h.HostNames, extraNames...)
		for _, n := range extraNames {
			hostAlias[NormalizeHost(n)] = nodeKey
		}
	}
	if cmdbID != "" {
		h.CMDBIDs = appendUnique(h.CMDBIDs, cmdbID)
	}
	if node != "" && h.Node == "" {
		h.Node = node
	}
	for _, ip := range ips {
		hostAlias[ip] = nodeKey
	}
}
