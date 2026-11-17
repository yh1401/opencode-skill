package service

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"devops-automation/api/tprel/tplog"
	"devops-automation/config"

	"go.uber.org/zap"
)

// =========================================================
// EasyOps 代理层 Apollo 客户端（对应附录 A：3 个外部接口）
//   接口1 GET /thirdApi/getApolloHostInfo      → GetHostList
//   接口2 GET /thirdApi/apollo/apps            → GetApps
//   接口3 GET /thirdApi/apollo/namespace       → GetNamespace
// =========================================================

// ApolloHost 接口1 返回的 Apollo 服务信息（list[].id 即 apolloHostId）
// 字段与《接口逻辑说明.md》§15.3 上游接口 1 规格对齐：host=实例访问地址、secondProductId=二级产品线数组、
// appCount=该实例下应用总数（入库为实例层归属元数据）
type ApolloHost struct {
	Id              int      `json:"id"`
	ApolloHostId    int      `json:"apolloHostId"` // 上游同名冗余字段（部分返回同时带 id）
	Name            string   `json:"name"`
	Host            string   `json:"host"`            // 实例访问地址（如 https://tyyy-…:8154）
	Token           string   `json:"token"`           // 加密 token（仅鉴权用，不落库）
	SecondProductId []string `json:"secondProductId"` // 二级产品线 id 列表
	AppCount        int      `json:"appCount"`        // 实例下应用总数
	Available       bool     `json:"available"`
	AvailableDetail string   `json:"availableDetail"`
}

// ApolloApp 接口2 返回的应用信息
// 说明：真实应用列表接口除 appId/name 外，还携带部门、负责人等运维元数据，
// 这里用多字段别名兜底解析（不同 EasyOps 版本字段名可能不同），供 owner 映射直接复用，
// 减少对硬编码 AppidOwnerMap 的依赖（数据来源更合理）。
type ApolloApp struct {
	AppId      string `json:"appId"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`      // 负责人（别名1）
	OwnerName  string `json:"ownerName"`  // 负责人（别名2）
	AppOwner   string `json:"appOwner"`   // 负责人（别名3）
	DeptName   string `json:"deptName"`   // 部门（别名1）
	Department string `json:"department"` // 部门（别名2）
	OrgName    string `json:"orgName"`    // 部门/组织（别名3）
}

