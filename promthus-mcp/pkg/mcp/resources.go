package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	resourcePrefix = "prometheus://" // 资源 URI 前缀
)

// 资源定义
var (
	// docsListResource 是官方 Prometheus 文档文件列表资源
	docsListResource = &mcp.Resource{
		URI:         resourcePrefix + "docs",
		Name:        "官方 Prometheus 文档文件列表",
		Description: "包含 prometheus/docs 仓库中官方 Prometheus 文档的 markdown 文件列表",
		MIMEType:    "text/plain",
	}

	// docsReadResourceTemplate 是用于读取特定文档文件的资源模板
	docsReadResourceTemplate = &mcp.ResourceTemplate{
		URITemplate: resourcePrefix + "docs/{+file}",
		Name:        "官方 Prometheus 文档",
		Description: "从 prometheus/docs 仓库读取包含官方 Prometheus 文档的指定 markdown 文件",
		MIMEType:    "text/markdown",
	}
)

// registerResources 向服务器注册所有 MCP 资源
func registerResources(server *mcp.Server, container *ServerContainer) {
	// 添加静态资源
	server.AddResource(docsListResource, container.DocsListResourceHandler)

	// 添加用于读取特定文档文件的资源模板
	server.AddResourceTemplate(docsReadResourceTemplate, container.DocsReadResourceHandler)
}

// 资源处理程序

// DocsListResourceHandler 处理文档列表资源请求
func (s *ServerContainer) DocsListResourceHandler(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	names, err := s.GetDocFileNames()
	if err != nil {
		return nil, fmt.Errorf("列出文档失败: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "text/plain",
				Text:     strings.Join(names, "\n"),
			},
		},
	}, nil
}

// DocsReadResourceHandler 通过资源模板处理读取特定文档文件的请求
func (s *ServerContainer) DocsReadResourceHandler(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	// 解析 URI 以正确处理 URL 编码的字符
	u, err := url.Parse(req.Params.URI)
	if err != nil {
		return nil, fmt.Errorf("无效的资源 URI: %w", err)
	}

	// 验证协议方案
	if u.Scheme != "prometheus" {
		return nil, fmt.Errorf("无效的文档资源 URI 方案: %s", u.Scheme)
	}

	// 获取文件路径。例如，prometheus://docs/file/to-read.md -> file/to-read.md
	filename := strings.TrimPrefix(u.Path, "/")
	if filename == "" {
		return nil, errors.New("请求读取文档时至少需要一个文件名")
	}

	content, err := s.GetDocFileContent(filename)
	if err != nil {
		return nil, fmt.Errorf("从文档读取文件失败: %w", err)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{
			{
				URI:      req.Params.URI,
				MIMEType: "text/markdown",
				Text:     content,
			},
		},
	}, nil
}
