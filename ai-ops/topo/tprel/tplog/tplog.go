// Package tplog 运维关系拓扑模块专用日志
// 输出到  {tprel}/logs/YYYY-MM-DD.log，并同步打印到控制台（便于本地联调观察）。
// 日志风格：结构化文本 + 可读时间，不用 JSON（符合项目日志规范）。
//
// 保留策略：目录内只保留最近 N 份按天日志（默认 5，可用 TPREL_LOG_KEEP 调整），
// 超出的旧文件在「打开新一天日志文件」时由本进程移入 archive 子目录归档：
//   - 不用 os.Remove 删除文件 —— 改用 os.Rename 原子移入 archive/，避免误删/删不掉/删错；
//   - 归档目录 archive/ 自动创建；若同名文件已存在，os.Rename 会覆盖归档目录里的旧副本；
//   - 只匹配文件名严格为 YYYY-MM-DD.log 且路径确实位于日志目录内的文件，其它文件一律不动；
//   - 永不归档当前正在写入的文件；任何归档错误都忽略，绝不影响业务写日志；
//   - archive 子目录本身不参与清理（ReadDir 遍历时 e.IsDir() 跳过），归档文件长期保留、供事后翻阅。
package tplog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logDirEnv   = "TPREL_LOG_DIR"  // 环境变量可覆盖日志目录
	logKeepEnv  = "TPREL_LOG_KEEP" // 环境变量可覆盖保留份数（默认 5；<=0 表示不清理）
	defaultDir  = "api/tprel/logs" // 默认目录（相对运行路径，即本模块文件夹下的 logs）
	defaultKeep = 5                // 默认只保留最近 5 份按天日志
	archiveName = "archive"        // 归档子目录名（与主目录平级）
)

var (
	mu        sync.Mutex
	logger    *zap.Logger
	curFile   string   // 当前日志文件路径（用于按天切换）
	curHandle *os.File // 当前日志文件句柄（跨天切换时关闭，避免句柄泄漏）
)

// logNameRe 可归档的日志文件名（严格 YYYY-MM-DD.log，其它文件一律不动）
var logNameRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.log$`)

// customTimeEncoder 可读时间格式：2026-09-02 23:15:01.123
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

// logDir 返回日志目录（环境变量优先，否则默认 logs/）
func logDir() string {
	if env := os.Getenv(logDirEnv); env != "" {
		return env
	}
	return defaultDir
}

// Dir 返回当前日志目录（供"读取同步日志"接口复用同一路径解析，环境变量 TPREL_LOG_DIR 优先）
func Dir() string {
	return logDir()
}

// keepCount 返回保留份数：TPREL_LOG_KEEP 优先（非法/为空 → 默认 5）；<=0 表示不清理
func keepCount() int {
	v := strings.TrimSpace(os.Getenv(logKeepEnv))
	if v == "" {
		return defaultKeep
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultKeep
	}
	return n
}

// pruneOldLogs 只保留最近 keep 份按天日志，其余移入 archive 子目录归档（默认保留 5 份）。
// 仅在打开新一天文件时调用一次；归档失败（如权限/占用/磁盘满）只跳过，不返回错误、不影响写日志。
func pruneOldLogs(dir, current string) {
	keep := keepCount()
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !logNameRe.MatchString(e.Name()) {
			continue // 子目录（含 archive/）、以及非 YYYY-MM-DD.log 的文件（如快照/手工文件）不参与归档
		}
		names = append(names, e.Name())
	}
	if len(names) <= keep {
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // 日期倒序：YYYY-MM-DD 字典序即时间序

	cleanDir := filepath.Clean(dir)
	archive := filepath.Join(cleanDir, archiveName)
	// 归档目录不存在时创建；创建失败则放弃本次归档，保留主目录文件原样不动
	if err := os.MkdirAll(archive, 0o755); err != nil {
		return
	}

	for _, name := range names[keep:] {
		src := filepath.Join(cleanDir, name)
		// 双保险：绝不动当前正在写的文件
		if src == filepath.Clean(current) {
			continue
		}
		// 双保险：待归档源路径必须确实位于日志目录内（防路径穿越）
		if !strings.HasPrefix(src, cleanDir+string(os.PathSeparator)) {
			continue
		}
		dst := filepath.Join(archive, name)
		// 同样校验目标归档路径必须位于 archive 目录内
		if !strings.HasPrefix(filepath.Clean(dst), archive+string(os.PathSeparator)) {
			continue
		}
		_ = os.Rename(src, dst) // 原子归档（若目标同名已存在则覆盖，属于预期行为）
	}
}

// initFile 打开（必要时创建）今日日志文件并构建 logger
func initFile() error {
	dir := logDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	now := time.Now()
	path := filepath.Join(dir, now.Format("2006-01-02")+".log")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}
	// 新文件已就绪后再关旧文件（先开新后关旧，中途失败也不影响已有日志输出），
	// 避免跨天切换时句柄一直累积。
	if logger != nil {
		_ = logger.Sync()
	}
	if curHandle != nil {
		_ = curHandle.Close()
		curHandle = nil
	}

	encoderCfg := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		MessageKey:     "msg",
		EncodeTime:     customTimeEncoder,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
	}
	core := zapcore.NewTee(
		zapcore.NewCore(zapcore.NewConsoleEncoder(encoderCfg), file, zapcore.DebugLevel),
		zapcore.NewCore(zapcore.NewConsoleEncoder(encoderCfg), zapcore.AddSync(os.Stdout), zapcore.DebugLevel),
	)
	logger = zap.New(core)
	curFile, curHandle = path, file

	// 只保留最近 N 份日志（默认 5）：进程启动首写、以及跨天新开文件时各执行一次
	pruneOldLogs(dir, path)
	return nil
}

// L 返回模块级 logger（按天惰性重建，跨天自动切换到新文件）
func L() *zap.Logger {
	mu.Lock()
	defer mu.Unlock()
	today := time.Now().Format("2006-01-02")
	if logger == nil || !stringsHasSuffix(curFile, today+".log") {
		if err := initFile(); err != nil {
			// 日志初始化失败绝不影响业务：退化为控制台输出
			logger = zap.New(zapcore.NewCore(
				zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
				zapcore.AddSync(os.Stdout),
				zapcore.DebugLevel,
			))
		}
	}
	return logger
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
