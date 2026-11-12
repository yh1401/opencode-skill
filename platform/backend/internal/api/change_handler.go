package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/changes"
	"aiops-platform/backend/internal/domain"
)

// ---------- 变更方案 / 风险评估 ----------

func (s *Server) changeTemplates(c *gin.Context) {
	tpls := changes.Templates()
	c.JSON(http.StatusOK, gin.H{"total": len(tpls), "templates": tpls})
}

func (s *Server) changeGenerate(c *gin.Context) {
	var tgt domain.ChangeTarget
	if err := c.ShouldBindJSON(&tgt); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	plan, err := s.changes.GeneratePlan(tgt)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (s *Server) changeGet(c *gin.Context) {
	id := c.Param("id")
	plan, ok := s.changes.GetPlan(id)
	if !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("方案不存在: %s", id))
		return
	}
	c.JSON(http.StatusOK, plan)
}

func (s *Server) changeList(c *gin.Context) {
	plans := s.changes.ListPlans()
	limit := atoiDefault(c.Query("limit"), 50)
	if limit > 0 && len(plans) > limit {
		plans = plans[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"total": len(plans), "plans": plans})
}
