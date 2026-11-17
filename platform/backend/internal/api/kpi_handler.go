package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/kpi"
)

// metricsKPI GET /api/metrics/kpi?days=7
// 汇总告警 → 排障 → 建单的时延分布、达标率与自动化执行率。
func (s *Server) metricsKPI(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	// 幂等同步：保证 root_caused 会话已生成工单，避免建单类指标偏低
	s.tickets.SyncFromSessions()
	c.JSON(http.StatusOK, kpi.Compute(s.store, days))
}
