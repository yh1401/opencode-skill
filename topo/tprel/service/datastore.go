package service

// =========================================================
// 数据源路由（本地文件快照 为主 · 数据库为后续能力）
//
// 当前阶段（模块内聚、不依赖数据库、不改动模块外配置）：
//   - 读接口（list/graph/detail/middlewareTypes）默认直接用本地快照文件
//     data/operation_topology.json（按 sync_date 分桶保留最近 N 份，默认 3 份，供前后对比）；
//   - 同步（Apollo 拉取）解析完成后直接写该文件（ReplaceSnapshot，替换当日桶并裁剪更早的桶）；
//   - 请求带 source=db 时“本次改用数据库”仅在已注入 DB 连接后可用
//     （InitDatastoreWithDB，数据库版本后续接入的入口；未注入则静默回退本地文件）。
//
// 设计要点（性能）：
//  1. 文件快照首次读取/同步替换时一次性加载进内存，查询只扫内存行，避免读盘；
//     文件按 sync_date 只保留最近 N 份（默认 3），内存占用可控。
//  2. 快照不可变（同步替换走原子写 tmp+rename），加载后常驻复用，数十万行
//     也只在首次加载付一次解析成本。
//  3. 过滤在内存逐行 match（LIKE 用 Contains），分页组内倒序得到 id desc 等效序
//     （组内行序 = 写入顺序，等价 DB id asc），单趟窗口收集避免整批拷贝；
//     graph/detail 聚合取数按文件行序正序输出（与 DB 端 Order("id") 语义一致）。
//  4. DB 分支（DBStore / auto 降级 / dbReachable 冷却）保留为后续接入代码，
//     未注入 DB 连接（dbStore==nil）时一律回退本地文件。
// =========================================================

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"devops-automation/api/tprel/model"
	"devops-automation/api/tprel/tplog"
	"devops-automation/global"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// fileSnapshotName data 目录下的本地快照文件名（单 JSON 全量）
	fileSnapshotName = "operation_topology.json"

	// defaultDataDir 本地快照目录（相对程序运行目录；模块内聚默认值，不依赖外部 config）
	defaultDataDir = "./api/tprel/data"

	// sourceDB / sourceFile 数据源标识（file=本地快照 / db=数据库；写入 /graph 返回体 dataSource 与日志）
	sourceDB   = "db"
	sourceFile = "file"

	// 数据库探测参数（保留给后续 DB 接入分支使用）
	probeInterval = 10 * time.Second
	pingTimeout   = 2 * time.Second
	retryAfter    = 30 * time.Second

	// defaultSnapshotKeep 拓扑快照默认保留份数（按 sync_date 分桶；
	// 每次同步替换当日桶并按日期保留最近 N 份，供前后对比，避免表/文件无限堆积）
	defaultSnapshotKeep = 3
)

// storeRouter 包级单例（service 各读接口经 currentRouter() 懒初始化获取）
// storeMu 保护单例初始化（懒加载并发安全）
var (
	storeRouter *DatastoreRouter
	storeMu     sync.Mutex
)

// ---------------------------------------------------------
// 查询条件 / 通用结构
// ---------------------------------------------------------

// RowFilter 关系行查询条件（等价于原 list/graph/detail 拼接的 SQL 过滤）
type RowFilter struct {
	SyncDate       string // 快照日期（空 = 默认最新）
	Env            string
	Node           string
	MiddlewareType string
	DepProject     string // 模糊
	ApolloAppId    string // 精确
	ApolloAppName  string // 模糊
	ApolloHostName string // 模糊
	AppDept        string // 模糊
	AppOwner       string // 模糊
	ConnectAddr    string // 模糊
	ApolloHostId   int    // 精确（>0 生效）
	// ApolloAppIdIn 经 跳①（app_dp_relation/关系接口）×跳②（deploy_pinpoint_relation）
	// 翻译后的 apollo_app_id 集合：非 nil 时生效，空集合 = 无匹配行。
	// 拓扑表 dep_project/node 为预留空列，「部署项目/节点」条件需先翻译成 apollo_app_id
	// 再下推；展开后仍需按条件复核（见 dp_relation.go）
	ApolloAppIdIn []string
	Source        string // 请求级数据源覆盖（db|file，空 = 按配置 mode 走）
	Page          int    // 分页（<=0 默认 1）
	PageSize      int    // 分页（<=0 默认 20，<0 表示不分页取全量）
}

// QueryResult 行查询结果（total 为过滤后总数，rows 为当前页）
type QueryResult struct {
	Rows  []model.OperationTopology
	Total int64
}

