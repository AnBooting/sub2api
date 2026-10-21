package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"
	"github.com/gin-gonic/gin"
)

// ShopSyncStatusHandler 暴露白标充值中心的商品同步状态（管理端只读）。
//
// 契约：GET /admin/shop/sync-status
//
// 设计约束：
//   - 未配置 / 上游不可用时也必须返回 200 + 明确状态，绝不 500——
//     管理端要能看到「为什么现在买不了」，而不是一个服务器错误；
//   - 响应里只有稳定错误码与本站时间戳，绝不出现凭据、上游域名或上游原始响应。
//
// 依赖由装配层显式注入（wire），不使用进程级全局引用：
// 管理面拿到的是与用户侧同一个业务服务实例，只做只读查询。
type ShopSyncStatusHandler struct {
	service *yunmaosvc.Service
}

// NewShopSyncStatusHandler 构造 handler。
//
// service 为 nil（服务未装配）时，接口仍然返回 200 + 「未配置」状态。
func NewShopSyncStatusHandler(service *yunmaosvc.Service) *ShopSyncStatusHandler {
	return &ShopSyncStatusHandler{service: service}
}

// GetSyncStatus 返回商品同步状态。
//
// GET /admin/shop/sync-status
func (h *ShopSyncStatusHandler) GetSyncStatus(c *gin.Context) {
	response.Success(c, dto.ToShopSyncStatusDTO(h.syncStatus(c)))
}

// syncStatus 汇总当前同步状态，任何情况下都不返回 error。
func (h *ShopSyncStatusHandler) syncStatus(c *gin.Context) yunmaosvc.SyncStatus {
	if h == nil || h.service == nil {
		return notConfiguredSyncStatus()
	}
	return h.service.CatalogSyncStatus(c.Request.Context())
}

// notConfiguredSyncStatus 是「服务未装配」时的兜底状态。
func notConfiguredSyncStatus() yunmaosvc.SyncStatus {
	return yunmaosvc.SyncStatus{
		Paused: true,
		Error:  string(yunmaosvc.CodeNotConfigured),
	}
}
