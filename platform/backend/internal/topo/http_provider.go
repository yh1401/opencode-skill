package topo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/domain"
)

// HTTPProvider 调用 topo 服务（/api/external/operationTopology/*）。
// 说明：topo 未提供“快照日期列表”接口，Dates() 只能拿到 graph 返回的 syncDate；
// 因此 HTTP 模式下 diff 需要显式传 from/to，否则视为数据不足。
type HTTPProvider struct {
	baseURL string
	token   string
	client  *http.Client

	mu        sync.Mutex
	rowsCache map[string][]domain.TopoRow // key: date|env
	cacheAt   time.Time
	latest    string
}

const httpPageSize = 500

func NewHTTPProvider(baseURL, token string, timeout time.Duration) *HTTPProvider {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &HTTPProvider{
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
		client:    &http.Client{Timeout: timeout},
		rowsCache: map[string][]domain.TopoRow{},
	}
}

func (p *HTTPProvider) Source() string { return "http:" + p.baseURL }

func (p *HTTPProvider) doGet(path string, q url.Values, out any) error {
	body, err := p.getRaw(path, q)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// getRaw 原始 GET（运维关系拓扑透传代理用）。
func (p *HTTPProvider) getRaw(path string, q url.Values) ([]byte, error) {
	u := p.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("token", p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("topo 返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// postRaw 原始 POST（手动同步透传代理用）。
func (p *HTTPProvider) postRaw(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, p.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if p.token != "" {
		req.Header.Set("token", p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("topo 返回 %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// Dates 探测一次 graph 取 syncDate；HTTP 模式下历史快照不可枚举。
func (p *HTTPProvider) Dates() []string {
	p.mu.Lock()
	cached := p.latest
	p.mu.Unlock()
	if cached != "" {
		return []string{cached}
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			SyncDate string `json:"syncDate"`
		} `json:"data"`
	}
	q := url.Values{}
	if err := p.doGet("/api/external/operationTopology/graph", q, &resp); err != nil {
		return nil
	}
	if resp.Data.SyncDate == "" {
		return nil
	}
	p.mu.Lock()
	p.latest = resp.Data.SyncDate
	p.mu.Unlock()
	return []string{resp.Data.SyncDate}
}

func (p *HTTPProvider) Rows(date, env string) ([]domain.TopoRow, error) {
	if date == "" {
		dates := p.Dates()
		if len(dates) == 0 {
			return nil, fmt.Errorf("无法从 topo 获取快照日期")
		}
		date = dates[len(dates)-1]
	}
	key := date + "|" + env
	p.mu.Lock()
	if rows, ok := p.rowsCache[key]; ok && time.Since(p.cacheAt) < time.Minute {
		p.mu.Unlock()
		return rows, nil
	}
	p.mu.Unlock()

	type listResp struct {
		Code      int              `json:"code"`
		List      []domain.TopoRow `json:"list"`
		PageTotal int              `json:"pageTotal"`
	}
	var all []domain.TopoRow
	for page := 1; page <= 100; page++ {
		q := url.Values{}
		q.Set("page", strconv.Itoa(page))
		q.Set("pageSize", strconv.Itoa(httpPageSize))
		q.Set("syncDate", date)
		if env != "" {
			q.Set("env", env)
		}
		q.Set("expandDepNode", "0") // 折叠口径：depProjects/nodes 多值返回，不扩行
		var resp listResp
		if err := p.doGet("/api/external/operationTopology/list", q, &resp); err != nil {
			return nil, err
		}
		if resp.Code != 0 && resp.Code != 200 {
			return nil, fmt.Errorf("topo /list 返回 code=%d", resp.Code)
		}
		all = append(all, resp.List...)
		if len(resp.List) == 0 || len(all) >= resp.PageTotal {
			break
		}
	}

	p.mu.Lock()
	p.rowsCache[key] = all
	p.cacheAt = time.Now()
	p.latest = date
	p.mu.Unlock()
	return all, nil
}

func (p *HTTPProvider) Apps(date, env string) ([]domain.AppBrief, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	return ComputeApps(rows, ViewFolded), nil
}

func (p *HTTPProvider) Graph(focusAppID, viewMode, date, env string) (*domain.Graph, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	if viewMode == "" {
		viewMode = ViewFolded
	}
	if date == "" && len(p.Dates()) > 0 {
		date = p.Dates()[0]
	}
	return ComputeGraph(rows, viewMode, date, env, focusAppID), nil
}

func (p *HTTPProvider) ImpactByApp(appID, appKey, date, env, viewMode string) (*domain.Impact, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	if viewMode == "" {
		viewMode = ViewFolded
	}
	if date == "" && len(p.Dates()) > 0 {
		date = p.Dates()[0]
	}
	return ComputeImpactByApp(rows, appID, appKey, viewMode, date), nil
}

func (p *HTTPProvider) ImpactByMiddleware(mwType, addr, date, env string) (*domain.Impact, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	if date == "" && len(p.Dates()) > 0 {
		date = p.Dates()[0]
	}
	return ComputeImpactByMW(rows, mwType, addr, ViewFolded, date), nil
}

func (p *HTTPProvider) Diff(from, to, env string) (*domain.TopoDiff, error) {
	if from == "" || to == "" {
		return nil, fmt.Errorf("HTTP 数据源无法枚举历史快照，请显式传 from 与 to")
	}
	fromRows, err := p.Rows(from, env)
	if err != nil {
		return nil, err
	}
	toRows, err := p.Rows(to, env)
	if err != nil {
		return nil, err
	}
	return ComputeDiff(fromRows, toRows, from, to), nil
}

func (p *HTTPProvider) MwTypes(date, env string) ([]domain.MwTypeStat, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	out := ComputeMwTypes(rows)
	sort.Slice(out, func(i, j int) bool { return out[i].Rows > out[j].Rows })
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
