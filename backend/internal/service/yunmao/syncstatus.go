package yunmao

import (
	"context"
	"strings"
	"time"
)

// SyncStatus 是商品同步状态的管理面视图（只读）。
//
// 白标红线：本结构只含本站语义（时间戳 + 稳定错误码 + 计数），
// 绝不含上游品牌、域名、店铺标识、凭据或上游原始响应（PRD-03 FE-04）。
type SyncStatus struct {
	// LastSuccessAt 是最近一次**成功**同步的时间；nil 表示从未成功同步过。
	LastSuccessAt *time.Time
	// LastAttemptAt 是最近一次同步尝试（成功或失败）的时间；nil 表示从未尝试。
	LastAttemptAt *time.Time
	// Error 是稳定错误码（与 APIError.Code 同一套取值）；空串表示当前无错误。
	Error string
	// Paused 表示当前是否暂停新建订单：
	// 未配置 / 从未成功同步 / 最近一次同步失败 / 当前没有商品 时为 true。
	// 语义与既有下单校验保持一致（PRD-03 §2.8 / BE-08）：
	// 同步失败会**保留**最近一次成功的商品数据，但暂停新订单创建。
	Paused bool
	// ProductCount 是当前展示的商品数（同步失败时保留上一次成功的数据）。
	ProductCount int
}

// CatalogSyncStatus 汇总商品同步状态。
//
// 只读：读本地商品目录快照，**不发起任何上游调用**。
// 暂停购买的判定与 Service.CatalogState 完全一致：
//
//	暂停购买 = !(已配置 && 最近一次同步成功 && 当前有商品)
//
// 区别只在于这里额外暴露「最近一次尝试时间」与「稳定错误码」，供管理端展示。
func (s *Service) CatalogSyncStatus(ctx context.Context) SyncStatus {
	configured := s.configured()
	status := SyncStatus{Paused: true}
	if !configured {
		// 未配置优先：管理员要先配置上游，其余信息次要。
		status.Error = string(CodeNotConfigured)
	}

	if s.store == nil {
		if status.Error == "" {
			status.Error = string(CodeNotConfigured)
		}
		return status
	}

	snapshot, err := s.store.LoadCatalog(ctx)
	if err != nil {
		// 读不到快照：fail closed，但只暴露稳定码，绝不把内部错误细节带给管理端。
		if status.Error == "" {
			status.Error = string(CodeUpstreamUnavailable)
		}
		return status
	}
	if snapshot == nil {
		// 从未同步过：既没有商品可展示，也无法下单。
		if status.Error == "" {
			status.Error = string(CodeProductSyncUnavailable)
		}
		return status
	}

	status.ProductCount = len(snapshot.Products)
	if successAt, ok := successSyncedAt(snapshot); ok {
		status.LastSuccessAt = successAt
	}
	status.LastAttemptAt = latestAttemptAt(snapshot)

	switch {
	case !configured:
		// 保留 CodeNotConfigured。
	case snapshot.SyncFailedAt != nil:
		// 同步失败：商品继续展示，但暂停购买（PRD-03 §2.8 / BE-08）。
		status.Error = safeSyncErrorCode(snapshot.SyncErrorCode)
	case len(snapshot.Products) == 0:
		status.Error = string(CodeProductSyncUnavailable)
	default:
		status.Error = ""
		status.Paused = false
	}
	return status
}

// successSyncedAt 解析「最近一次成功同步时间」。
//
// 仓储层在「首次同步就失败」时会写入 synced_at = sync_failed_at（此前没有更早的成功记录），
// 这种情况下 synced_at 不能当作成功时间返回，否则管理端会把失败时间误读成成功时间。
func successSyncedAt(snapshot *CatalogSnapshot) (*time.Time, bool) {
	if snapshot.SyncedAt.IsZero() {
		return nil, false
	}
	if snapshot.SyncFailedAt != nil && snapshot.SyncedAt.Equal(*snapshot.SyncFailedAt) {
		return nil, false
	}
	syncedAt := snapshot.SyncedAt
	return &syncedAt, true
}

// latestAttemptAt 返回最近一次同步尝试（成功或失败）的时间。
func latestAttemptAt(snapshot *CatalogSnapshot) *time.Time {
	var latest time.Time
	if !snapshot.SyncedAt.IsZero() {
		latest = snapshot.SyncedAt
	}
	if snapshot.SyncFailedAt != nil && snapshot.SyncFailedAt.After(latest) {
		latest = *snapshot.SyncFailedAt
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

// safeSyncErrorCode 只放行本站稳定错误码。
//
// 库里出现未知内容（例如历史脏数据或被污染的上游原文）时一律降级为通用码，
// 绝不把原文透给管理端。
func safeSyncErrorCode(raw string) string {
	trimmed := strings.TrimSpace(raw)
	switch ErrorCode(trimmed) {
	case CodeUpstreamUnavailable,
		CodeUpstreamRejected,
		CodeUpstreamResponseInvalid,
		CodeProductSyncUnavailable,
		CodeNotConfigured:
		return trimmed
	default:
		return string(CodeProductSyncUnavailable)
	}
}
