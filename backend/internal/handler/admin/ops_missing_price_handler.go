package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ListMissingPriceRejections 返回缺价拒绝清单（按「模型 + 分组」聚合）。
//
// GET /admin/ops/missing-price-rejections?window_hours=24
//
// 缺价不得放行：未定价模型在转发上游前被拒（503 + pricing_unavailable）。
// 这个接口只做**观察**——不改写错误记录、不放行缺价模型、不按零价计费。
// 数据来自既有的 ops_error_logs（审计机制），不新增表、不新增迁移。
func (h *OpsHandler) ListMissingPriceRejections(c *gin.Context) {
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	window := service.DefaultMissingPriceRejectionWindow
	if raw := strings.TrimSpace(c.Query("window_hours")); raw != "" {
		hours, err := strconv.Atoi(raw)
		if err != nil || hours <= 0 || hours > 24*30 {
			response.BadRequest(c, "Invalid window_hours")
			return
		}
		window = time.Duration(hours) * time.Hour
	}

	now := time.Now()
	items, err := h.opsService.MissingPriceRejections(c.Request.Context(), &service.MissingPriceRejectionFilter{
		StartTime: now.Add(-window),
		EndTime:   now,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}
