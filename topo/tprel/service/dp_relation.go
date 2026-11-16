package service

// =========================================================
// 跳① 关联层：apollo_app_id ↔ 部署项目（app_dp_relation）
//
// 关联链（《运维拓扑.md》§3.3、《拓扑查询总体逻辑.md》§2.4）：
//
//	operation_topology.apollo_app_id
//	  --跳① 关系接口 / app_dp_relation 表--> deploy_project_name
//	  --跳② deploy_pinpoint_relation--> pp_application / node / eg_project
//
// 设计：
//   - 采集期（与拓扑主表同一节奏：cron 每日 01:30 / 手动 /sync）由 SyncAppDpRelation 调关系接口
//     一次拉取全量（paginator=false + pageSize=-1）→ 与库内比对，有变化才整表替换落库 app_dp_relation（无变化跳过）→ 回写本地镜像 → 失效内存索引；
//     接口未启用 / 拉取失败 / 返回 0 行时一律保留上一版关系数据（不覆盖）；
//   - 查询期（dpTargets）只读「库 → 本地镜像 data/app_dp_relation.json」，接口仅在前两者均无数据时兜底，
//     故查询链路不再依赖上游接口可用性；均不可用时置空并记日志（读接口按原行返回）；
//   - 跳② 沿用 pinpoint_relation.go 的 DPR 内存映射，两跳在内存中一次 join 成
//     「apollo_app_id|环境 → 属性项列表」索引，带 TTL 缓存（默认 60s，配置 relation_api_ttl 可调）；
//   - 属性补充（attachAppAttrs，折叠模式 / expandDepNode=false）：拓扑数据集粒度恒为「appId × 中间件」，
//     两跳命中数据只作为 appId 组件的属性补充；属性多值时聚合为列表
//     （dep_projects / eng_projects / nodes / pp_applications），单值列取属性首值 —— 行数与行序不变，
//     绝不按属性组合把关系行展开（原 expandRows「一行 → N 行」已废弃，避免关系数据集被属性扩维）；
//   - 部署点展开（expandByDeploySlot，展开模式 / expandDepNode=true）：「部署项目 + 节点」是应用节点的
//     唯一条件，一行按该 appId 的有效部署点复制为 N 行（N = 部署点个数），dep_project/node 单值。
//     展开只在查询期进行；拓扑表唯一索引不含 dep_project/node，落库展开会撞索引；
//   - 环境口径以接口字段 apolloAppEnv（PRO/LPT 字符串）为准，与拓扑表 env 精确对齐；
//   - 过滤下推：部署项目（模糊）/节点（精确）条件经本层索引翻译成 apollo_app_id 集合下推，
//     属性补充后复核。折叠模式复核粒度是 appId（该 appId 属性任一命中即保留其全部行，见 filterAttachRows）；
//     展开模式复核粒度是部署点（按行的单值 dep_project/node 判定，只保留命中的那个部署点的行，见 filterSlotRows）。
// =========================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"devops-automation/api/tprel/model"
	"devops-automation/api/tprel/tplog"
	"devops-automation/config"
	"devops-automation/global"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// relationMirrorName 跳① 关系数据本地镜像文件名（接口与关系表均不可用时的兜底）
	relationMirrorName = "app_dp_relation.json"
	// relationSourceAPI 数据来源标识：关系接口
	relationSourceAPI = "api"

	// 以下为配置缺省值（config 未填时生效）
	relationDefaultTTL      = 60  // 内存索引刷新周期（秒）
	relationFullPageSize    = -1  // pageSize=-1：不分页、一次取全量（上游 paginator=false 直接返回全表）
	relationPagedPageSize   = 200 // 上游真分页时的兜底每页条数
	relationDefaultMaxPages = 200 // 分页兜底上限（仅上游未一次返回全量时生效）
)

var (
	relationMu       sync.Mutex
	relationDB       *gorm.DB
	relationIndex    map[string][]dpTarget // apollo_app_id|env → 属性项列表（跳①×跳② join 结果）
	relationLoadedAt time.Time
	relationOK       bool   // 上次是否成功加载（用于抑制重复告警）
	relationSource   string // 本次索引数据来源：api|db|file（空 = 无可用来源）
	relationRows     int    // 本次索引的跳① 关系行数（日志用）

	// relationLoadMu 串行化索引加载：TTL 失效瞬间多个请求同时到达时，避免都去做一次
	// 全表读 + 镜像写（惊群）。仅第一个请求加载，其余等在缓存上。
	relationLoadMu sync.Mutex
	// relationMirrorMu 串行化镜像文件写入：同步任务与查询路径可能并发刷新镜像，
	// 共用一个 tmp 文件名会互相覆盖，写出损坏内容。
	relationMirrorMu sync.Mutex
)

// dpTarget 跳①×跳② 关联后的一个「appId 属性项」：一次属性组合（部署项目 + 其 DPR 归属）。
// 跳②（DPR）未命中该部署项目时仅带 DepProject，其余三列留空。
// 注：属性项不再用于「展开行」，只用于把 appId 的多值属性聚合为列表（见 attachAppAttrs）。
type dpTarget struct {
	DepProject    string
	EgProject     string
	Node          string
	PpApplication string
}

// relationRow 跳① 关系行（本层只需 appId/环境/部署项目 三列，与本地镜像文件同构）。
type relationRow struct {
	ApolloAppId       string `json:"apolloAppId"`
	ApolloAppEnv      string `json:"apolloAppEnv"`
	DeployProjectName string `json:"deployProjectName"`
	Node              string `json:"node,omitempty"`
}

