//go:build unit

package yunmao

import (
	"context"
	"errors"
	"testing"
	"time"
)

// syncStatusTestConfig 返回一份「已配置」的适配层配置（只用于状态判定，不发起任何请求）。
func syncStatusTestConfig() Config {
	return Config{
		Enabled: true,
		BaseURL: "https://upstream.example.com",
		Endpoints: Endpoints{
			ListProducts: "/products",
			CreateOrder:  "/orders",
			QueryOrder:   "/orders/query",
			FetchCards:   "/cards",
		},
		Credentials: Credentials{AppID: "app-id", AppSecret: "app-secret"},
	}
}

// erroringCatalogStore 模拟「读不到商品快照」（例如数据库不可用）。
type erroringCatalogStore struct {
	memoryStore
	err error
}

func (s *erroringCatalogStore) LoadCatalog(context.Context) (*CatalogSnapshot, error) {
	return nil, s.err
}

// TestCatalogSyncStatus_NotConfigured 未配置时必须给出明确状态，且不得 500。
func TestCatalogSyncStatus_NotConfigured(t *testing.T) {
	store := newMemoryStore()
	store.catalog = &CatalogSnapshot{
		Products: []Product{{ID: "sku-1", Name: "Credits"}},
		SyncedAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
	}
	svc := NewService(Config{}, nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.Error != string(CodeNotConfigured) {
		t.Fatalf("未配置时 error 必须是 %s，实际 %q", CodeNotConfigured, status.Error)
	}
	if !status.Paused {
		t.Fatal("未配置时必须暂停购买")
	}
	// 未配置时仍保留快照商品数：与用户侧目录接口（CatalogState）保持一致。
	if status.ProductCount != 1 {
		t.Fatalf("商品数应为 1，实际 %d", status.ProductCount)
	}
}

// TestCatalogSyncStatus_SyncFailed 已配置但同步失败：保留商品、暂停购买、暴露稳定错误码。
func TestCatalogSyncStatus_SyncFailed(t *testing.T) {
	syncedAt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	failedAt := syncedAt.Add(90 * time.Minute)
	store := newMemoryStore()
	store.catalog = &CatalogSnapshot{
		Products:      []Product{{ID: "sku-1", Name: "Credits"}},
		SyncedAt:      syncedAt,
		SyncFailedAt:  &failedAt,
		SyncErrorCode: string(CodeUpstreamUnavailable),
	}
	svc := NewService(syncStatusTestConfig(), nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.Error != string(CodeUpstreamUnavailable) {
		t.Fatalf("同步失败时 error 必须是 %s，实际 %q", CodeUpstreamUnavailable, status.Error)
	}
	if !status.Paused {
		t.Fatal("同步失败时必须暂停购买（PRD-03 BE-08）")
	}
	if status.ProductCount != 1 {
		t.Fatalf("同步失败必须保留上一次成功的商品，实际 %d", status.ProductCount)
	}
	if status.LastSuccessAt == nil || !status.LastSuccessAt.Equal(syncedAt) {
		t.Fatalf("最近一次成功同步时间应为 %s，实际 %v", syncedAt, status.LastSuccessAt)
	}
	if status.LastAttemptAt == nil || !status.LastAttemptAt.Equal(failedAt) {
		t.Fatalf("最近一次尝试时间应为 %s，实际 %v", failedAt, status.LastAttemptAt)
	}
}

// TestCatalogSyncStatus_Success 同步成功：不暂停、无错误。
func TestCatalogSyncStatus_Success(t *testing.T) {
	syncedAt := time.Date(2026, 10, 2, 8, 30, 0, 0, time.UTC)
	store := newMemoryStore()
	store.catalog = &CatalogSnapshot{
		Products: []Product{{ID: "sku-1"}, {ID: "sku-2"}},
		SyncedAt: syncedAt,
	}
	svc := NewService(syncStatusTestConfig(), nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.Error != "" {
		t.Fatalf("同步成功时 error 必须为空，实际 %q", status.Error)
	}
	if status.Paused {
		t.Fatal("同步成功且有商品时不应暂停购买")
	}
	if status.ProductCount != 2 {
		t.Fatalf("商品数应为 2，实际 %d", status.ProductCount)
	}
	if status.LastSuccessAt == nil || !status.LastSuccessAt.Equal(syncedAt) {
		t.Fatalf("最近一次成功同步时间应为 %s，实际 %v", syncedAt, status.LastSuccessAt)
	}
	if status.LastAttemptAt == nil || !status.LastAttemptAt.Equal(syncedAt) {
		t.Fatalf("最近一次尝试时间应为 %s，实际 %v", syncedAt, status.LastAttemptAt)
	}
}

// TestCatalogSyncStatus_NeverSynced 从未同步过：没有商品、无法下单。
func TestCatalogSyncStatus_NeverSynced(t *testing.T) {
	store := newMemoryStore()
	svc := NewService(syncStatusTestConfig(), nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.Error != string(CodeProductSyncUnavailable) {
		t.Fatalf("从未同步时 error 必须是 %s，实际 %q", CodeProductSyncUnavailable, status.Error)
	}
	if !status.Paused {
		t.Fatal("从未同步时必须暂停购买")
	}
	if status.LastSuccessAt != nil || status.LastAttemptAt != nil {
		t.Fatalf("从未同步时不应有时间戳：%v / %v", status.LastSuccessAt, status.LastAttemptAt)
	}
	if status.ProductCount != 0 {
		t.Fatalf("从未同步时商品数应为 0，实际 %d", status.ProductCount)
	}
}

// TestCatalogSyncStatus_FirstAttemptFailed 首次同步就失败时，
// 仓储层写入的 synced_at 与 sync_failed_at 相同，不能把失败时间当成「成功同步时间」。
func TestCatalogSyncStatus_FirstAttemptFailed(t *testing.T) {
	failedAt := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	if err := store.MarkCatalogSyncFailure(context.Background(), CodeUpstreamUnavailable, failedAt); err != nil {
		t.Fatalf("写入失败标记出错：%v", err)
	}
	svc := NewService(syncStatusTestConfig(), nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.LastSuccessAt != nil {
		t.Fatalf("从未成功同步时 last_success_at 必须为 nil，实际 %v", status.LastSuccessAt)
	}
	if status.LastAttemptAt == nil || !status.LastAttemptAt.Equal(failedAt) {
		t.Fatalf("最近一次尝试时间应为 %s，实际 %v", failedAt, status.LastAttemptAt)
	}
	if status.Error != string(CodeUpstreamUnavailable) {
		t.Fatalf("error 必须是 %s，实际 %q", CodeUpstreamUnavailable, status.Error)
	}
	if !status.Paused {
		t.Fatal("同步失败时必须暂停购买")
	}
}

// TestCatalogSyncStatus_UnknownErrorCodeDowngraded 库里出现未知内容时降级为稳定码，绝不透出原文。
func TestCatalogSyncStatus_UnknownErrorCodeDowngraded(t *testing.T) {
	failedAt := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	store := newMemoryStore()
	store.catalog = &CatalogSnapshot{
		SyncedAt:      failedAt,
		SyncFailedAt:  &failedAt,
		SyncErrorCode: "<html>upstream raw body</html>",
	}
	svc := NewService(syncStatusTestConfig(), nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.Error != string(CodeProductSyncUnavailable) {
		t.Fatalf("未知错误码必须降级为 %s，实际 %q", CodeProductSyncUnavailable, status.Error)
	}
}

// TestCatalogSyncStatus_StoreUnavailable 读不到快照时 fail closed，只暴露稳定码。
func TestCatalogSyncStatus_StoreUnavailable(t *testing.T) {
	store := &erroringCatalogStore{err: errors.New("db down")}
	svc := NewService(syncStatusTestConfig(), nil, store, nil)

	status := svc.CatalogSyncStatus(context.Background())

	if status.Error != string(CodeUpstreamUnavailable) {
		t.Fatalf("读不到快照时 error 必须是 %s，实际 %q", CodeUpstreamUnavailable, status.Error)
	}
	if !status.Paused {
		t.Fatal("读不到快照时必须暂停购买")
	}
}
