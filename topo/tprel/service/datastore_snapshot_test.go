package service

// 快照保留策略回归测试（对应《运维关系拓扑图方案》§3 快照策略）：
//   - FileStore.Replace(rows, keep)：替换当日桶 + 按 sync_date 只保留最近 keep 份，更早的桶丢弃；
//   - DatastoreRouter.mirrorWriteDB(rows, keep)：DB 侧同口径（保留最近 keep 个 sync_date 桶）；
//   - snapshotKeep()：配置未填 / <=0 一律回落默认 3 份。
//
// 说明：keep 的取值来自配置 topology_snapshot_keep（默认 3），DB 侧用例复用内存 sqlite，
// 与 service 包既有惯例（glebarez/sqlite）一致。

import (
	"sort"
	"testing"

	"devops-automation/api/tprel/model"
	"devops-automation/global"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// snapshotTestDB 内存 sqlite（快照/关系表落库用例共用的建库入口）
func snapshotTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开内存 sqlite 失败: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	return db
}

// snapRow 构造一行拓扑关系（addr 兼作唯一键区分列，避免撞 uk_snapshot_relation）
func snapRow(date, env, addr string) model.OperationTopology {
	return model.OperationTopology{
		SyncDate: date, Env: env, ApolloAppId: "app-" + addr,
		ApolloHostId: 1, MiddlewareType: "mysql", ConnectAddr: addr,
	}
}

