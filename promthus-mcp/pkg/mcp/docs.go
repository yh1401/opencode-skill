package mcp

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	stripFrontmatterRegex = regexp.MustCompile(`(?s)^---\n.*?\n---\n?`)
)

// stripFrontmatter 从 markdown 块的开头移除 frontmatter 块（如果存在）。
// frontmatter 是以 `---` 行开头和结尾的开头块。
func stripFrontmatter(content string) string {
	return stripFrontmatterRegex.ReplaceAllString(content, "")
}

// getDocFileNames 遍历文件系统，返回所有 markdown 文件的路径列表。
func getDocFileNames(fsys fs.FS) ([]string, error) {
	var names []string

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.ToLower(filepath.Ext(path)) == ".md" {
			names = append(names, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历文档目录失败: %w", err)
	}

	return names, nil
}

// getDocFileContent 从文件系统中读取指定路径的文档文件内容，
// 并移除 frontmatter 后返回。
func getDocFileContent(fsys fs.FS, path string) (string, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return "", fmt.Errorf("从文件系统打开文件失败: %w", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("从文件系统读取文件失败: %w", err)
	}

	return stripFrontmatter(string(content)), nil
}

const (
	docChunkSize           = 8 * 1024  // 文档分块大小（8KB）
	docChunkOverlap        = 1 * 1024  // 分块重叠大小（1KB）
	defaultDocsSearchLimit = 25        // 默认文档搜索结果限制
)

// chunk 表示文档分块的结构，用于搜索索引
type chunk struct {
	ID      int    // 分块 ID
	Name    string // 文件名
	Content string // 分块内容
}

// String 返回分块的唯一标识符，格式为 "文件名#ID"
func (c *chunk) String() string {
	return fmt.Sprintf("%s#%d", c.Name, c.ID)
}
