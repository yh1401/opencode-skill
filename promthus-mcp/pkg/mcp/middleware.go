package mcp

import (
	"context"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
)

// MCP 方法名称常量。需要定义这些常量是因为 go-sdk 没有导出它们。
// 参考: https://github.com/modelcontextprotocol/go-sdk/blob/13488f7da1ed8eda47413df3420ab64026696798/mcp/protocol.go#L1330-L1357
const (
	methodInitialize    = "initialize"    // 初始化方法
	methodToolsCall     = "tools/call"    // 工具调用方法
	methodResourcesRead = "resources/read" // 资源读取方法
)

// telemetryMiddleware 创建一个 MCP 中间件，用于为 MCP 方法调用添加指标和日志监控。
func telemetryMiddleware(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			switch method {
			case methodInitialize:
				return telemetryHandleInitialize(ctx, method, req, next, logger)
			case methodToolsCall:
				return telemetryHandleToolCall(ctx, method, req, next, logger)
			case methodResourcesRead:
				return telemetryHandleResourceRead(ctx, method, req, next, logger)
			}

			// 如果不匹配任何支持的方法，则直接运行处理程序，不添加额外的监控
			return next(ctx, method, req)
		}
	}
}

// telemetryHandleInitialize 处理 MCP 初始化请求，记录客户端信息，
// 并在初始化成功后设置服务器就绪指标。
func telemetryHandleInitialize(ctx context.Context, method string, req mcp.Request, next mcp.MethodHandler, logger *slog.Logger) (mcp.Result, error) {
	params, ok := req.GetParams().(*mcp.InitializeParams)
	if !ok {
		// 无法提取初始化参数，直接传递，不添加监控
		logger.Warn("无法提取初始化参数用于遥测", "method", method)
		return next(ctx, method, req)
	}

	result, err := next(ctx, method, req)
	if err != nil {
		logger.Error("MCP 初始化失败", "error", err)
		return result, err
	}

	// 提取客户端信息用于日志记录
	clientName := ""
	clientVersion := ""
	if params.ClientInfo != nil {
		clientName = params.ClientInfo.Name
		clientVersion = params.ClientInfo.Version
	}

	// 从结果中提取服务器信息用于日志记录
	serverName := ""
	serverVersion := ""
	if initResult, ok := result.(*mcp.InitializeResult); ok && initResult.ServerInfo != nil {
		serverName = initResult.ServerInfo.Name
		serverVersion = initResult.ServerInfo.Version
	}

	logger.Debug("MCP 服务器初始化完成",
		"client_name", clientName,
		"client_version", clientVersion,
		"protocol_version", params.ProtocolVersion,
		"server_name", serverName,
		"server_version", serverVersion,
	)

	metricServerReady.Set(1)

	return result, nil
}

// telemetryHandleToolCall 为 tools/call 请求添加指标和日志监控。
func telemetryHandleToolCall(ctx context.Context, method string, req mcp.Request, next mcp.MethodHandler, logger *slog.Logger) (mcp.Result, error) {
	params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
	if !ok {
		// 无法提取工具信息，直接传递，不添加监控
		logger.Warn("无法提取工具参数用于遥测", "method", method)
		return next(ctx, method, req)
	}

	toolName := params.Name
	args := params.Arguments
	logger = logger.With("tool_name", toolName, "request_arguments", args)

	logger.Debug("正在调用工具")
	startTime := time.Now()
	result, err := next(ctx, method, req)
	duration := time.Since(startTime)

	metricToolCallDuration.With(prometheus.Labels{"tool_name": toolName}).Observe(duration.Seconds())
	logger.Debug("工具调用完成", "duration", duration)
	toolResult, ok := result.(*mcp.CallToolResult)
	if !ok {
		metricToolCallsFailed.With(prometheus.Labels{"tool_name": toolName}).Inc()
		logger.Error("无法将结果转换为工具调用结果")
		return result, err
	}
	if err != nil || toolResult.IsError {
		metricToolCallsFailed.With(prometheus.Labels{"tool_name": toolName}).Inc()
		logger.Error("工具调用失败", "result", result, "error", err)
	}

	return result, err
}

// telemetryHandleResourceRead 为 resources/read 请求添加指标和日志监控。
func telemetryHandleResourceRead(ctx context.Context, method string, req mcp.Request, next mcp.MethodHandler, logger *slog.Logger) (mcp.Result, error) {
	params, ok := req.GetParams().(*mcp.ReadResourceParams)
	if !ok {
		// 无法提取资源信息，直接传递，不添加监控
		logger.Warn("无法提取资源参数用于遥测", "method", method)
		return next(ctx, method, req)
	}

	uri := params.URI
	logger = logger.With("resource_uri", uri)

	logger.Debug("正在调用资源")

	startTime := time.Now()
	result, err := next(ctx, method, req)
	duration := time.Since(startTime)

	metricResourceCallDuration.With(prometheus.Labels{"resource_uri": uri}).Observe(duration.Seconds())
	logger.Debug("资源调用完成", "duration", duration)

	if err != nil {
		metricResourceCallsFailed.With(prometheus.Labels{"resource_uri": uri}).Inc()
		logger.Error("资源调用失败", "result", result, "error", err)
	}

	return result, err
}
