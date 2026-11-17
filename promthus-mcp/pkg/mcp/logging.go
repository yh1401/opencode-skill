package mcp

import (
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/common/promslog"
)

// clientLoggingInterval 是发送到 MCP 客户端的日志消息之间的最小间隔。
// 这用于限制客户端通知的速率，防止消息泛滥。
const clientLoggingInterval = 100 * time.Millisecond

// getClientLogger 创建一个 slog.Logger，通过协议通知将日志发送到 MCP 客户端。
// 如果由于某种原因无法获取会话，则返回 nil。
// 该记录器使用速率限制来防止向客户端发送过多消息。
// loggerName 出现在日志通知的 "logger" 字段中。
func getClientLogger(req *mcp.CallToolRequest, loggerName string) *slog.Logger {
	if req == nil {
		return nil
	}

	serverSession, ok := req.GetSession().(*mcp.ServerSession)
	if !ok || serverSession == nil {
		return nil
	}

	return slog.New(mcp.NewLoggingHandler(serverSession, &mcp.LoggingHandlerOptions{
		LoggerName:  loggerName,
		MinInterval: clientLoggingInterval,
	}))
}

// getChainedLogger 创建一个 slog.Logger，同时记录到应用程序日志和 MCP 客户端。
// 如果由于某种原因（没有会话等）无法进行客户端日志记录，则返回应用程序日志记录器不变。
// 如果 logger 为 nil，则仅尝试 MCP 客户端通知日志记录。
//
// 这使工具处理程序能够记录同时出现在服务器日志中并作为通知发送到连接的 LLM 客户端的消息。
func getChainedLogger(logger *slog.Logger, req *mcp.CallToolRequest, loggerName string) *slog.Logger {
	var chainedLogger *slog.Logger
	clientLogger := getClientLogger(req, loggerName)

	switch {
	case logger == nil && clientLogger == nil:
		chainedLogger = promslog.NewNopLogger()
	case logger != nil && clientLogger == nil:
		chainedLogger = logger
	case logger == nil && clientLogger != nil:
		chainedLogger = clientLogger
	default:
		chainedLogger = slog.New(slog.NewMultiHandler(logger.Handler(), clientLogger.Handler()))
	}

	return chainedLogger
}