// RowSource 行数据源抽象（DB 与本地文件两个实现，读接口聚合逻辑共用）
type RowSource interface {
	Name() string
	Query(f RowFilter) (QueryResult, error)
	LatestSyncDate(env string) string
	SyncDates(env string) []string // 降序
	MiddlewareTypes() []string
}

// isConnErr 是否为数据库连接类错误（只有这类错误才触发 auto 降级）
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, gorm.ErrInvalidDB) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "database is closed") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "bad connection") ||
		strings.Contains(msg, "server closed")
}

// ---------------------------------------------------------
// DB 实现（保留原 SQL 语义，未改聚合逻辑）
// ---------------------------------------------------------

type DBStore struct {
	db *gorm.DB
}

func (s *DBStore) Name() string { return sourceDB }

func (s *DBStore) Query(f RowFilter) (QueryResult, error) {
	var res QueryResult
	page, pageSize := f.Page, f.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	db := s.db.Model(&model.OperationTopology{})
	if f.SyncDate != "" {
		db = db.Where("sync_date = ?", f.SyncDate)
	}
	if f.Node != "" {
		db = db.Where("node = ?", f.Node)
	}
	if f.Env != "" {
		db = db.Where("env = ?", f.Env)
	}
	if f.MiddlewareType != "" {
		db = db.Where("middleware_type = ?", f.MiddlewareType)
	}
	if f.DepProject != "" {
		db = db.Where("dep_project LIKE ?", "%"+f.DepProject+"%")
	}
	if f.ApolloAppId != "" {
		db = db.Where("apollo_app_id = ?", f.ApolloAppId)
	}
	if f.ApolloAppName != "" {
		db = db.Where("apollo_app_name LIKE ?", "%"+f.ApolloAppName+"%")
	}
	if f.ApolloHostName != "" {
		db = db.Where("apollo_host_name LIKE ?", "%"+f.ApolloHostName+"%")
	}
	if f.AppDept != "" {
		db = db.Where("app_dept LIKE ?", "%"+f.AppDept+"%")
	}
	if f.AppOwner != "" {
		db = db.Where("app_owner LIKE ?", "%"+f.AppOwner+"%")
	}
	if f.ConnectAddr != "" {
		db = db.Where("connect_addr LIKE ?", "%"+f.ConnectAddr+"%")
	}
	if f.ApolloHostId > 0 {
		db = db.Where("apollo_host_id = ?", f.ApolloHostId)
	}
	if f.ApolloAppIdIn != nil {
		db = db.Where("apollo_app_id IN ?", f.ApolloAppIdIn)
	}
	if err := db.Count(&res.Total).Error; err != nil {
		return res, err
	}
	if pageSize > 0 {
		db = db.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize)
	} else if pageSize < 0 {
		// 聚合取数（graph/detail）：显式 id asc，与本地文件快照的行序一致，保证两源输出顺序相同
		db = db.Order("id")
	}
	if err := db.Find(&res.Rows).Error; err != nil {
		return res, err
	}
	return res, nil
}

func (s *DBStore) LatestSyncDate(env string) string {
	var latest string
	db := s.db.Model(&model.OperationTopology{})
	if env != "" {
		db = db.Where("env = ?", env)
	}
	db.Select("MAX(sync_date)").Scan(&latest)
	return latest
}

func (s *DBStore) SyncDates(env string) []string {
	var dates []string
	db := s.db.Model(&model.OperationTopology{})
	if env != "" {
		db = db.Where("env = ?", env)
	}
	db.Distinct("sync_date").Order("sync_date DESC").Pluck("sync_date", &dates)
	return dates
}

func (s *DBStore) MiddlewareTypes() []string {
	var types []string
	s.db.Model(&model.OperationTopology{}).
		Distinct("middleware_type").
		Order("middleware_type").
		Pluck("middleware_type", &types)
	return types
}

// ---------------------------------------------------------
// 本地文件实现（data/operation_topology.json 单 JSON 全量快照）
// ---------------------------------------------------------

// snapshotData 本地快照文件格式：
//
//	{
//	  "version": 1, "exportedAt": "…", "totalRows": N,
//	  "data": { "2026-09-07": { "PRO": [ {行…}, … ] }, … }
//	}
//
// 行字段与 model.OperationTopology 的 json tag 一致；组内行序 = 导出时 DB id asc。
type snapshotData struct {
	Version    int                                             `json:"version"`
	ExportedAt string                                          `json:"exportedAt"`
	TotalRows  int                                             `json:"totalRows"`
	Data       map[string]map[string][]model.OperationTopology `json:"data"` // date -> env -> rows
}

