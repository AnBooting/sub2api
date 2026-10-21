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
// 【用途】回答管理员的一个问题：「现在有没有人在因缺价被拒」。
// 缺价不得放行：未定价模型在转发上游前被拒（503 + pricing_unavailable），
// 而这条拒绝本来就会落一条错误记录——本接口把这些记录按「模型 + 分组」聚合，
// 让「忘了配价」不再是一个只有用户能看到的静默故障。
//
// 【只读观察】不改写错误记录、不放行缺价模型、不按零价计费；
// 数据来自既有的 ops_error_logs（审计机制），不新增表、不新增迁移。
//
// 【数据前提与保留期】沿用既有 ops 端点的约定：**运维监控与错误日志都要开着**才会写入，
// 因此关闭监控时本列表为空；数据保留期跟随错误日志的保留与清理策略——
// 日志被清理后历史查不到属预期行为，本接口不做额外留痕。
//
// 【被动通知】目前**没有**针对缺价的推送通道：既有 ops 告警规则（ops_alert_rules）
// 只能按指标类型 + 平台/分组/区域配置，无法按「缺价」这一错误分类过滤，
// 因此规则只能覆盖整体错误率、不能只盯缺价。要做精确推送需要新的指标管道，列为待办。
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
