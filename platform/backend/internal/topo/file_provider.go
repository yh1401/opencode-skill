package topo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"aiops-platform/backend/internal/domain"
)

type snapshotFile struct {
	Version   int                                    `json:"version"`
	TotalRows int                                    `json:"totalRows"`
	Data      map[string]map[string][]domain.TopoRow `json:"data"`
}

type adrFile struct {
	Rows []adrRow `json:"rows"`
}

type adrRow struct {
	ApolloAppID       string `json:"apolloAppId"`
	ApolloAppEnv      string `json:"apolloAppEnv"`
	DeployProjectName string `json:"deployProjectName"`
	Node              string `json:"node"`
}

type dprFile struct {
	Rows []dprRow `json:"rows"`
}

// dprRow 部署点↔PP 应用关系（跳②），用于补齐工程名/PP 应用/机房属性。
type dprRow struct {
	DepProject    string `json:"depProject"`
	PpApplication string `json:"ppApplication"`
	EgProject     string `json:"egProject"`
	Node          string `json:"node"`
}

// FileProvider 读取 topo/tprel/data 下的本地快照，零依赖可运行。
type FileProvider struct {
	dir string

	mu    sync.RWMutex
	data  map[string]map[string][]domain.TopoRow
	mtime time.Time
}

func NewFileProvider(dir string) (*FileProvider, error) {
	p := &FileProvider{dir: dir}
	if err := p.reload(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *FileProvider) Source() string { return "file:" + p.dir }

func (p *FileProvider) reload() error {
	topoPath := filepath.Join(p.dir, "operation_topology.json")
	st, err := os.Stat(topoPath)
	if err != nil {
		return fmt.Errorf("读取拓扑快照失败: %w", err)
	}
	raw, err := os.ReadFile(topoPath)
	if err != nil {
		return err
	}
	var sf snapshotFile
	if err := json.Unmarshal(raw, &sf); err != nil {
		return fmt.Errorf("解析 operation_topology.json 失败: %w", err)
	}
	adr := loadADR(filepath.Join(p.dir, "app_dp_relation.json"))
	dpr := loadDPR(filepath.Join(p.dir, "deploy_pinpoint_relation.json"))

	for date, envs := range sf.Data {
		for env, rows := range envs {
			for i := range rows {
				enrich(&rows[i], adr, dpr)
			}
			sf.Data[date][env] = rows
		}
	}

	p.mu.Lock()
	p.data = sf.Data
	p.mtime = st.ModTime()
	p.mu.Unlock()
	return nil
}

func loadADR(path string) map[string][]adrRow {
	out := map[string][]adrRow{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f adrFile
	if json.Unmarshal(raw, &f) != nil {
		return out
	}
	for _, r := range f.Rows {
		k := r.ApolloAppID + "|" + r.ApolloAppEnv
		out[k] = append(out[k], r)
	}
	return out
}

func loadDPR(path string) map[string][]dprRow {
	out := map[string][]dprRow{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f dprFile
	if json.Unmarshal(raw, &f) != nil {
		return out
	}
	for _, r := range f.Rows {
		if r.DepProject != "" {
			out[r.DepProject] = append(out[r.DepProject], r)
		}
	}
	return out
}

// enrich 按 topo 查询期口径补齐 appId 属性：跳①（app_dp_relation）给部署项目/节点，
// 跳②（deploy_pinpoint_relation）按部署项目补工程名/PP 应用/机房。
func enrich(r *domain.TopoRow, adr map[string][]adrRow, dpr map[string][]dprRow) {
	if r.ApolloAppID == "" {
		return
	}
	cands := adr[r.ApolloAppID+"|"+r.Env]
	if len(cands) == 0 {
		cands = adr[r.ApolloAppID+"|PRO"]
	}
	deps := map[string]struct{}{}
	nodes := map[string]struct{}{}
	for _, c := range cands {
		if c.DeployProjectName != "" {
			deps[c.DeployProjectName] = struct{}{}
		}
		if c.Node != "" {
			nodes[c.Node] = struct{}{}
		}
	}
	engs := map[string]struct{}{}
	pps := map[string]struct{}{}
	for dep := range deps {
		for _, d := range dpr[dep] {
			if d.EgProject != "" {
				engs[d.EgProject] = struct{}{}
			}
			if d.PpApplication != "" {
				pps[d.PpApplication] = struct{}{}
			}
			if d.Node != "" {
				nodes[d.Node] = struct{}{}
			}
		}
	}
	if len(deps) > 0 {
		r.DepProjectList = sortedKeys(deps)
		if r.DepProject == "" {
			r.DepProject = r.DepProjectList[0]
		}
	}
	if len(nodes) > 0 {
		r.NodeList = sortedKeys(nodes)
		if r.Node == "" {
			r.Node = r.NodeList[0]
		}
	}
	if len(engs) > 0 {
		r.EngProjectList = sortedKeys(engs)
		if r.EngProject == "" {
			r.EngProject = r.EngProjectList[0]
		}
	}
	if len(pps) > 0 {
		r.PpApplicationList = sortedKeys(pps)
		if r.PPApplication == "" {
			r.PPApplication = r.PpApplicationList[0]
		}
	}
}

func (p *FileProvider) ensureFresh() {
	st, err := os.Stat(filepath.Join(p.dir, "operation_topology.json"))
	if err != nil {
		return
	}
	p.mu.RLock()
	old := p.mtime
	p.mu.RUnlock()
	if st.ModTime().After(old) {
		_ = p.reload()
	}
}

func (p *FileProvider) Dates() []string {
	p.ensureFresh()
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, 0, len(p.data))
	for d := range p.data {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func (p *FileProvider) latest() string {
	dates := p.Dates()
	if len(dates) == 0 {
		return ""
	}
	return dates[len(dates)-1]
}

func (p *FileProvider) Rows(date, env string) ([]domain.TopoRow, error) {
	p.ensureFresh()
	p.mu.RLock()
	defer p.mu.RUnlock()
	if date == "" {
		date = p.latestLocked()
	}
	envs, ok := p.data[date]
	if !ok {
		return nil, fmt.Errorf("快照 %s 不存在", date)
	}
	var out []domain.TopoRow
	for e, rows := range envs {
		if env != "" && !strings.EqualFold(e, env) {
			continue
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (p *FileProvider) latestLocked() string {
	best := ""
	for d := range p.data {
		if d > best {
			best = d
		}
	}
	return best
}

func (p *FileProvider) Apps(date, env string) ([]domain.AppBrief, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	return ComputeApps(rows, ViewFolded), nil
}

func (p *FileProvider) Graph(focusAppID, viewMode, date, env string) (*domain.Graph, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	if viewMode == "" {
		viewMode = ViewFolded
	}
	if date == "" {
		date = p.latest()
	}
	return ComputeGraph(rows, viewMode, date, env, focusAppID), nil
}

func (p *FileProvider) ImpactByApp(appID, appKey, date, env, viewMode string) (*domain.Impact, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	if viewMode == "" {
		viewMode = ViewFolded
	}
	if date == "" {
		date = p.latest()
	}
	return ComputeImpactByApp(rows, appID, appKey, viewMode, date), nil
}

func (p *FileProvider) ImpactByMiddleware(mwType, addr, date, env string) (*domain.Impact, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	if date == "" {
		date = p.latest()
	}
	return ComputeImpactByMW(rows, mwType, addr, ViewFolded, date), nil
}

func (p *FileProvider) Diff(from, to, env string) (*domain.TopoDiff, error) {
	dates := p.Dates()
	if from == "" && len(dates) >= 2 {
		from = dates[len(dates)-2]
	}
	if to == "" && len(dates) >= 1 {
		to = dates[len(dates)-1]
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("至少需要两个快照才能做 diff")
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

func (p *FileProvider) MwTypes(date, env string) ([]domain.MwTypeStat, error) {
	rows, err := p.Rows(date, env)
	if err != nil {
		return nil, err
	}
	return ComputeMwTypes(rows), nil
}