// dateEnvGroup 内存分桶（按 sync_date desc 排序，组内行序 = 文件行序）
type dateEnvGroup struct {
	date string
	env  string
	rows []model.OperationTopology
}

type FileStore struct {
	mu     sync.Mutex
	path   string
	groups []dateEnvGroup // 按 date desc 有序
	dates  []string       // 去重后的 date，desc
	loaded bool
}

func newFileStore(dataDir string) *FileStore {
	return &FileStore{path: filepath.Join(dataDir, fileSnapshotName)}
}

// ensure 首次访问（或 refresh 后）加载文件，double-check + 互斥防止并发重复解析
func (s *FileStore) ensure() {
	if s.loaded {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	if err := s.loadLocked(); err != nil {
		// 加载失败不 panic：仅当文件缺失/损坏时置空，由外层查询回退/报错
		tplog.L().Warn("[datastore] 本地快照加载失败（auto 模式查询将回退数据库）",
			zap.String("file", s.path), zap.Error(err))
		s.groups, s.dates = nil, nil
		s.loaded = true // 避免每次查询重复报错；文件更新后由 refresh 重置
	}
}

func (s *FileStore) loadLocked() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("本地快照文件不存在: %s（先同步一次或配置 seed_from_db）", s.path)
		}
		return err
	}
	defer f.Close()

	var snap snapshotData
	if err := json.NewDecoder(f).Decode(&snap); err != nil {
		return fmt.Errorf("本地快照解析失败(%s): %w", s.path, err)
	}
	if snap.Version != 1 {
		return fmt.Errorf("本地快照版本不支持: version=%d", snap.Version)
	}

	// 转分桶并排序：date desc；组内保留行序（= DB id asc）
	dateOrder := make(map[string]bool, len(snap.Data))
	s.groups = s.groups[:0]
	for date, envMap := range snap.Data {
		if len(envMap) == 0 {
			continue
		}
		dateOrder[date] = true
		for env, rows := range envMap {
			if len(rows) == 0 {
				continue
			}
			s.groups = append(s.groups, dateEnvGroup{date: date, env: env, rows: rows})
		}
	}
	sort.SliceStable(s.groups, func(i, j int) bool { return s.groups[i].date > s.groups[j].date })
	s.dates = make([]string, 0, len(dateOrder))
	for d := range dateOrder {
		s.dates = append(s.dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(s.dates)))
	s.loaded = true
	tplog.L().Info("[datastore] 本地快照已加载",
		zap.String("file", s.path), zap.Int("dates", len(s.dates)), zap.Int("totalRows", snap.TotalRows))
	return nil
}

// refresh 文件变更（同步导出后）重载
func (s *FileStore) refresh() {
	s.mu.Lock()
	s.loaded = false
	s.groups, s.dates = nil, nil
	s.mu.Unlock()
	s.ensure()
}