// Leader 返回应用负责人（按字段优先级取第一个非空）
func (a ApolloApp) Leader() string {
	for _, s := range []string{a.OwnerName, a.Owner, a.AppOwner} {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// Dept 返回应用所属部门（按字段优先级取第一个非空）
func (a ApolloApp) Dept() string {
	for _, s := range []string{a.DeptName, a.Department, a.OrgName} {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// ApolloItem 接口3 返回的配置项
type ApolloItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// 接口1 成功响应（code 为字符串 "success"）
type hostListResp struct {
	Code string       `json:"code"`
	Msg  string       `json:"message"`
	List []ApolloHost `json:"list"`
}

// 接口2 响应（data 有两种结构：直接数组 / {total, items}）
type appsResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"message"`
	Data json.RawMessage `json:"data"`
}

type appsItems struct {
	Total int         `json:"total"`
	Items []ApolloApp `json:"items"`
}

// 接口3 响应
type namespaceResp struct {
	Code int    `json:"code"`
	Msg  string `json:"message"`
	Data struct {
		AppId         string       `json:"appId"`
		ClusterName   string       `json:"clusterName"`
		NamespaceName string       `json:"namespaceName"`
		Items         []ApolloItem `json:"items"`
	} `json:"data"`
}

// AppDpRelationRow 关系接口（跳①：appId ↔ 部署项目）单行。
// 字段与 app_dp_relation 表一一对应；接口中 del_flag / isReleased 为布尔、时间列为字符串，
// 故单独建 DTO，不复用表模型（避免 bool → int 反序列化失败）。
type AppDpRelationRow struct {
	Id                int64  `json:"id"`
	DelFlag           bool   `json:"del_flag"`
	CreateTime        string `json:"create_time"`
	UpdateTime        string `json:"update_time"`
	ProjectType       string `json:"projectType"`
	ProjectId         string `json:"projectId"`
	ProjectName       string `json:"projectName"`
	DeployProjectId   string `json:"deployProjectId"`
	DeployProjectName string `json:"deployProjectName"`
	Node              string `json:"node"`
	Env               int    `json:"env"`
	ApolloHostId      int    `json:"apolloHostId"`
	ApolloAppId       string `json:"apolloAppId"`
	ApolloAppEnv      string `json:"apolloAppEnv"`
	ApolloAppCluster  string `json:"apolloAppCluster"`
	IsReleased        bool   `json:"isReleased"`
	Operation         string `json:"operation"`
	Operator          int    `json:"operator"`
}

// 关系接口响应：{ "code": "success", "list": [行…], "pageTotal": N }
type appDpRelationResp struct {
	Code      string             `json:"code"`
	Message   string             `json:"message"`
	List      []AppDpRelationRow `json:"list"`
	PageTotal int                `json:"pageTotal"`
}

// ProbeResult 单套探活结果（对应参考实现 _probe_host_available）
type ProbeResult struct {
	Available bool   // 该套代理可用且能返回应用列表
	AppCount  int    // 应用数量
	Error     string // 不可用原因
	CheckedAt time.Time
}

// ApolloClient EasyOps 代理层客户端（带 Cookie sessionId 鉴权）
type ApolloClient struct {
	baseUrl      string
	sessionId    string
	client       *http.Client
	hostTimout   time.Duration // 接口1 超时
	reqTimeout   time.Duration // 接口2/3 超时
	maxRetries   int           // 接口1 重试次数
	probeWorkers int           // 探活/并发抓取 worker 数

	mu           sync.Mutex
	probeCache   map[int]ProbeResult // hostId → 探活结果（TTL 内复用，避免重复等待）
	probeTTL     time.Duration
	probeEnabled bool                // 探活开关（对应参考 APOLLO_HOST_PROBE_ENABLED，false 时跳过探活）
	appsCache    map[int][]ApolloApp // hostId → 应用列表（探活时缓存，仅供一次同步内复用，避免 probe+fetch 重复请求）
}

func NewApolloClient(cfg config.Apollo) *ApolloClient {
	// baseUrl：env 覆盖 APOLLO_HOST_API_BASE，否则用内置默认值（对齐参考实现 mcp_server.py L278）
	baseUrl := cfg.BaseUrl
	if env := os.Getenv("APOLLO_HOST_API_BASE"); env != "" {
		baseUrl = env
	}
	if baseUrl == "" {
		baseUrl = "https://easyops.tech.ctseelink.cn"
	}
	baseUrl = strings.TrimRight(baseUrl, "/")
	// sessionId：env 覆盖 APOLLO_HOST_SESSION_ID，否则用内置默认值（对齐参考实现 mcp_server.py L283）
	// 内置默认来自参考实现 _DEFAULT_SESSION_ID，便于本地直连 EasyOps 侧到测试
	sessionId := cfg.SessionId
	if s := os.Getenv("APOLLO_HOST_SESSION_ID"); s != "" {
		sessionId = s
	}
	if sessionId == "" {
		sessionId = "e5e27a7d1805758400287ae86741f889"
	}
	hostTimeout := time.Duration(cfg.HostTimeout) * time.Second
	if hostTimeout <= 0 {
		hostTimeout = 8 * time.Second
	}
	reqTimeout := time.Duration(cfg.RequestTimeout) * time.Second
	if reqTimeout <= 0 {
		reqTimeout = 6 * time.Second
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 8
	}
	tr := &http.Transport{}
	if cfg.InsecureEnabled() {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &ApolloClient{
		baseUrl:      baseUrl,
		sessionId:    sessionId,
		client:       &http.Client{Transport: tr},
		hostTimout:   hostTimeout,
		reqTimeout:   reqTimeout,
		maxRetries:   cfg.MaxRetries,
		probeWorkers: workers,
		probeCache:   make(map[int]ProbeResult),
		appsCache:    make(map[int][]ApolloApp),
		probeTTL:     60 * time.Second,                                                   // 探活结果缓存时长（对应参考 APOLLO_HOST_PROBE_TTL=60）
		probeEnabled: strings.ToLower(os.Getenv("APOLLO_HOST_PROBE_ENABLED")) != "false", // 默认开启，参考 APOLLO_HOST_PROBE_ENABLED
	}
}

// decryptToken 解密接口1 返回的加密 token：Base64 解码后与 sessionId 循环逐字节 XOR
// 对应参考实现 mcp_server.py decrypt_token（EasyOps 加密密钥即 sessionId）
func (a *ApolloClient) decryptToken(encryptedToken string) string {
	if encryptedToken == "" || a.sessionId == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(encryptedToken)
	if err != nil {
		tplog.L().Warn("[operation_topology] token 解密失败", zap.Error(err))
		return ""
	}
	sid := []byte(a.sessionId)
	out := make([]byte, len(decoded))
	for i, b := range decoded {
		out[i] = b ^ sid[i%len(sid)]
	}
	return string(out)
}

// errHTTP404 HTTP 404 哨兵错误：标识“资源不存在”（如 namespace 不存在）。
// 属于预期态：不打印 ERROR、不重试、不把所属应用计入“未完成应用”。
var errHTTP404 = errors.New("resource not found (404)")

// 日志模块前缀：与各链路既有日志保持一致，便于按模块检索
const (
	logModuleTopology   = "[operation_topology]"
	logModuleDpRelation = "[dp_relation]"
)

// doGet 通用 GET 请求（Cookie sessionId 鉴权 + 日志）。
// module 为日志模块前缀（拓扑主表用 [operation_topology]，跳① 关系用 [dp_relation]），便于按链路检索。
func (a *ApolloClient) doGet(path string, params url.Values, timeout time.Duration, module, tag string) ([]byte, error) {
	reqUrl := a.baseUrl + path
	if len(params) > 0 {
		reqUrl += "?" + params.Encode()
	}
	start := time.Now()
	body, err := a.doGetRaw(reqUrl, timeout)
	cost := time.Since(start).Milliseconds()
	if err != nil {
		// 404（namespace 不存在等预期场景）不刷 ERROR，避免噪音（真实环境 85% 请求为 404）
		if !errors.Is(err, errHTTP404) {
			tplog.L().Error(module+" 请求失败", zap.String("tag", tag),
				zap.String("url", reqUrl), zap.Int64("costMs", cost), zap.Error(err))
		}
		return nil, err
	}
	tplog.L().Info(module+" 请求完成", zap.String("tag", tag),
		zap.String("url", reqUrl), zap.Int64("costMs", cost), zap.Int("bytes", len(body)))
	return body, nil
}

func (a *ApolloClient) doGetRaw(reqUrl string, timeout time.Duration) ([]byte, error) {
	var lastErr error
	attempts := 1 + a.maxRetries
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "sessionId="+a.sessionId)
		resp, err := a.client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		// 读 body
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 4096)
		for {
			n, rErr := resp.Body.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if rErr != nil {
				break
			}
		}
		resp.Body.Close()
		cancel()
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode == http.StatusNotFound {
				// 404：资源不存在（namespace 等），预期态——不重试、直接返回哨兵错误
				lastErr = fmt.Errorf("http %d: %w", resp.StatusCode, errHTTP404)
				break
			}
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, string(buf))
			continue
		}
		return buf, nil
	}
	if lastErr == nil {
		lastErr = errors.New("unknown request error")
	}
	return nil, lastErr
}

// GetHostList 接口1：获取全部 Apollo 服务列表
func (a *ApolloClient) GetHostList() ([]ApolloHost, error) {
	params := url.Values{}
	params.Set("paginator", "false")
	params.Set("pageIndex", "1")
	params.Set("pageSize", "100")
	body, err := a.doGet("/thirdApi/getApolloHostInfo", params, a.hostTimout, logModuleTopology, "Host列表")
	if err != nil {
		return nil, err
	}
	var resp hostListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("host list 解析失败: %w", err)
	}
	if resp.Code != "success" {
		return nil, fmt.Errorf("host list 返回失败: %s", resp.Msg)
	}
	return resp.List, nil
}

// GetAppDpRelations 关系接口（跳①）：拉取 appId ↔ 部署项目 关系行，返回本次行数与上游 total。
// 路径由配置 relation_api_path 提供（真实路径 `/thirdApi/apolloApp/list`；为空即不启用本接口）。
//
// 上游契约（2026-09-17 生产抓包核实）：`paginator=false` 时服务端**不分页、直接返回全表**，
// 此时返回体里的 `pageTotal` 是**总记录数**（1426），并非总页数。故固定传
// `paginator=false` + `pageSize=-1`，一次请求即取回全量；pageIndex/pageSize 仅在
// 上游真做分页（paginator 生效）时才影响结果。
func (a *ApolloClient) GetAppDpRelations(path string, pageIndex, pageSize int) ([]AppDpRelationRow, int, error) {
	if pageSize <= 0 {
		pageSize = relationFullPageSize // -1 = 不分页、取全量
	}
	params := url.Values{}
	params.Set("paginator", "false")
	params.Set("pageIndex", fmt.Sprintf("%d", pageIndex))
	params.Set("pageSize", fmt.Sprintf("%d", pageSize))
	body, err := a.doGet(path, params, a.reqTimeout, logModuleDpRelation, "appId部署项目关系")
	if err != nil {
		return nil, 0, err
	}
	var resp appDpRelationResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("关系接口解析失败: %w", err)
	}
	if resp.Code != "success" {
		return nil, 0, fmt.Errorf("关系接口返回失败: code=%s msg=%s", resp.Code, resp.Message)
	}
	return resp.List, resp.PageTotal, nil
}

// FilterHostsByName 按 APOLLO_HOST_NAME 模糊过滤套列表（对应参考实现 _select_target_host）
// 环境变量未设置时返回全部套；设置后仅保留名称包含匹配串（不区分大小写）的套，供同步遍历筛选
func (a *ApolloClient) FilterHostsByName(hosts []ApolloHost) []ApolloHost {
	nameFilter := os.Getenv("APOLLO_HOST_NAME")
	if nameFilter == "" {
		return hosts
	}
	needle := strings.ToLower(nameFilter)
	filtered := make([]ApolloHost, 0, len(hosts))
	for _, h := range hosts {
		if strings.Contains(strings.ToLower(h.Name), needle) {
			filtered = append(filtered, h)
		}
	}
	if len(filtered) > 0 {
		tplog.L().Info("[operation_topology] APOLLO_HOST_NAME 过滤套", zap.String("match", nameFilter), zap.Int("kept", len(filtered)), zap.Int("total", len(hosts)))
	} else {
		tplog.L().Warn("[operation_topology] APOLLO_HOST_NAME 无匹配套，回退全部", zap.String("match", nameFilter))
		filtered = hosts
	}
	return filtered
}

// GetApps 接口2：获取指定套的应用列表（兼容数组 / items 两种结构）
func (a *ApolloClient) GetApps(hostId int) ([]ApolloApp, error) {
	params := url.Values{}
	params.Set("apolloHostId", fmt.Sprintf("%d", hostId))
	body, err := a.doGet("/thirdApi/apollo/apps", params, a.reqTimeout, logModuleTopology, "应用列表")
	if err != nil {
		return nil, err
	}
	var resp appsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("apps 解析失败: %w", err)
	}
	if resp.Code != 200 {
		return nil, fmt.Errorf("apps 返回失败: code=%d %s", resp.Code, resp.Msg)
	}
	// 结构 A：直接数组
	var arr []ApolloApp
	if err := json.Unmarshal(resp.Data, &arr); err == nil {
		return arr, nil
	}
	// 结构 B：{total, items}
	var items appsItems
	if err := json.Unmarshal(resp.Data, &items); err != nil {
		return nil, fmt.Errorf("apps data 结构无法解析: %w", err)
	}
	return items.Items, nil
}

// GetNamespace 接口3：查询单个 namespace 配置项
func (a *ApolloClient) GetNamespace(hostId int, env, appId, cluster, namespace string) ([]ApolloItem, error) {
	if cluster == "" {
		cluster = "default"
	}
	if namespace == "" {
		namespace = "application"
	}
	params := url.Values{}
	params.Set("apolloHostId", fmt.Sprintf("%d", hostId))
	params.Set("env", env)
	params.Set("appId", appId)
	params.Set("clusterName", cluster)
	params.Set("namespaceName", namespace)
	body, err := a.doGet("/thirdApi/apollo/namespace", params, a.reqTimeout, logModuleTopology, "配置查询")
	if err != nil {
		// HTTP 404（网关层 namespace 不存在）：非致命，但透传哨兵错误，供调用方区分“缺失”做应用级提示
		return nil, err
	}
	var resp namespaceResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("namespace 解析失败: %w", err)
	}
	if resp.Code != 200 {
		// 包装 code=404（namespace 不存在/未授权）：同哨兵语义返回（非致命、可区分“缺失”）
		if resp.Code == 404 {
			return nil, errHTTP404
		}
		return nil, fmt.Errorf("namespace 返回失败: code=%d %s", resp.Code, resp.Msg)
	}
	return resp.Data.Items, nil
}

// probeHost 探活单套 Apollo：调用 GetApps（轻量快速失败）判定是否可用
// 对应参考实现 _probe_host_available。命中 apps 缓存时直接复用，不重复请求。
func (a *ApolloClient) probeHost(hostId int, name string) ProbeResult {
	a.mu.Lock()
	cachedApps, has := a.appsCache[hostId]
	a.mu.Unlock()
	var apps []ApolloApp
	if has {
		// 已缓存（含空列表）直接复用，避免对空实例重复请求
		apps = cachedApps
	} else {
		var err error
		apps, err = a.GetApps(hostId)
		if err != nil {
			// 仅请求失败才算不可用（503/500/超时等）
			tplog.L().Warn("[operation_topology] 探活套不可用", zap.Int("hostId", hostId), zap.String("name", name), zap.Error(err))
			return ProbeResult{Available: false, AppCount: 0, Error: err.Error(), CheckedAt: time.Now()}
		}
		a.mu.Lock()
		a.appsCache[hostId] = apps
		a.mu.Unlock()
	}
	if len(apps) == 0 {
		// 请求成功但 0 应用：可用的空实例（真实环境如 hostId 11/10），不是故障
		tplog.L().Info("[operation_topology] 探活套可用(空实例)", zap.Int("hostId", hostId), zap.String("name", name), zap.Int("appCount", 0))
		return ProbeResult{Available: true, AppCount: 0, CheckedAt: time.Now()}
	}
	tplog.L().Info("[operation_topology] 探活套可用", zap.Int("hostId", hostId), zap.String("name", name), zap.Int("appCount", len(apps)))
	return ProbeResult{Available: true, AppCount: len(apps), CheckedAt: time.Now()}
}

// ProbeHosts 并发探活所有套（8 worker + TTL 缓存），返回 {hostId: ProbeResult}
// 对应参考实现 _probe_hosts_concurrent：
//   - 命中 TTL 缓存直接复用，不发请求（默认 60s）
//   - worker 并发探活，避免 N 套串行等待
//   - timeout>0 时为硬预算：超时未完成的套标记 available=false（对应参考 futures_wait 超时 pending 判定）
func (a *ApolloClient) ProbeHosts(hosts []ApolloHost, timeout time.Duration) map[int]ProbeResult {
	// 探活开关：APOLLO_HOST_PROBE_ENABLED=false 时跳过，返回空（对应参考 L423，走原全量逻辑）
	if !a.probeEnabled {
		return map[int]ProbeResult{}
	}
	result := make(map[int]ProbeResult, len(hosts))
	// 命中 TTL 缓存直接复用
	var toProbe []ApolloHost
	now := time.Now()
	a.mu.Lock()
	for _, h := range hosts {
		if c, ok := a.probeCache[h.Id]; ok && now.Sub(c.CheckedAt) < a.probeTTL {
			result[h.Id] = c
		} else {
			toProbe = append(toProbe, h)
		}
	}
	a.mu.Unlock()

	if len(toProbe) > 0 {
		workers := a.probeWorkers
		if workers > len(toProbe) {
			workers = len(toProbe)
		}
		// hostId → toProbe 下标，避免在 worker 内线性查找
		indexOf := make(map[int]int, len(toProbe))
		for idx := range toProbe {
			indexOf[toProbe[idx].Id] = idx
		}
		collect := make([]ProbeResult, len(toProbe))
		// 超时覆盖整个并发段（发送 + worker 消费），避免发送阻塞在满通道上导致超时形同虚设
		var probeCtx context.Context
		var cancel context.CancelFunc
		if timeout > 0 {
			probeCtx, cancel = context.WithTimeout(context.Background(), timeout)
		} else {
			probeCtx, cancel = context.WithCancel(context.Background())
		}
		defer cancel()

		var wg sync.WaitGroup
		var mu sync.Mutex
		ch := make(chan ApolloHost)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for h := range ch {
					r := a.probeHost(h.Id, h.Name)
					if idx, ok := indexOf[h.Id]; ok {
						mu.Lock()
						collect[idx] = r
						mu.Unlock()
					}
				}
			}()
		}
		// 发送任务（受 ctx 约束：超时/取消即停，不再往已无消费者的通道阻塞发送）
		go func() {
			defer close(ch)
			for _, h := range toProbe {
				select {
				case <-probeCtx.Done():
					return
				case ch <- h:
				}
			}
		}()
		// 等待全部完成（超时由 probeCtx 驱动发送端提前 close(ch) 收尾）
		wg.Wait()
		timedOut := probeCtx.Err() != nil
		if timedOut {
			tplog.L().Warn("[operation_topology] 探活预算超时，未完成套标记为不可用",
				zap.Duration("timeout", timeout), zap.Int("toProbe", len(toProbe)))
		}
		// 写回缓存并组装结果（未完成的 collect 下标为零值 → 标记不可用）
		a.mu.Lock()
		for _, h := range toProbe {
			r := collect[indexOf[h.Id]]
			if r.CheckedAt.IsZero() {
				r = ProbeResult{Available: false, AppCount: 0, Error: "probe_timeout", CheckedAt: now}
			}
			result[h.Id] = r
			a.probeCache[h.Id] = r
		}
		a.mu.Unlock()
	}
	return result
}

// GetAppsConcurrent 并发获取所有可用套的应用列表，返回 {hostId: apps}
// 对应参考实现 fetch_proxy_apps_first_available：可用套并发抓取，不可用套跳过（fast-fail）
func (a *ApolloClient) GetAppsConcurrent(hosts []ApolloHost, probe map[int]ProbeResult) (map[int][]ApolloApp, int, int) {
	appsByHost := make(map[int][]ApolloApp)
	skipped := 0
	var available []ApolloHost
	for _, h := range hosts {
		if p, ok := probe[h.Id]; ok && !p.Available {
			skipped++
			continue
		}
		available = append(available, h)
	}
	workers := a.probeWorkers
	if workers > len(available) {
		workers = len(available)
	}
	if workers <= 0 {
		return appsByHost, skipped, len(available)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ch := make(chan ApolloHost)
	errCnt := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for h := range ch {
				// 复用探活时缓存的 apps，避免 probe+fetch 重复请求（对应参考实现 _cached_fetch_apps）
				a.mu.Lock()
				cached, has := a.appsCache[h.Id]
				a.mu.Unlock()
				if has && len(cached) > 0 {
					mu.Lock()
					appsByHost[h.Id] = cached
					mu.Unlock()
					continue
				}
				apps, err := a.GetApps(h.Id)
				if err != nil {
					mu.Lock()
					errCnt++
					mu.Unlock()
					continue
				}
				mu.Lock()
				appsByHost[h.Id] = apps
				mu.Unlock()
			}
		}()
	}
	for _, h := range available {
		ch <- h
	}
	close(ch)
	wg.Wait()
	return appsByHost, skipped, len(available) - errCnt
}
