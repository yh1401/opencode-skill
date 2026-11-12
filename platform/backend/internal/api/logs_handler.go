package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// logsHealth GET /api/logs/health
// 由后端探测外部 log_analyzer（LOG_ANALYZER_URL）的 /api/health，前端据此决定是嵌入 iframe
// 还是展示「服务未就绪」的降级提示；服务端探测可避免浏览器跨域限制，也能区分「连不上」与
// 「端口被别的服务占用」两种情况。
func (s *Server) logsHealth(c *gin.Context) {
	url := strings.TrimRight(s.cfg.LogAnalyzerURL, "/")
	resp := gin.H{
		"enabled":   s.cfg.LogAnalyzerEnabled && url != "",
		"url":       url,
		"reachable": false,
		"status":    0,
		"latencyMs": 0,
		"detail":    "",
		"hint":      "",
	}
	if url == "" {
		resp["hint"] = "未配置 LOG_ANALYZER_URL，日志分析服务地址为空"
		c.JSON(http.StatusOK, resp)
		return
	}
	if !s.cfg.LogAnalyzerEnabled {
		resp["hint"] = "LOG_ANALYZER_ENABLED=false，日志分析服务已在配置中关闭"
		c.JSON(http.StatusOK, resp)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/health", nil)
	if err != nil {
		resp["hint"] = "日志分析服务地址不合法：" + err.Error()
		c.JSON(http.StatusOK, resp)
		return
	}

	start := time.Now()
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	resp["latencyMs"] = time.Since(start).Milliseconds()
	if err != nil {
		resp["detail"] = err.Error()
		resp["hint"] = fmt.Sprintf("无法连接 %s，服务未启动或端口被占用", url)
		c.JSON(http.StatusOK, resp)
		return
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 160 {
		snippet = snippet[:160]
	}
	resp["status"] = res.StatusCode
	resp["detail"] = snippet

	switch {
	case res.StatusCode != http.StatusOK:
		resp["hint"] = fmt.Sprintf("%s 有响应但不是 log_analyzer（/api/health 返回 HTTP %d），该端口可能被其他服务占用", url, res.StatusCode)
	case !json.Valid(body):
		resp["hint"] = fmt.Sprintf("%s 返回的不是 JSON，可能不是 log_analyzer 服务", url)
	default:
		resp["reachable"] = true
	}
	c.JSON(http.StatusOK, resp)
}
