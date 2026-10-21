// Package yunmao 提供白标充值中心的 HTTP 接口。
//
// 白标红线（PRD-03 FE-04）：本包所有响应字段与错误文案都不得出现上游品牌、域名、
// 店铺标识；上游订单号、凭据、卡密密文一律不出现在响应里。
package yunmao

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"

	"github.com/gin-gonic/gin"
)

// maxCallbackBodyBytes 限制回调请求体大小。
const maxCallbackBodyBytes = 1 << 20 // 1 MiB

// ShopHandler 是白标充值中心的用户侧与回调侧 handler。
type ShopHandler struct {
	service *yunmaosvc.Service
}

// NewShopHandler 构造 handler。service 为 nil 时所有接口都 fail closed（503 未配置）。
func NewShopHandler(service *yunmaosvc.Service) *ShopHandler {
	return &ShopHandler{service: service}
}

// GetProducts 返回商品目录。
//
// GET /api/v1/shop/products
func (h *ShopHandler) GetProducts(c *gin.Context) {
	if h.service == nil {
		writeError(c, yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "白标充值服务未配置", nil))
		return
	}
	response.Success(c, toCatalogDTO(h.service.CatalogState(c.Request.Context())))
}

// SyncProducts 主动触发一次商品同步（管理员或定时任务使用）。
//
// POST /api/v1/shop/products/sync
//
// 仅管理员：同步会发起上游调用并改写商品目录，普通用户不得触发。
func (h *ShopHandler) SyncProducts(c *gin.Context) {
	if h.service == nil {
		writeError(c, yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "白标充值服务未配置", nil))
		return
	}
	role, ok := middleware2.GetUserRoleFromContext(c)
	if !ok || role != service.RoleAdmin {
		response.Forbidden(c, "需要管理员权限")
		return
	}
	state, err := h.service.SyncCatalog(c.Request.Context())
	if err != nil {
		// 同步失败时仍返回当前目录状态（保留上一次成功商品），并带明确错误码。
		writeError(c, err)
		return
	}
	response.Success(c, toCatalogDTO(state))
}

// CreateOrder 下单。
//
// POST /api/v1/shop/orders
func (h *ShopHandler) CreateOrder(c *gin.Context) {
	if h.service == nil {
		writeError(c, yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "白标充值服务未配置", nil))
		return
	}
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	var req createOrderRequestDTO
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorWithDetails(c, http.StatusBadRequest, "请求体非法", "shop_request_invalid", nil)
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		// 幂等键必填：缺了就无法保证「重复点击只有一条映射」（PRD-03 BE-04）。
		response.ErrorWithDetails(c, http.StatusBadRequest, "缺少请求标识", "shop_idempotency_key_required", nil)
		return
	}
	if req.Quantity < 1 {
		req.Quantity = 1
	}
	order, err := h.service.CreateOrder(c.Request.Context(), subject.UserID, yunmaosvc.CreateOrderRequest{
		ProductID:      req.ProductID,
		Quantity:       req.Quantity,
		IdempotencyKey: req.IdempotencyKey,
		BuyerRef:       "u" + strconv.FormatInt(subject.UserID, 10),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toOrderDTO(*order))
}

// ListOrders 列出当前用户的订单。
//
// GET /api/v1/shop/orders
func (h *ShopHandler) ListOrders(c *gin.Context) {
	if h.service == nil {
		writeError(c, yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "白标充值服务未配置", nil))
		return
	}
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	limit := 20
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	orders, err := h.service.ListOrders(c.Request.Context(), subject.UserID, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	items := make([]orderDTO, 0, len(orders))
	for _, order := range orders {
		items = append(items, toOrderDTO(order))
	}
	response.Success(c, items)
}

// GetOrder 读取订单详情（仅本人可见，含卡密）。
//
// GET /api/v1/shop/orders/:orderNo
func (h *ShopHandler) GetOrder(c *gin.Context) {
	if h.service == nil {
		writeError(c, yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "白标充值服务未配置", nil))
		return
	}
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	order, err := h.service.GetOrder(c.Request.Context(), subject.UserID, c.Param("orderNo"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toOrderDTO(*order))
}

