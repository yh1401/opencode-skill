package cronTask

import (
	"time"

	"devops-automation/api/tprel/service"
	"devops-automation/api/tprel/tplog"
	"devops-automation/global"

	"go.uber.org/zap"
)

// SyncOperationTopology 每日定时同步运维关系拓扑（§5 定时同步）
// 注册写死在 core/server.go（0 30 1 * * * ?，每天凌晨 01:30）；此处 cronExpr 仅用于日志展示/兜底
func SyncOperationTopology() {
	cronExpr := global.GVA_CONFIG.Apollo.SyncCron
	if cronExpr == "" {
		cronExpr = "0 30 1 * * * ?" // 兜底与 core/server.go 注册值一致：每天凌晨 01:30
	}
	tplog.L().Info("[operation_topology] 定时任务被触发",
		zap.String("triggerAt", time.Now().Format("2006-01-02 15:04:05.000")),
		zap.String("cron", cronExpr),
		zap.Bool("syncEnable", global.GVA_CONFIG.Apollo.SyncEnabled()))
	if !global.GVA_CONFIG.Apollo.SyncEnabled() {
		tplog.L().Info("[operation_topology] 定时同步未启用（apollo.sync_enable=false），本次跳过，不执行采集")
		return
	}
	result, err := service.SyncOperationTopology("cron定时(" + cronExpr + ")")
	if err != nil {
		tplog.L().Error("[operation_topology] 定时同步失败（执行中的详细过程见其上同步日志）", zap.Error(err))
		return
	}
	tplog.L().Info("[operation_topology] 定时同步结束（本次采集完成，详见其上同步日志）",
		zap.Int("insert", result.InsertCount),
		zap.Bool("partial", result.Partial),
		zap.Int("new", len(result.NewRelations)),
		zap.Int("lost", len(result.LostRelations)),
		zap.Int("requests", result.RequestCount),
		zap.String("syncDate", time.Now().Format("2006-01-02")))
}
