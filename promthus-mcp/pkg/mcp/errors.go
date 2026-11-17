package mcp

import (
	"errors"
	"fmt"
	"net/http"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// ErrEndpointNotSupported 表示 Prometheus API 端点返回 HTTP 404，
// 这通常意味着该端点在运行的 Prometheus 版本上不可用。
// 它包装原始错误以便解包。
type ErrEndpointNotSupported struct {
	Endpoint   string // 端点路径
	StatusCode int    // HTTP 状态码
	Err        error  // 原始错误
}

// Error 返回用户友好的消息，解释 404 错误并建议与版本相关的补救措施。
func (e *ErrEndpointNotSupported) Error() string {
	return fmt.Sprintf(
		"API 端点 %q 返回 HTTP %d (%s) -- "+
			"此端点可能不受您的 Prometheus 版本支持。"+
			"考虑升级 Prometheus 或使用 build_info 工具检查运行版本的文档。",
		e.Endpoint,
		e.StatusCode,
		http.StatusText(e.StatusCode),
	)
}

// Unwrap 返回底层错误，以支持 errors.Is/As 兼容性。
func (e *ErrEndpointNotSupported) Unwrap() error {
	return e.Err
}

// isNotFoundError 检查 prometheus client_golang API 客户端返回的错误
// 是否表示 HTTP 404 Not Found 响应。
//
// client_golang 将 4xx 错误消息构造为 "client error: <status_code>"
// (参见 prometheus/client_golang 中的 ErrorType/Error 类型)。
// 我们使用精确相等而不是子字符串匹配来避免来自恰好包含 "404" 的消息的误报
// (例如 "client error: 4040")。
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	var promErr *promv1.Error
	if !errors.As(err, &promErr) {
		return false
	}

	return promErr.Type == promv1.ErrClient && promErr.Msg == "client error: 404"
}

// wrapErrorIfNotFound 将 client_golang 的 404 错误包装为 ErrEndpointNotSupported，
// 并附带给定的端点路径。非 404 错误保持不变返回。
func wrapErrorIfNotFound(err error, endpoint string) error {
	if isNotFoundError(err) {
		return &ErrEndpointNotSupported{
			Endpoint:   endpoint,
			StatusCode: http.StatusNotFound,
			Err:        err,
		}
	}
	return err
}