// QueryOrder 主动查单：支付结果的唯一可信来源之一（PRD-03 §2.4 / BE-03）。
//
// POST /api/v1/shop/orders/:orderNo/query
func (h *ShopHandler) QueryOrder(c *gin.Context) {
	if h.service == nil {
		writeError(c, yunmaosvc.NewError(yunmaosvc.CodeNotConfigured, "白标充值服务未配置", nil))
		return
	}
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	order, err := h.service.QueryOrder(c.Request.Context(), subject.UserID, c.Param("orderNo"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toOrderDTO(*order))
}

// Notify 处理上游支付回调。
//
// POST /api/v1/shop/notify
//
// 高危红线：验签未通过或签名算法未确定时一律 401 且**不改任何状态**，
// 绝不因为请求体里写了 status=paid 就把订单当作已支付（PRD-03 §2.4 / AC-B1）。
func (h *ShopHandler) Notify(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusUnauthorized, response.Response{
			Code:    http.StatusUnauthorized,
			Message: "回调验签不可用",
			Reason:  string(yunmaosvc.CodeCallbackSignatureUnavailable),
		})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCallbackBodyBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, response.Response{
			Code:    http.StatusBadRequest,
			Message: "回调请求体读取失败",
			Reason:  string(yunmaosvc.CodeCallbackPayloadInvalid),
		})
		return
	}
	if len(body) > maxCallbackBodyBytes {
		c.JSON(http.StatusRequestEntityTooLarge, response.Response{
			Code:    http.StatusRequestEntityTooLarge,
			Message: "回调请求体过大",
			Reason:  string(yunmaosvc.CodeCallbackPayloadInvalid),
		})
		return
	}

	decision := h.service.ApplyCallback(c.Request.Context(), c.Request.Header, body)
	if !decision.Accepted {
		// 按错误码区分类别：只有签名/载荷校验失败才是 401；未知订单是 404、
		// 上游暂不可用是 503。统一返回 401 会让上游把可恢复错误当成鉴权失败，
		// 无意义地反复重试，也掩盖了真实原因。不改状态、不回显上游内容。
		status := callbackStatusForCode(decision.Code)
		c.JSON(status, response.Response{
			Code:    status,
			Message: "回调未通过校验",
			Reason:  string(decision.Code),
		})
		return
	}
	response.Success(c, gin.H{"accepted": true})
}

// ---------------------------------------------------------------------------
// 内部
// ---------------------------------------------------------------------------

// callbackStatusForCode 把回调判定码映射为 HTTP 状态码。
func callbackStatusForCode(code yunmaosvc.ErrorCode) int {
	switch code {
	// 签名缺失/算法未确定/签名不匹配都属于鉴权失败，必须保持 401（PRD-03 AC-B1 红线）。
	case yunmaosvc.CodeCallbackSignatureUnavailable, yunmaosvc.CodeCallbackSignatureInvalid, yunmaosvc.CodeCallbackPayloadInvalid:
		return http.StatusUnauthorized
	case yunmaosvc.CodeOrderNotFound:
		return http.StatusNotFound
	case yunmaosvc.CodeNotConfigured:
		return http.StatusServiceUnavailable
	default:
		// 其余（上游不可用/响应非法/结算失败）按暂时性故障处理，便于上游重试。
		return http.StatusServiceUnavailable
	}
}

func requireAuth(c *gin.Context) (middleware2.AuthSubject, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return middleware2.AuthSubject{}, false
	}
	return subject, true
}

// writeError 把适配层错误映射为 HTTP 响应。
//
// 只使用本站语义的 message 与稳定 reason；对非适配层错误一律用通用文案，
// 避免把内部错误细节（可能含上游信息）透给前端。
func writeError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	var apiErr *yunmaosvc.APIError
	if !errors.As(err, &apiErr) {
		response.ErrorWithDetails(c, http.StatusInternalServerError, "服务暂时不可用", "shop_internal_error", nil)
		return
	}
	response.ErrorWithDetails(c, statusForCode(apiErr.Code), apiErr.Message, string(apiErr.Code), nil)
}

func statusForCode(code yunmaosvc.ErrorCode) int {
	switch code {
	case yunmaosvc.CodeNotConfigured:
		return http.StatusServiceUnavailable
	case yunmaosvc.CodeProductSyncUnavailable:
		// 同步失败：保留商品展示但暂停购买（PRD-03 §2.8）。
		return http.StatusServiceUnavailable
	case yunmaosvc.CodeUpstreamUnavailable:
		return http.StatusBadGateway
	case yunmaosvc.CodeUpstreamRejected, yunmaosvc.CodeUpstreamResponseInvalid:
		return http.StatusBadGateway
	case yunmaosvc.CodeProductNotFound, yunmaosvc.CodeOrderNotFound:
		return http.StatusNotFound
	case yunmaosvc.CodeIdempotencyConflict:
		return http.StatusConflict
	case yunmaosvc.CodeCallbackSignatureUnavailable, yunmaosvc.CodeCallbackSignatureInvalid:
		return http.StatusUnauthorized
	case yunmaosvc.CodeCallbackPayloadInvalid:
		return http.StatusBadRequest
	case yunmaosvc.CodeOrderNotPaid, yunmaosvc.CodeDeliveryPending:
		return http.StatusOK
	default:
		return http.StatusInternalServerError
	}
}
