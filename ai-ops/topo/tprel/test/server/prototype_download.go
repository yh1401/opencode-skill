// 本地测试服务专用：前端原型包下载（对应 doc.go 的「对接文档下载」，同样仅本地提供）
//
//	GET /api/external/operationTopology/prototype/files     → JSON { code, data:{ dir, files:[{ name, rel, sizeBytes }] } }
//	GET /api/external/operationTopology/prototype/download  → operation-topology-prototype.zip 附件下载（目录内全文件打包）
//
// 原型目录默认 ./api/tprel/prototype，可用环境变量 TPREL_PROTOTYPE_DIR 覆盖。
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/gin-gonic/gin"
)

const (
	prototypeZipName    = "operation-topology-prototype.zip"
	defaultPrototypeDir = "./api/tprel/prototype"
)

func prototypeDir() string {
	if d := os.Getenv("TPREL_PROTOTYPE_DIR"); d != "" {
		return d
	}
	return defaultPrototypeDir
}

// listPrototypeFiles 返回原型目录内的文件清单（含相对路径与大小），供页面展示/逐个下载。
func listPrototypeFiles(c *gin.Context) {
	root := prototypeDir()
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		c.JSON(http.StatusOK, gin.H{"code": 7, "msg": "原型目录不存在：" + root, "data": gin.H{"dir": root, "files": []gin.H{}}})
		return
	}

	var files []gin.H
	err = filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			fmt.Printf("[prototype-download] 遍历失败 %s: %v\n", p, werr)
			return nil // 单个文件失败不中断，尽量返回剩余清单
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, gin.H{
			"name":      info.Name(),
			"rel":       filepath.ToSlash(rel),
			"sizeBytes": info.Size(),
		})
		return nil
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 7, "msg": "读取原型目录失败：" + err.Error(), "data": gin.H{"dir": root, "files": []gin.H{}}})
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i]["rel"].(string) < files[j]["rel"].(string) })

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "操作成功", "data": gin.H{"dir": root, "files": files}})
}

// downloadPrototypeZip 将整个原型目录打包为 zip 附件下发。
func downloadPrototypeZip(c *gin.Context) {
	root := prototypeDir()
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		c.String(http.StatusOK, "原型目录不存在："+root)
		return
	}

	// 附件响应头必须在写 zip body 之前设置（否则写入后已 flush，头会丢失）
	c.Header("Content-Disposition",
		`attachment; filename="`+prototypeZipName+`"; filename*=UTF-8''`+url.PathEscape(prototypeZipName))
	c.Header("Content-Type", "application/zip")

	zw := zip.NewWriter(c.Writer)
	var walkErr error
	filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			fmt.Printf("[prototype-download] 遍历失败 %s: %v\n", p, werr)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel) // zip 内统一用 / 分隔
		hdr := &zip.FileHeader{
			Name:     rel,
			Method:   zip.Deflate,
			Modified: info.ModTime(),
		}
		w, werr := zw.CreateHeader(hdr)
		if werr != nil {
			walkErr = fmt.Errorf("zip 创建 %s: %w", rel, werr)
			return walkErr
		}
		f, ferr := os.Open(p)
		if ferr != nil {
			walkErr = ferr
			return ferr
		}
		defer f.Close()
		if _, cerr := io.Copy(w, f); cerr != nil {
			walkErr = cerr
			return cerr
		}
		return nil
	})

	if cerr := zw.Close(); cerr != nil && walkErr == nil {
		walkErr = cerr
	}
	if walkErr != nil {
		fmt.Printf("[prototype-download] 打包失败: %v\n", walkErr)
	}
}