// relationMirrorFile 本地镜像文件结构
type relationMirrorFile struct {
	Version    int           `json:"version"`
	ExportedAt string        `json:"exportedAt"`
	TotalRows  int           `json:"totalRows"`
	Rows       []relationRow `json:"rows"`
}

// InitDpRelation 注入关系表数据源（启动时调用；db 可为 nil，此时走关系接口/本地镜像）。
func InitDpRelation(db *gorm.DB) {
	relationMu.Lock()
	defer relationMu.Unlock()
	relationDB = db
	relationIndex, relationLoadedAt, relationOK, relationSource = nil, time.Time{}, false, ""
}

// relationMirrorPath 本地镜像文件路径
func relationMirrorPath() string {
	return filepath.Join(defaultDataDir, relationMirrorName)
}

// RelationSyncStat 跳① 关系同步结果（随 /sync 返回与日志总览展示）
type RelationSyncStat struct {
	Rows   int    `json:"rows"`   // 接口拉取并过滤 del_flag 后的关系行数
	DBRows int    `json:"dbRows"` // app_dp_relation 表内行数（无变化跳过时为现有行数；0 = 未落库）
	Wrote  bool   `json:"wrote"`  // 是否执行了「清空 + 重写」（数据无变化时 false，属正常结果）
	Err    bool   `json:"err"`    // 是否属于「异常」（上游拉取失败 / 关系表缺失或写库失败），需运维介入
	Msg    string `json:"msg"`    // 结果说明（跳过/失败原因）
}

// relationSyncText 关系同步结果文案（日志结果总览用）。异常项加前缀，便于在总览里一眼定位。
func relationSyncText(rows int, wrote, isErr bool, msg string) string {
	if msg != "" {
		if isErr {
			return "【异常】" + msg
		}
		return msg
	}
	if wrote {
		return fmt.Sprintf("已整表替换落库 %d 行（app_dp_relation）", rows)
	}
	return fmt.Sprintf("拉取 %d 行，未落库（已写本地镜像）", rows)
}

// SyncAppDpRelation 采集期全量同步跳① 关系（appId ↔ 部署项目）：
// 关系接口分页拉取全量 → 与库内比对，有变化才整表替换落库 app_dp_relation（无变化跳过）→
// 回写本地镜像 → 失效内存索引。
// 与拓扑主表同步同节奏（cron 每日 01:30 / 手动 /sync），保证查询期只读库/镜像。
// 任何一步失败（接口未启用/拉取失败/0 行/无表）都不覆盖现有关系数据，仅记录说明。
func SyncAppDpRelation() RelationSyncStat {
	cfg := global.GVA_CONFIG.Apollo
	if !cfg.RelationEnabled() || cfg.RelationApiPath == "" {
		msg := "关系接口未启用（relation_api_enable=false 或 relation_api_path 为空），保留现有关系数据"
		tplog.L().Info("[dp_relation] 跳① 关系同步跳过", zap.String("reason", msg))
		return RelationSyncStat{Msg: msg}
	}
	items, err := fetchRelationDTOs(cfg)
	if err != nil {
		msg := "关系接口拉取失败，保留现有关系数据: " + err.Error()
		tplog.L().Warn("[dp_relation] 跳① 关系同步失败", zap.String("path", cfg.RelationApiPath), zap.Error(err))
		return RelationSyncStat{Err: true, Msg: msg}
	}
	rows := make([]relationRow, 0, len(items))
	dbRows := make([]model.AppDpRelation, 0, len(items))
	for _, it := range items {
		if it.DelFlag || it.ApolloAppId == "" || it.DeployProjectName == "" {
			continue
		}
		rows = append(rows, relationRow{
			ApolloAppId: it.ApolloAppId, ApolloAppEnv: it.ApolloAppEnv,
			DeployProjectName: it.DeployProjectName, Node: it.Node,
		})
		dbRows = append(dbRows, toAppDpRelation(it))
	}
	if len(rows) == 0 {
		msg := "关系接口返回 0 行有效关系，保留现有关系数据（不覆盖）"
		tplog.L().Warn("[dp_relation] 跳① 关系同步跳过", zap.String("reason", msg))
		return RelationSyncStat{Msg: msg}
	}

	writeRelationMirror(rows)
	stat := RelationSyncStat{Rows: len(rows)}
	var dbErr string
	var unchanged bool
	stat.DBRows, stat.Wrote, unchanged, dbErr = replaceRelationDB(dbRows)
	invalidateRelationIndex()

	switch {
	case stat.Wrote:
		stat.Msg = fmt.Sprintf("整表替换落库 %d 行", stat.DBRows)
	case unchanged:
		// 与库内数据完全一致：不做清空重写（正常结果，非异常）
		stat.Msg = fmt.Sprintf("关系数据无变化，跳过替换落库（表内 %d 行）", stat.DBRows)
	case dbErr != "":
		// 关系表缺失 / 写库失败：属异常，需运维介入；本次查询走本地镜像兜底
		stat.Err = true
		stat.Msg = dbErr + "；本次仅写本地镜像"
	default:
		stat.Msg = "未落库（无 DB 连接），仅写本地镜像"
	}
	tplog.L().Info("[dp_relation] 跳① 关系同步完成",
		zap.Int("rows", stat.Rows), zap.Int("dbRows", stat.DBRows),
		zap.Bool("wrote", stat.Wrote), zap.Bool("err", stat.Err), zap.String("msg", stat.Msg))
	return stat
}

