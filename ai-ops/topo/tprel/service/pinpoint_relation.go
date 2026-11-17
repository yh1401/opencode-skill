package service

// =========================================================
// 部署项目映射（deploy_pinpoint_relation）关联层 —— 跳②
//
// 背景：跳①（app_dp_relation/关系接口）给出 apollo_app_id → 部署项目，
// 部署项目 → PP 应用/工程项目/节点 的归属由本表（DPR）提供，关联键为 deploy_project。
//
// 设计：
//   - 映射表规模很小（千行级），整体加载进内存按 pp_application 索引后复用，避免每条关系行回表；
//   - 数据来源与拓扑数据源保持一致的双形态：优先数据库，DB 不可用则回退本地镜像文件
//     data/deploy_pinpoint_relation.json（由 DB 加载成功后自动镜像写出）；
//   - 本层只提供内存映射（pinpointMap），两跳的 join 与展开见 dp_relation.go。
// =========================================================

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"devops-automation/api/tprel/model"
	"devops-automation/api/tprel/tplog"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// pinpointMirrorName 映射表本地镜像文件名（文件模式/DB 不可用时的兜底数据源）
	pinpointMirrorName = "deploy_pinpoint_relation.json"
	// pinpointTTL 内存映射的刷新周期（映射表变更很少，无需每次请求回表）
	pinpointTTL = 60 * time.Second
)

var (
	pinpointMu       sync.Mutex
	pinpointDB       *gorm.DB
	pinpointOwners   map[string][]model.PinpointOwner
	pinpointLoadedAt time.Time
	pinpointOK       bool // 上次是否成功加载（用于抑制重复告警）
)

// InitPinpointRelation 注入映射表数据源（启动时调用；db 可为 nil，此时仅用本地镜像）。
func InitPinpointRelation(db *gorm.DB) {
	pinpointMu.Lock()
	defer pinpointMu.Unlock()
	pinpointDB = db
	pinpointOwners, pinpointLoadedAt, pinpointOK = nil, time.Time{}, false
}

// pinpointMirrorPath 本地镜像文件路径
func pinpointMirrorPath() string {
	return filepath.Join(defaultDataDir, pinpointMirrorName)
}

// pinpointMap 返回 deploy_project → 归属行列表 的映射（带 TTL 缓存）。
// 键取 deploy_project（跳① 的落点）：同一部署项目可对应多个 PP 应用，故值为列表、全量保留。
// ok=false 表示映射不可用（DB 无连接且本地无镜像），调用方需回退原始列过滤。
func pinpointMap() (map[string][]model.PinpointOwner, bool) {
	pinpointMu.Lock()
	defer pinpointMu.Unlock()
	if pinpointOwners != nil && time.Since(pinpointLoadedAt) < pinpointTTL {
		return pinpointOwners, pinpointOK
	}
	if m := loadPinpointFromDB(); m != nil {
		writePinpointMirror(m)
		pinpointOwners, pinpointOK = mergePinpointSupplement(m), true
	} else if m := loadPinpointFromFile(); m != nil {
		pinpointOwners, pinpointOK = mergePinpointSupplement(m), true
	} else {
		pinpointOwners, pinpointOK = nil, false
	}
	pinpointLoadedAt = time.Now()
	tplog.L().Info("[pinpoint] 部署项目映射已加载",
		zap.Int("deps", len(pinpointOwners)), zap.Bool("ok", pinpointOK))
	return pinpointOwners, pinpointOK
}

// loadPinpointFromDB 从数据库整表加载；无连接/不可达/无表 返回 nil（交由文件兜底）
func loadPinpointFromDB() map[string][]model.PinpointOwner {
	if pinpointDB == nil {
		return nil
	}
	var rows []model.DeployPinpointRelation
	if err := pinpointDB.Find(&rows).Error; err != nil {
		if pinpointOK { // 仅在状态由可用转为不可用时告警一次，避免每次刷新刷屏
			tplog.L().Warn("[pinpoint] 映射表读取失败，改用本地镜像", zap.Error(err))
		}
		return nil
	}
	return groupPinpointOwners(rows)
}

// pinpointMirrorFile 本地镜像文件结构（与 DB 行一一对应）
type pinpointMirrorFile struct {
	Version    int                            `json:"version"`
	ExportedAt string                         `json:"exportedAt"`
	TotalRows  int                            `json:"totalRows"`
	Rows       []model.DeployPinpointRelation `json:"rows"`
}

