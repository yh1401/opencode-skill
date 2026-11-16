package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"devops-automation/api/tprel/model"
	tprelService "devops-automation/api/tprel/service"
	"devops-automation/api/tprel/tplog"
	"devops-automation/global"
	"devops-automation/model/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GetOperationTopologyList 关系列表（§6.1 GET /operationTopology/list）
func GetOperationTopologyList(c *gin.Context) {
	var req model.OperationTopologyListReq
	_ = c.ShouldBindQuery(&req)
	list, total, _, err := tprelService.GetOperationTopologyList(req)
	if err != nil {
		global.GVA_LOG.Error("[operation_topology] 查询关系列表失败", zap.Error(err))
		response.FailWithMessage("查询失败: "+err.Error(), c)
		return
	}
	// /list 信封为 {code, list, pageTotal}（无 msg/data，前端翻页直接读 pageTotal）；
	// code 统一为本模块口径的数字 0 表示成功（历史返回字符串 "success"，已废弃）。
	c.JSON(http.StatusOK, gin.H{
		"code":      response.SUCCESS,
		"list":      list,
		"pageTotal": total,
	})
}

// GetOperationTopologyGraph 拓扑图（§6.1 GET /operationTopology/graph）
func GetOperationTopologyGraph(c *gin.Context) {
	var req model.GraphQuery
	_ = c.ShouldBindQuery(&req)
	graph, _ := tprelService.GetGraph(req)
	if len(graph.Nodes) == 0 {
		response.OkWithDetailed(graph, "暂无数据（今日未同步或无匹配）", c)
		return
	}
	response.OkWithData(graph, c)
}

// GetOperationTopologyDetail 节点详情（§6.2 GET /operationTopology/detail）
func GetOperationTopologyDetail(c *gin.Context) {
	var req model.NodeDetailQuery
	_ = c.ShouldBindQuery(&req)
	if req.Id == "" {
		response.FailWithMessage("缺少节点 id", c)
		return
	}
	detail, _, err := tprelService.GetNodeDetail(req)
	if err != nil {
		global.GVA_LOG.Error("[operation_topology] 查询节点详情失败", zap.String("id", req.Id), zap.Error(err))
		response.FailWithMessage("详情获取失败: "+err.Error(), c)
		return
	}
	response.OkWithData(detail, c)
}

// SyncOperationTopology 手动触发同步（异步提交，§6.1 POST /operationTopology/sync）
// 全量同步为分钟级长任务，走 HTTP 网关（LVS/Tengine）必然超时（504）；
// 改为“提交后立即返回”，由前端/网关轮询 GET /operationTopology/sync/status 获取进度与结果。
func SyncOperationTopology(c *gin.Context) {
	started, state := tprelService.StartSyncTask("手动同步(页面/sync接口)")
	if !started {
		response.OkWithDetailed(map[string]interface{}{
			"started": false,
			"message": "已有同步任务在执行/排队，请轮询 /operationTopology/sync/status",
			"state":   state,
		}, "同步任务已在进行中", c)
		return
	}
	response.OkWithDetailed(map[string]interface{}{
		"started": true,
		"message": "同步任务已提交，请轮询 /operationTopology/sync/status 获取结果",
		"state":   state,
	}, "同步任务已提交", c)
}

// GetSyncTaskStatus 查询异步同步任务状态（§6.1 GET /operationTopology/sync/status，轮询用）
func GetSyncTaskStatus(c *gin.Context) {
	response.OkWithData(tprelService.SyncTaskStatus(), c)
}

// GetOperationTopologyMiddlewareTypes 中间件类型枚举（§6.1 GET /operationTopology/middlewareTypes）
func GetOperationTopologyMiddlewareTypes(c *gin.Context) {
	types, _ := tprelService.GetMiddlewareTypes(c.Query("source"))
	response.OkWithData(types, c)
}

// SyncLogFileInfo 同步日志文件条目（列表展示用）
type SyncLogFileInfo struct {
	Date      string `json:"date"`      // 日志日期 YYYY-MM-DD
	File      string `json:"file"`      // 文件名（YYYY-MM-DD.log）
	SizeBytes int64  `json:"sizeBytes"` // 文件大小（字节）
}