// replaceRelationDB 关系表落库：仅当本次拉取结果与库内现有数据不一致时才「清空 + 重写」，
// 无变化直接跳过（避免每次同步都重建整表，表内不产生无谓膨胀）。
// 返回值：rows = 本次写入（无变化时为库内现有）行数；wrote = 是否执行了清空重写；
// unchanged = 数据无变化已跳过；errMsg 非空表示「异常」（关系表缺失 / 读库失败 / 写库失败），需运维介入；
// 无 DB 连接返回 (0,false,false,"")（属正常降级，交由本地镜像兜底）。
func replaceRelationDB(rows []model.AppDpRelation) (int, bool, bool, string) {
	relationMu.Lock()
	db := relationDB
	relationMu.Unlock()
	if db == nil || len(rows) == 0 {
		return 0, false, false, ""
	}
	// 表由 AutoMigrate 自动创建（initialize/gorm.go 的 MysqlTables）；缺失即视为异常并上报
	if !db.Migrator().HasTable(&model.AppDpRelation{}) {
		msg := "关系表 app_dp_relation 不存在，未落库（启动时 AutoMigrate 未建表，请检查 DB 连接与权限）"
		tplog.L().Warn("[dp_relation] " + msg)
		return 0, false, false, msg
	}
	// 变化检测：读取库内现有数据与本次拉取结果比对（同一批 = 跳过，不做任何写操作）
	var existing []model.AppDpRelation
	if err := db.Find(&existing).Error; err != nil {
		msg := "关系表现有数据读取失败，未落库（保留原有数据）: " + err.Error()
		tplog.L().Warn("[dp_relation] " + msg)
		return 0, false, false, msg
	}
	if relationRowsEqual(existing, rows) {
		tplog.L().Info("[dp_relation] 关系数据无变化，跳过替换落库",
			zap.Int("rows", len(rows)), zap.Int("dbRows", len(existing)))
		return len(rows), false, true, ""
	}
	tx := db.Begin()
	if tx.Error != nil {
		msg := "关系表落库事务开启失败，未落库: " + tx.Error.Error()
		tplog.L().Warn("[dp_relation] " + msg)
		return 0, false, false, msg
	}
	if err := tx.Where("1 = 1").Delete(&model.AppDpRelation{}).Error; err != nil {
		tx.Rollback()
		msg := "关系表清空失败，未落库（保留原有数据）: " + err.Error()
		tplog.L().Warn("[dp_relation] " + msg)
		return 0, false, false, msg
	}
	const batchSize = 500
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := tx.Create(rows[i:end]).Error; err != nil {
			tx.Rollback()
			msg := "关系表写入失败，已回滚（保留原有数据）: " + err.Error()
			tplog.L().Warn("[dp_relation] " + msg)
			return 0, false, false, msg
		}
	}
	if err := tx.Commit().Error; err != nil {
		msg := "关系表落库提交失败，未落库: " + err.Error()
		tplog.L().Warn("[dp_relation] " + msg)
		return 0, false, false, msg
	}
	return len(rows), true, false, ""
}

