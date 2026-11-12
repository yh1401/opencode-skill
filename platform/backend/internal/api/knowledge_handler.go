package api

import (
	"net/http"

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