// Replace 同步直写本地快照（原子写 tmp+rename，成功后重载内存）。
// rows 必须同一 sync_date（同步解析产物）：
//   - 本次只替换 syncDate 桶，其余历史日期桶原样保留（供前后对比）；
//   - 再按日期倒序保留最近 keep 份（含本次，keep<=0 表示不清理），更早的桶丢弃，
//     避免文件随同步次数无限增长。
//
// 组内行序 = 传入顺序（导出方保证为 DB id asc 语义）。
func (s *FileStore) Replace(rows []model.OperationTopology, keep int) error {
	if len(rows) == 0 {
		return errors.New("待写快照行为空")
	}
	syncDate := rows[0].SyncDate
	envMap := make(map[string][]model.OperationTopology)
	for _, r := range rows {
		envMap[r.Env] = append(envMap[r.Env], r)
	}
	// 取现有快照中的历史日期（除当日），按日期倒序，保留 keep-1 份（本次固定占 1 份）
	s.ensure()
	others := make([]string, 0, len(s.dates))
	for _, d := range s.dates {
		if d != syncDate {
			others = append(others, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(others)))
	kept := others
	if keep > 0 && len(others) > keep-1 {
		kept = others[:keep-1]
	}
	data := make(map[string]map[string][]model.OperationTopology, len(kept)+1)
	totalRows := 0
	for _, d := range kept {
		envs := make(map[string][]model.OperationTopology)
		for _, g := range s.groups {
			if g.date == d {
				envs[g.env] = g.rows
			}
		}
		if len(envs) == 0 {
			continue
		}
		data[d] = envs
		for _, rs := range envs {
			totalRows += len(rs)
		}
	}
	data[syncDate] = envMap
	totalRows += len(rows)

	snap := snapshotData{
		Version:    1,
		ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
		TotalRows:  totalRows,
		Data:       data,
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建快照目录失败(%s): %w", dir, err)
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建临时快照文件失败(%s): %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	writeErr := enc.Encode(snap)
	if writeErr == nil {
		writeErr = f.Close()
	} else {
		f.Close()
	}
	if writeErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("写快照文件失败(%s): %w", tmp, writeErr)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("替换快照文件失败(%s): %w", s.path, err)
	}
	s.refresh()
	tplog.L().Info("[datastore] 本地快照已直写",
		zap.String("file", s.path), zap.String("syncDate", syncDate),
		zap.Int("rows", len(rows)), zap.Int("totalRows", snap.TotalRows),
		zap.Int("snapshots", len(data)), zap.Int("droppedDates", len(others)-len(kept)))
	return nil
}

// Previous 返回内存中除 exclude 日期外最近一次日期的全部行（组内行序 = 文件行序）。
func (s *FileStore) Previous(exclude string) (string, []model.OperationTopology) {
	s.ensure()
	for _, d := range s.dates {
		if d == exclude {
			continue
		}
		var out []model.OperationTopology
		for _, g := range s.groups {
			if g.date == d {
				out = append(out, g.rows...)
			}
		}
		if len(out) > 0 {
			return d, out
		}
	}
	return "", nil
}

// CountOf 指定日期的总行数（内存快照内统计）。
func (s *FileStore) CountOf(date string) int {
	s.ensure()
	n := 0
	for _, g := range s.groups {
		if g.date == date {
			n += len(g.rows)
		}
	}
	return n
}

// Available 是否有可用的本地快照数据
func (s *FileStore) Available() bool {
	s.ensure()
	return len(s.groups) > 0
}

func (s *FileStore) Name() string { return sourceFile }

func (s *FileStore) Query(f RowFilter) (QueryResult, error) {
	s.ensure()
	if len(s.groups) == 0 {
		return QueryResult{}, errors.New("本地快照不可用（data 目录无数据文件或文件为空）")
	}
	page, pageSize := f.Page, f.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	// apollo_app_id 集合过滤（关系链翻译结果）：预构建 set，避免逐行线性查找；
	// 空集合（非 nil）表示无匹配行，与 DB 端 IN () 语义一致
	var appIds map[string]bool
	if f.ApolloAppIdIn != nil {
		appIds = make(map[string]bool, len(f.ApolloAppIdIn))
		for _, a := range f.ApolloAppIdIn {
			appIds[a] = true
		}
	}
	// 只扫描目标 date/env 的分桶；列表分页（pageSize>0）组内倒序（文件行序 = DB id asc
	// → 倒序 ≈ id desc，与 DB 分页一致）；不分页（graph/detail 聚合）按文件行序正序输出，
	// 与 DB 端显式 Order("id") 保持一致，保证两源聚合输入顺序完全相同
	inScope := func(g dateEnvGroup) bool {
		return (f.SyncDate == "" || g.date == f.SyncDate) && (f.Env == "" || g.env == f.Env)
	}
	paged := pageSize > 0
	if paged {
		// 列表分页：单趟窗口收集，只保留当前页行，避免把整批匹配行先全量拷进内存再切片
		rows := make([]model.OperationTopology, 0, pageSize)
		skip, total := (page-1)*pageSize, 0
		for _, g := range s.groups {
			if !inScope(g) {
				continue
			}
			for i := len(g.rows) - 1; i >= 0; i-- {
				if matchRow(g.rows[i], f, appIds) {
					if total >= skip && len(rows) < pageSize {
						rows = append(rows, g.rows[i])
					}
					total++
				}
			}
		}
		return QueryResult{Rows: rows, Total: int64(total)}, nil
	}
	// 不分页（graph/detail 聚合用）：先按组内行数预分配容量，再单趟正序收集（id asc）
	capacity := 0
	for _, g := range s.groups {
		if inScope(g) {
			capacity += len(g.rows)
		}
	}
	rows := make([]model.OperationTopology, 0, capacity)
	for _, g := range s.groups {
		if !inScope(g) {
			continue
		}
		for i := 0; i < len(g.rows); i++ {
			if matchRow(g.rows[i], f, appIds) {
				rows = append(rows, g.rows[i])
			}
		}
	}
	return QueryResult{Rows: rows, Total: int64(len(rows))}, nil
}

func (s *FileStore) LatestSyncDate(env string) string {
	s.ensure()
	for _, d := range s.dates {
		if env == "" {
			return d
		}
		for _, g := range s.groups {
			if g.date == d && g.env == env {
				return d
			}
		}
	}
	return ""
}

func (s *FileStore) SyncDates(env string) []string {
	s.ensure()
	if env == "" {
		return s.dates
	}
	out := make([]string, 0, len(s.dates))
	for _, d := range s.dates {
		for _, g := range s.groups {
			if g.date == d && g.env == env {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

func (s *FileStore) MiddlewareTypes() []string {
	s.ensure()
	set := make(map[string]bool)
	for _, g := range s.groups {
		for _, r := range g.rows {
			if r.MiddlewareType != "" {
				set[r.MiddlewareType] = true
			}
		}
	}
	types := make([]string, 0, len(set))
	for t := range set {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// matchRow 行级过滤（与 DBStore 的 WHERE 语义一一对应）；
// appIds 非 nil 时按 apollo_app_id 集合过滤（关系链翻译结果，见 dp_relation.go）
func matchRow(r model.OperationTopology, f RowFilter, appIds map[string]bool) bool {
	if f.SyncDate != "" && r.SyncDate != f.SyncDate {
		return false
	}
	if f.Env != "" && r.Env != f.Env {
		return false
	}
	if f.Node != "" && r.Node != f.Node {
		return false
	}
	if f.MiddlewareType != "" && r.MiddlewareType != f.MiddlewareType {
		return false
	}
	if f.DepProject != "" && !strings.Contains(r.DepProject, f.DepProject) {
		return false
	}
	if f.ApolloAppId != "" && r.ApolloAppId != f.ApolloAppId {
		return false
	}
	if f.ApolloAppName != "" && !strings.Contains(r.ApolloAppName, f.ApolloAppName) {
		return false
	}
	if f.ApolloHostName != "" && !strings.Contains(r.ApolloHostName, f.ApolloHostName) {
		return false
	}
	if f.AppDept != "" && !strings.Contains(r.AppDept, f.AppDept) {
		return false
	}
	if f.AppOwner != "" && !strings.Contains(r.AppOwner, f.AppOwner) {
		return false
	}
	if f.ConnectAddr != "" && !strings.Contains(r.ConnectAddr, f.ConnectAddr) {
		return false
	}
	if f.ApolloHostId > 0 && r.ApolloHostId != f.ApolloHostId {
		return false
	}
	// apollo_app_id 集合过滤：appIds 非 nil 才有意义（nil = 未启用该条件）
	if appIds != nil && !appIds[r.ApolloAppId] {
		return false
	}
	return true
}

// ---------------------------------------------------------
// 数据源路由：file（默认，唯一生效）+ db/auto（DB 后续接入分支）
// ---------------------------------------------------------

// datastoreCfg 数据源内部配置（模块内聚，不依赖外部 config/datastore 段）。
// 现阶段固定 mode=file：读与同步都只使用本地快照文件；
// 数据库（db/auto）为后续能力，通过 InitDatastoreWithDB 注入连接后启用。
type datastoreCfg struct {
	mode    string // file（默认）| db | auto
	dataDir string
}

type DatastoreRouter struct {
	mu        sync.RWMutex
	mode      string // file（默认）| db | auto
	dataDir   string
	db        *gorm.DB  // 后续 DB 接入时注入（默认 nil）
	dbStore   RowSource // 后续 DB 接入时创建（默认 nil，未注入一律回退本地文件）
	fileStore *FileStore
	dbHealthy bool // 当前 DB 是否健康（db/auto 分支有效）
	lastPing  time.Time
	degraded  time.Time // 最近一次降级/失败时刻
	active    string    // 当前活跃源（响应头/日志用）
}

// newDatastoreRouter 构造数据源路由（内部使用；主流程只走 file）
func newDatastoreRouter(cfg datastoreCfg, db *gorm.DB) *DatastoreRouter {
	r := &DatastoreRouter{
		mode: cfg.mode, dataDir: cfg.dataDir, db: db,
		fileStore: newFileStore(cfg.dataDir), active: sourceFile,
	}
	if db != nil {
		r.dbStore = &DBStore{db: db}
		if cfg.mode != sourceFile {
			// db/auto：启动探测 DB，失败立即进入文件降级态（冷却后 resolve 自动重试）
			r.dbHealthy = pingDB(db, pingTimeout)
			if r.dbHealthy {
				r.lastPing = time.Now()
			} else {
				r.degraded = time.Now()
				tplog.L().Warn("[datastore] 数据库不可达，读走本地文件数据源",
					zap.Duration("retryAfter", retryAfter))
			}
		}
	}
	if cfg.mode == sourceFile || cfg.mode == "auto" {
		r.fileStore.Available() // 提前加载，尽早暴露文件缺失/损坏问题
	}
	return r
}

// pingDB 对 gorm 底层连接做一次带超时的健康探测
func pingDB(db *gorm.DB, timeout time.Duration) bool {
	if db == nil {
		return false
	}
	sqlDB, err := db.DB()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return sqlDB.PingContext(ctx) == nil
}

// InitDatastore 初始化数据源路由（幂等，模块内聚，无外部依赖/无参数）。
// 当前默认即主流程形态：读接口与同步都只使用本地快照文件，不依赖数据库。
// 由 service 首次读写懒初始化即可，无需在模块外显式注册。
func InitDatastore() *DatastoreRouter {
	storeMu.Lock()
	defer storeMu.Unlock()
	if storeRouter != nil {
		return storeRouter
	}
	storeRouter = newDatastoreRouter(datastoreCfg{mode: sourceFile, dataDir: defaultDataDir}, nil)
	tplog.L().Info("[datastore] 数据源就绪（file 模式：读/写均走本地快照，不依赖数据库）",
		zap.String("file", storeRouter.fileStore.path))
	return storeRouter
}

// InitDatastoreWithDB 数据库版接入入口（保留，后续 DB 实现时启用）：
// 注入 gorm 连接后，mode=db 固定读数据库；mode=auto 则 DB 优先、故障自动降级本地文件。
func InitDatastoreWithDB(db *gorm.DB, mode string) *DatastoreRouter {
	storeMu.Lock()
	defer storeMu.Unlock()
	if storeRouter != nil {
		return storeRouter
	}
	if db == nil {
		tplog.L().Warn("[datastore] 初始化跳过：数据库句柄为空")
		return nil
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != sourceDB && mode != "auto" {
		mode = sourceFile
	}
	storeRouter = newDatastoreRouter(datastoreCfg{mode: mode, dataDir: defaultDataDir}, db)
	return storeRouter
}

// currentRouter 懒初始化（service 读/写函数统一入口；file 模式下无需外部注册）
func currentRouter() *DatastoreRouter {
	if storeRouter != nil {
		return storeRouter
	}
	return InitDatastore()
}

// probeNow DB 健康探测（1 次，保留给 db/auto 分支）
func (r *DatastoreRouter) probeNow() bool {
	return pingDB(r.db, pingTimeout)
}

// resolve 选择本次请求实际使用的数据源。
// prefer 为请求级覆盖（source=db / source=file，空 = 按配置 mode 走）：
//   - mode=file（默认）：默认读本地文件；prefer=db 时本次改用数据库，
//     DB 不可达则静默回退本地文件；
//   - mode=db：固定读数据库；
//   - mode=auto：DB 优先，故障自动降级文件，冷却后自动切回。
func (r *DatastoreRouter) resolve(prefer string) RowSource {
	if r == nil {
		return nil
	}
	prefer = strings.ToLower(strings.TrimSpace(prefer))
	if prefer != sourceDB && prefer != sourceFile {
		prefer = ""
	}
	// 未注入 DB 连接（dbStore==nil，当前默认形态）：一律走本地文件
	if r.dbStore == nil {
		r.setActive(sourceFile)
		return r.fileStore
	}
	switch r.mode {
	case sourceDB:
		r.setActive(sourceDB)
		return r.dbStore
	case sourceFile:
		if prefer == sourceDB && r.dbReachable() {
			r.setActive(sourceDB)
			return r.dbStore
		}
		r.setActive(sourceFile)
		return r.fileStore
	}
	// auto：定时探测 + 冷却重试（prefer=file 且文件可用时本次直接读文件）
	r.mu.Lock()
	if prefer == sourceFile && r.fileStore != nil && r.fileStore.Available() {
		r.active = sourceFile
		r.mu.Unlock()
		return r.fileStore
	}
	if r.dbHealthy && time.Since(r.lastPing) >= probeInterval {
		if r.probeNow() {
			r.dbHealthy, r.lastPing = true, time.Now()
		} else {
			r.dbHealthy, r.degraded = false, time.Now()
			tplog.L().Warn("[datastore] DB 探测失败，切换本地文件数据源",
				zap.Duration("retryAfter", retryAfter))
		}
	} else if !r.dbHealthy && time.Since(r.degraded) >= retryAfter {
		if r.probeNow() {
			r.dbHealthy, r.lastPing = true, time.Now()
			tplog.L().Info("[datastore] 数据库已恢复，切回数据库数据源")
		} else {
			r.degraded = time.Now()
		}
	}
	if r.dbHealthy {
		r.active = sourceDB
		r.mu.Unlock()
		return r.dbStore
	}
	r.active = sourceFile
	r.mu.Unlock()
	return r.fileStore
}

// dbReachable DB 健康快判（带冷却）：健康且近期探测过直接复用结果；
// 故障冷却期内直接返回 false（避免 DB 故障期每个 source=db 请求都打 2s 探测）
func (r *DatastoreRouter) dbReachable() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbHealthy && time.Since(r.lastPing) < probeInterval {
		return true
	}
	if !r.dbHealthy && time.Since(r.degraded) < retryAfter {
		return false
	}
	ok := r.probeNow()
	r.dbHealthy = ok
	if ok {
		r.lastPing = time.Now()
		r.degraded = time.Time{}
	} else {
		r.degraded = time.Now()
	}
	return ok
}

// otherSource 返回与当前源互补的另一数据源（跨源兜底用）
func (r *DatastoreRouter) otherSource(src RowSource) RowSource {
	if r == nil || src == nil {
		return nil
	}
	if src.Name() == sourceFile {
		return r.dbStore
	}
	if src.Name() == sourceDB && r.fileStore != nil && r.fileStore.Available() {
		return r.fileStore
	}
	return nil
}

func (r *DatastoreRouter) setActive(s string) {
	r.mu.Lock()
	r.active = s
	r.mu.Unlock()
}

// DataDir 本地快照目录（启动日志展示用）
func (r *DatastoreRouter) DataDir() string {
	if r == nil {
		return ""
	}
	return r.dataDir
}

// Mode 当前数据源模式（file|db|auto，启动日志展示用）
func (r *DatastoreRouter) Mode() string {
	if r == nil {
		return ""
	}
	return r.mode
}

func (r *DatastoreRouter) onDBError() {
	r.mu.Lock()
	if r.dbHealthy {
		r.dbHealthy = false
		r.degraded = time.Now()
		tplog.L().Warn("[datastore] DB 查询连接错误，自动降级本地文件数据源",
			zap.Duration("retryAfter", retryAfter))
	}
	r.mu.Unlock()
}

// fallbackFile 连接错误后尝试本地文件兜底
func (r *DatastoreRouter) fallbackFile(f RowFilter) (QueryResult, bool) {
	if r.fileStore == nil || !r.fileStore.Available() {
		return QueryResult{}, false
	}
	res, err := r.fileStore.Query(f)
	if err != nil {
		return QueryResult{}, false
	}
	r.setActive(sourceFile)
	return res, true
}

// ListRows 关系列表读取（数据源路由包装，返回活跃源名）
func (r *DatastoreRouter) ListRows(f RowFilter) (QueryResult, string, error) {
	src := r.resolve(f.Source)
	res, err := src.Query(f)
	if err != nil && isConnErr(err) && r.canFallback(f.Source) {
		r.onDBError()
		if fb, ok := r.fallbackFile(f); ok {
			return fb, sourceFile, nil
		}
	}
	if err != nil {
		return res, src.Name(), err
	}
	r.setActive(src.Name())
	return res, src.Name(), nil
}

// canFallback 连接错误是否允许回退本地文件（file 模式 source=db 覆盖请求有效）
func (r *DatastoreRouter) canFallback(prefer string) bool {
	if r.mode == "auto" {
		return true
	}
	return r.mode == sourceFile && prefer == sourceDB
}

// QueryAll 不分页取全部匹配行（graph/detail 聚合用）
func (r *DatastoreRouter) QueryAll(f RowFilter) ([]model.OperationTopology, string, error) {
	f.Page, f.PageSize = 1, -1
	q, src, err := r.ListRows(f)
	if err != nil {
		return nil, src, err
	}
	return q.Rows, src, nil
}

// LatestSyncDateOf 最新快照日期（数据源路由包装；prefer 请求级覆盖）
func (r *DatastoreRouter) LatestSyncDateOf(env, prefer string) (string, string) {
	src := r.resolve(prefer)
	if v := src.LatestSyncDate(env); v != "" {
		r.setActive(src.Name())
		return v, src.Name()
	}
	// 当前源无数据 → auto 模式，或 file 模式下显式 source=db 但 DB 恰无数据时，
	// 尝试另一源兜底，防止“刚好换源/启动早期文件未生成”返回空
	if r.mode == "auto" || (r.mode == sourceFile && prefer == sourceDB) {
		if other := r.otherSource(src); other != nil {
			if v := other.LatestSyncDate(env); v != "" {
				r.setActive(other.Name())
				return v, other.Name()
			}
		}
	}
	return "", src.Name()
}

// TypesOf 中间件类型枚举（数据源路由包装；prefer 请求级覆盖）
func (r *DatastoreRouter) TypesOf(prefer string) ([]string, string) {
	src := r.resolve(prefer)
	types := src.MiddlewareTypes()
	r.setActive(src.Name())
	// 同 LatestSyncDateOf：仅 auto / file+source=db 场景允许跨源兜底
	if len(types) == 0 && (r.mode == "auto" || (r.mode == sourceFile && prefer == sourceDB)) {
		if other := r.otherSource(src); other != nil {
			if v := other.MiddlewareTypes(); len(v) > 0 {
				r.setActive(other.Name())
				return v, other.Name()
			}
		}
	}
	return types, src.Name()
}

// ---------------------------------------------------------
// 快照直写（同步直写本地文件 · 按日期保留最近 N 份供前后对比）
// ---------------------------------------------------------

// snapshotKeep 拓扑快照保留份数（按 sync_date 分桶；配置未填 / <=0 一律取默认 3 份）
func snapshotKeep() int {
	if n := global.GVA_CONFIG.Apollo.TopologySnapshotKeep; n > 0 {
		return n
	}
	return defaultSnapshotKeep
}

// ReplaceSnapshot 同步直写：用一批行替换当日快照（原子写 tmp+rename）并重载内存。
// rows 必须同一 sync_date（同步解析产物）；历史日期桶保留最近 N 份（topology_snapshot_keep，默认 3），
// 更早的桶清理，既能前后对比又不会无限堆积。
// 已注入 DB 连接（InitDatastoreWithDB，数据库后续接入入口）时，追加一次镜像落库（同口径裁剪），
// 保证 DB 源与文件源数据一致；当前 file 模式 db==nil，只写文件不碰数据库。
func (r *DatastoreRouter) ReplaceSnapshot(rows []model.OperationTopology) error {
	if r == nil || r.fileStore == nil {
		return errors.New("本地文件存储未初始化，无法写快照")
	}
	keep := snapshotKeep()
	if err := r.fileStore.Replace(rows, keep); err != nil {
		return err
	}
	return r.mirrorWriteDB(rows, keep)
}

// mirrorWriteDB 数据库镜像写（保留给后续 DB 接入阶段；未注入 DB 连接时跳过）。
// 语义：先删当日快照再全量批量插入（幂等重跑），随后只保留最近 keep 个 sync_date 桶，
// 更早的历史快照删除，避免表内随每日同步无限堆积。
func (r *DatastoreRouter) mirrorWriteDB(rows []model.OperationTopology, keep int) error {
	if r.db == nil || len(rows) == 0 {
		return nil
	}
	syncDate := rows[0].SyncDate
	tx := r.db.Begin()
	if err := tx.Where("sync_date = ?", syncDate).Delete(&model.OperationTopology{}).Error; err != nil {
		tx.Rollback()
		return err
	}
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := tx.Create(rows[i:end]).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	// 保留最近 keep 个 sync_date 桶（含本次），更早的整桶删除
	var dropped int64
	if keep > 0 {
		var kept []string
		if err := tx.Model(&model.OperationTopology{}).
			Distinct("sync_date").Order("sync_date desc").Limit(keep).
			Pluck("sync_date", &kept).Error; err != nil {
			tx.Rollback()
			return err
		}
		if len(kept) == keep {
			res := tx.Where("sync_date NOT IN ?", kept).Delete(&model.OperationTopology{})
			if res.Error != nil {
				tx.Rollback()
				return res.Error
			}
			dropped = res.RowsAffected
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	tplog.L().Info("[datastore] 快照镜像落库完成",
		zap.String("syncDate", syncDate), zap.Int("rows", len(rows)),
		zap.Int("keep", keep), zap.Int64("droppedRows", dropped))
	return nil
}

// PreviousSnapshot 当前内存快照中“除 exclude 日期外”最近一次的行（同步对比日志用）。
// 文件按日期保留最近 N 份快照，可拿到上一版（乃至更早）数据用于前后对比。
func (r *DatastoreRouter) PreviousSnapshot(exclude string) (string, []model.OperationTopology) {
	if r == nil || r.fileStore == nil {
		return "", nil
	}
	return r.fileStore.Previous(exclude)
}

// SnapshotCount 当前内存快照中指定日期的总行数（本次同步无结果时的 kept=N 日志）
func (r *DatastoreRouter) SnapshotCount(date string) int {
	if r == nil || r.fileStore == nil {
		return 0
	}
	return r.fileStore.CountOf(date)
}