// relationRowsEqual 判定库内现有关系行与本次拉取结果是否为同一批（多重集比较，行序无关）。
// 只比业务列，时间列（create_time/update_time）不参与：上游仅刷新时间戳不代表关系变化。
func relationRowsEqual(existing, incoming []model.AppDpRelation) bool {
	if len(existing) != len(incoming) {
		return false
	}
	a := make([]string, 0, len(existing))
	for _, r := range existing {
		a = append(a, relationRowSignature(r))
	}
	b := make([]string, 0, len(incoming))
	for _, r := range incoming {
		b = append(b, relationRowSignature(r))
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// relationRowSignature 关系行业务列签名（变化检测用；分隔符取不可见字符避免字段值拼接歧义）
func relationRowSignature(r model.AppDpRelation) string {
	return fmt.Sprintf("%d\x1f%d\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%d\x1f%d\x1f%s\x1f%s\x1f%s\x1f%d\x1f%s\x1f%d",
		r.Id, r.DelFlag, r.ProjectType, r.ProjectId, r.ProjectName,
		r.DeployProjectId, r.DeployProjectName, r.Node, r.Env, r.ApolloHostId,
		r.ApolloAppId, r.ApolloAppEnv, r.ApolloAppCluster, r.IsReleased, r.Operation, r.Operator)
}

// toAppDpRelation 接口行 → 表模型（bool→0/1，时间字符串→*time.Time）
func toAppDpRelation(it AppDpRelationRow) model.AppDpRelation {
	return model.AppDpRelation{
		Id:      it.Id,
		DelFlag: boolToInt(it.DelFlag),
		// 时间列可空：解析失败置 NULL，避免写入 0000-00-00 被严格模式拒绝
		CreateTime:        parseRelationTime(it.CreateTime),
		UpdateTime:        parseRelationTime(it.UpdateTime),
		ProjectType:       it.ProjectType,
		ProjectId:         it.ProjectId,
		ProjectName:       it.ProjectName,
		DeployProjectId:   it.DeployProjectId,
		DeployProjectName: it.DeployProjectName,
		Node:              it.Node,
		Env:               it.Env,
		ApolloHostId:      it.ApolloHostId,
		ApolloAppId:       it.ApolloAppId,
		ApolloAppEnv:      it.ApolloAppEnv,
		ApolloAppCluster:  it.ApolloAppCluster,
		IsReleased:        boolToInt(it.IsReleased),
		Operation:         it.Operation,
		Operator:          it.Operator,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// parseRelationTime 解析接口时间字符串（"2006-01-02 15:04:05"）；空/非法返回 nil
func parseRelationTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		return nil
	}
	return &t
}

// invalidateRelationIndex 失效查询期内存索引（同步落库后调用，下次查询重建）
func invalidateRelationIndex() {
	relationMu.Lock()
	defer relationMu.Unlock()
	relationIndex, relationLoadedAt, relationOK, relationSource, relationRows = nil, time.Time{}, false, "", 0
}

// relationKeyOf 跳①/展开的索引键：apollo_app_id + 环境（apolloAppEnv 字符串口径）
func relationKeyOf(appId, env string) string {
	return appId + "|" + env
}

// dpTargets 返回 索引键 → 属性项列表 的映射（带 TTL 缓存）。
// ok=false 表示跳① 无可用数据源，调用方需按原行返回。
func dpTargets() (map[string][]dpTarget, bool) {
	cfg := global.GVA_CONFIG.Apollo
	ttl := time.Duration(cfg.RelationApiTTL) * time.Second
	if ttl <= 0 {
		ttl = relationDefaultTTL * time.Second
	}
	if index, ok, hit := cachedDpTargets(ttl); hit {
		return index, ok
	}

	// 加载串行化：TTL 失效瞬间多个并发请求会同时走到这里，若都去加载就会「惊群」
	// （重复全表读 + 重复写镜像，并发压测实测同一秒内重复加载 8 次）。
	// 拿到加载锁后再查一次缓存，只有第一个请求真正加载，其余直接复用结果。
	relationLoadMu.Lock()
	defer relationLoadMu.Unlock()
	if index, ok, hit := cachedDpTargets(ttl); hit {
		return index, ok
	}

	// 加载必须在 relationMu 之外进行：loadRelationRows 内部（loadRelationFromDB）
	// 会再次获取 relationMu，sync.Mutex 不可重入，持锁调用会自锁死（查询接口永久阻塞）。
	rows, src := loadRelationRows(cfg)
	index, ok := joinDpTargets(rows), src != ""

	relationMu.Lock()
	relationRows, relationSource = len(rows), src
	relationIndex, relationOK = index, ok
	relationLoadedAt = time.Now()
	relationMu.Unlock()

	tplog.L().Info("[dp_relation] appId↔部署项目 关系索引已加载",
		zap.String("source", src), zap.Int("rows", len(rows)),
		zap.Int("apps", len(index)), zap.Bool("ok", ok))
	return index, ok
}

// cachedDpTargets 读取未过期的内存索引；hit=false 表示缓存未命中（需重新加载）。
// 索引一经构建不再原地修改（失效时整体置 nil），故可在锁外安全读取返回的 map。
func cachedDpTargets(ttl time.Duration) (map[string][]dpTarget, bool, bool) {
	relationMu.Lock()
	defer relationMu.Unlock()
	if relationIndex != nil && time.Since(relationLoadedAt) < ttl {
		return relationIndex, relationOK, true
	}
	return nil, false, false
}

// loadRelationRows 查询期加载跳① 关系行：库 → 本地镜像 → 关系接口兜底（仅首次、库与镜像均无数据时）。
// 均不可用时返回 nil（读接口按原行返回）。
func loadRelationRows(cfg config.Apollo) ([]relationRow, string) {
	if rows := loadRelationFromDB(); len(rows) > 0 {
		writeRelationMirror(rows)
		return rows, sourceDB
	}
	if rows := loadRelationFromFile(); len(rows) > 0 {
		return rows, sourceFile
	}
	// 库与镜像均无数据（首次启动且尚未同步过）：关系接口兜底，保证首次可用
	if cfg.RelationEnabled() && cfg.RelationApiPath != "" {
		rows, err := fetchRelationRows(cfg)
		if err == nil && len(rows) > 0 {
			return rows, relationSourceAPI
		}
		if err != nil {
			tplog.L().Warn("[dp_relation] 关系接口兜底拉取失败",
				zap.String("path", cfg.RelationApiPath), zap.Error(err))
		}
	}
	tplog.L().Warn("[dp_relation] 跳① 关系数据不可用（库/镜像/接口均无），部署项目相关列留空")
	return nil, ""
}

// fetchRelationRows 分页循环全量拉取关系接口并降维为关系行（任一页失败即整体降级，不做部分采用）
func fetchRelationRows(cfg config.Apollo) ([]relationRow, error) {
	items, err := fetchRelationDTOs(cfg)
	if err != nil {
		return nil, err
	}
	rows := make([]relationRow, 0, len(items))
	for _, it := range items {
		if it.DelFlag { // 逻辑删除行不参与关联
			continue
		}
		rows = append(rows, relationRow{
			ApolloAppId: it.ApolloAppId, ApolloAppEnv: it.ApolloAppEnv,
			DeployProjectName: it.DeployProjectName, Node: it.Node,
		})
	}
	return rows, nil
}

// fetchRelationDTOs 全量拉取关系接口原始行。
// 上游（`/thirdApi/apolloApp/list`）在 paginator=false 下**不分页、直接返回全表**，
// 返回体的 pageTotal 为**总记录数**；因此默认 `pageSize=-1` 时一次请求即取全量。
// 仅当上游确实做了分页（返回行数 < total）时，才按 total 换算页数补齐，并以 maxPages 兜底防死循环。
func fetchRelationDTOs(cfg config.Apollo) ([]AppDpRelationRow, error) {
	pageSize := cfg.RelationApiPageSize
	if pageSize == 0 {
		pageSize = relationFullPageSize // 缺省不分页
	}
	maxPages := cfg.RelationApiMaxPages
	if maxPages <= 0 {
		maxPages = relationDefaultMaxPages
	}
	client := NewApolloClient(cfg)
	start := time.Now()
	tplog.L().Info("[dp_relation] 关系接口拉取开始",
		zap.String("path", cfg.RelationApiPath), zap.Int("pageSize", pageSize))
	items, total, err := client.GetAppDpRelations(cfg.RelationApiPath, 1, pageSize)
	if err != nil {
		tplog.L().Error("[dp_relation] 关系接口拉取失败",
			zap.String("path", cfg.RelationApiPath), zap.Int("page", 1), zap.Error(err))
		return nil, err
	}
	if total <= len(items) {
		// paginator=false 语义命中的常规路径：上游 total 即总记录数，本次已取全，不再发请求
		tplog.L().Info("[dp_relation] 关系接口拉取完成",
			zap.Int("rows", len(items)), zap.Int("upstreamTotal", total), zap.Int("pages", 1),
			zap.Int64("costMs", time.Since(start).Milliseconds()))
		return items, nil
	}
	// 上游仍分页：按 total 换算页数补齐（每页条数取配置值，<=0 时用兜底值）
	if pageSize <= 0 {
		pageSize = relationPagedPageSize
	}
	pageCount := (total + pageSize - 1) / pageSize
	if pageCount > maxPages {
		tplog.L().Warn("[dp_relation] 关系接口总页数超上限，仅拉取前若干页",
			zap.Int("pageCount", pageCount), zap.Int("maxPages", maxPages))
		pageCount = maxPages
	}
	tplog.L().Info("[dp_relation] 关系接口分页完成",
		zap.Int("page", 1), zap.Int("pageCount", pageCount),
		zap.Int("rows", len(items)), zap.Int("upstreamTotal", total))
	all := make([]AppDpRelationRow, 0, total)
	all = append(all, items...)
	for p := 2; p <= pageCount; p++ {
		page, _, err := client.GetAppDpRelations(cfg.RelationApiPath, p, pageSize)
		if err != nil {
			tplog.L().Error("[dp_relation] 关系接口拉取失败",
				zap.String("path", cfg.RelationApiPath), zap.Int("page", p), zap.Error(err))
			return nil, fmt.Errorf("第 %d/%d 页拉取失败: %w", p, pageCount, err)
		}
		all = append(all, page...)
		tplog.L().Info("[dp_relation] 关系接口分页完成",
			zap.Int("page", p), zap.Int("pageCount", pageCount),
			zap.Int("rows", len(page)), zap.Int("accRows", len(all)))
	}
	tplog.L().Info("[dp_relation] 关系接口拉取完成",
		zap.Int("rows", len(all)), zap.Int("pages", pageCount),
		zap.Int64("costMs", time.Since(start).Milliseconds()))
	return all, nil
}

// loadRelationFromDB 整表加载关系表（过滤逻辑删除行）→ 精简关系行；
// 无连接/不可达/无表/无数据 返回 nil（交由镜像或接口兜底）
func loadRelationFromDB() []relationRow {
	relationMu.Lock()
	db, prevOK := relationDB, relationOK
	relationMu.Unlock()
	if db == nil {
		return nil
	}
	var rows []model.AppDpRelation
	if err := db.Where("del_flag = 0").Find(&rows).Error; err != nil {
		if prevOK { // 仅在状态由可用转为不可用时告警一次，避免每次刷新刷屏
			tplog.L().Warn("[dp_relation] 关系表读取失败，降级本地镜像（镜像缺失时回退关系接口）", zap.Error(err))
		}
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	out := make([]relationRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, relationRow{
			ApolloAppId: r.ApolloAppId, ApolloAppEnv: r.ApolloAppEnv,
			DeployProjectName: r.DeployProjectName, Node: r.Node,
		})
	}
	return out
}

// loadRelationFromFile 读取本地镜像；文件不存在/损坏/空 返回 nil
func loadRelationFromFile() []relationRow {
	b, err := os.ReadFile(relationMirrorPath())
	if err != nil {
		return nil
	}
	var f relationMirrorFile
	if err := json.Unmarshal(b, &f); err != nil {
		tplog.L().Warn("[dp_relation] 本地镜像解析失败", zap.String("file", relationMirrorPath()), zap.Error(err))
		return nil
	}
	if len(f.Rows) == 0 {
		return nil
	}
	return f.Rows
}

// writeRelationMirror 把关系表加载结果镜像到本地文件，供无 DB/接口不可用时兜底（失败不影响主流程）
func writeRelationMirror(rows []relationRow) {
	sorted := make([]relationRow, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ApolloAppId != sorted[j].ApolloAppId {
			return sorted[i].ApolloAppId < sorted[j].ApolloAppId
		}
		return sorted[i].DeployProjectName < sorted[j].DeployProjectName
	})
	out := relationMirrorFile{
		Version: 1, ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
		TotalRows: len(sorted), Rows: sorted,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(relationMirrorPath()), 0o755); err != nil {
		return
	}
	// 写锁串行化：同步任务（SyncAppDpRelation）与查询路径（loadRelationRows）可能并发刷新镜像，
	// 两者共用同一个 tmp 文件名会互相覆盖，写出损坏的镜像内容。
	relationMirrorMu.Lock()
	defer relationMirrorMu.Unlock()
	// 原子写：tmp + rename，避免读端读到半截文件
	tmp := relationMirrorPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, relationMirrorPath()); err != nil {
		_ = os.Remove(tmp)
	}
}

