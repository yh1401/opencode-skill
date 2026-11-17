package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// qualityOverview GET /api/quality/overview?appId=
func (s *Server) qualityOverview(c *gin.Context) {
	c.JSON(http.StatusOK, s.alarm.QualityOverview(c.Query("appId")))
}

// qualityScan POST /api/quality/scan  body: {"appId":"...","windowMin":60}
func (s *Server) qualityScan(c *gin.Context) {
	var body struct {
		AppID     string `json:"appId"`
		WindowMin int    `json:"windowMin"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.AppID) == "" {
		fail(c, http.StatusBadRequest, nil)
		return
	}
	report, err := s.alarm.AnalyzeQuality(c.Request.Context(), body.AppID, body.WindowMin)
	if err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, report)
}

// qualityReports GET /api/quality/reports?appId=&limit=
func (s *Server) qualityReports(c *gin.Context) {
	limit := atoiDefault(c.Query("limit"), 20)
	reports := s.alarm.ListQualityReports(c.Query("appId"), limit)
	c.JSON(http.StatusOK, gin.H{"total": len(reports), "reports": reports})
}

// qualityReportGet GET /api/quality/reports/:id
func (s *Server) qualityReportGet(c *gin.Context) {
	report, ok := s.alarm.GetQualityReport(c.Param("id"))
	if !ok {
		fail(c, http.StatusNotFound, nil)
		return
	}
	c.JSON(http.StatusOK, report)
}
