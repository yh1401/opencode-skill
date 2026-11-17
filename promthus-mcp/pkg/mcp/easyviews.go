package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
)

// ==================== easyviews 平台集成 ====================
//
// easyviews 平台通过统一网关 getTykjProvince 转发对内部拓扑与监控接口的调用。
// 所有请求均为 POST，请求体携带 provinceDetail 鉴权信息，并通过 reqParam
// 指定目标接口地址所需的参数。本文件封装该调用模式，并实现架构优化分析
// 所需的 5 个数据采集工具。

// easyviews 内部目标接口地址
const (
	easyviewsModuleTopologyURL   = "https://easyviews.tech.ctseelink.cn/api/autoOperation/topology/getTopologyByProductName"
	easyviewsChainTopologyURL    = "https://easyviews.tech.ctseelink.cn/api/autoOperation/topology/getTopologyByNodeProject/"
	easyviewsNodeListURL         = "https://easyviews.tech.ctseelink.cn/api/external/monitorData/getNode"
	easyviewsVirtualStructureURL = "https://easyviews.tech.ctseelink.cn/api/external/monitor/getVirtualStructure"
	easyviewsProjectTopologyURL  = "https://easyviews.tech.ctseelink.cn/api/external/monitorData/getTopologyInfo"
)

// easyviewsProvinceDetail 是网关请求的鉴权信息
type easyviewsProvinceDetail struct {
	Province  string `json:"province"`
	AppID     string `json:"appId"`
	AppSecret string `json:"appSecret"`
}

// easyviewsGatewayRequest 是发送到统一网关的请求体
type easyviewsGatewayRequest struct {
	URL            string                  `json:"url"`
	Types          string                  `json:"types"`
	ProvinceDetail easyviewsProvinceDetail `json:"provinceDetail"`
	ReqParam       any                     `json:"reqParam,omitempty"`
}

// easyviewsGatewayResponse 是网关响应的统一信封
type easyviewsGatewayResponse struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
	Msg  string          `json:"msg"`
}

// easyviewsConfigured 判断 easyviews 平台集成是否已配置鉴权信息
func (s *ServerContainer) easyviewsConfigured() bool {
	return s.easyviewsBaseURL != "" && s.easyviewsAppID != "" && s.easyviewsAppSecret != ""
}

// easyviewsCall 通过统一网关调用 easyviews 内部接口，返回响应信封中的 data 字段。
// targetMethod 是目标接口自身的请求方法（GET/POST），网关始终以 POST 转发。
func (s *ServerContainer) easyviewsCall(ctx context.Context, targetURL, targetMethod string, reqParam any) (json.RawMessage, error) {
	if !s.easyviewsConfigured() {
		return nil, fmt.Errorf("easyviews 平台未配置，请通过 --easyviews.app-id 与 --easyviews.app-secret 提供鉴权信息")
	}

	payload := easyviewsGatewayRequest{
		URL:   targetURL,
		Types: targetMethod,
		ProvinceDetail: easyviewsProvinceDetail{
			Province:  s.easyviewsProvince,
			AppID:     s.easyviewsAppID,
			AppSecret: s.easyviewsAppSecret,
		},
		ReqParam: reqParam,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 easyviews 请求失败: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, s.easyviewsTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, s.easyviewsBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建 easyviews 请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	path := "easyviews:" + targetURL
	startTs := time.Now()
	resp, err := s.easyviewsHTTPClient.Do(httpReq)
	metricAPICallDuration.With(prometheus.Labels{"target_path": path}).Observe(time.Since(startTs).Seconds())
	if err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return nil, fmt.Errorf("请求 easyviews 网关失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return nil, fmt.Errorf("easyviews 网关返回非 200 状态码: %d", resp.StatusCode)
	}

	var envelope easyviewsGatewayResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return nil, fmt.Errorf("解析 easyviews 响应失败: %w", err)
	}

	if envelope.Code != 0 {
		metricAPICallsFailed.With(prometheus.Labels{"target_path": path}).Inc()
		return nil, fmt.Errorf("easyviews 返回业务错误，code=%d, msg=%s", envelope.Code, envelope.Msg)
	}

	return envelope.Data, nil
}

// formatEasyviewsData 将 easyviews 返回的 data 按全局截断限制格式化为工具文本结果
func (s *ServerContainer) formatEasyviewsData(data json.RawMessage) *mcp.CallToolResult {
	limit := s.GetEffectiveTruncationLimit(0)
	text, truncated := truncateStringByLines(string(data), limit)
	if truncated {
		text += displayTruncationWarning(limit)
	}
	return newToolTextResult(text)
}

// easyviewsCenterValue 返回 center 参数的取值，为空时默认为 true
func easyviewsCenterValue(center string) string {
	if center == "" {
		return "true"
	}
	return center
}

