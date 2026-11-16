package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"devops-automation/api/tprel/tplog"

	"go.uber.org/zap"
)

// =========================================================
// ns 学习定向（同步优化 D+A 组合 · 零上游依赖）
//   背景：固定 namespace 名单逐个猜测导致大量 404 无效请求（真实环境 85%+）；
//   思路：把“查询返回成功（200）”的 namespace 记入学习状态，后续只扫命中集定向查询，
//         同时按周期（ns_guess_days）对全名单兜底重猜，覆盖新增/改名 namespace 的漂移。
//   语义：
//     - 学习键 = hostId|env|appId（套 × 环境 × 应用 三元组；同一应用跨套/跨环境各自学习）
//     - 命中 = GetNamespace 返回成功（200，含空空间）；404 = 该 ns 不存在
//     - 批末合并回写：命中集 ∪ 旧集 − 本批 404 集；跑过全名单（首扫/兜底）的三元组刷新 lastGuessAt
//   落盘：api/tprel/data/operation_topology_learned.json
//         （独立于关系快照、同 data 目录、tmp+rename 原子写；读取失败回退空集不阻断同步）
// =========================================================

const nsLearnFileName = "operation_topology_learned.json"

// nsLearnEntry 单个三元组的命中学习状态
type nsLearnEntry struct {
	Namespaces  []string `json:"namespaces"`  // 历史命中（200 返回）的 namespace（排序去重）
	LastGuessAt string   `json:"lastGuessAt"` // 最近一次全名单猜测日期 YYYY-MM-DD（空 = 尚未全量跑过）
}

// nsLearnData 学习文件格式
type nsLearnData struct {
	Version   int                      `json:"version"`
	UpdatedAt string                   `json:"updatedAt"`
	Apps      map[string]*nsLearnEntry `json:"apps"` // 学习键 → 状态
}

// nsLearnStore 学习状态存取（进程内缓存 + 懒加载；同步流程由 syncMu 串行互斥）
type nsLearnStore struct {
	mu     sync.Mutex
	path   string
	loaded bool
	data   map[string]*nsLearnEntry
	dirty  bool
}

// nsLearner 进程内单例（与快照 store 同级目录，默认 "./api/tprel/data"）
var nsLearner = &nsLearnStore{path: filepath.Join(defaultDataDir, nsLearnFileName)}

// nsLearnKey 学习键 = hostId|env|appId
func nsLearnKey(hostId int, env, appId string) string {
	return fmt.Sprintf("%d|%s|%s", hostId, env, appId)
}

// ensure 首次访问加载学习文件；文件缺失/损坏按空集继续（Warn 一次，不阻断同步）
func (s *nsLearnStore) ensure() {
	if s.loaded {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.data = map[string]*nsLearnEntry{}
	s.loaded = true
	f, err := os.Open(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			tplog.L().Warn("[operation_topology] ns 学习文件打开失败，按空学习集继续",
				zap.String("file", s.path), zap.Error(err))
		}
		return
	}
	defer f.Close()
	var doc nsLearnData
	if err := json.NewDecoder(f).Decode(&doc); err != nil {
		tplog.L().Warn("[operation_topology] ns 学习文件解析失败，按空学习集继续",
			zap.String("file", s.path), zap.Error(err))
		return
	}
	if doc.Version == 1 && doc.Apps != nil {
		s.data = doc.Apps
		tplog.L().Info("[operation_topology] ns 学习状态已加载",
			zap.String("file", s.path), zap.Int("keys", len(s.data)))
	}
}

// get 返回指定学习键的条目拷贝（无记录返回 nil）
func (s *nsLearnStore) get(key string) *nsLearnEntry {
	s.ensure()
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.data[key]; e != nil {
		cp := *e
		return &cp
	}
	return nil
}

// merge 批末合并学习状态：
//   - hits：本批查询成功（200）的 (key → ns 集合)，并入旧集
//   - missing：本批 404 的 (key → ns 集合)，从旧集移除（自愈“上次命中、现已删除”的漂移）
//   - guessed：本批跑过全名单（首扫/兜底）的 key，lastGuessAt 刷新为 today
func (s *nsLearnStore) merge(hits, missing map[string]map[string]bool, guessed map[string]bool, today string) {
	if len(hits) == 0 && len(missing) == 0 && len(guessed) == 0 {
		return
	}
	s.ensure()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, nsSet := range hits {
		e := s.data[key]
		if e == nil {
			e = &nsLearnEntry{}
			s.data[key] = e
		}
		for ns := range nsSet {
			if !containsStr(e.Namespaces, ns) {
				e.Namespaces = append(e.Namespaces, ns)
				s.dirty = true
			}
		}
	}
	for key, nsSet := range missing {
		e := s.data[key]
		if e == nil {
			continue
		}
		filtered := make([]string, 0, len(e.Namespaces))
		for _, ns := range e.Namespaces {
			if !nsSet[ns] {
				filtered = append(filtered, ns)
			}
		}
		if len(filtered) != len(e.Namespaces) {
			e.Namespaces = filtered
			s.dirty = true
		}
	}
	for key := range guessed {
		e := s.data[key]
		if e == nil {
			e = &nsLearnEntry{}
			s.data[key] = e
			s.dirty = true
		}
		if e.LastGuessAt != today {
			e.LastGuessAt = today
			s.dirty = true
		}
	}
	// 清理：无命中且从未全量跑过的空条目删除；其余归一排序
	for key, e := range s.data {
		if len(e.Namespaces) == 0 && e.LastGuessAt == "" {
			delete(s.data, key)
			s.dirty = true
			continue
		}
		sort.Strings(e.Namespaces)
	}
}

// flush 有变更时原子写回（tmp+rename），无变更直接返回
func (s *nsLearnStore) flush() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	s.dirty = false
	doc := nsLearnData{
		Version:   1,
		UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
		Apps:      s.data,
	}
	s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建 data 目录失败(%s): %w", dir, err)
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("创建 ns 学习临时文件失败(%s): %w", tmp, err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	writeErr := enc.Encode(doc)
	if writeErr == nil {
		writeErr = f.Close()
	} else {
		f.Close()
	}
	if writeErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("写 ns 学习文件失败(%s): %w", tmp, writeErr)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("替换 ns 学习文件失败(%s): %w", s.path, err)
	}
	tplog.L().Info("[operation_topology] ns 学习状态已回写",
		zap.String("file", s.path), zap.Int("keys", len(doc.Apps)))
	return nil
}

// containsStr 小集合线性包含判断（学习集合元素少，无需额外索引）
func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ensureApplication 定向名单保底 application：命中集来自“历史返回 200”，理论上必含 application，
// 但为防应用 ns 被整体清理/改名导致 application 缺失，强制兜底补齐，避免漏扫主配置。
func ensureApplication(ns []string) []string {
	if containsStr(ns, "application") {
		return ns
	}
	out := make([]string, 0, len(ns)+1)
	out = append(out, "application")
	out = append(out, ns...)
	return out
}

// nsGuessDue 判断是否到期需要全名单兜底重猜：
// lastGuessAt 为空（从未全量跑过）或距 today 已超过 guessDays 天 → 到期
func nsGuessDue(lastGuessAt string, guessDays int, today string) bool {
	if guessDays <= 0 || lastGuessAt == "" {
		return lastGuessAt == ""
	}
	t, err := time.Parse("2006-01-02", lastGuessAt)
	if err != nil {
		return true // 日期格式异常按到期处理（重新全量一次自愈）
	}
	base, err := time.Parse("2006-01-02", today)
	if err != nil {
		return true
	}
	return int(base.Sub(t).Hours()/24) >= guessDays
}