// joinDpTargets 跳① 关系行 → 「appId|环境 → 属性项列表」索引：
// 先按 (appId, 环境, 部署项目, 节点) 去重（同一 appId 可跨多 node 出现多行），再按跳② DPR 列出该部署项目下的全部归属。
//
// 来源口径：部署项目（DepProject）与节点（Node）恒取跳① app_dp_relation，跳① 节点为空则为空、不回退跳②；
// 跳② DPR 只提供 PP 应用（及工程项目）。DPR 未命中该部署项目时仅保留部署项目与节点（不阻断跳① 结果）。
func joinDpTargets(rows []relationRow) map[string][]dpTarget {
	if len(rows) == 0 {
		return nil
	}
	// 跳①：appId|环境 → 去重后的 (部署项目, 节点) 列表（节点为 ADR 口径，允许为空）
	type adrEntry struct {
		DepProject string
		Node       string
	}
	seen := make(map[string]map[string]bool, len(rows))
	entries := make(map[string][]adrEntry, len(rows))
	for _, r := range rows {
		if r.ApolloAppId == "" || r.DeployProjectName == "" {
			continue
		}
		k := relationKeyOf(r.ApolloAppId, r.ApolloAppEnv)
		ek := r.DeployProjectName + "|" + r.Node
		if seen[k] == nil {
			seen[k] = map[string]bool{}
		}
		if seen[k][ek] {
			continue
		}
		seen[k][ek] = true
		entries[k] = append(entries[k], adrEntry{DepProject: r.DeployProjectName, Node: r.Node})
	}
	// 跳② 正向索引：部署项目 → 该部署项目下的 PP 应用归属行（DPR 一个部署项目可对应多行 PP 应用，全部保留）
	owners, _ := pinpointMap()
	byDep := make(map[string][]dpTarget, len(owners))
	for dep, list := range owners {
		for _, o := range list {
			byDep[dep] = append(byDep[dep], dpTarget{
				DepProject: o.DepProject, EgProject: o.EgProject,
				PpApplication: o.PpApplication,
			})
		}
	}
	for dep := range byDep {
		sort.Slice(byDep[dep], func(i, j int) bool {
			return byDep[dep][i].PpApplication < byDep[dep][j].PpApplication
		})
	}
	out := make(map[string][]dpTarget, len(entries))
	for k, list := range entries {
		sort.Slice(list, func(i, j int) bool {
			if list[i].DepProject != list[j].DepProject {
				return list[i].DepProject < list[j].DepProject
			}
			return list[i].Node < list[j].Node
		})
		targets := make([]dpTarget, 0, len(list))
		for _, e := range list {
			hit := byDep[e.DepProject]
			if len(hit) == 0 {
				targets = append(targets, dpTarget{DepProject: e.DepProject, Node: e.Node})
				continue
			}
			for _, h := range hit {
				targets = append(targets, dpTarget{
					DepProject: e.DepProject, Node: e.Node,
					EgProject: h.EgProject, PpApplication: h.PpApplication,
				})
			}
		}
		out[k] = targets
	}
	return out
}