// snapshotDatesOf 读取库内现有 sync_date 分桶（降序，与 DBStore.SyncDates 同口径）
func snapshotDatesOf(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var dates []string
	if err := db.Model(&model.OperationTopology{}).
		Distinct("sync_date").Order("sync_date desc").Pluck("sync_date", &dates).Error; err != nil {
		t.Fatalf("读取 sync_date 分桶失败: %v", err)
	}
	return dates
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// snapshotKeep：配置未填 / <=0 回落默认 3；配置 >0 时按配置
func TestSnapshotKeepFallback(t *testing.T) {
	prev := global.GVA_CONFIG.Apollo.TopologySnapshotKeep
	t.Cleanup(func() { global.GVA_CONFIG.Apollo.TopologySnapshotKeep = prev })

	for _, tc := range []struct {
		cfg  int
		want int
	}{
		{0, defaultSnapshotKeep},
		{-1, defaultSnapshotKeep},
		{5, 5},
	} {
		global.GVA_CONFIG.Apollo.TopologySnapshotKeep = tc.cfg
		if got := snapshotKeep(); got != tc.want {
			t.Errorf("topology_snapshot_keep=%d 时 snapshotKeep()=%d，期望 %d", tc.cfg, got, tc.want)
		}
	}
}

// FileStore.Replace：第 4 次同步后只留最近 3 个日期桶，最早的被丢弃；当日桶替换而非追加
func TestFileStoreReplaceKeepsRecentDateBuckets(t *testing.T) {
	fs := newFileStore(t.TempDir())
	dates := []string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17"}
	for _, d := range dates {
		if err := fs.Replace([]model.OperationTopology{snapRow(d, "PRO", "addr-"+d)}, 3); err != nil {
			t.Fatalf("Replace(%s) 失败: %v", d, err)
		}
	}
	want := []string{"2026-09-17", "2026-09-16", "2026-09-15"}
	if got := fs.SyncDates(""); !eqStrings(got, want) {
		t.Errorf("保留日期桶 = %v，期望 %v（最早的 2026-09-14 应被丢弃）", got, want)
	}

	// 同一日期再次同步：替换当日桶，不新增日期；行数按新批次为准
	if err := fs.Replace([]model.OperationTopology{
		snapRow("2026-09-17", "PRO", "addr-x"),
		snapRow("2026-09-17", "PRO", "addr-y"),
	}, 3); err != nil {
		t.Fatalf("同日重跑 Replace 失败: %v", err)
	}
	if got, want := fs.SyncDates(""), []string{"2026-09-17", "2026-09-16", "2026-09-15"}; !eqStrings(got, want) {
		t.Errorf("同日重跑后日期桶 = %v，期望 %v", got, want)
	}
	if got := fs.CountOf("2026-09-17"); got != 2 {
		t.Errorf("当日桶行数 = %d，期望 2（替换而非追加）", got)
	}

	// 历史桶仍可读取，供前后对比
	prevDate, prevRows := fs.Previous("2026-09-17")
	if prevDate != "2026-09-16" || len(prevRows) != 1 || prevRows[0].ConnectAddr != "addr-2026-09-16" {
		t.Errorf("Previous = (%s, %d 行)，期望 (2026-09-16, 1 行 addr-2026-09-16)", prevDate, len(prevRows))
	}
}

// FileStore.Replace：keep<=0 表示不清理历史桶（保留全部日期）
func TestFileStoreReplaceKeepZeroRetainsAll(t *testing.T) {
	fs := newFileStore(t.TempDir())
	dates := []string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17"}
	for _, d := range dates {
		if err := fs.Replace([]model.OperationTopology{snapRow(d, "PRO", "addr-"+d)}, 0); err != nil {
			t.Fatalf("Replace(%s) 失败: %v", d, err)
		}
	}
	got := fs.SyncDates("")
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if !eqStrings(got, dates) {
		t.Errorf("keep=0 时日期桶 = %v，期望全部保留 %v", got, dates)
	}
}

// mirrorWriteDB：DB 侧按 sync_date 只保留最近 keep 个桶，更早整桶删除；同日重跑替换不追加
func TestMirrorWriteDBKeepsRecentSyncDateBuckets(t *testing.T) {
	db := snapshotTestDB(t, &model.OperationTopology{})
	r := newDatastoreRouter(datastoreCfg{mode: sourceFile, dataDir: t.TempDir()}, db)

	for _, d := range []string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17"} {
		if err := r.ReplaceSnapshot([]model.OperationTopology{snapRow(d, "PRO", "addr-"+d)}); err != nil {
			t.Fatalf("ReplaceSnapshot(%s) 失败: %v", d, err)
		}
	}
	want := []string{"2026-09-17", "2026-09-16", "2026-09-15"}
	if got := snapshotDatesOf(t, db); !eqStrings(got, want) {
		t.Errorf("库内保留日期桶 = %v，期望 %v", got, want)
	}

	// 同日重跑：当日桶先删后写，行数以新批次为准
	if err := r.ReplaceSnapshot([]model.OperationTopology{
		snapRow("2026-09-17", "PRO", "addr-x"),
		snapRow("2026-09-17", "PRO", "addr-y"),
	}); err != nil {
		t.Fatalf("同日重跑 ReplaceSnapshot 失败: %v", err)
	}
	if got := snapshotDatesOf(t, db); !eqStrings(got, want) {
		t.Errorf("同日重跑后日期桶 = %v，期望 %v", got, want)
	}
	var cnt int64
	db.Model(&model.OperationTopology{}).Where("sync_date = ?", "2026-09-17").Count(&cnt)
	if cnt != 2 {
		t.Errorf("当日桶行数 = %d，期望 2（替换而非追加）", cnt)
	}
}

// keep 大于历史桶数时不误删：桶数不足 keep 时按现有全部保留
func TestMirrorWriteDBFewerBucketsThanKeep(t *testing.T) {
	db := snapshotTestDB(t, &model.OperationTopology{})
	r := newDatastoreRouter(datastoreCfg{mode: sourceFile, dataDir: t.TempDir()}, db)

	for _, d := range []string{"2026-09-16", "2026-09-17"} {
		if err := r.ReplaceSnapshot([]model.OperationTopology{snapRow(d, "PRO", "addr-"+d)}); err != nil {
			t.Fatalf("ReplaceSnapshot(%s) 失败: %v", d, err)
		}
	}
	if got, want := snapshotDatesOf(t, db), []string{"2026-09-17", "2026-09-16"}; !eqStrings(got, want) {
		t.Errorf("日期桶 = %v，期望 %v（keep=3 时不应删除任何桶）", got, want)
	}
}
