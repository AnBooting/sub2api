//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"
	"github.com/gin-gonic/gin"
)

// syncStatusStore 是只读状态接口用的 OrderStore 替身：只有商品快照会被读取。
type syncStatusStore struct {
	catalog *yunmaosvc.CatalogSnapshot
}

func (s *syncStatusStore) Create(context.Context, yunmaosvc.CreateOrderRecord) (*yunmaosvc.OrderRecord, error) {
	return nil, errors.New("unexpected call")
}

func (s *syncStatusStore) FindByOrderNo(context.Context, string) (*yunmaosvc.OrderRecord, error) {
	return nil, yunmaosvc.ErrOrderNotFound
}

func (s *syncStatusStore) FindByIdempotencyKey(context.Context, int64, string) (*yunmaosvc.OrderRecord, error) {
	return nil, yunmaosvc.ErrOrderNotFound
}

func (s *syncStatusStore) FindByUpstreamOrderNo(context.Context, string) (*yunmaosvc.OrderRecord, error) {
	return nil, yunmaosvc.ErrOrderNotFound
}

func (s *syncStatusStore) Update(context.Context, string, yunmaosvc.OrderPatch) (*yunmaosvc.OrderRecord, error) {
	return nil, yunmaosvc.ErrOrderNotFound
}

func (s *syncStatusStore) ListByUser(context.Context, int64, int) ([]yunmaosvc.OrderRecord, error) {
	return nil, nil
}

func (s *syncStatusStore) LoadCatalog(context.Context) (*yunmaosvc.CatalogSnapshot, error) {
	return s.catalog, nil
}

func (s *syncStatusStore) SaveCatalog(context.Context, []yunmaosvc.Product, time.Time) error {
	return errors.New("unexpected call")
}

func (s *syncStatusStore) MarkCatalogSyncFailure(context.Context, yunmaosvc.ErrorCode, time.Time) error {
	return errors.New("unexpected call")
}

// syncStatusEnvelope 是管理端响应信封 + 状态字段。
type syncStatusEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		LastSuccessAt *string `json:"last_success_at"`
		LastAttemptAt *string `json:"last_attempt_at"`
		Error         string  `json:"error"`
		Paused        bool    `json:"paused"`
		ProductCount  int     `json:"product_count"`
	} `json:"data"`
}

// syncStatusConfiguredConfig 返回一份「已配置」的适配层配置。
func syncStatusConfiguredConfig() yunmaosvc.Config {
	return yunmaosvc.Config{
		Enabled: true,
		BaseURL: "https://upstream.example.com",
		Endpoints: yunmaosvc.Endpoints{
			ListProducts: "/products",
			CreateOrder:  "/orders",
			QueryOrder:   "/orders/query",
			FetchCards:   "/cards",
		},
		Credentials: yunmaosvc.Credentials{AppID: "app-id-9", AppSecret: "super-secret-9"},
	}
}

// callSyncStatus 发起一次管理端请求并返回状态码、原始响应体与解析后的信封。
//
// service 为 nil 模拟「服务未装配」。
func callSyncStatus(t *testing.T, service *yunmaosvc.Service) (int, string, syncStatusEnvelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/admin/shop/sync-status", NewShopSyncStatusHandler(service).GetSyncStatus)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/shop/sync-status", nil)
	router.ServeHTTP(recorder, request)

	var envelope syncStatusEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是合法 JSON：%v（body=%s）", err, recorder.Body.String())
	}
	return recorder.Code, recorder.Body.String(), envelope
}

// TestShopSyncStatusHandler_NotConfigured 未配置：200 + 明确状态，不泄露任何配置内容。
func TestShopSyncStatusHandler_NotConfigured(t *testing.T) {
	service := yunmaosvc.NewService(yunmaosvc.Config{}, nil, &syncStatusStore{}, nil)

	status, body, envelope := callSyncStatus(t, service)

	if status != http.StatusOK {
		t.Fatalf("未配置时必须返回 200，实际 %d（body=%s）", status, body)
	}
	if envelope.Data.Error != string(yunmaosvc.CodeNotConfigured) {
		t.Fatalf("error 必须是 %s，实际 %q", yunmaosvc.CodeNotConfigured, envelope.Data.Error)
	}
	if !envelope.Data.Paused {
		t.Fatal("未配置时必须暂停购买")
	}
	if envelope.Data.ProductCount != 0 {
		t.Fatalf("商品数应为 0，实际 %d", envelope.Data.ProductCount)
	}
}