// attachAppAttrs 关联 跳①×跳② 后，把命中数据作为「appId 组件的属性」补充到每一行上。
//
// 口径（《运维拓扑图方案》数据模型 /《拓扑查询总体逻辑.md》§4.3）：
// 拓扑数据集粒度恒为「appId × 中间件」——deploy_project / eng_project / node / pp_application
// 都是 appId 的属性，属性多值时聚合为列表（DepProjects / EngProjects / Nodes / PpApplications），
// 单值列取属性首值（兼容既有前端字段口径）。**行数与行序不变、不复制行**，
// 原 expandRows「一行按 部署项目 × PP 应用 展开为 N 行」会按属性组合把关系数据集扩维，已废弃。
// 跳① 未命中（无关系数据 / 该 appId 无关系 / 环境不匹配）时原样保留，保证不丢行。
//
// 返回新切片（不修改入参）：数据源可能返回底层快照切片的子切片，原地改写会污染内存快照。
func attachAppAttrs(rows []model.OperationTopology) []model.OperationTopology {
	out := make([]model.OperationTopology, len(rows))
	copy(out, rows)
	if len(out) == 0 {
		return out
	}
	targets, ok := dpTargets()
	if !ok || len(targets) == 0 {
		return out
	}
	for i := range out {
		t := targets[relationKeyOf(out[i].ApolloAppId, out[i].Env)]
		if len(t) == 0 {
			continue
		}
		deps := make([]string, 0, len(t))
		engs := make([]string, 0, len(t))
		nodes := make([]string, 0, len(t))
		pps := make([]string, 0, len(t))
		for _, tg := range t {
			if tg.DepProject != "" {
				deps = append(deps, tg.DepProject)
			}
			if tg.EgProject != "" {
				engs = append(engs, tg.EgProject)
			}
			if tg.Node != "" {
				nodes = append(nodes, tg.Node)
			}
			if tg.PpApplication != "" {
				pps = append(pps, tg.PpApplication)
			}
		}
		r := &out[i]
		r.DepProjects, r.EngProjects = dedupSorted(deps), dedupSorted(engs)
		r.Nodes, r.PpApplications = dedupSorted(nodes), dedupSorted(pps)
		// 单值列取属性首值（与列表首项一致）；列表为空时保持原值
		// （PP 应用为空时保留采集期占位值，避免把已有值清空）
		setFirst(&r.DepProject, r.DepProjects)
		setFirst(&r.EngProject, r.EngProjects)
		setFirst(&r.Node, r.Nodes)
		setFirst(&r.PpApplication, r.PpApplications)
	}
	return out
}