// intOrDefault 返回 value，当 value 为 0 时返回默认值 fallback
func intOrDefault(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// ==================== 工具处理程序 ====================

// GetModuleTopologyHandler 处理 get_module_topology 工具
func (s *ServerContainer) GetModuleTopologyHandler(ctx context.Context, req *mcp.CallToolRequest, input ModuleTopologyInput) (*mcp.CallToolResult, any, error) {
	if input.Node == "" {
		return newToolErrorResult("node 参数是必需的"), nil, nil
	}
	if input.ProductName == "" {
		return newToolErrorResult("product_name 参数是必需的"), nil, nil
	}

	reqParam := map[string]any{
		"center":      easyviewsCenterValue(input.Center),
		"node":        input.Node,
		"productName": input.ProductName,
	}

	data, err := s.easyviewsCall(ctx, easyviewsModuleTopologyURL, http.MethodGet, reqParam)
	if err != nil {
		return newToolErrorResult("获取模块拓扑失败: " + err.Error()), nil, nil
	}
	return s.formatEasyviewsData(data), nil, nil
}

// GetChainTopologyHandler 处理 get_chain_topology 工具
func (s *ServerContainer) GetChainTopologyHandler(ctx context.Context, req *mcp.CallToolRequest, input ChainTopologyInput) (*mcp.CallToolResult, any, error) {
	if input.Node == "" {
		return newToolErrorResult("node 参数是必需的"), nil, nil
	}
	if len(input.Projects) == 0 {
		return newToolErrorResult("projects 参数是必需的，且至少需要一个项目名称"), nil, nil
	}

	reqParam := map[string]any{
		"center":      easyviewsCenterValue(input.Center),
		"calleeRange": intOrDefault(input.CalleeRange, 1),
		"callerRange": intOrDefault(input.CallerRange, 1),
		"filterNode":  input.FilterNode,
		"minute":      intOrDefault(input.Minute, 5),
		"nodeType":    input.NodeType,
		"node":        input.Node,
		"projects":    input.Projects,
	}

	data, err := s.easyviewsCall(ctx, easyviewsChainTopologyURL, http.MethodPost, reqParam)
	if err != nil {
		return newToolErrorResult("获取服务调用链路拓扑失败: " + err.Error()), nil, nil
	}
	return s.formatEasyviewsData(data), nil, nil
}

// GetNodeListHandler 处理 get_node_list 工具
func (s *ServerContainer) GetNodeListHandler(ctx context.Context, req *mcp.CallToolRequest, input NodeListInput) (*mcp.CallToolResult, any, error) {
	reqParam := map[string]any{
		"center": easyviewsCenterValue(input.Center),
	}

	data, err := s.easyviewsCall(ctx, easyviewsNodeListURL, http.MethodGet, reqParam)
	if err != nil {
		return newToolErrorResult("获取节点列表失败: " + err.Error()), nil, nil
	}

	if input.NamePrefix == "" {
		return s.formatEasyviewsData(data), nil, nil
	}

	var nodes []map[string]any
	if err := json.Unmarshal(data, &nodes); err != nil {
		return newToolErrorResult("解析节点列表失败: " + err.Error()), nil, nil
	}

	filtered := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		if name, ok := n["node"].(string); ok && strings.HasPrefix(name, input.NamePrefix) {
			filtered = append(filtered, n)
		}
	}

	filteredData, err := json.Marshal(filtered)
	if err != nil {
		return newToolErrorResult("序列化过滤后的节点列表失败: " + err.Error()), nil, nil
	}
	return s.formatEasyviewsData(filteredData), nil, nil
}

// GetVirtualStructureHandler 处理 get_virtual_structure 工具
func (s *ServerContainer) GetVirtualStructureHandler(ctx context.Context, req *mcp.CallToolRequest, input VirtualStructureInput) (*mcp.CallToolResult, any, error) {
	if input.Node == "" {
		return newToolErrorResult("node 参数是必需的"), nil, nil
	}

	reqParam := map[string]any{
		"center": easyviewsCenterValue(input.Center),
		"node":   input.Node,
	}

	data, err := s.easyviewsCall(ctx, easyviewsVirtualStructureURL, http.MethodGet, reqParam)
	if err != nil {
		return newToolErrorResult("获取物理机与云主机关系失败: " + err.Error()), nil, nil
	}
	return s.formatEasyviewsData(data), nil, nil
}

// GetProjectTopologyHandler 处理 get_project_topology 工具
func (s *ServerContainer) GetProjectTopologyHandler(ctx context.Context, req *mcp.CallToolRequest, input ProjectTopologyInput) (*mcp.CallToolResult, any, error) {
	if input.Node == "" {
		return newToolErrorResult("node 参数是必需的"), nil, nil
	}

	reqParam := map[string]any{
		"center": easyviewsCenterValue(input.Center),
		"node":   input.Node,
	}

	data, err := s.easyviewsCall(ctx, easyviewsProjectTopologyURL, http.MethodGet, reqParam)
	if err != nil {
		return newToolErrorResult("获取项目及云主机部署关系失败: " + err.Error()), nil, nil
	}
	return s.formatEasyviewsData(data), nil, nil
}
