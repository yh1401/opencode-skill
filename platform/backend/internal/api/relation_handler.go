package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// relationScan POST /api/relations/scan
// 基于拓扑 Diff + 最近一次稽核 C3 孤儿生成关系修正建议（幂等）。
func (s *Server) relationScan(c *gin.Context) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
		Env  string `json:"env"`
	}
	_ = c.ShouldBindJSON(&req)
	env := strings.TrimSpace(req.Env)
	if env == "" {
		env = s.cfg.DefaultEnv
	}
	res, err := s.rel.Scan(strings.TrimSpace(req.From), strings.TrimSpace(req.To), env)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// relationList GET /api/relations?status=&kind=
func (s *Server) relationList(c *gin.Context) {
	out := s.rel.List(c.Query("status"), c.Query("kind"))
	c.JSON(http.StatusOK, gin.H{"total": len(out), "proposals": out})
}

// relationStats GET /api/relations/stats
func (s *Server) relationStats(c *gin.Context) {
	c.JSON(http.StatusOK, s.rel.Stats())
}

// relationOverrides GET /api/relations/overrides
func (s *Server) relationOverrides(c *gin.Context) {
	out := s.rel.Overrides()
	c.JSON(http.StatusOK, gin.H{"total": len(out), "overrides": out})
}

// relationUpdate PATCH /api/relations/:id  审批：approved / rejected
func (s *Server) relationUpdate(c *gin.Context) {
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	p, err := s.rel.UpdateStatus(c.Param("id"), req.Status, req.Note)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

// relationApply POST /api/relations/:id/apply  执行修正（仅 approved 且可自动修正）
func (s *Server) relationApply(c *gin.Context) {
	p, err := s.rel.Apply(c.Param("id"))
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, p)
}
