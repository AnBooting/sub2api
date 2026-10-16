package routes

import (
	yunmaohandler "github.com/Wei-Shaw/sub2api/internal/handler/yunmao"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterShopRoutes 注册白标充值中心路由。
//
// 用户侧（需登录）：商品目录、下单、订单列表/详情、主动查单。
// 公开侧：上游回调（由验签兜底，见 yunmaohandler.ShopHandler.Notify）。
//
// 注意：这里**不注册**任何指向上游页面的跳转路由——白标要求全程本站内闭环
// （PRD-03 §2.3 / FE-04）。
func RegisterShopRoutes(
	v1 *gin.RouterGroup,
	shopHandler *yunmaohandler.ShopHandler,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	if shopHandler == nil {
		return
	}

	authenticated := v1.Group("/shop")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	authenticated.Use(panelRateLimiter.Global())
	authenticated.Use(gin.HandlerFunc(auditLog))
	{
		authenticated.GET("/products", shopHandler.GetProducts)
		authenticated.POST("/products/sync", shopHandler.SyncProducts)
		authenticated.GET("/orders", shopHandler.ListOrders)
		authenticated.POST("/orders", shopHandler.CreateOrder)
		authenticated.GET("/orders/:orderNo", shopHandler.GetOrder)
		authenticated.POST("/orders/:orderNo/query", shopHandler.QueryOrder)
	}

	// 上游回调：无认证，靠验签兜底（算法未确定时一律拒绝，绝不默认放行）。
	v1.POST("/shop/notify", shopHandler.Notify)
}
