package yunmao

import (
	"time"

	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"
)

// productDTO 是商品列表项。
//
// 白标约束：字段全部是本站语义，不含上游品牌、店铺标识、上游商品页地址或域名
// （PRD-03 FE-04 / AC-F1）。新增字段前先问：这个字段会暴露供应商吗？
type productDTO struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	PriceCNY         float64  `json:"price_cny"`
	OriginalPriceCNY *float64 `json:"original_price_cny,omitempty"`
	Stock            *int     `json:"stock,omitempty"`
	Listed           bool     `json:"listed"`
	Purchasable      bool     `json:"purchasable"`
	CreditPerCNY     float64  `json:"credit_per_cny"`
	SyncedAt         string   `json:"synced_at,omitempty"`
}

// catalogDTO 是商品目录响应。
type catalogDTO struct {
	Configured    bool         `json:"configured"`
	Available     bool         `json:"available"`
	Reason        string       `json:"unavailable_reason,omitempty"`
	Products      []productDTO `json:"products"`
	SyncedAt      string       `json:"synced_at,omitempty"`
	SyncFailedAt  string       `json:"sync_failed_at,omitempty"`
	SyncErrorCode string       `json:"sync_error_code,omitempty"`
}

// createOrderRequestDTO 是下单请求。
type createOrderRequestDTO struct {
	ProductID      string `json:"product_id"`
	Quantity       int    `json:"quantity"`
	IdempotencyKey string `json:"idempotency_key"`
}

// paymentDTO 是支付指引（白名单结构，未声明的上游字段不会到达前端）。
type paymentDTO struct {
	Kind        string `json:"kind"`
	QRContent   string `json:"qr_content,omitempty"`
	RedirectURL string `json:"redirect_url,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// orderDTO 是订单视图。
//
// 红线：**不含**上游订单号、不含上游域名、不含凭据；卡密只在请求者是订单所属用户
// 且订单已交付时才有值（PRD-03 §2.7 / AC-B3）。
type orderDTO struct {
	OrderNo           string      `json:"order_no"`
	ProductID         string      `json:"product_id"`
	ProductName       string      `json:"product_name,omitempty"`
	Quantity          int         `json:"quantity"`
	AmountCNY         float64     `json:"amount_cny"`
	DisplayStatus     string      `json:"display_status"`
	DeliveryErrorCode string      `json:"delivery_error_code,omitempty"`
	CardSecret        string      `json:"card_secret,omitempty"`
	CardHint          string      `json:"card_hint,omitempty"`
	RedeemPath        string      `json:"redeem_path,omitempty"`
	Payment           *paymentDTO `json:"payment,omitempty"`
	PaidAt            string      `json:"paid_at,omitempty"`
	CreatedAt         string      `json:"created_at,omitempty"`
	UpdatedAt         string      `json:"updated_at,omitempty"`
}

func toProductDTO(product yunmaosvc.Product) productDTO {
	return productDTO{
		ID:               product.ID,
		Name:             product.Name,
		Description:      product.Description,
		PriceCNY:         product.PriceCNY,
		OriginalPriceCNY: product.OriginalPriceCNY,
		Stock:            product.Stock,
		Listed:           product.Listed,
		Purchasable:      product.Purchasable(),
		CreditPerCNY:     product.CreditPerCNY,
		SyncedAt:         formatTime(product.SyncedAt),
	}
}

func toCatalogDTO(state yunmaosvc.CatalogState) catalogDTO {
	products := make([]productDTO, 0, len(state.Products))
	for _, product := range state.Products {
		products = append(products, toProductDTO(product))
	}
	return catalogDTO{
		Configured:    state.Configured,
		Available:     state.Available,
		Reason:        string(state.UnavailableReason),
		Products:      products,
		SyncedAt:      formatTime(state.SyncedAt),
		SyncFailedAt:  formatTimePtr(state.SyncFailedAt),
		SyncErrorCode: string(state.SyncErrorCode),
	}
}

func toOrderDTO(view yunmaosvc.OrderView) orderDTO {
	dto := orderDTO{
		OrderNo:           view.OrderNo,
		ProductID:         view.ProductID,
		ProductName:       view.ProductName,
		Quantity:          view.Quantity,
		AmountCNY:         view.AmountCNY,
		DisplayStatus:     string(view.DisplayStatus),
		DeliveryErrorCode: view.DeliveryErrorCode,
		CardSecret:        view.CardSecret,
		CardHint:          view.CardHint,
		RedeemPath:        view.RedeemPath,
		PaidAt:            formatTimePtr(view.PaidAt),
		CreatedAt:         formatTime(view.CreatedAt),
		UpdatedAt:         formatTime(view.UpdatedAt),
	}
	if view.Payment.Kind != "" && view.Payment.Kind != yunmaosvc.PayNone {
		dto.Payment = &paymentDTO{
			Kind:        string(view.Payment.Kind),
			QRContent:   view.Payment.QRContent,
			RedirectURL: view.Payment.RedirectURL,
			ExpiresAt:   formatTimePtr(view.Payment.ExpiresAt),
		}
	}
	return dto
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}