// dpSlot 一个「部署点」= 部署项目 × 节点，对应拓扑图中的一个应用节点。
// 「部署项目 + 节点」是应用节点的唯一条件，一个部署点一条记录。
// 同节点下可有多个 PP 应用（DPR 唯一键为 (dep_project, pp_application)），故 PpApps 仍是多值。
type dpSlot struct {
	DepProject string   // 部署项目（单值）
	Node       string   // 节点/机房（单值）
	EgProject  string   // 工程项目（同一部署点内通常同值，取首值）
	PpApps     []string // 该部署点下的 PP 应用（去重升序）
}

// deploySlots 由「appId|环境 → 属性项列表」聚出「appId|环境 → 部署点列表」：
// 按 (部署项目, 节点) 去重，节点单值；同槽内工程项目取首值、PP 应用保留全量。
func deploySlots(targets map[string][]dpTarget) map[string][]dpSlot {
	out := make(map[string][]dpSlot, len(targets))
	for k, list := range targets {
		idx := make(map[string]int, len(list))
		slots := make([]dpSlot, 0, len(list))
		for _, t := range list {
			if t.DepProject == "" {
				continue
			}
			ck := t.DepProject + "|" + t.Node
			i, ok := idx[ck]
			if !ok {
				i = len(slots)
				idx[ck] = i
				slots = append(slots, dpSlot{DepProject: t.DepProject, Node: t.Node, EgProject: t.EgProject})
			}
			if slots[i].EgProject == "" {
				slots[i].EgProject = t.EgProject
			}
			if t.PpApplication != "" {
				slots[i].PpApps = append(slots[i].PpApps, t.PpApplication)
			}
		}
		for i := range slots {
			slots[i].PpApps = dedupSorted(slots[i].PpApps)
		}
		out[k] = slots
	}
	return out
}

// expandByDeploySlot 「appId × 中间件」行 → 「(appId, 部署项目, 节点) × 中间件」行（部署点展开模式）。
//
// 一行按该 appId 的有效部署点复制为 N 行（N = 部署点个数）：dep_project / node / eng_project 单值，
// 多值列（dep_projects / nodes / eng_projects）清空（前端改读单值列）；pp_applications 仍为该部署点
// 下的 PP 应用列表、pp_application 取首值。
// 部署点不可用（跳① 无数据源 / 该 appId 无关系 / 环境不匹配）时原行返回 —— 不丢行。
//
// 注：展开只在查询期进行。拓扑表唯一索引 uk_snapshot_relation 不含 dep_project/node，落库展开必撞索引。
func expandByDeploySlot(rows []model.OperationTopology) []model.OperationTopology {
	targets, ok := dpTargets()
	if !ok || len(targets) == 0 {
		return rows
	}
	slots := deploySlots(targets)
	out := make([]model.OperationTopology, 0, len(rows))
	for _, r := range rows {
		ss := slots[relationKeyOf(r.ApolloAppId, r.Env)]
		if len(ss) == 0 {
			out = append(out, r)
			continue
		}
		for _, s := range ss {
			nr := r
			nr.DepProject, nr.Node, nr.EngProject = s.DepProject, s.Node, s.EgProject
			nr.PpApplications = s.PpApps
			setFirst(&nr.PpApplication, s.PpApps)
			nr.DepProjects, nr.Nodes, nr.EngProjects = nil, nil, nil
			out = append(out, nr)
		}
	}
	return out
}