// TestShopSyncStatusHandler_SyncFailed 已配置但同步失败：保留商品、暂停购买、给出稳定错误码。
func TestShopSyncStatusHandler_SyncFailed(t *testing.T) {
	syncedAt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	failedAt := syncedAt.Add(time.Hour)
	store := &syncStatusStore{catalog: &yunmaosvc.CatalogSnapshot{
		Products:      []yunmaosvc.Product{{ID: "sku-1", Name: "Credits"}},
		SyncedAt:      syncedAt,
		SyncFailedAt:  &failedAt,
		SyncErrorCode: string(yunmaosvc.CodeUpstreamUnavailable),
	}}
	service := yunmaosvc.NewService(syncStatusConfiguredConfig(), nil, store, nil)

	status, body, envelope := callSyncStatus(t, service)

	if status != http.StatusOK {
		t.Fatalf("同步失败时必须返回 200，实际 %d（body=%s）", status, body)
	}
	if envelope.Data.Error != string(yunmaosvc.CodeUpstreamUnavailable) {
		t.Fatalf("error 必须是 %s，实际 %q", yunmaosvc.CodeUpstreamUnavailable, envelope.Data.Error)
	}
	if !envelope.Data.Paused {
		t.Fatal("同步失败时必须暂停购买")
	}
	if envelope.Data.ProductCount != 1 {
		t.Fatalf("同步失败必须保留上一次成功的商品，实际 %d", envelope.Data.ProductCount)
	}
	if envelope.Data.LastSuccessAt == nil || *envelope.Data.LastSuccessAt != syncedAt.Format(time.RFC3339) {
		t.Fatalf("last_success_at 应为 %s，实际 %v", syncedAt.Format(time.RFC3339), envelope.Data.LastSuccessAt)
	}
	if envelope.Data.LastAttemptAt == nil || *envelope.Data.LastAttemptAt != failedAt.Format(time.RFC3339) {
		t.Fatalf("last_attempt_at 应为 %s，实际 %v", failedAt.Format(time.RFC3339), envelope.Data.LastAttemptAt)
	}
}

// TestShopSyncStatusHandler_Success 同步成功：不暂停、无错误、商品数正确。
func TestShopSyncStatusHandler_Success(t *testing.T) {
	syncedAt := time.Date(2026, 10, 2, 8, 30, 0, 0, time.UTC)
	store := &syncStatusStore{catalog: &yunmaosvc.CatalogSnapshot{
		Products: []yunmaosvc.Product{{ID: "sku-1"}, {ID: "sku-2"}},
		SyncedAt: syncedAt,
	}}
	service := yunmaosvc.NewService(syncStatusConfiguredConfig(), nil, store, nil)

	status, body, envelope := callSyncStatus(t, service)

	if status != http.StatusOK {
		t.Fatalf("同步成功时必须返回 200，实际 %d（body=%s）", status, body)
	}
	if envelope.Data.Error != "" {
		t.Fatalf("同步成功时 error 必须为空，实际 %q", envelope.Data.Error)
	}
	if envelope.Data.Paused {
		t.Fatal("同步成功且有商品时不应暂停购买")
	}
	if envelope.Data.ProductCount != 2 {
		t.Fatalf("商品数应为 2，实际 %d", envelope.Data.ProductCount)
	}
}

// TestShopSyncStatusHandler_ServiceNotWired 服务未装配时仍然返回 200 + 未配置状态（绝不 500）。
func TestShopSyncStatusHandler_ServiceNotWired(t *testing.T) {
	status, body, envelope := callSyncStatus(t, nil)

	if status != http.StatusOK {
		t.Fatalf("服务未装配时必须返回 200，实际 %d（body=%s）", status, body)
	}
	if envelope.Data.Error != string(yunmaosvc.CodeNotConfigured) {
		t.Fatalf("error 必须是 %s，实际 %q", yunmaosvc.CodeNotConfigured, envelope.Data.Error)
	}
	if !envelope.Data.Paused {
		t.Fatal("服务未装配时必须暂停购买")
	}
}

// TestShopSyncStatusHandler_NoSecretLeak 响应体不得出现凭据、上游域名或上游原始响应。
func TestShopSyncStatusHandler_NoSecretLeak(t *testing.T) {
	failedAt := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	store := &syncStatusStore{catalog: &yunmaosvc.CatalogSnapshot{
		Products:     []yunmaosvc.Product{{ID: "sku-1", Name: "Credits"}},
		SyncedAt:     failedAt,
		SyncFailedAt: &failedAt,
		// 模拟库里被污染的「上游原文」。
		SyncErrorCode: "upstream raw body: secret=super-secret-9",
	}}
	service := yunmaosvc.NewService(syncStatusConfiguredConfig(), nil, store, nil)

	status, body, envelope := callSyncStatus(t, service)

	if status != http.StatusOK {
		t.Fatalf("必须返回 200，实际 %d（body=%s）", status, body)
	}
	forbidden := []string{
		"super-secret-9",
		"app-id-9",
		"upstream.example.com",
		"upstream raw body",
		"secret=",
	}
	for _, needle := range forbidden {
		if strings.Contains(body, needle) {
			t.Fatalf("响应体泄露了 %q：%s", needle, body)
		}
	}
	// 未知错误码必须降级为稳定码。
	if envelope.Data.Error != string(yunmaosvc.CodeProductSyncUnavailable) {
		t.Fatalf("error 必须是 %s，实际 %q", yunmaosvc.CodeProductSyncUnavailable, envelope.Data.Error)
	}
}
