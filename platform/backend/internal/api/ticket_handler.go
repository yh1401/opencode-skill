package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/domain"
)

// ---------- 隐患与工单 ----------

func (s *Server) ticketList(c *gin.Context) {
	tickets := s.tickets.List()
	limit := atoiDefault(c.Query("limit"), 50)
	if limit > 0 && len(tickets) > limit {
		tickets = tickets[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"total": len(tickets), "tickets": tickets})
}

func (s *Server) ticketGet(c *gin.Context) {
	id := c.Param("id")
	t, ok := s.tickets.Get(id)
	if !ok {
		fail(c, http.StatusNotFound, fmt.Errorf("工单不存在: %s", id))
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) ticketUpdateStatus(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Status   domain.TicketStatus `json:"status"`
		Assignee string              `json:"assignee,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	t, err := s.tickets.UpdateStatus(id, req.Status, req.Assignee)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) ticketResolve(c *gin.Context) {
	id := c.Param("id")
	t, err := s.tickets.Resolve(id)
	if err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, t)
}
