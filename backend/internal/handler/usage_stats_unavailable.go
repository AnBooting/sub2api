package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// respondUsageStatsIncomplete 是 service.ErrUsageStatsIncomplete 在 HTTP 层的共用映射（N-02）。
//
// 契约常量（reason/message/状态码语义）**不在本包定义**，唯一事实源是
// service.UsageStatsIncompleteReason / service.UsageStatsIncompleteMessage：
// handler（用户端）与 handler/admin（管理端）都引用同一份常量，因此两端文案与
// 码值不可能漂移。admin 包有同名文件 admin/usage_stats_unavailable.go，同样只做映射。
//
// 该状态不是故障：预聚合数据尚未覆盖所请求区间时，仓库层刻意**不回退**到原始
// usage_logs（否则会把不完整区间当完整结果返回）。此前各 handler 把它映射成
// 500 "Failed to ..."，预期业务态被报成服务端故障；用户端更糟——静默返回空数组，
// 会被读成"当天没有用量"。
//
// 返回 true 表示已写出响应，调用方必须直接 return；返回 false 表示不是该业务态，
// 调用方继续走原有错误映射（真实基础设施故障仍然必须是 500）。
func respondUsageStatsIncomplete(c *gin.Context, err error) bool {
	if !errors.Is(err, service.ErrUsageStatsIncomplete) {
		return false
	}
	response.ErrorWithDetails(c, http.StatusServiceUnavailable,
		service.UsageStatsIncompleteMessage, service.UsageStatsIncompleteReason, nil)
	return true
}
