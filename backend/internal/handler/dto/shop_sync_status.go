package dto

import (
	"time"

	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"
)

// ShopSyncStatusDTO 是管理端「商品同步状态」响应体。
//
// 契约：GET /admin/shop/sync-status
//
// 字段语义：
//   - last_success_at：最近一次**成功**同步的时间（RFC3339，UTC）；null 表示从未成功同步过。
//   - last_attempt_at：最近一次同步尝试（成功或失败）的时间（RFC3339，UTC）；null 表示从未尝试。
//   - error：稳定错误码（与 yunmao.ErrorCode 同一套取值）；空串表示当前无错误。
//   - paused：是否暂停新建订单（未配置 / 从未成功同步 / 最近一次同步失败 / 当前无商品）。
//   - product_count：当前展示的商品数（同步失败时保留上一次成功的数据）。
//
// 白标红线：只含本站语义，绝不含上游品牌、域名、店铺标识、凭据或上游原始响应。
type ShopSyncStatusDTO struct {
	LastSuccessAt *string `json:"last_success_at"`
	LastAttemptAt *string `json:"last_attempt_at"`
	Error         string  `json:"error"`
	Paused        bool    `json:"paused"`
	ProductCount  int     `json:"product_count"`
}

// ToShopSyncStatusDTO 把服务层视图映射成响应体。
func ToShopSyncStatusDTO(status yunmaosvc.SyncStatus) ShopSyncStatusDTO {
	return ShopSyncStatusDTO{
		LastSuccessAt: formatSyncStatusTime(status.LastSuccessAt),
		LastAttemptAt: formatSyncStatusTime(status.LastAttemptAt),
		Error:         status.Error,
		Paused:        status.Paused,
		ProductCount:  status.ProductCount,
	}
}

// formatSyncStatusTime 统一按 RFC3339（UTC）输出；缺省时间输出 null 而不是空串，
// 便于前端区分「没有这个时间点」与「时间是空」。
func formatSyncStatusTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
