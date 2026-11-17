package mcp

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing/fstest"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/prometheus/client_golang/prometheus"
	promconfig "github.com/prometheus/common/config"

	"github.com/tjhop/prometheus-mcp-server/internal/metrics"
	"github.com/tjhop/prometheus-mcp-server/internal/version"
)

const (
	// DocsUpdateInterval 是文档更新检查之间的默认间隔
	DocsUpdateInterval = 24 * time.Hour

	docsRepoURL    = "https://github.com/prometheus/docs.git"           // 文档仓库 URL
	docsArchiveURL = "https://github.com/prometheus/docs/archive/refs/heads/main.tar.gz" // 文档归档 URL

	// maxArchiveSize 是下载的压缩归档文件的最大大小（50 MB）
	maxArchiveSize = 50 * 1024 * 1024

	// maxDecompressedSize 是提取内容的最大总大小（200 MB）
	// 这可以防止解压缩炸弹，即小的压缩归档会展开成意外大的大小
	maxDecompressedSize = 200 * 1024 * 1024

	// maxFileCount 是从归档中提取的最大文件数
	// 这可以防止文件计数炸弹，即归档包含许多小文件
	maxFileCount = 10000

	// httpClientTimeout 是下载归档的 HTTP 请求超时时间
	httpClientTimeout = 300 * time.Second
)

var (
	metricDocsUpdateTimestamp = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(metrics.MetricNamespace, "docs", "last_update_timestamp_seconds"),
		Help: "上次成功文档更新的 Unix 时间戳",
	})

	metricDocsUpdateFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: prometheus.BuildFQName(metrics.MetricNamespace, "docs", "update_failures_total"),
		Help: "文档更新失败的总数",
	})
)

func init() {
	metrics.Registry.MustRegister(
		metricDocsUpdateTimestamp,
		metricDocsUpdateFailures,
	)
}

// remoteRefLister 抽象了列出远程 Git 引用的能力。
// 此接口使测试无需网络访问即可进行。
type remoteRefLister interface {
	// ListRefs 返回配置仓库的远程引用。
	// 它应该返回引用名称到其 SHA 哈希的映射。
	ListRefs(ctx context.Context) (map[string]string, error)
}

// gitRemoteLister 使用 go-git 实现 remoteRefLister
type gitRemoteLister struct {
	repoURL string // Git 仓库 URL
}

// ListRefs 查询远程仓库并返回所有引用
func (g *gitRemoteLister) ListRefs(ctx context.Context) (map[string]string, error) {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{g.repoURL},
	})

	refs, err := rem.ListContext(ctx, &git.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("列出远程引用失败: %w", err)
	}

	result := make(map[string]string, len(refs))
	for _, ref := range refs {
		result[ref.Name().String()] = ref.Hash().String()
	}
	return result, nil
}

// DocsUpdater 定期检查官方 prometheus/docs 仓库的更改，
// 并在运行时更新嵌入的文档。它使用 go-git 查询远程引用
//（类似于 `git ls-remote`）以高效检测更改而不受速率限制，
// 并下载仓库归档以将文档提取到内存中的 fs.FS。
type DocsUpdater struct {
	mu          sync.Mutex         // 互斥锁，保护并发访问
	logger      *slog.Logger       // 日志记录器
	container   *ServerContainer   // 服务器容器引用
	httpClient  *http.Client       // HTTP 客户端
	currentHash string             // 最后已知的提交哈希（以嵌入版本开始）
	repoURL     string             // 用于检查最新提交的 Git 仓库 URL
	archiveURL  string             // 用于下载文档 tarball 的 GitHub 归档 URL
	refLister   remoteRefLister    // 用于列出远程引用的接口（用于可测试性）
}

// NewDocsUpdater 创建一个新的 DocsUpdater，可以检查文档更新
// 并将其应用到给定的 ServerContainer。
// embeddedHash 是构建时嵌入的文档子模块的 git 提交哈希。
func NewDocsUpdater(logger *slog.Logger, container *ServerContainer, embeddedHash string) *DocsUpdater {
	return &DocsUpdater{
		logger:    logger.With("component", "docs_updater"),
		container: container,
		httpClient: &http.Client{
			Timeout:   httpClientTimeout,
			Transport: promconfig.NewUserAgentRoundTripper(version.UserAgent(), http.DefaultTransport),
		},
		currentHash: embeddedHash,
		repoURL:     docsRepoURL,
		archiveURL:  docsArchiveURL,
		refLister:   &gitRemoteLister{repoURL: docsRepoURL},
	}
}

// Update 执行单次文档更新检查，如果上游有新版本可用则有条件地应用更改。
// 它支持上下文取消，并且可以安全地从多个 goroutine 并发调用。
//
// 如果文档已经是最新的或更新成功，则返回 nil。
// 如果更新检查或应用失败，则返回错误。
func (u *DocsUpdater) Update(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	newCommit, changed, err := u.checkForUpdate(ctx)
	if err != nil {
		metricDocsUpdateFailures.Inc()
		return fmt.Errorf("文档更新检查失败: %w", err)
	}
	if !changed {
		return nil
	}

	oldCommit := u.currentHash
	if err := u.fetchAndUpdateDocsFS(ctx); err != nil {
		metricDocsUpdateFailures.Inc()
		return fmt.Errorf("更新文档到新提交 <%s> 失败: %w", newCommit, err)
	}

	u.currentHash = newCommit
	metricDocsUpdateTimestamp.SetToCurrentTime()
	u.logger.Info("文档更新成功", "old_commit", oldCommit, "new_commit", newCommit)
	return nil
}

