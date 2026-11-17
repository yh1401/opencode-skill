package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"aiops-platform/backend/internal/auth"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/tickets"
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

// ticketActionReq 动作接口统一入参：version 用于乐观锁校验，note 为操作说明。
type ticketActionReq struct {
	Version    *int   `json:"version"`
	Note       string `json:"note"`
	AssigneeID string `json:"assigneeId"`
}

func (s *Server) ticketClaim(c *gin.Context) {
	req, u := s.ticketActionContext(c)
	t, err := s.tickets.Claim(c.Param("id"), u, req.Version)
	ticketResult(c, t, err)
}

func (s *Server) ticketAssign(c *gin.Context) {
	req, u := s.ticketActionContext(c)
	if u == nil || u.Role != domain.RoleAdmin {
		fail(c, http.StatusForbidden, tickets.ErrForbidden)
		return
	}
	assigneeID := strings.TrimSpace(req.AssigneeID)
	if assigneeID == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("必须指定处理人 assigneeId"))
		return
	}
	assignee, ok := s.auth.GetUser(assigneeID)
	if !ok {
		fail(c, http.StatusBadRequest, fmt.Errorf("处理人不存在: %s", assigneeID))
		return
	}
	if assignee.Role == domain.RoleViewer || !assignee.Enabled {
		fail(c, http.StatusBadRequest, fmt.Errorf("该用户不可作为处理人"))
		return
	}
	t, err := s.tickets.Assign(c.Param("id"), assignee, u, req.Note, req.Version)
	ticketResult(c, t, err)
}

func (s *Server) ticketStart(c *gin.Context) {
	req, u := s.ticketActionContext(c)
	t, err := s.tickets.Start(c.Param("id"), u, req.Version)
	ticketResult(c, t, err)
}

func (s *Server) ticketResolve(c *gin.Context) {
	req, u := s.ticketActionContext(c)
	t, err := s.tickets.Resolve(c.Param("id"), u, req.Note, req.Version)
	ticketResult(c, t, err)
}

func (s *Server) ticketClose(c *gin.Context) {
	req, u := s.ticketActionContext(c)
	t, err := s.tickets.Close(c.Param("id"), u, req.Note, req.Version)
	ticketResult(c, t, err)
}

func (s *Server) ticketReopen(c *gin.Context) {
	req, u := s.ticketActionContext(c)
	t, err := s.tickets.Reopen(c.Param("id"), u, req.Note, req.Version)
	ticketResult(c, t, err)
}

// ticketActionContext 取出当前用户与动作参数（无请求体时使用默认值）。
func (s *Server) ticketActionContext(c *gin.Context) (ticketActionReq, *domain.User) {
	u, _ := auth.CurrentUser(c)
	var req ticketActionReq
	if c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&req)
	}
	return req, u
}

func ticketResult(c *gin.Context, t *domain.Ticket, err error) {
	if err == nil {
		c.JSON(http.StatusOK, t)
		return
	}
	switch {
	case err == tickets.ErrForbidden:
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case err == tickets.ErrVersionConflict:
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case strings.Contains(err.Error(), "不存在"):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}
