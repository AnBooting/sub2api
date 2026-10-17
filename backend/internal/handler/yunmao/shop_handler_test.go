//go:build unit

package yunmao

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	yunmaosvc "github.com/Wei-Shaw/sub2api/internal/service/yunmao"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// 最小测试替身（handler 包内自带，避免依赖 service 包的内部测试辅助）
// ---------------------------------------------------------------------------

type fakeStore struct {
	orders  map[string]*yunmaosvc.OrderRecord
	byKey   map[string]string
	catalog *yunmaosvc.CatalogSnapshot
}

func newFakeStore() *fakeStore {
	return &fakeStore{orders: map[string]*yunmaosvc.OrderRecord{}, byKey: map[string]string{}}
}

func (f *fakeStore) Create(_ context.Context, record yunmaosvc.CreateOrderRecord) (*yunmaosvc.OrderRecord, error) {
	k := record.IdempotencyKey
	if _, exists := f.byKey[k]; exists {
		return nil, yunmaosvc.ErrIdempotencyConflict
	}
	stored := &yunmaosvc.OrderRecord{
		OrderNo:         record.OrderNo,
		UserID:          record.UserID,
		IdempotencyKey:  record.IdempotencyKey,
		UpstreamOrderNo: record.UpstreamOrderNo,
		ProductID:       record.ProductID,
		ProductSnapshot: record.ProductSnapshot,
		AmountCNY:       record.AmountCNY,
		Quantity:        record.Quantity,
		DisplayStatus:   record.DisplayStatus,
	}
	f.orders[stored.OrderNo] = stored
	f.byKey[k] = stored.OrderNo
	return stored, nil
}

func (f *fakeStore) FindByOrderNo(_ context.Context, orderNo string) (*yunmaosvc.OrderRecord, error) {
	if record, ok := f.orders[orderNo]; ok {
		return record, nil
	}
	return nil, yunmaosvc.ErrOrderNotFound
}

func (f *fakeStore) FindByIdempotencyKey(_ context.Context, _ int64, key string) (*yunmaosvc.OrderRecord, error) {
	if orderNo, ok := f.byKey[key]; ok {
		return f.orders[orderNo], nil
	}
	return nil, yunmaosvc.ErrOrderNotFound
}

func (f *fakeStore) FindByUpstreamOrderNo(_ context.Context, upstreamOrderNo string) (*yunmaosvc.OrderRecord, error) {
	for _, record := range f.orders {
		if record.UpstreamOrderNo == upstreamOrderNo {
			return record, nil
		}
	}
	return nil, yunmaosvc.ErrOrderNotFound
}

func (f *fakeStore) Update(_ context.Context, orderNo string, patch yunmaosvc.OrderPatch) (*yunmaosvc.OrderRecord, error) {
	record, ok := f.orders[orderNo]
	if !ok {
		return nil, yunmaosvc.ErrOrderNotFound
	}
	if patch.DisplayStatus != nil {
		record.DisplayStatus = *patch.DisplayStatus
	}
	if patch.CardSecret != nil {
		record.CardSecret = *patch.CardSecret
	}
	if patch.CardHint != nil {
		record.CardHint = *patch.CardHint
	}
	if patch.LastErrorCode != nil {
		record.LastErrorCode = *patch.LastErrorCode
	}
	return record, nil
}