// logDateRe 日志日期严格校验（YYYY-MM-DD，防目录穿越：仅允许纯日期入文件名）
var logDateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// GetSyncLogs 读取最近同步日志（§6.1 GET /operationTopology/sync/logs）
// 背景：同步任务为后台长任务，/sync/status 只回最终统计，排障需看过程日志；
// 此接口直接复用 tplog 模块当日 YYYY-MM-DD.log（目录支持 TPREL_LOG_DIR 环境变量覆盖），
// 默认以 JSON 展示日志内容与可用文件清单，download=1 时以附件形式下载完整日志。
//
// 参数（query）：
//
//	date     可选，日志日期 YYYY-MM-DD；缺省自动选择最近有内容的日志文件
//	download 可选，"1"/"true" 时直接以附件下载（text/plain），不返回 JSON
//	说明：展示模式仅返回最近 256KB 内容（日志按天累积可能很大），下载模式返回完整文件
//
// 返回（展示模式 data）：
//
//	date / file / sizeBytes / content / truncated / files（可用日志清单，日期倒序，最多 30 条）
func GetSyncLogs(c *gin.Context) {
	dir := tplog.Dir()
	date := c.Query("date")
	download := c.Query("download") == "1" || strings.EqualFold(c.Query("download"), "true")

	// 1. 目录内可用日志清单（YYYY-MM-DD.log，日期倒序）
	files := listLogFiles(dir)

	if date == "" {
		if len(files) == 0 {
			response.FailWithMessage("暂无同步日志（日志目录为空）", c)
			return
		}
		date = files[0].Date // 默认：最近一天
	} else if !logDateRe.MatchString(date) {
		response.FailWithMessage("date 参数格式非法（应为 YYYY-MM-DD）", c)
		return
	}

	absPath := filepath.Join(dir, date+".log")
	// 防御性校验：解析后的路径必须仍位于日志目录内（date 已严格正则校验，双保险防目录穿越）
	if !strings.HasPrefix(filepath.Clean(absPath), filepath.Clean(dir)+string(os.PathSeparator)) {
		response.FailWithMessage("非法日志路径", c)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		response.FailWithMessage("该日期无同步日志: "+date, c)
		return
	}

	// 2. 下载模式：原样附件输出（完整文件）
	if download {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.FileAttachment(absPath, "operation-topology_"+date+".log")
		return
	}

	// 3. 展示模式：默认返回内容（超长仅取尾部，避免响应过大）
	content, truncated, err := readLogTail(absPath, info.Size())
	if err != nil {
		response.FailWithMessage("读取日志失败: "+err.Error(), c)
		return
	}
	response.OkWithData(gin.H{
		"date":      date,
		"file":      date + ".log",
		"sizeBytes": info.Size(),
		"content":   content,
		"truncated": truncated,
		"files":     files,
	}, c)
}

// listLogFiles 列出日志目录内 YYYY-MM-DD.log 文件（日期倒序，最多 30 条）
func listLogFiles(dir string) []SyncLogFileInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []SyncLogFileInfo{}
	}
	files := make([]SyncLogFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		d := strings.TrimSuffix(e.Name(), ".log")
		if !logDateRe.MatchString(d) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, SyncLogFileInfo{Date: d, File: e.Name(), SizeBytes: info.Size()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Date > files[j].Date })
	if len(files) > 30 {
		files = files[:30]
	}
	return files
}

// readLogTail 读取日志内容：文件 ≤ maxTailBytes 时整体返回；更大时只读尾部
// （日志按天累积、可能非常大，展示只需最近一段；下载模式不受此限制）
func readLogTail(path string, size int64) (string, bool, error) {
	const maxTailBytes = 256 * 1024 // 展示上限 256KB
	if size <= maxTailBytes {
		b, err := os.ReadFile(path)
		return string(b), false, err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	if _, err := f.Seek(size-maxTailBytes, io.SeekStart); err != nil {
		return "", false, err
	}
	b, err := io.ReadAll(f)
	return string(b), true, err
}