// filterSlotRows 部署点展开模式下按「部署点」复核关系行：行粒度已是 (appId, 部署项目, 节点) × 中间件，
// 故直接按行的单值列判定 —— 部署项目模糊、节点/工程项目精确、PP 应用命中该行的 PP 应用列表。
// 与 filterAttachRows（appId 粒度）的区别：这里只保留满足条件的那个部署点的行，
// 同一 appId 的其它部署点行会被剔除（图节点粒度与筛选粒度一致）。
func filterSlotRows(rows []model.OperationTopology, q appAttrQuery) []model.OperationTopology {
	out := make([]model.OperationTopology, 0, len(rows))
	for _, r := range rows {
		if len(q.DepProject) > 0 && !attrHit(nil, r.DepProject, q.DepProject, true) {
			continue
		}
		if len(q.EngProject) > 0 && !attrHit(nil, r.EngProject, q.EngProject, false) {
			continue
		}
		if len(q.Node) > 0 && !attrHit(nil, r.Node, q.Node, false) {
			continue
		}
		if len(q.PpApplication) > 0 && !attrHit(r.PpApplications, r.PpApplication, q.PpApplication, false) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// dedupSorted 去重并升序排序（入参可为未排序、含重复）；无有效元素返回 nil
func dedupSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// unionSorted 两个「已去重升序」列表的并集（去重、升序）；入参可为 nil
func unionSorted(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]string, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			out = append(out, a[i])
			i++
		case a[i] > b[j]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i, j = i+1, j+1
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}

// setFirst 单值字段取属性列表首项；列表为空时保持原值
func setFirst(dst *string, list []string) {
	if len(list) > 0 {
		*dst = list[0]
	}
}

// attrHit 属性命中判定：优先在属性列表内匹配；列表为空（关联属性未补充）时回退单值列，保证不误杀。
// fuzzy=true 模糊匹配（部署项目口径），false 精确匹配（节点口径）。
func attrHit(list []string, single string, wants []string, fuzzy bool) bool {
	if len(wants) == 0 {
		return true
	}
	matchOne := func(val string) bool {
		for _, w := range wants {
			if fuzzy {
				if strings.Contains(val, w) {
					return true
				}
			} else if val == w {
				return true
			}
		}
		return false
	}
	if len(list) == 0 {
		return matchOne(single)
	}
	for _, v := range list {
		if matchOne(v) {
			return true
		}
	}
	return false
}

// pageRows 内存分页（行序保持数据源行序）：
// pageSize<0 不分页取全量；pageSize==0 采用默认 20（与数据源层口径一致）。
func pageRows(rows []model.OperationTopology, page, pageSize int) []model.OperationTopology {
	if pageSize < 0 {
		return rows
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * pageSize
	if start >= len(rows) {
		return []model.OperationTopology{}
	}
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

// appAttrQuery 应用（appId）属性筛选条件：deploy_project / eng_project / node / pp_application
// 都是 appId 的属性（拓扑数据集粒度恒为 appId × 中间件），任一维度命中即保留该 appId 的全部关系行。
// 支持多选（前端多选下拉）：每个维度是一组值，维度内 OR（任一命中即满足该维度），维度间 AND。
// 空维度不参与筛选；DepProject 维度内沿用页面旧口径做模糊匹配，其余维度精确匹配。
type appAttrQuery struct {
	DepProject    []string // 部署项目（多选，维度内各自模糊）
	EngProject    []string // 工程项目（多选，精确）
	Node          []string // 节点/机房（多选，精确）
	PpApplication []string // PP 应用（多选，精确）
}

// empty 是否无条件（无条件时调用方走原始列下推的旧路径，不启用属性下推/复核）
func (q appAttrQuery) empty() bool {
	return len(q.DepProject) == 0 && len(q.EngProject) == 0 && len(q.Node) == 0 && len(q.PpApplication) == 0
}

// splitAttrCSV 把逗号分隔的筛选串拆成去重切片（支持前端多选：维度内 OR）。
// 空串 / 全空格 / 无有效项返回 nil。
func splitAttrCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// firstOf 取切片首值（空切片返回空串）——用于关系链不可用时回退原始列过滤（仅下推首值，最佳努力）。
func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// relationFilterApps 把 appId 属性条件（部署项目模糊 / 工程项目·节点·PP 应用精确）经 跳①×跳② 索引
// 翻译为 apollo_app_id 集合，供数据源下推（收窄候选行）。命中即该 appId 的全部关系行都是候选
// （属性命中以 appId 为粒度）。
// ok=false 表示关系链不可用，调用方需回退到对原始列的过滤（保持旧行为）。
func relationFilterApps(q appAttrQuery) ([]string, bool) {
	if q.empty() {
		return nil, false
	}
	targets, ok := dpTargets()
	if !ok {
		return nil, false
	}
	set := map[string]bool{}
	for k, list := range targets {
		appId, _, _ := strings.Cut(k, "|")
		if appId == "" {
			continue
		}
		// 属性以 appId 为粒度聚合（与 attachAppAttrs 的并集口径一致）：各维度分别在该 appId
		// 的属性列表内匹配，不能要求「同一条属性项同时满足所有维度」——否则
		// 「属性项1 满足维度甲、属性项2 满足维度乙」的 appId 会被下推误剔除，
		// 而下推即候选行范围，误剔除会导致复核阶段再也拿不到这些行（结果少数据）。
		deps := make([]string, 0, len(list))
		engs := make([]string, 0, len(list))
		nodes := make([]string, 0, len(list))
		pps := make([]string, 0, len(list))
		for _, t := range list {
			if t.DepProject != "" {
				deps = append(deps, t.DepProject)
			}
			if t.EgProject != "" {
				engs = append(engs, t.EgProject)
			}
			if t.Node != "" {
				nodes = append(nodes, t.Node)
			}
			if t.PpApplication != "" {
				pps = append(pps, t.PpApplication)
			}
		}
		if len(q.DepProject) > 0 && !attrHit(deps, "", q.DepProject, true) {
			continue
		}
		if len(q.EngProject) > 0 && !attrHit(engs, "", q.EngProject, false) {
			continue
		}
		if len(q.Node) > 0 && !attrHit(nodes, "", q.Node, false) {
			continue
		}
		if len(q.PpApplication) > 0 && !attrHit(pps, "", q.PpApplication, false) {
			continue
		}
		set[appId] = true
	}
	apps := make([]string, 0, len(set))
	for a := range set {
		apps = append(apps, a)
	}
	sort.Strings(apps)
	return apps, true
}

// filterAttachRows 属性补充后按 appId 属性条件复核关系行。
//
// 复核粒度是 appId（该行 appId 的属性列表中任一命中即保留），不是「属性组合行」：
// 关系数据集为 appId × 中间件，条件命中的 appId 的全部中间件关系行都保留，
// 不因该 appId 属性多值而只留下某一个属性组合的行。
// 下推（relationFilterApps）只做候选收窄，不能代替本复核。
func filterAttachRows(rows []model.OperationTopology, q appAttrQuery) []model.OperationTopology {
	out := make([]model.OperationTopology, 0, len(rows))
	for _, r := range rows {
		if len(q.DepProject) > 0 && !attrHit(r.DepProjects, r.DepProject, q.DepProject, true) {
			continue
		}
		if len(q.EngProject) > 0 && !attrHit(r.EngProjects, r.EngProject, q.EngProject, false) {
			continue
		}
		if len(q.Node) > 0 && !attrHit(r.Nodes, r.Node, q.Node, false) {
			continue
		}
		if len(q.PpApplication) > 0 && !attrHit(r.PpApplications, r.PpApplication, q.PpApplication, false) {
			continue
		}
		out = append(out, r)
	}
	return out
}