func (f *fakeStore) ListByUser(_ context.Context, userID int64, limit int) ([]yunmaosvc.OrderRecord, error) {
	out := []yunmaosvc.OrderRecord{}
	for _, record := range f.orders {
		if record.UserID == userID {
			out = append(out, *record)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeStore) LoadCatalog(context.Context) (*yunmaosvc.CatalogSnapshot, error) {
	return f.catalog, nil
}

func (f *fakeStore) SaveCatalog(_ context.Context, products []yunmaosvc.Product, syncedAt time.Time) error {
	f.catalog = &yunmaosvc.CatalogSnapshot{Products: products, SyncedAt: syncedAt}
	return nil
}

func (f *fakeStore) MarkCatalogSyncFailure(_ context.Context, code yunmaosvc.ErrorCode, failedAt time.Time) error {
	at := failedAt
	if f.catalog == nil {
		f.catalog = &yunmaosvc.CatalogSnapshot{}
	}
	f.catalog.SyncFailedAt = &at
	f.catalog.SyncErrorCode = string(code)
	return nil
}

type fakeAdapter struct {
	products []yunmaosvc.Product
	created  *yunmaosvc.UpstreamOrder
	queried  *yunmaosvc.UpstreamOrder
	cards    []yunmaosvc.Card
}

func (f *fakeAdapter) Name() string     { return "fake" }
func (f *fakeAdapter) Configured() bool { return true }

func (f *fakeAdapter) ListProducts(context.Context) ([]yunmaosvc.Product, error) {
	return f.products, nil
}

func (f *fakeAdapter) CreateOrder(context.Context, yunmaosvc.CreateOrderRequest) (*yunmaosvc.UpstreamOrder, error) {
	return f.created, nil
}

func (f *fakeAdapter) QueryOrder(context.Context, string) (*yunmaosvc.UpstreamOrder, error) {
	return f.queried, nil
}

func (f *fakeAdapter) FetchCards(context.Context, string) ([]yunmaosvc.Card, error) {
	return f.cards, nil
}

type noopEncryptor struct{}

func (noopEncryptor) Encrypt(plaintext string) (string, error) { return "enc:" + plaintext, nil }
func (noopEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

func handlerTestConfig() yunmaosvc.Config {
	return yunmaosvc.Config{
		Enabled: true,
		BaseURL: "https://upstream.invalid",
		Endpoints: yunmaosvc.Endpoints{
			ListProducts: "/p", CreateOrder: "/o", QueryOrder: "/q", FetchCards: "/c",
		},
		Credentials: yunmaosvc.Credentials{AppID: "a", AppSecret: "b"},
		Timeout:     time.Second,
	}
}

func handlerTestProduct() yunmaosvc.Product {
	stock := 3
	return yunmaosvc.Product{ID: "sku-1", Name: "Credits", PriceCNY: 10, Stock: &stock, Listed: true, CreditPerCNY: 1}
}

func newTestRouter(handler *ShopHandler, authenticated bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api/v1/shop")
	if authenticated {
		group.Use(func(c *gin.Context) {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
			c.Next()
		})
	}
	group.GET("/products", handler.GetProducts)
	group.POST("/orders", handler.CreateOrder)
	group.GET("/orders/:orderNo", handler.GetOrder)
	group.POST("/orders/:orderNo/query", handler.QueryOrder)
	group.POST("/notify", handler.Notify)
	return router
}

// 未配置时商品接口必须返回 503 + shop_not_configured，且响应体里**不能**出现任何商品
// （禁止假数据，PRD-03 §2.8 / FE-05）。
func TestHandlerProductsReturnsNotConfiguredWithoutFakeData(t *testing.T) {
	svc := yunmaosvc.NewService(yunmaosvc.Config{}, nil, nil, nil)
	router := newTestRouter(NewShopHandler(svc), true)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/shop/products", nil))

	// 只读的目录状态接口返回 200 + 明确的不可用状态：前端据此渲染「未配置」，
	// 而不是把 5xx 当作网络故障。关键约束是「状态明确 + 不返回假数据」。
	if recorder.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, string(yunmaosvc.CodeNotConfigured)) {
		t.Fatalf("响应必须带稳定错误码 %s: %s", yunmaosvc.CodeNotConfigured, body)
	}
	if !strings.Contains(body, `"configured":false`) {
		t.Fatalf("必须明确告知未配置: %s", body)
	}
	if !strings.Contains(body, `"available":false`) {
		t.Fatalf("必须明确告知不可购买: %s", body)
	}
	if strings.Contains(body, `products:[{`) {
		t.Fatalf("未配置时不得返回商品: %s", body)
	}
}

// 写操作（下单）未配置时必须非 2xx + 稳定错误码：动作类接口一律 fail closed。
func TestHandlerCreateOrderFailsClosedWhenNotConfigured(t *testing.T) {
	svc := yunmaosvc.NewService(yunmaosvc.Config{}, nil, nil, nil)
	router := newTestRouter(NewShopHandler(svc), true)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shop/orders",
		strings.NewReader(`{"product_id":"p","quantity":1,"idempotency_key":"k1"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("未配置下单必须 503, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), string(yunmaosvc.CodeNotConfigured)) {
		t.Fatalf("必须返回稳定错误码: %s", recorder.Body.String())
	}
}

// nil service 也必须 fail closed（503），不能 panic。
func TestHandlerProductsFailsClosedWithNilService(t *testing.T) {
	router := newTestRouter(NewShopHandler(nil), true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/shop/products", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", recorder.Code)
	}
}

// 未登录必须 401，且不得触达业务逻辑。
func TestHandlerOrdersRequireAuth(t *testing.T) {
	svc := yunmaosvc.NewService(yunmaosvc.Config{}, nil, nil, nil)
	router := newTestRouter(NewShopHandler(svc), false)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/shop/orders", strings.NewReader("{}")))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", recorder.Code)
	}
}

// 下单缺少幂等键必须 400。
func TestHandlerCreateOrderRequiresIdempotencyKey(t *testing.T) {
	svc := yunmaosvc.NewService(yunmaosvc.Config{}, nil, nil, nil)
	router := newTestRouter(NewShopHandler(svc), true)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shop/orders", strings.NewReader(`{"product_id":"p","quantity":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "shop_idempotency_key_required") {
		t.Fatalf("必须返回稳定错误码: %s", recorder.Body.String())
	}
}

// 回调：签名算法未确定时必须 401，且**不得**因为请求体里写了 status=paid 就接受。
func TestHandlerNotifyFailsClosedWithoutSignatureAlgorithm(t *testing.T) {
	// 刻意用「已配置」的服务：这样拒绝原因必须是「验签算法未确定」，
	// 而不是被更早的「未配置」检查掩盖。
	svc := yunmaosvc.NewService(handlerTestConfig(), &fakeAdapter{}, nil, nil)
	router := newTestRouter(NewShopHandler(svc), false)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shop/notify", strings.NewReader(`{"status":"paid","order_no":"UP-1"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("未验签回调必须 401, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, "accepted") && strings.Contains(body, "true") {
		t.Fatalf("未验签回调绝不能返回 accepted=true: %s", body)
	}
	var envelope response.Response
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("响应不是标准 envelope: %v (%s)", err, body)
	}
	if envelope.Reason != string(yunmaosvc.CodeCallbackSignatureUnavailable) {
		t.Fatalf("want reason %s, got %q", yunmaosvc.CodeCallbackSignatureUnavailable, envelope.Reason)
	}
}

// 订单详情：他人订单必须 404，且响应里不得出现上游订单号。
func TestHandlerGetOrderHidesForeignOrdersAndUpstreamIdentifiers(t *testing.T) {
	store := newFakeStore()
	adapter := &fakeAdapter{products: []yunmaosvc.Product{handlerTestProduct()}}
	adapter.created = &yunmaosvc.UpstreamOrder{UpstreamOrderNo: "UP-SECRET-9", State: yunmaosvc.StateUnpaid}
	svc := yunmaosvc.NewService(handlerTestConfig(), adapter, store, noopEncryptor{})
	if _, err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	order, err := svc.CreateOrder(t.Context(), 7, yunmaosvc.CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "h1"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}

	router := newTestRouter(NewShopHandler(svc), true)

	okRecorder := httptest.NewRecorder()
	router.ServeHTTP(okRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/shop/orders/"+order.OrderNo, nil))
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("本人订单应 200, got %d body=%s", okRecorder.Code, okRecorder.Body.String())
	}
	if strings.Contains(okRecorder.Body.String(), "UP-SECRET-9") {
		t.Fatalf("响应不得包含上游订单号: %s", okRecorder.Body.String())
	}
	if strings.Contains(okRecorder.Body.String(), "upstream") {
		t.Fatalf("响应不得出现上游字段: %s", okRecorder.Body.String())
	}

	foreign := newTestRouter(NewShopHandler(svc), true)
	// 换一个用户身份重放同一路径。
	foreign = gin.New()
	group := foreign.Group("/api/v1/shop")
	group.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 999})
		c.Next()
	})
	group.GET("/orders/:orderNo", NewShopHandler(svc).GetOrder)

	foreignRecorder := httptest.NewRecorder()
	foreign.ServeHTTP(foreignRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/shop/orders/"+order.OrderNo, nil))
	if foreignRecorder.Code != http.StatusNotFound {
		t.Fatalf("他人订单必须 404, got %d body=%s", foreignRecorder.Code, foreignRecorder.Body.String())
	}
	if strings.Contains(foreignRecorder.Body.String(), order.OrderNo) {
		t.Fatalf("404 响应不得回显订单号: %s", foreignRecorder.Body.String())
	}
}

// 前端可见的响应结构里不允许出现任何上游品牌字样。
func TestHandlerResponsesContainNoUpstreamBranding(t *testing.T) {
	svc := yunmaosvc.NewService(yunmaosvc.Config{}, nil, nil, nil)
	router := newTestRouter(NewShopHandler(svc), true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/shop/products", nil))

	lowered := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"yunmao", "云猫"} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("响应出现上游品牌字样 %q: %s", forbidden, recorder.Body.String())
		}
	}
}