// checkForUpdate 查询配置的 ref lister 以获取最新的提交哈希。
// 返回最新提交 SHA、它是否与当前哈希不同，以及任何错误。
func (u *DocsUpdater) checkForUpdate(ctx context.Context) (string, bool, error) {
	u.logger.Debug("检查文档更新", "repo_url", u.repoURL)

	// 使用配置的 ref lister 列出远程引用
	refs, err := u.refLister.ListRefs(ctx)
	if err != nil {
		return "", false, fmt.Errorf("查询远程引用失败: %w", err)
	}

	// 查找 refs/heads/main 并获取其哈希
	mainBranch := "main"
	mainRefName := plumbing.NewBranchReferenceName(mainBranch).String()
	sha, ok := refs[mainRefName]
	if !ok {
		return "", false, fmt.Errorf("远程仓库中未找到 refs/heads/%s", mainBranch)
	}

	changed := sha != u.currentHash
	if changed {
		u.logger.Debug("发现新的文档提交", "current", u.currentHash, "new", sha)
	}
	return sha, changed, nil
}

// fetchAndUpdateDocsFS 从 GitHub 下载文档归档，将 markdown 文件提取到内存文件系统，
// 构建新的搜索索引，并原子性地交换 ServerContainer 中的文档状态。
func (u *DocsUpdater) fetchAndUpdateDocsFS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.archiveURL, nil)
	if err != nil {
		return fmt.Errorf("创建归档 HTTP 请求失败: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载归档失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 读取一部分错误响应体用于调试上下文
		archiveDownloadErrMsg := "归档下载返回状态 " + strconv.Itoa(resp.StatusCode)

		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if len(errBody) > 0 {
			archiveDownloadErrMsg = fmt.Sprintf("%s: %s", archiveDownloadErrMsg, string(errBody))
		}
		return errors.New(archiveDownloadErrMsg)
	}

	// 限制下载大小以防止无限制的内存使用
	memFS, err := extractDocsFromArchive(io.LimitReader(resp.Body, maxArchiveSize))
	if err != nil {
		return fmt.Errorf("从归档提取文档失败: %w", err)
	}

	newState, err := buildDocsState(u.logger, memFS)
	if err != nil {
		return fmt.Errorf("构建文档状态失败: %w", err)
	}

	u.container.swapDocsState(newState)
	return nil
}

// extractDocsFromArchive 解压缩 gzip+tar 归档，并将 docs/ 目录下的 markdown 文件提取到内存中的 fs.FS。
// 归档根目录（例如 "docs-main/"）被剥离，只包含匹配 docs/**/*.md 的文件。
//
// 安全性：该函数验证防止路径遍历攻击，并强制执行最大解压缩大小以防止解压缩炸弹。
func extractDocsFromArchive(r io.Reader) (fs.FS, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("创建 gzip 读取器失败: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	memFS := make(fstest.MapFS)
	var totalExtracted int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取 tar 头部条目失败: %w", err)
		}

		// 归档根目录是 "docs-{ref}/" -- 剥离它并查找 "docs/" 前缀
		// 例如，"docs-main/docs/alerting/alerting-rules.md" -> "alerting/alerting-rules.md"
		parts := strings.SplitN(hdr.Name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		relPath := parts[1]

		if !strings.HasPrefix(relPath, "docs/") {
			continue
		}
		docPath := strings.TrimPrefix(relPath, "docs/")
		if docPath == "" {
			continue
		}

		// 出于安全考虑，拒绝非本地路径（即路径遍历等）
		if !filepath.IsLocal(docPath) {
			continue
		}

		// 仅处理常规文件；跳过目录、符号链接等
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(docPath), ".md") {
			continue
		}

		// 防止负大小（格式错误的 tar 条目）
		if hdr.Size < 0 {
			continue
		}
		// 通过将比较重新表述为减法来防止 int64 溢出
		if hdr.Size > maxDecompressedSize-totalExtracted {
			return nil, fmt.Errorf("归档超过最大解压缩大小: 已提取 %d 字节，下一个文件 (%s, %d 字节) 将超过 %d 字节的限制", totalExtracted, hdr.Name, hdr.Size, maxDecompressedSize)
		}
		if len(memFS) >= maxFileCount {
			return nil, fmt.Errorf("归档超过最大文件数 %d", maxFileCount)
		}

		data, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
		if err != nil {
			return nil, fmt.Errorf("读取 tar 条目 %s 失败: %w", hdr.Name, err)
		}
		totalExtracted += int64(len(data))
		memFS[docPath] = &fstest.MapFile{Data: data}
	}

	if len(memFS) == 0 {
		return nil, errors.New("归档中未找到 markdown 文件")
	}
	return memFS, nil
}
