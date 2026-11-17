// 运维关系拓扑图 · 本地独立测试服务
// 用途：绕过主项目（swagger 路由冲突等遗留问题），独立暴露 tprel 7 个接口
//
//	并静态托管前端原型，本地联调前后端。
//
// 启动：go run ./api/tprel/test/server（需先启动 mock apollo：python3 api/tprel/mock/mock_server.py）
// 访问：http://127.0.0.1:8098/prototype/topology_g6.html
//
// 数据源：file 模式（模块自初始化，无需注册）。同步（Apollo 拉取）直接写本地快照
// api/tprel/data/operation_topology.json，读接口默认读该文件；数据库为后续能力，
// 未注入 DB 连接即不依赖数据库。
package main

import (
	"fmt"
	"log"
	"net/http"

	tprelApi "devops-automation/api/tprel/api"
	tprelService "devops-automation/api/tprel/service"
	"devops-automation/config"
	"devops-automation/global"
	"devops-automation/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// 1. 日志
	loggerZap, _ := zap.NewDevelopment()
	global.GVA_LOG = loggerZap

	// 2. Apollo 配置（指向本地 Mock 服务）
	//    Mock 数据 = 生产真实数据（5 套实例 / 108 应用 / 341 中间件 / 756 依赖，见 mock/mock_server.py）
	global.GVA_CONFIG = config.Server{
		Apollo: config.Apollo{
			BaseUrl:   "http://127.0.0.1:18080",
			SessionId: "e5e27a7d1805758400287ae86741f889",
			Envs:      []string{"PRO"},
			// ScanNamespaces / MaxNsPerApp 不设置 → 走代码默认（14 个空间、上限 15），与生产扫描范围一致
			HostTimeout:    8,
			RequestTimeout: 6,
			MaxRetries:     0,
			Workers:        8,
			ScanBudgetSec:  30,
			// AppidOwnerMap：生产「应用 → PP 应用」归属映射。生产仅配置了下面 2 个应用
			// （其余应用未配置，ppApp 回退为应用名）——依据生产 /graph 返回：这 2 个应用
			// 的 ppApp 与应用名兜底结果不一致，只能来自该映射，此处按生产返回对齐。
			AppidOwnerMap: map[string]config.AppidOwner{
				"ehome-account-new-service": {PpApplication: "ehome-account-new-service"},
				"gb-cascade-gateway":        {PpApplication: "自研国标级联网关"},
			},
			// 跳① 关系接口（appId ↔ 部署项目）：生产真实路径 /thirdApi/apolloApp/list，
			// 本地联调指向 Mock 服务同路径占位接口。pageSize=-1 = 不分页、一次取全量。
			RelationApiPath:     "/thirdApi/apolloApp/list",
			RelationApiPageSize: -1,
			RelationApiTTL:      60,
			RelationApiMaxPages: 200,
		},
	}
	fmt.Println("✅ Apollo 配置加载完成（Mock: http://127.0.0.1:18080）")

	// 3. 数据源：file 模式（模块内自初始化，懒加载，无需外部注册/DB）
	fmt.Println("✅ 数据源就绪（file 模式：读/写均走 api/tprel/data 本地快照，不依赖数据库）")

	// 4. 启动时自动同步一次（保证打开页面即有数据；同步直写本地快照）
	fmt.Println("\n== 启动时自动同步 ==")
	syncResult, err := tprelService.SyncOperationTopology("启动自检")
	if err != nil {
		fmt.Printf("⚠️  同步失败（不影响服务启动，可稍后手动触发）: %v\n", err)
	} else {
		fmt.Printf("✅ 同步完成：套数=%d 应用=%d namespace=%d 请求=%d 直写快照=%d 新增=%d 消失=%d\n",
			syncResult.HostCount, syncResult.AppCount, syncResult.NamespaceCount,
			syncResult.RequestCount, syncResult.InsertCount,
			len(syncResult.NewRelations), len(syncResult.LostRelations))
	}

	// 5. gin 服务：CORS + 4 个接口 + 静态文件
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(middleware.Cors())

	// 健康检查
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"}) })

	// 业务接口（复用 api 层 handler，挂载到与主项目一致的路径前缀）
	group := r.Group("/api/external/operationTopology")
	{
		group.GET("list", tprelApi.GetOperationTopologyList)
		group.GET("graph", tprelApi.GetOperationTopologyGraph)
		group.GET("detail", tprelApi.GetOperationTopologyDetail)
		group.POST("sync", tprelApi.SyncOperationTopology)
		group.GET("sync/status", tprelApi.GetSyncTaskStatus)
		group.GET("sync/logs", tprelApi.GetSyncLogs)
		group.GET("middlewareTypes", tprelApi.GetOperationTopologyMiddlewareTypes)
		// 本地专用：前端对接文档在线查看/下载（仅本地测试服务提供，见 doc.go）
		group.GET("doc", getLocalDoc)
		// 本地专用：前端原型包下载（文件清单 / 整个目录 zip 打包，仅本地测试服务提供，见 prototype_download.go）
		group.GET("prototype/files", listPrototypeFiles)
		group.GET("prototype/download", downloadPrototypeZip)
	}

	// 静态托管前端原型（同源访问，避免跨域）
	r.Static("/prototype", "./api/tprel/prototype")

	addr := "0.0.0.0:8098" // 监听所有网卡，便于同一局域网内其它电脑访问（同事用 http://<本机局域网IP>:8098）
	fmt.Printf("\n🚀 本地测试服务已启动: http://%s\n", addr)
	fmt.Printf("   本机访问:    http://127.0.0.1:8098\n")
	fmt.Printf("   前端原型:   http://127.0.0.1:8098/prototype/topology_prototype.html\n")
	fmt.Printf("   拓扑图接口: http://127.0.0.1:8098/api/external/operationTopology/graph\n")
	fmt.Println("   (Ctrl+C 停止)")
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
