package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"aiops-platform/backend/internal/alarm"
)

// bizOverview GET /api/biz-monitor/overview
func (s *Server) bizOverview(c *gin.Context) {
	c.JSON(http.StatusOK, s.alarm.BizOverview())
}

// bizRules GET /api/biz-monitor/rules
func (s *Server) bizRules(c *gin.Context) {
	rules := s.alarm.ListBizRules()
	c.JSON(http.StatusOK, gin.H{"total": len(rules), "rules": rules})
}

// bizRuleCreate POST /api/biz-monitor/rules
func (s *Server) bizRuleCreate(c *gin.Context) {
	var rule alarm.BizRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = uuid.NewString()
	}
	if err := s.alarm.SaveBizRule(&rule); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, rule)
}

// bizRuleDelete DELETE /api/biz-monitor/rules/:id
func (s *Server) bizRuleDelete(c *gin.Context) {
	id := c.Param("id")
	if err := s.alarm.DeleteBizRule(id); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

// bizScan POST /api/biz-monitor/scan
func (s *Server) bizScan(c *gin.Context) {
	res, err := s.alarm.ScanBiz(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, res)
}
