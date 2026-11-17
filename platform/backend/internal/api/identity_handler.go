package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// identityStats GET /api/identity/stats
func (s *Server) identityStats(c *gin.Context) {
	if s.identity == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": false, "stats": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": s.identity.Enabled(), "stats": s.identity.Stats()})
}

// identityApps GET /api/identity/apps?q=&limit=
func (s *Server) identityApps(c *gin.Context) {
	if s.identity == nil {
		c.JSON(http.StatusOK, gin.H{"total": 0, "apps": []any{}})
		return
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	limit := atoiDefault(c.Query("limit"), 100)
	all := s.identity.Apps()
	out := make([]any, 0, len(all))
	for _, a := range all {
		if q != "" && !strings.Contains(strings.ToLower(a.Key+" "+a.ApolloAppID+" "+a.ApolloAppName+" "+a.PPApplication+" "+a.PinpointAppName), q) {
			continue
		}
		out = append(out, a)
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "apps": out})
}

// identityHosts GET /api/identity/hosts?q=&app=&limit=
func (s *Server) identityHosts(c *gin.Context) {
	if s.identity == nil {
		c.JSON(http.StatusOK, gin.H{"total": 0, "hosts": []any{}})
		return
	}
	q := strings.ToLower(strings.TrimSpace(c.Query("q")))
	app := strings.TrimSpace(c.Query("app"))
	appKey := ""
	if app != "" {
		if a, ok := s.identity.ResolveApp(app); ok {
			appKey = a.Key
		}
	}
	limit := atoiDefault(c.Query("limit"), 100)
	all := s.identity.Hosts()
	out := make([]any, 0, len(all))
	for _, h := range all {
		if appKey != "" && !containsFold(h.Apps, appKey) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(h.Key+" "+strings.Join(h.IPs, " ")+" "+strings.Join(h.HostNames, " ")), q) {
			continue
		}
		out = append(out, h)
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "hosts": out})
}

// identityRebuild POST /api/identity/rebuild {date, env}
func (s *Server) identityRebuild(c *gin.Context) {
	if s.identity == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "身份映射未初始化"})
		return
	}
	var req struct {
		Date string `json:"date"`
		Env  string `json:"env"`
	}
	_ = c.ShouldBindJSON(&req)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	stats, err := s.identity.Rebuild(ctx, req.Date, req.Env)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "stats": stats})
		return
	}
	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// identityResolve GET /api/identity/resolve?appId=&addr=
func (s *Server) identityResolve(c *gin.Context) {
	out := gin.H{}
	if s.identity != nil {
		if appID := strings.TrimSpace(c.Query("appId")); appID != "" {
			if a, ok := s.identity.ResolveApp(appID); ok {
				out["app"] = a
			}
		}
		if addr := strings.TrimSpace(c.Query("addr")); addr != "" {
			if h, ok := s.identity.ResolveHost(addr); ok {
				out["host"] = h
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

func containsFold(list []string, target string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
