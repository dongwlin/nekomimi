package httpapi

import (
	"net/http"
	"time"

	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/gin-gonic/gin"
)

type dashboardHandler struct {
	collector         *metrics.Collector
	llmStatusProvider LLMStatusProvider
}

func newDashboardHandler(collector *metrics.Collector, llmStatusProvider LLMStatusProvider) *dashboardHandler {
	if llmStatusProvider == nil {
		llmStatusProvider = func() metrics.LLMStatus {
			return metrics.LLMStatus{}
		}
	}
	return &dashboardHandler{
		collector:         collector,
		llmStatusProvider: llmStatusProvider,
	}
}

// overview godoc
//
//	@Summary		Get dashboard overview
//	@Description	Returns runtime status, daily/total KPIs, type distribution, and 24-hour trend.
//	@Tags			dashboard
//	@ID				dashboardOverview
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	metrics.Overview
//	@Failure		500	{object}	errorResponse
//	@Router			/dashboard/overview [get]
func (h *dashboardHandler) overview(c *gin.Context) {
	if h == nil || h.collector == nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	overview, err := h.collector.BuildOverview(time.Now(), h.llmStatusProvider())
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}
	c.JSON(http.StatusOK, overview)
}
