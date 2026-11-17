package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/inspect"
	"aiops-platform/backend/internal/store"
)

// ---------- 规则配置化 ----------

// inspectRules 列出所有内置规则定义（只读）。
func (s *Server) inspectRules(c *gin.Context) {
	rules := inspect.GetRules()
	c.JSON(http.StatusOK, gin.H{"total": len(rules), "rules": rules})
}

// inspectUpdateTask 更新巡检任务——核心是 rules 字段的覆盖。
func (s *Server) inspectUpdateTask(c *gin.Context) {
	id := c.Param("id")
	var task domain.InspectionTask
	if !store.GetInto(s.store, "inspection_task", id, &task) {
		fail(c, http.StatusNotFound, fmt.Errorf("任务不存在: %s", id))
		return
	}
	// 只允许更新 rules / scope / enabled / cron / name / scene
	var patch struct {
		Name    string                         `json:"name"`
		Scene   string                         `json:"scene"`
		Scope   *domain.Scope                  `json:"scope"`
		Cron    string                         `json:"cron"`
		Enabled *bool                          `json:"enabled"`
		Rules   map[string]domain.RuleOverride `json:"rules"`
	}
	if err := c.ShouldBindJSON(&patch); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if patch.Name != "" {
		task.Name = strings.TrimSpace(patch.Name)
	}
	if patch.Scene != "" {
		task.Scene = strings.TrimSpace(patch.Scene)
	}
	if patch.Scope != nil {
		task.Scope = *patch.Scope
	}
	if patch.Cron != "" {
		expr := strings.TrimSpace(patch.Cron)
		if expr != "-" && expr != "off" { // 保留清除 cron 的写法
			if err := inspect.ValidateCron(expr); err != nil {
				fail(c, http.StatusBadRequest, err)
				return
			}
			task.Cron = expr
		} else {
			task.Cron = ""
		}
	}
	if patch.Enabled != nil {
		task.Enabled = *patch.Enabled
	}
	if patch.Rules != nil {
		if task.Rules == nil {
			task.Rules = map[string]domain.RuleOverride{}
		}
		for code, ov := range patch.Rules {
			task.Rules[code] = ov
		}
	}
	if err := s.store.Put("inspection_task", task.ID, task); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, task)
}
