// 本地测试服务专用：前端对接文档在线查看 / 下载
//
//	GET /api/external/operationTopology/doc             → JSON（file/path/sizeBytes/updatedAt/content）
//	GET /api/external/operationTopology/doc?download=1  → text/markdown 附件下载
//
// 仅本地联调使用（生产服务不注册该路由）；文档目录可用环境变量 TPREL_DOC_DIR 覆盖（默认 ./api/tprel/docs）。
package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const (
	docFileName   = "前端对接说明.md"
	defaultDocDir = "./api/tprel/docs"
)

// getLocalDoc 读取模块文档并返回内容（或作为附件下载）
func getLocalDoc(c *gin.Context) {
	dir := os.Getenv("TPREL_DOC_DIR")
	if dir == "" {
		dir = defaultDocDir
	}
	p := filepath.Join(dir, docFileName)
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		c.JSON(http.StatusOK, gin.H{"code": 7, "msg": "文档不存在：" + p, "data": nil})
		return
	}

	if c.Query("download") == "1" {
		c.Header("Content-Type", "text/markdown; charset=utf-8")
		// ASCII 文件名 + RFC5987 中文名（兼容各浏览器）
		c.Header("Content-Disposition",
			`attachment; filename="frontend-api-doc.md"; filename*=UTF-8''`+url.PathEscape(docFileName))
		c.File(p)
		return
	}

	b, err := os.ReadFile(p)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 7, "msg": "读取文档失败：" + err.Error(), "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "操作成功", "data": gin.H{
		"file":      docFileName,
		"path":      p,
		"sizeBytes": fi.Size(),
		"updatedAt": fi.ModTime().Format("2006-01-02 15:04:05"),
		"content":   string(b),
	}})
}
