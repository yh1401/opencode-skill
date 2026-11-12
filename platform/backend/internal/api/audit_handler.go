package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ---------- 资源稽核（CMDB 对账 + Prometheus 佐证） ----------

// auditRun POST /api/audit/run?date=&env=&maxHosts=&host= (host 可多次：白名单过滤，从拓扑节点钻取时传 1 个)
func (s *Server) auditRun(c *gin.Context) {
	if !s.mcp.Enabled() {
		fail(c, http.StatusServiceUnavailable, fmt.Errorf("上游服务未启用，稽核需要 CMDB / Prometheus 数据源"))
		return
	}
	hosts := c.QueryArray("host")
	maxHosts := atoiDefault(c.Query("maxHosts"), 20)
	if len(hosts) > 0 {
		maxHosts = len(hosts) // 白名单模式下强制等于 host 数
	}
	run, err := s.audit.Run(c.Query("date"), c.Query("env"), maxHosts, hosts...)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, run)
}

// auditRuns GET /api/audit/runs
func (s *Server) auditRuns(c *gin.Context) {
	runs := s.audit.ListRun(atoiDefault(c.Query("limit"), 20))
	c.JSON(http.StatusOK, gin.H{"total": len(runs), "runs": runs})
}

// auditRunGet GET /api/audit/runs/:id
func (s *Server) auditRunGet(c *gin.Context) {
	run, ok := s.audit.GetRun(c.Param("id"))
	if !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("稽核批次不存在: %s", c.Param("id")))
		return
	}
	c.JSON(http.StatusOK, run)
}

// auditFindings GET /api/audit/runs/:id/findings
func (s *Server) auditFindings(c *gin.Context) {
	id := c.Param("id")
	if _, ok := s.audit.GetRun(id); !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("稽核批次不存在: %s", id))
		return
	}
	findings := s.audit.ListFindings(id)
	c.JSON(http.StatusOK, gin.H{"total": len(findings), "findings": findings})
}
