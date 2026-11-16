package router

import (
	"devops-automation/api/tprel/api"

	"github.com/gin-gonic/gin"
)

// InitOperationTopologyRouter 运维关系拓扑路由（挂载 /api/external 组，见 initialize/router.go）
func InitOperationTopologyRouter(Router *gin.RouterGroup) {
	Group := Router.Group("operationTopology")
	{
		Group.GET("list", api.GetOperationTopologyList)                       // 关系列表
		Group.GET("graph", api.GetOperationTopologyGraph)                     // 拓扑图
		Group.GET("detail", api.GetOperationTopologyDetail)                   // 节点详情
		Group.POST("sync", api.SyncOperationTopology)                         // 手动同步（异步提交，立即返回）
		Group.GET("sync/status", api.GetSyncTaskStatus)                       // 同步任务状态（轮询）
		Group.GET("sync/logs", api.GetSyncLogs)                               // 读取最近同步日志（默认展示内容，download=1 下载）
		Group.GET("middlewareTypes", api.GetOperationTopologyMiddlewareTypes) // 中间件类型枚举
	}
}
