package tplog

import (
	"os"
	"path/filepath"
	"testing"
)

// makeLogDir 在 t.TempDir() 下创建日志目录，并写入指定日期的 .log 文件
func makeLogDir(t *testing.T, dates []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range dates {
		p := filepath.Join(dir, d+".log")
		if err := os.WriteFile(p, []byte("log line for "+d+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func listArchive(t *testing.T, dir string) []string {
	t.Helper()
	arch := filepath.Join(dir, "archive")
	entries, err := os.ReadDir(arch)
	if err != nil {
		return nil // archive 不存在算空
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// ---------- pruneOldLogs 核心测试 ----------

func TestPruneOldLogs_ArchiveExcess(t *testing.T) {
	// 8 个日志文件，keep=5 → 主目录留 5，archive 归档 3
	os.Setenv("TPREL_LOG_KEEP", "5")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dates := []string{"2026-09-20", "2026-09-21", "2026-09-22", "2026-09-23", "2026-09-24", "2026-09-25", "2026-09-26", "2026-09-27"}
	dir := makeLogDir(t, dates)
	current := filepath.Join(dir, "2026-09-27.log") // 模拟当前正在写的文件

	pruneOldLogs(dir, current)

	main := listFiles(t, dir)
	arch := listArchive(t, dir)

	if len(main) != 5 {
		t.Fatalf("主目录应保留 5 份，实际 %d: %v", len(main), main)
	}
	if len(arch) != 3 {
		t.Fatalf("archive 应归档 3 份，实际 %d: %v", len(arch), arch)
	}
	// 主目录保留最近 5 天
	expected := map[string]bool{"2026-09-23.log": true, "2026-09-24.log": true, "2026-09-25.log": true, "2026-09-26.log": true, "2026-09-27.log": true}
	for _, f := range main {
		if !expected[f] {
			t.Errorf("主目录不应包含 %s", f)
		}
	}
	// archive 归档最旧 3 天
	archExpected := map[string]bool{"2026-09-20.log": true, "2026-09-21.log": true, "2026-09-22.log": true}
	for _, f := range arch {
		if !archExpected[f] {
			t.Errorf("archive 不应包含 %s", f)
		}
	}
}

func TestPruneOldLogs_NotExceedKeep(t *testing.T) {
	// 3 个日志文件，keep=5 → 不需要归档
	os.Setenv("TPREL_LOG_KEEP", "5")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dates := []string{"2026-09-25", "2026-09-26", "2026-09-27"}
	dir := makeLogDir(t, dates)
	current := filepath.Join(dir, "2026-09-27.log")

	pruneOldLogs(dir, current)

	main := listFiles(t, dir)
	arch := listArchive(t, dir)

	if len(main) != 3 {
		t.Fatalf("主目录应保留 3 份，实际 %d", len(main))
	}
	if len(arch) != 0 {
		t.Fatalf("archive 应为空，实际 %d: %v", len(arch), arch)
	}
}

func TestPruneOldLogs_CurrentFileNeverArchived(t *testing.T) {
	// 8 个文件，keep=5，但当前文件恰好在需要归档的范围里 → 应跳过
	os.Setenv("TPREL_LOG_KEEP", "5")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dates := []string{"2026-09-20", "2026-09-21", "2026-09-22", "2026-09-23", "2026-09-24", "2026-09-25", "2026-09-26", "2026-09-27"}
	dir := makeLogDir(t, dates)
	// current 指向最旧的文件（理应被归档，但因为是 current 所以必须跳过）
	current := filepath.Join(dir, "2026-09-20.log")

	pruneOldLogs(dir, current)

	main := listFiles(t, dir)
	arch := listArchive(t, dir)

	// 主目录 = 5 + 1（current 被跳过，所以主目录有 6 个文件）
	if len(main) != 6 {
		t.Fatalf("主目录应有 6 份（5 保留 + 1 current 跳过），实际 %d: %v", len(main), main)
	}
	if len(arch) != 2 {
		t.Fatalf("archive 应归档 2 份（跳过了 current），实际 %d: %v", len(arch), arch)
	}
	// current 仍在主目录
	found := false
	for _, f := range main {
		if f == "2026-09-20.log" {
			found = true
		}
	}
	if !found {
		t.Fatal("current 文件 2026-09-20.log 不应在主目录消失")
	}
}

func TestPruneOldLogs_NonLogFilesUntouched(t *testing.T) {
	// 混入非 YYYY-MM-DD.log 文件 → 不应被归档
	os.Setenv("TPREL_LOG_KEEP", "2")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dir := t.TempDir()
	// 3 个合法日志 + 杂项文件
	for _, d := range []string{"2026-09-25", "2026-09-26", "2026-09-27"} {
		os.WriteFile(filepath.Join(dir, d+".log"), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "README.txt"), []byte("readme"), 0o644)
	os.WriteFile(filepath.Join(dir, "2026-09-27.txt"), []byte("not log"), 0o644)   // 扩展名不对
	os.WriteFile(filepath.Join(dir, "2026-9-27.log"), []byte("bad format"), 0o644) // 格式不严格

	current := filepath.Join(dir, "2026-09-27.log")
	pruneOldLogs(dir, current)

	main := listFiles(t, dir)
	arch := listArchive(t, dir)

	// 合法日志：keep=2，主目录留 2（2026-09-26、2026-09-27），archive 归档 1（2026-09-25）
	// 杂项文件不受影响
	for _, f := range main {
		switch f {
		case "README.txt", "2026-09-27.txt", "2026-9-27.log":
			// 这些文件应保留在主目录
		case "2026-09-25.log":
			t.Errorf("2026-09-25.log 应被归档，不应在主目录")
		case "2026-09-26.log", "2026-09-27.log":
			// 正确保留
		}
	}
	// 确认杂项文件还在主目录
	found := false
	for _, f := range main {
		if f == "README.txt" {
			found = true
		}
	}
	if !found {
		t.Error("README.txt 不应被归档或删除")
	}
	// archive 只有 1 个合法日志
	if len(arch) != 1 || arch[0] != "2026-09-25.log" {
		t.Errorf("archive 应只含 2026-09-25.log，实际 %v", arch)
	}
}

func TestPruneOldLogs_ArchiveAlreadyExists(t *testing.T) {
	// archive 子目录已存在且有同名旧文件 → 应被覆盖
	os.Setenv("TPREL_LOG_KEEP", "2")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dir := t.TempDir()
	for _, d := range []string{"2026-09-25", "2026-09-26", "2026-09-27"} {
		os.WriteFile(filepath.Join(dir, d+".log"), []byte("new"), 0o644)
	}
	// 预建 archive 并放一个旧版 2026-09-25.log
	os.MkdirAll(filepath.Join(dir, "archive"), 0o755)
	os.WriteFile(filepath.Join(dir, "archive", "2026-09-25.log"), []byte("old content"), 0o644)

	current := filepath.Join(dir, "2026-09-27.log")
	pruneOldLogs(dir, current)

	// archive 里的 2026-09-25.log 应被覆盖为 "new"
	content, err := os.ReadFile(filepath.Join(dir, "archive", "2026-09-25.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Errorf("archive 内同名文件应被覆盖，期望 'new'，实际 '%s'", string(content))
	}
}

func TestPruneOldLogs_KeepZeroNoAction(t *testing.T) {
	// keep=0 表示不清理 → 所有文件留在主目录
	os.Setenv("TPREL_LOG_KEEP", "0")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dates := []string{"2026-09-20", "2026-09-21", "2026-09-22", "2026-09-23", "2026-09-24", "2026-09-25", "2026-09-26", "2026-09-27"}
	dir := makeLogDir(t, dates)
	current := filepath.Join(dir, "2026-09-27.log")

	pruneOldLogs(dir, current)

	main := listFiles(t, dir)
	if len(main) != 8 {
		t.Fatalf("keep=0 不应归档任何文件，主目录应有 8 份，实际 %d", len(main))
	}
}

func TestPruneOldLogs_EmptyDir(t *testing.T) {
	// 空目录 → 不 panic，不创建 archive
	os.Setenv("TPREL_LOG_KEEP", "5")
	defer os.Unsetenv("TPREL_LOG_KEEP")

	dir := t.TempDir()
	current := filepath.Join(dir, "2026-09-27.log")

	pruneOldLogs(dir, current)

	// 不应创建 archive 目录
	if _, err := os.Stat(filepath.Join(dir, "archive")); !os.IsNotExist(err) {
		t.Error("空目录不应创建 archive 子目录")
	}
}

// ---------- keepCount 环境变量解析 ----------

func TestKeepCount_Default(t *testing.T) {
	os.Unsetenv("TPREL_LOG_KEEP")
	if got := keepCount(); got != 5 {
		t.Errorf("默认应返回 5，实际 %d", got)
	}
}

func TestKeepCount_EnvOverride(t *testing.T) {
	os.Setenv("TPREL_LOG_KEEP", "3")
	defer os.Unsetenv("TPREL_LOG_KEEP")
	if got := keepCount(); got != 3 {
		t.Errorf("TPREL_LOG_KEEP=3 应返回 3，实际 %d", got)
	}
}

func TestKeepCount_InvalidEnv(t *testing.T) {
	os.Setenv("TPREL_LOG_KEEP", "abc")
	defer os.Unsetenv("TPREL_LOG_KEEP")
	if got := keepCount(); got != 5 {
		t.Errorf("非法值应回退默认 5，实际 %d", got)
	}
}

func TestKeepCount_ZeroOrNegative(t *testing.T) {
	os.Setenv("TPREL_LOG_KEEP", "0")
	defer os.Unsetenv("TPREL_LOG_KEEP")
	if got := keepCount(); got != 0 {
		t.Errorf("TPREL_LOG_KEEP=0 应返回 0（表示不清理），实际 %d", got)
	}
}

// ---------- logNameRe 正则匹配 ----------

func TestLogNameRe(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"2026-09-27.log", true},
		{"2026-01-01.log", true},
		{"2026-9-27.log", false},  // 月/日不补零
		{"2026-09-27.txt", false}, // 扩展名不对
		{"2026-09-27.log.bak", false},
		{"README.md", false},
		{".log", false},
		{"", false},
	}
	for _, c := range cases {
		if got := logNameRe.MatchString(c.name); got != c.want {
			t.Errorf("logNameRe.MatchString(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
