package prometheus

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/config"

	"github.com/tjhop/prometheus-mcp-server/internal/version"
)

// NewAPIClient 创建一个配置了自定义 User-Agent 头部的 Prometheus API 客户端。
// 如果 rt 为 nil，则使用 http.DefaultTransport。
func NewAPIClient(prometheusURL string, rt http.RoundTripper) (promv1.API, error) {
	if rt == nil {
		rt = http.DefaultTransport
	}

	uart := config.NewUserAgentRoundTripper(version.UserAgent(), rt)

	client, err := api.NewClient(api.Config{
		Address:      prometheusURL,
		RoundTripper: uart,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 API 客户端失败: %w", err)
	}

	return promv1.NewAPI(client), nil
}
