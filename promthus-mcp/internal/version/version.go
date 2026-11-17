package version

import (
	"fmt"
	"runtime"
)

var (
	Version    string // 版本号，由链接器在构建时填充
	BuildDate  string // 构建日期，由链接器在构建时填充
	Commit     string // Git 提交哈希，由链接器在构建时填充
	DocsCommit string // 嵌入的 prometheus/docs 子模块的 Git 提交哈希，由嵌入文件中的提交哈希填充
)

// Print 输出人类可读的二进制构建信息到标准输出。
// 参考实现: github.com/prometheus/common/version.Print()
func Print(programName string) string {
	return fmt.Sprintf("%s 构建信息:\n\tversion: %s\n\tbuild date: %s\n\tcommit: %s\n\tdocs commit: %s\n\tgo version: %s\n",
		programName,
		Version,
		BuildDate,
		Commit,
		DocsCommit,
		runtime.Version(),
	)
}

// UserAgent 返回此项目中 HTTP 客户端使用的 User-Agent 字符串。
// 这是一个函数而不是包级变量，因为 Version 是在构建时由链接器填充的，
// 在变量初始化期间可能未设置。
func UserAgent() string {
	return fmt.Sprintf("prometheus-mcp-server/%s (https://github.com/tjhop/prometheus-mcp-server)", Version)
}

// Info 以更紧凑的单行格式打印构建信息。
// 参考实现: github.com/prometheus/common/version.Info()
func Info() string {
	return fmt.Sprintf("(version=%s, build_date=%s, commit=%s, docs_commit=%s)", Version, BuildDate, Commit, DocsCommit)
}
