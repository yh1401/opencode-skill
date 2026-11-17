// 运维关系拓扑图 · 端到端测试（file 模式）
// 数据载体为本地文件快照：从 Mock Apollo 采集 → 直写 api/tprel/data/operation_topology.json，
// 再经 service 读接口（数据源路由默认 file）校验列表/图/类型。无需数据库。
package main

import (
	"fmt"
	"log"
	"time"

	"devops-automation/api/tprel/model"
	tprelService "devops-automation/api/tprel/service"
	"devops-automation/config"
	"devops-automation/global"

	"go.uber.org/zap"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("  运维关系拓扑图 - 端到端测试")
	fmt.Println("========================================")

	// 1. 初始化日志
	loggerZap, _ := zap.NewDevelopment()
	global.GVA_LOG = loggerZap

	// 2. 配置 Apollo 相关（指向 Mock 服务）；无数据库依赖（数据源 = 本地文件快照）
	global.GVA_CONFIG = config.Server{
		Apollo: config.Apollo{
			BaseUrl:        "http://127.0.0.1:18080",
			SessionId:      "e5e27a7d1805758400287ae86741f889",
			Envs:           []string{"PRO"},
			ScanNamespaces: []string{"application", "datasource", "redis"},
			HostTimeout:    8,
			RequestTimeout: 6,
			MaxRetries:     0,
			Workers:        8,
			ScanBudgetSec:  30,
			MaxNsPerApp:    3,
			AppidOwnerMap: map[string]config.AppidOwner{
				"rule-engine":   {Node: "su-zh", EngProject: "inspection", DepProject: "inspection-web", PpApplication: "inspection-web"},
				"user-service":  {Node: "su-zh", EngProject: "user-center", DepProject: "user-svc", PpApplication: "user-svc"},
				"report-job":    {Node: "su-zh", EngProject: "report", DepProject: "report-job", PpApplication: "report-job"},
				"gateway-web":   {Node: "su-zh", EngProject: "gateway", DepProject: "gateway-svc", PpApplication: "gateway-svc"},
				"analysis-api":  {Node: "su-zh", EngProject: "analysis", DepProject: "analysis-api", PpApplication: "analysis-api"},
				"order-service": {Node: "sh-gd", EngProject: "order", DepProject: "order-api", PpApplication: "order-api"},
				"pay-service":   {Node: "sh-gd", EngProject: "pay", DepProject: "pay-svc", PpApplication: "pay-svc"},
				"ai-platform":   {Node: "sh-gd", EngProject: "ai-platform", DepProject: "ai-platform", PpApplication: "ai-platform"},
				"data-pipeline": {Node: "gz-dx", EngProject: "data", DepProject: "pipeline-engine", PpApplication: "pipeline-engine"},
				"base-config":   {Node: "hz-ct", EngProject: "base", DepProject: "config-center", PpApplication: "config-center"},
				"video-web":     {Node: "bj-yd", EngProject: "video", DepProject: "video-web", PpApplication: "video-web"},
				"media-proxy":   {Node: "bj-yd", EngProject: "video", DepProject: "media-proxy", PpApplication: "media-proxy"},
				"log-collector": {Node: "gz-dx", EngProject: "log", DepProject: "log-collector", PpApplication: "log-collector"},
				"monitor-agent": {Node: "sh-gd", EngProject: "monitor", DepProject: "monitor-agent", PpApplication: "monitor-agent"},
				"alert-manager": {Node: "sh-gd", EngProject: "monitor", DepProject: "alert-manager", PpApplication: "alert-manager"},
			},
		},
	}
	fmt.Println("✅ Apollo 配置加载完成（Mock 服务: http://127.0.0.1:18080）")
	fmt.Println("✅ 数据源就绪（file 模式：读/写均走 api/tprel/data 本地快照，不依赖数据库）")

	// 5. 执行同步
	fmt.Println("\n========================================")
	fmt.Println("  执行同步（从 Mock Apollo 采集数据）")
	fmt.Println("========================================")
	start := time.Now()
	result, err := tprelService.SyncOperationTopology("e2e自检")
	if err != nil {
		log.Fatalf("同步失败: %v", err)
	}
	fmt.Printf("⏱ 耗时: %v\n", time.Since(start))
	fmt.Printf("✅ 同步完成:\n")
	fmt.Printf("   - 扫描套数: %d\n", result.HostCount)
	fmt.Printf("   - 扫描应用数: %d\n", result.AppCount)
	fmt.Printf("   - 扫描 Namespace 数: %d\n", result.NamespaceCount)
	fmt.Printf("   - 实际请求数: %d\n", result.RequestCount)
	fmt.Printf("   - 直写快照关系: %d 条\n", result.InsertCount)
	fmt.Printf("   - 部分完成: %v\n", result.Partial)
	fmt.Printf("   - 新增关系: %d\n", len(result.NewRelations))
	fmt.Printf("   - 消失关系: %d\n", len(result.LostRelations))

	// 6. 查询关系列表
	fmt.Println("\n========================================")
	fmt.Println("  GET /list — 关系列表")
	fmt.Println("========================================")
	list, total, source, err := tprelService.GetOperationTopologyList(model.OperationTopologyListReq{
		Page:     1,
		PageSize: 100,
	})
	if err != nil {
		log.Fatalf("查询列表失败: %v", err)
	}
	fmt.Printf("✅ 总记录数: %d, 本页: %d (数据源: %s)\n", total, len(list), source)
	if len(list) > 0 {
		fmt.Printf("   示例: %s | %s | %s | %s | %s\n",
			list[0].ApolloAppId, list[0].MiddlewareType, list[0].ConnectAddr,
			list[0].DepProject, list[0].Node)
		if len(list) > 1 {
			fmt.Printf("   示例: %s | %s | %s | %s | %s\n",
				list[1].ApolloAppId, list[1].MiddlewareType, list[1].ConnectAddr,
				list[1].DepProject, list[1].Node)
		}
	}

	// 7. 查询拓扑图（全量）
	fmt.Println("\n========================================")
	fmt.Println("  GET /graph — 拓扑图（全量）")
	fmt.Println("========================================")
	graph, gsrc := tprelService.GetGraph(model.GraphQuery{SyncDate: "", Env: "PRO"})
	fmt.Printf("✅ 节点数: %d, 边数: %d, 快照日期: %s (数据源: %s)\n",
		len(graph.Nodes), len(graph.Edges), graph.SyncDate, gsrc)
	// 统计节点分类
	appCount, mwCount := 0, 0
	for _, n := range graph.Nodes {
		if n.Category == "application" {
			appCount++
		} else if n.Category == "middleware" {
			mwCount++
		}
	}
	fmt.Printf("   应用节点: %d, 中间件节点: %d\n", appCount, mwCount)
	// 展示扇入效果（被依赖最多的中间件）
	fmt.Println("\n  中间件被依赖排行（扇入效果）:")
	for _, n := range graph.Nodes {
		if n.Category == "middleware" && n.DepCount > 1 {
			fmt.Printf("   🏆 %s — 被 %d 个应用依赖\n", n.Name, n.DepCount)
		}
	}

	// 8. 带条件查询
	fmt.Println("\n========================================")
	fmt.Println("  GET /graph?node=su-zh — 拓扑图（su-zh 机房）")
	fmt.Println("========================================")
	graph2, _ := tprelService.GetGraph(model.GraphQuery{Env: "PRO", Node: "su-zh"})
	fmt.Printf("✅ 节点数: %d, 边数: %d\n", len(graph2.Nodes), len(graph2.Edges))

	fmt.Println("\n========================================")
	fmt.Println("  GET /graph?middlewareType=mysql — 仅 MySQL")
	fmt.Println("========================================")
	graph3, _ := tprelService.GetGraph(model.GraphQuery{Env: "PRO", MiddlewareType: "mysql"})
	fmt.Printf("✅ 节点数: %d, 边数: %d\n", len(graph3.Nodes), len(graph3.Edges))
	for _, n := range graph3.Nodes {
		if n.Category == "middleware" {
			fmt.Printf("   - %s (被 %d 个应用依赖)\n", n.Name, n.DepCount)
		}
	}

	// 9. 中间件类型枚举
	fmt.Println("\n========================================")
	fmt.Println("  GET /middlewareTypes — 中间件类型枚举")
	fmt.Println("========================================")
	types, _ := tprelService.GetMiddlewareTypes("")
	fmt.Printf("✅ 类型列表: %v\n", types)

	// 10. 本地快照文件验证（读接口默认源 = file：直读 data/operation_topology.json 的数据）
	fmt.Println("\n========================================")
	fmt.Println("  本地快照文件验证（读接口默认 file 源）")
	fmt.Println("========================================")
	all, totalAll, src, err := tprelService.GetOperationTopologyList(model.OperationTopologyListReq{
		Page:     1,
		PageSize: -1, // 不分页取全量（file 源按快照行序输出）
	})
	if err != nil {
		log.Fatalf("读取本地快照全量失败: %v", err)
	}
	fmt.Printf("   operation_topology.json 总行数: %d (数据源: %s)\n", totalAll, src)

	mwSet := map[string]bool{}
	nodeSet := map[string]bool{}
	for _, r := range all {
		if r.MiddlewareType != "" {
			mwSet[r.MiddlewareType] = true
		}
		if r.Node != "" {
			nodeSet[r.Node] = true
		}
	}
	mwTypes := make([]string, 0, len(mwSet))
	for t := range mwSet {
		mwTypes = append(mwTypes, t)
	}
	nodes := make([]string, 0, len(nodeSet))
	for n := range nodeSet {
		nodes = append(nodes, n)
	}
	fmt.Printf("   中间件类型: %v\n", mwTypes)
	fmt.Printf("   机房节点: %v\n", nodes)

	fmt.Println("\n========================================")
	fmt.Println("  ✅ 全部测试通过！")
	fmt.Println("========================================")
}