// loadPinpointFromFile 读取本地镜像；文件不存在/损坏 返回 nil
func loadPinpointFromFile() map[string][]model.PinpointOwner {
	b, err := os.ReadFile(pinpointMirrorPath())
	if err != nil {
		return nil
	}
	var f pinpointMirrorFile
	if err := json.Unmarshal(b, &f); err != nil {
		tplog.L().Warn("[pinpoint] 本地镜像解析失败", zap.String("file", pinpointMirrorPath()), zap.Error(err))
		return nil
	}
	return groupPinpointOwners(f.Rows)
}

// groupPinpointOwners DPR 行 → deploy_project 分组的归属行列表（同部署项目的多 PP 应用行全部保留）
func groupPinpointOwners(rows []model.DeployPinpointRelation) map[string][]model.PinpointOwner {
	m := make(map[string][]model.PinpointOwner, len(rows))
	for _, r := range rows {
		if r.DepProject == "" {
			continue
		}
		m[r.DepProject] = append(m[r.DepProject], model.PinpointOwner{
			DepProject: r.DepProject, EgProject: r.EgProject, Node: r.Node,
			PpApplication: r.PpApplication,
		})
	}
	return m
}

// pinpointSupplement 查询期补数清单：DPR 表侧暂不可改（运维无法入库/落库）时，
// 由本清单在内存映射层补齐遗漏的「部署项目 → PP 应用/工程项目」归属，与表内数据一并参与跳② 关联。
//
// 口径：
//   - 仅覆盖 dep_project / pp_application / eg_project 三列（DPR 的 node 列已废弃，Node 恒取跳① ADR）；
//   - 键为 (dep_project, pp_application)，与 DPR 表唯一键一致，表内已有同键记录时不重复追加；
//   - 只作用于内存映射，不写库、不回写本地镜像（镜像仍保持纯 DB 口径）。
//
// 来源：运维同事补充（2026-09-30）；仅下列 4 条有效，同期反馈的其余数据均已失效、不再使用。
var pinpointSupplement = []model.PinpointOwner{
	{DepProject: "gb-access-gateway-jc-dep", PpApplication: "SPYW-Product_gb-access-gateway_spyw-pro_gb-access-gateway-jc-dep", EgProject: "gb-access-gateway"},
	{DepProject: "gb-access-gateway-pro-153707-dep", PpApplication: "hefei2_gb-access-gateway_spyw-pro_gb-access-gateway-pro-15", EgProject: "gb-access-gateway"},
	{DepProject: "gb-access-gateway-pro-153707-dep", PpApplication: "节点名称_gb-access-gateway_spyw-pro_gb-access-gateway-pro-15", EgProject: "gb-access-gateway"},
	{DepProject: "gb-dispatcher-dep", PpApplication: "SPYW-Product_gb-dispatcher_spyw-pro_gb-dispatcher-dep", EgProject: "gb-access-gateway"},
}

// mergePinpointSupplement 把查询期补数清单并入 DPR 映射：按 (dep_project, pp_application) 去重，
// 表内已有同键记录时保留表内数据。入参 m 由加载函数新建，可原地追加。
func mergePinpointSupplement(m map[string][]model.PinpointOwner) map[string][]model.PinpointOwner {
	if len(pinpointSupplement) == 0 {
		return m
	}
	if m == nil {
		m = make(map[string][]model.PinpointOwner, len(pinpointSupplement))
	}
	for _, s := range pinpointSupplement {
		if s.DepProject == "" || s.PpApplication == "" {
			continue
		}
		dup := false
		for _, o := range m[s.DepProject] {
			if o.PpApplication == s.PpApplication {
				dup = true
				break
			}
		}
		if !dup {
			m[s.DepProject] = append(m[s.DepProject], s)
		}
	}
	return m
}

// writePinpointMirror 把 DB 加载结果镜像到本地文件，供无 DB 的文件模式复用（失败不影响主流程）
func writePinpointMirror(m map[string][]model.PinpointOwner) {
	rows := make([]model.DeployPinpointRelation, 0, len(m))
	for _, list := range m {
		for _, o := range list {
			rows = append(rows, model.DeployPinpointRelation{
				DepProject: o.DepProject, PpApplication: o.PpApplication, EgProject: o.EgProject,
				Node: o.Node,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DepProject != rows[j].DepProject {
			return rows[i].DepProject < rows[j].DepProject
		}
		return rows[i].PpApplication < rows[j].PpApplication
	})
	out := pinpointMirrorFile{
		Version: 1, ExportedAt: time.Now().Format("2006-01-02 15:04:05"),
		TotalRows: len(rows), Rows: rows,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(pinpointMirrorPath()), 0o755); err != nil {
		return
	}
	// 原子写：tmp + rename，避免读端读到半截文件
	tmp := pinpointMirrorPath() + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, pinpointMirrorPath()); err != nil {
		_ = os.Remove(tmp)
	}
}
