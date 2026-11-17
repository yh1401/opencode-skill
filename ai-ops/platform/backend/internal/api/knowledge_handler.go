package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// knowledgeList GET /api/knowledge?q=&appId=&limit=
func (s *Server) knowledgeList(c *gin.Context) {
	out := s.kb.List(c.Query("q"), c.Query("appId"), atoiDefault(c.Query("limit"), 100))
	c.JSON(http.StatusOK, gin.H{"total": len(out), "cases": out})
}

// knowledgeStats GET /api/knowledge/stats
func (s *Server) knowledgeStats(c *gin.Context) {
	c.JSON(http.StatusOK, s.kb.Stats())
}

// knowledgeSimilar GET /api/knowledge/similar?appId=&symptom=&limit=
func (s *Server) knowledgeSimilar(c *gin.Context) {
	out := s.kb.Similar(c.Query("appId"), c.Query("symptom"), atoiDefault(c.Query("limit"), 5))
	c.JSON(http.StatusOK, gin.H{"total": len(out), "cases": out})
}

// knowledgeGet GET /api/knowledge/:id  案例 ID 即排障会话 ID
func (s *Server) knowledgeGet(c *gin.Context) {
	item, ok := s.kb.Get(c.Param("id"))
	if !ok {
		fail(c, http.StatusNotFound, nil)
		return
	}
	c.JSON(http.StatusOK, item)
}

// knowledgeReindex POST /api/knowledge/reindex  全量归档排障会话
func (s *Server) knowledgeReindex(c *gin.Context) {
	n := s.kb.Reindex()
	c.JSON(http.StatusOK, gin.H{"indexed": n})
}

// knowledgeEnrich POST /api/knowledge/enrich?id=&all=1&fresh=1&limit=N
// LLM 对知识库案例做结构化总结（summary / category / keywords / confidence）。
// 参数：
//   id       只 enrich 单条 Case ID；非空时 all/fresh/limit 忽略
//   all=1    全量 enrich 所有 Case
//   fresh=1  只 enrich 还没 AI 总结过的（默认）
//   limit=N  全量模式下最多处理多少条
// 静默降级：LLM 调用失败时单条跳过不阻塞整体，返回 enriched/failed 计数。
func (s *Server) knowledgeEnrich(c *gin.Context) {
	if !s.kb.EnrichEnabled() {
		fail(c, http.StatusServiceUnavailable, fmt.Errorf("LLM_API_KEY 未配置，需要设置 LLM_API_KEY / LLM_BASE_URL / LLM_MODEL 后重启后端"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()

	id := c.Query("id")
	if id != "" {
		if err := s.kb.EnrichOne(ctx, id); err != nil {
			fail(c, http.StatusUnprocessableEntity, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"enriched": 1, "failed": 0})
		return
	}

	all := c.Query("all") == "1"
	limit := atoiDefault(c.Query("limit"), 0)
	var total, enriched, failed int
	if all {
		total, enriched, failed = s.kb.EnrichAll(ctx, limit)
	} else {
		total, enriched, failed = s.kb.EnrichFresh(ctx)
	}
	c.JSON(http.StatusOK, gin.H{
		"total":    total,
		"enriched": enriched,
		"failed":   failed,
		"mode":     func() string { if all { return "all" }; return "fresh" }(),
	})
}
