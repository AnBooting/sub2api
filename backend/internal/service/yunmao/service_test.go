//go:build unit

package yunmao

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 测试替身
// ---------------------------------------------------------------------------

// memoryStore 是 OrderStore 的内存实现，用于验证业务规则（幂等、状态映射、归属校验）。
type memoryStore struct {
	orders  map[string]*OrderRecord
	byKey   map[string]string
	catalog *CatalogSnapshot
	failCat bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		orders: map[string]*OrderRecord{},
		byKey:  map[string]string{},
	}
}

func idemKey(userID int64, idem string) string {
	return strconv.FormatInt(userID, 10) + ":" + idem
}

func (m *memoryStore) Create(_ context.Context, record CreateOrderRecord) (*OrderRecord, error) {
	k := idemKey(record.UserID, record.IdempotencyKey)
	if _, exists := m.byKey[k]; exists {
		return nil, ErrIdempotencyConflict
	}
	stored := &OrderRecord{
		ID:              int64(len(m.orders) + 1),
		OrderNo:         record.OrderNo,
		UserID:          record.UserID,
		IdempotencyKey:  record.IdempotencyKey,
		UpstreamOrderNo: record.UpstreamOrderNo,
		ProductID:       record.ProductID,
		ProductSnapshot: record.ProductSnapshot,
		AmountCNY:       record.AmountCNY,
		Quantity:        record.Quantity,
		DisplayStatus:   record.DisplayStatus,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	m.orders[stored.OrderNo] = stored
	m.byKey[k] = stored.OrderNo
	return stored, nil
}

func (m *memoryStore) FindByOrderNo(_ context.Context, orderNo string) (*OrderRecord, error) {
	if record, ok := m.orders[orderNo]; ok {
		return record, nil
	}
	return nil, ErrOrderNotFound
}

func (m *memoryStore) FindByIdempotencyKey(_ context.Context, userID int64, idem string) (*OrderRecord, error) {
	if orderNo, ok := m.byKey[idemKey(userID, idem)]; ok {
		return m.orders[orderNo], nil
	}
	return nil, ErrOrderNotFound
}

func (m *memoryStore) FindByUpstreamOrderNo(_ context.Context, upstreamOrderNo string) (*OrderRecord, error) {
	for _, record := range m.orders {
		if record.UpstreamOrderNo == upstreamOrderNo {
			return record, nil
		}
	}
	return nil, ErrOrderNotFound
}

func (m *memoryStore) Update(_ context.Context, orderNo string, patch OrderPatch) (*OrderRecord, error) {
	record, ok := m.orders[orderNo]
	if !ok {
		return nil, ErrOrderNotFound
	}
	if patch.UpstreamOrderNo != nil {
		record.UpstreamOrderNo = *patch.UpstreamOrderNo
	}
	if patch.DisplayStatus != nil {
		record.DisplayStatus = *patch.DisplayStatus
	}
	if patch.UpstreamStatus != nil {
		record.UpstreamStatus = *patch.UpstreamStatus
	}
	if patch.CardSecret != nil {
		record.CardSecret = *patch.CardSecret
	}
	if patch.CardHint != nil {
		record.CardHint = *patch.CardHint
	}
	if patch.DeliveryAttempts != nil {
		record.DeliveryAttempts = *patch.DeliveryAttempts
	}
	if patch.LastErrorCode != nil {
		record.LastErrorCode = *patch.LastErrorCode
	}
	if patch.LastQueriedAt != nil {
		record.LastQueriedAt = patch.LastQueriedAt
	}
	if patch.PaidAt != nil {
		record.PaidAt = patch.PaidAt
	}
	if patch.DeliveredAt != nil {
		record.DeliveredAt = patch.DeliveredAt
	}
	record.UpdatedAt = time.Now().UTC()
	return record, nil
}

func (m *memoryStore) ListByUser(_ context.Context, userID int64, limit int) ([]OrderRecord, error) {
	out := []OrderRecord{}
	for _, record := range m.orders {
		if record.UserID == userID {
			out = append(out, *record)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryStore) LoadCatalog(context.Context) (*CatalogSnapshot, error) {
	return m.catalog, nil
}

func (m *memoryStore) SaveCatalog(_ context.Context, products []Product, syncedAt time.Time) error {
	m.catalog = &CatalogSnapshot{Products: products, SyncedAt: syncedAt}
	return nil
}

func (m *memoryStore) MarkCatalogSyncFailure(_ context.Context, code ErrorCode, failedAt time.Time) error {
	m.failCat = true
	if m.catalog == nil {
		m.catalog = &CatalogSnapshot{SyncedAt: failedAt}
	}
	at := failedAt
	m.catalog.SyncFailedAt = &at
	m.catalog.SyncErrorCode = string(code)
	return nil
}

// stubAdapter 是 Adapter 的测试替身。
type stubAdapter struct {
	configured bool
	products   []Product
	created    *UpstreamOrder
	queried    *UpstreamOrder
	cards      []Card
	listErr    error
	createErr  error
	queryErr   error
	createCall int32
}

func (s *stubAdapter) Name() string     { return "stub" }
func (s *stubAdapter) Configured() bool { return s.configured }

func (s *stubAdapter) ListProducts(context.Context) ([]Product, error) {
	if !s.configured {
		return nil, notConfiguredError("stub 未配置")
	}
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.products, nil
}

func (s *stubAdapter) CreateOrder(context.Context, CreateOrderRequest) (*UpstreamOrder, error) {
	atomic.AddInt32(&s.createCall, 1)
	if s.createErr != nil {
		return nil, s.createErr
	}
	return s.created, nil
}

func (s *stubAdapter) QueryOrder(context.Context, string) (*UpstreamOrder, error) {
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return s.queried, nil
}

func (s *stubAdapter) FetchCards(context.Context, string) ([]Card, error) {
	return s.cards, nil
}

// aesEncryptor 是与 repository.NewAESEncryptor 同构的 AES-256-GCM 测试实现。
//
// 刻意使用真实算法而不是「可逆拼接」替身：只有这样，"卡密不以明文落库" 才是被验证的
// 事实，而不是被替身实现掩盖的假设。
type aesEncryptor struct{ key []byte }

func newAESEncryptor() *aesEncryptor {
	return &aesEncryptor{key: []byte("0123456789abcdef0123456789abcdef")}
}

func (e *aesEncryptor) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

func (e *aesEncryptor) Decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func configuredConfig(baseURL string) Config {
	return Config{
		Enabled: true,
		BaseURL: baseURL,
		Endpoints: Endpoints{
			ListProducts: "/products",
			CreateOrder:  "/orders",
			QueryOrder:   "/orders/query",
			FetchCards:   "/orders/cards",
		},
		Credentials: Credentials{AppID: "app-id", AppSecret: "super-secret-value"},
		Timeout:     2 * time.Second,
	}
}

func sampleProduct() Product {
	stock := 5
	return Product{
		ID:           "sku-1",
		Name:         "Credits 100",
		PriceCNY:     10,
		Stock:        &stock,
		Listed:       true,
		CreditPerCNY: 1,
	}
}

// ---------------------------------------------------------------------------
// fail closed：未配置
// ---------------------------------------------------------------------------

func TestServiceFailsClosedWhenNotConfigured(t *testing.T) {
	svc := NewService(Config{}, nil, nil, nil)

	if _, err := svc.SyncCatalog(context.Background()); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("SyncCatalog: want %s, got %v", CodeNotConfigured, err)
	}
	if _, err := svc.CreateOrder(context.Background(), 1, CreateOrderRequest{ProductID: "p", Quantity: 1, IdempotencyKey: "k"}); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("CreateOrder: want %s, got %v", CodeNotConfigured, err)
	}
	if _, err := svc.QueryOrder(context.Background(), 1, "RC1"); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("QueryOrder: want %s, got %v", CodeNotConfigured, err)
	}
	if _, err := svc.GetOrder(context.Background(), 1, "RC1"); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("GetOrder: want %s, got %v", CodeNotConfigured, err)
	}

	state := svc.CatalogState(context.Background())
	if state.Configured || state.Available {
		t.Fatalf("未配置时目录状态必须不可用: %+v", state)
	}
	if state.UnavailableReason != CodeNotConfigured {
		t.Fatalf("未配置时原因必须为 %s, got %s", CodeNotConfigured, state.UnavailableReason)
	}
	if len(state.Products) != 0 {
		t.Fatalf("未配置时不得返回任何商品（禁止假数据）: %+v", state.Products)
	}
}

func TestNotConfiguredErrorIsSentinel(t *testing.T) {
	svc := NewService(Config{}, nil, nil, nil)
	_, err := svc.SyncCatalog(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置错误必须可被 errors.Is(err, ErrNotConfigured) 命中: %v", err)
	}
}

func TestHTTPAdapterUnconfiguredSendsNoRequest(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewHTTPAdapter(Config{Enabled: true, BaseURL: server.URL, Endpoints: Endpoints{
		ListProducts: "/products", CreateOrder: "/orders", QueryOrder: "/q", FetchCards: "/c",
	}})
	if adapter.Configured() {
		t.Fatal("缺少凭据时 Configured() 必须为 false")
	}
	if _, err := adapter.ListProducts(context.Background()); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("want %s, got %v", CodeNotConfigured, err)
	}
	if _, err := adapter.CreateOrder(context.Background(), CreateOrderRequest{ProductID: "p", Quantity: 1, IdempotencyKey: "k"}); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("CreateOrder: want %s, got %v", CodeNotConfigured, err)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("未配置时不得发起任何上游请求, got %d", got)
	}
}

func TestHTTPAdapterRequiresEveryEndpointSlot(t *testing.T) {
	base := configuredConfig("https://upstream.invalid")
	cases := map[string]func(*Config){
		"missing list":     func(c *Config) { c.Endpoints.ListProducts = "" },
		"missing create":   func(c *Config) { c.Endpoints.CreateOrder = "" },
		"missing query":    func(c *Config) { c.Endpoints.QueryOrder = "" },
		"missing cards":    func(c *Config) { c.Endpoints.FetchCards = "" },
		"missing base url": func(c *Config) { c.BaseURL = "" },
		"missing app id":   func(c *Config) { c.Credentials.AppID = "" },
		"missing secret":   func(c *Config) { c.Credentials.AppSecret = "" },
		"disabled":         func(c *Config) { c.Enabled = false },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if err := ValidateConfig(cfg); !IsCode(err, CodeNotConfigured) {
				t.Fatalf("want %s, got %v", CodeNotConfigured, err)
			}
			if NewHTTPAdapter(cfg).Configured() {
				t.Fatal("槽位缺失时 Configured() 必须为 false")
			}
		})
	}
}

func TestHTTPAdapterRejectsAbsoluteEndpointPath(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	cfg.Endpoints.ListProducts = "https://evil.example.com/products"
	if _, err := cfg.resolveEndpoint(cfg.Endpoints.ListProducts); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("绝对地址必须被拒绝, got %v", err)
	}
}

func TestHTTPAdapterConfiguredSendsRequestAndFailsClosedOnPendingDecoder(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"whatever":true}`))
	}))
	defer server.Close()

	adapter := NewHTTPAdapter(configuredConfig(server.URL))
	if !adapter.Configured() {
		t.Fatal("槽位齐备时 Configured() 必须为 true")
	}
	// 默认解码器是 PendingResponseDecoder：必须 fail closed，不猜字段。
	if _, err := adapter.ListProducts(context.Background()); !IsCode(err, CodeUpstreamResponseInvalid) {
		t.Fatalf("默认解码器必须 fail closed, got %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("已配置时应发起一次请求, got %d", got)
	}
}

func TestHTTPAdapterNeverLeaksCredentialsInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("secret=super-secret-value"))
	}))
	defer server.Close()

	adapter := NewHTTPAdapter(configuredConfig(server.URL))
	_, err := adapter.ListProducts(context.Background())
	if err == nil {
		t.Fatal("401 必须返回错误")
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("错误信息泄漏凭据: %v", err)
	}
	if strings.Contains(err.Error(), server.URL) {
		t.Fatalf("错误信息泄漏上游地址: %v", err)
	}
}

func TestConfigRedactedHidesSecretValues(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	summary := cfg.Redacted()
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "super-secret-value") || strings.Contains(string(encoded), "upstream.invalid") {
		t.Fatalf("Redacted() 不得包含凭据或地址: %s", encoded)
	}
	if summary["app_secret_set"] != true {
		t.Fatalf("Redacted() 必须保留「有没有配」的信息: %+v", summary)
	}

	redacted := cfg.RedactText("failed with super-secret-value at https://upstream.invalid")
	if strings.Contains(redacted, "super-secret-value") {
		t.Fatalf("RedactText 未抹除凭据: %s", redacted)
	}
}

// ---------------------------------------------------------------------------
// 回调验签：高危红线 —— 绝不默认放行
// ---------------------------------------------------------------------------

func TestEvaluateCallbackRejectsWhenSignatureAlgorithmUnknown(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	decision := EvaluateCallback(cfg, PendingCallbackVerifier{}, context.Background(), http.Header{}, []byte(`{"status":"paid"}`))
	if decision.Accepted {
		t.Fatal("签名算法未确定时绝不能接受回调")
	}
	if decision.Code != CodeCallbackSignatureUnavailable {
		t.Fatalf("want %s, got %s", CodeCallbackSignatureUnavailable, decision.Code)
	}
}

func TestEvaluateCallbackRejectsWhenVerifierMissing(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	decision := EvaluateCallback(cfg, nil, context.Background(), http.Header{}, []byte("{}"))
	if decision.Accepted || decision.Code != CodeCallbackSignatureUnavailable {
		t.Fatalf("缺少验签器必须拒绝: %+v", decision)
	}
}

func TestEvaluateCallbackRejectsWhenNotConfigured(t *testing.T) {
	decision := EvaluateCallback(Config{}, PendingCallbackVerifier{}, context.Background(), http.Header{}, []byte("{}"))
	if decision.Accepted || decision.Code != CodeNotConfigured {
		t.Fatalf("未配置必须拒绝: %+v", decision)
	}
}

type fakeVerifier struct {
	algorithm string
	verifyErr error
	event     *CallbackEvent
	decodeErr error
	verified  bool
}

func (f *fakeVerifier) SignatureAlgorithm() string { return f.algorithm }
func (f *fakeVerifier) Verify(context.Context, http.Header, []byte) error {
	if f.verifyErr != nil {
		return f.verifyErr
	}
	f.verified = true
	return nil
}
func (f *fakeVerifier) Decode(context.Context, []byte) (*CallbackEvent, error) {
	return f.event, f.decodeErr
}

func TestEvaluateCallbackRejectsInvalidSignature(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	verifier := &fakeVerifier{algorithm: "hmac-sha256", verifyErr: NewError(CodeCallbackSignatureInvalid, "bad", nil)}
	decision := EvaluateCallback(cfg, verifier, context.Background(), http.Header{}, []byte("{}"))
	if decision.Accepted || decision.Code != CodeCallbackSignatureInvalid {
		t.Fatalf("验签失败必须拒绝: %+v", decision)
	}
	if verifier.verified {
		t.Fatal("验签失败时不应进入解码")
	}
}

func TestEvaluateCallbackRejectsDecodeFailureAfterVerification(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	verifier := &fakeVerifier{algorithm: "hmac-sha256", decodeErr: NewError(CodeCallbackPayloadInvalid, "bad body", nil)}
	decision := EvaluateCallback(cfg, verifier, context.Background(), http.Header{}, []byte("{}"))
	if decision.Accepted || decision.Code != CodeCallbackPayloadInvalid {
		t.Fatalf("解码失败必须拒绝: %+v", decision)
	}
}

func TestEvaluateCallbackRejectsEventWithoutOrderNo(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	verifier := &fakeVerifier{algorithm: "hmac-sha256", event: &CallbackEvent{State: StatePaid}}
	decision := EvaluateCallback(cfg, verifier, context.Background(), http.Header{}, []byte("{}"))
	if decision.Accepted || decision.Code != CodeCallbackPayloadInvalid {
		t.Fatalf("缺少订单号必须拒绝: %+v", decision)
	}
}

func TestEvaluateCallbackAcceptsOnlyAfterVerification(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	verifier := &fakeVerifier{algorithm: "hmac-sha256", event: &CallbackEvent{UpstreamOrderNo: "UP-1", State: StatePaid}}
	decision := EvaluateCallback(cfg, verifier, context.Background(), http.Header{}, []byte("{}"))
	if !decision.Accepted || decision.Event == nil || decision.Event.UpstreamOrderNo != "UP-1" {
		t.Fatalf("验签通过后必须接受: %+v", decision)
	}
}

func TestHMACCallbackVerifierRejectsEveryIncompleteCase(t *testing.T) {
	secret := []byte("shared-secret")
	payload := func(_ http.Header, body []byte) string { return string(body) }
	valid := &HMACCallbackVerifier{
		Secret:          secret,
		SignatureHeader: "X-Signature",
		TimestampHeader: "X-Timestamp",
		MaxSkew:         time.Minute,
		Payload:         payload,
	}

	body := []byte("order=UP-1&status=paid")
	now := time.Now().UTC()

	t.Run("valid", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Signature", SignHMACSHA256Hex(secret, string(body)))
		headers.Set("X-Timestamp", now.Format(time.RFC3339))
		if err := valid.Verify(context.Background(), headers, body); err != nil {
			t.Fatalf("合法签名必须通过: %v", err)
		}
	})

	t.Run("wrong signature", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Signature", SignHMACSHA256Hex([]byte("other-secret"), string(body)))
		headers.Set("X-Timestamp", now.Format(time.RFC3339))
		if err := valid.Verify(context.Background(), headers, body); !IsCode(err, CodeCallbackSignatureInvalid) {
			t.Fatalf("签名错误必须拒绝: %v", err)
		}
	})

	t.Run("missing signature", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Timestamp", now.Format(time.RFC3339))
		if err := valid.Verify(context.Background(), headers, body); !IsCode(err, CodeCallbackSignatureInvalid) {
			t.Fatalf("缺少签名必须拒绝: %v", err)
		}
	})

	t.Run("stale timestamp", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-Signature", SignHMACSHA256Hex(secret, string(body)))
		headers.Set("X-Timestamp", now.Add(-2*time.Hour).Format(time.RFC3339))
		if err := valid.Verify(context.Background(), headers, body); !IsCode(err, CodeCallbackSignatureInvalid) {
			t.Fatalf("时间戳越界必须拒绝: %v", err)
		}
	})

	t.Run("no secret configured", func(t *testing.T) {
		broken := &HMACCallbackVerifier{SignatureHeader: "X-Signature", Payload: payload}
		if err := broken.Verify(context.Background(), http.Header{}, body); !IsCode(err, CodeCallbackSignatureUnavailable) {
			t.Fatalf("密钥缺失必须 fail closed: %v", err)
		}
	})

	t.Run("no payload rule", func(t *testing.T) {
		broken := &HMACCallbackVerifier{Secret: secret, SignatureHeader: "X-Signature"}
		if err := broken.Verify(context.Background(), http.Header{}, body); !IsCode(err, CodeCallbackSignatureUnavailable) {
			t.Fatalf("待签串规则缺失必须 fail closed: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// 商品同步与购买暂停
// ---------------------------------------------------------------------------

func TestSyncCatalogFailureKeepsLastGoodProductsAndPausesPurchase(t *testing.T) {
	store := newMemoryStore()
	adapter := &stubAdapter{configured: true, products: []Product{sampleProduct()}}
	svc := NewService(configuredConfig("https://upstream.invalid"), adapter, store, newAESEncryptor())

	if _, err := svc.SyncCatalog(context.Background()); err != nil {
		t.Fatalf("首次同步应成功: %v", err)
	}
	if state := svc.CatalogState(context.Background()); !state.Available || len(state.Products) != 1 {
		t.Fatalf("同步成功后目录应可用: %+v", state)
	}

	adapter.listErr = NewError(CodeUpstreamUnavailable, "上游不可用", nil)
	if _, err := svc.SyncCatalog(context.Background()); err == nil {
		t.Fatal("上游不可用时同步必须失败")
	}

	state := svc.CatalogState(context.Background())
	if state.Available {
		t.Fatal("同步失败后必须暂停购买")
	}
	if state.UnavailableReason != CodeProductSyncUnavailable {
		t.Fatalf("want %s, got %s", CodeProductSyncUnavailable, state.UnavailableReason)
	}
	if len(state.Products) != 1 {
		t.Fatalf("同步失败不得删除最近一次成功商品: %+v", state.Products)
	}

	if _, err := svc.CreateOrder(context.Background(), 7, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "k1"}); !IsCode(err, CodeProductSyncUnavailable) {
		t.Fatalf("同步失败时下单必须被拒, got %v", err)
	}
}

func TestSyncCatalogNotConfiguredDoesNotTouchCatalog(t *testing.T) {
	store := newMemoryStore()
	store.catalog = &CatalogSnapshot{Products: []Product{sampleProduct()}, SyncedAt: time.Now().UTC()}
	svc := NewService(Config{}, nil, store, newAESEncryptor())

	if _, err := svc.SyncCatalog(context.Background()); !IsCode(err, CodeNotConfigured) {
		t.Fatalf("want %s, got %v", CodeNotConfigured, err)
	}
	if store.failCat {
		t.Fatal("未配置时不得写入同步失败标记")
	}
	if len(store.catalog.Products) != 1 {
		t.Fatal("未配置时不得清空商品")
	}
}

// ---------------------------------------------------------------------------
// 下单：幂等与归属
// ---------------------------------------------------------------------------

func newOrderService(t *testing.T) (*Service, *memoryStore, *stubAdapter) {
	t.Helper()
	store := newMemoryStore()
	adapter := &stubAdapter{configured: true, products: []Product{sampleProduct()}}
	adapter.created = &UpstreamOrder{UpstreamOrderNo: "UP-100", State: StateUnpaid, AmountCNY: 10}
	svc := NewService(configuredConfig("https://upstream.invalid"), adapter, store, newAESEncryptor())
	if _, err := svc.SyncCatalog(context.Background()); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	return svc, store, adapter
}

func TestCreateOrderIsIdempotentByRequestKey(t *testing.T) {
	svc, store, adapter := newOrderService(t)
	req := CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "idem-1"}

	first, err := svc.CreateOrder(context.Background(), 42, req)
	if err != nil {
		t.Fatalf("首次下单失败: %v", err)
	}
	second, err := svc.CreateOrder(context.Background(), 42, req)
	if err != nil {
		t.Fatalf("重复下单失败: %v", err)
	}
	if first.OrderNo != second.OrderNo {
		t.Fatalf("同一幂等键必须映射同一条订单: %s vs %s", first.OrderNo, second.OrderNo)
	}
	if len(store.orders) != 1 {
		t.Fatalf("重复点击只能有一条映射, got %d", len(store.orders))
	}
	if got := atomic.LoadInt32(&adapter.createCall); got != 1 {
		t.Fatalf("重复点击不得重复调用上游下单, got %d", got)
	}
}

func TestCreateOrderRejectsSameKeyWithDifferentContent(t *testing.T) {
	svc, _, _ := newOrderService(t)
	if _, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "idem-x"}); err != nil {
		t.Fatalf("首次下单失败: %v", err)
	}
	_, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 3, IdempotencyKey: "idem-x"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("同一幂等键不同内容必须冲突, got %v", err)
	}
}

func TestCreateOrderRequiresIdempotencyKey(t *testing.T) {
	svc, _, _ := newOrderService(t)
	if _, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1}); err == nil {
		t.Fatal("缺少幂等键必须拒绝")
	}
}

func TestCreateOrderRejectsUnknownOrUnlistedProduct(t *testing.T) {
	svc, _, _ := newOrderService(t)
	if _, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "nope", Quantity: 1, IdempotencyKey: "a"}); !IsCode(err, CodeProductNotFound) {
		t.Fatalf("未知商品必须拒绝: %v", err)
	}

	store := newMemoryStore()
	unlisted := sampleProduct()
	unlisted.Listed = false
	adapter := &stubAdapter{configured: true, products: []Product{unlisted}}
	adapter.created = &UpstreamOrder{UpstreamOrderNo: "UP-1", State: StateUnpaid}
	svc2 := NewService(configuredConfig("https://upstream.invalid"), adapter, store, newAESEncryptor())
	if _, err := svc2.SyncCatalog(context.Background()); err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	if _, err := svc2.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "b"}); !IsCode(err, CodeProductNotFound) {
		t.Fatalf("已下架商品必须拒绝: %v", err)
	}
}

func TestCreateOrderRejectsZeroStockProduct(t *testing.T) {
	store := newMemoryStore()
	outOfStock := sampleProduct()
	zero := 0
	outOfStock.Stock = &zero
	adapter := &stubAdapter{configured: true, products: []Product{outOfStock}}
	adapter.created = &UpstreamOrder{UpstreamOrderNo: "UP-1", State: StateUnpaid}
	svc := NewService(configuredConfig("https://upstream.invalid"), adapter, store, newAESEncryptor())
	if _, err := svc.SyncCatalog(context.Background()); err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	if _, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "c"}); !IsCode(err, CodeProductNotFound) {
		t.Fatalf("无库存商品必须拒绝: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 查单与卡密
// ---------------------------------------------------------------------------

func TestQueryOrderUnknownStateIsNotTreatedAsPaid(t *testing.T) {
	svc, _, adapter := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "q1"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	adapter.queried = &UpstreamOrder{UpstreamOrderNo: "UP-100", State: StateUnknown, UpstreamStatusRaw: "weird"}

	view, err := svc.QueryOrder(context.Background(), 42, order.OrderNo)
	if err != nil {
		t.Fatalf("查单失败: %v", err)
	}
	if view.DisplayStatus != DisplayPending {
		t.Fatalf("未知状态绝不能当作已支付, got %s", view.DisplayStatus)
	}
	if view.CardSecret != "" {
		t.Fatal("未支付不得返回卡密")
	}
}

func TestQueryOrderPaidWithoutCardsMarksDeliveryPending(t *testing.T) {
	svc, _, adapter := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "q2"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	adapter.queried = &UpstreamOrder{UpstreamOrderNo: "UP-100", State: StatePaid}
	adapter.cards = nil

	view, err := svc.QueryOrder(context.Background(), 42, order.OrderNo)
	if err != nil {
		t.Fatalf("查单失败: %v", err)
	}
	if view.DisplayStatus != DisplayDeliveryPending {
		t.Fatalf("已支付但卡密未就绪必须标记交付异常, got %s", view.DisplayStatus)
	}
	if view.DeliveryErrorCode != string(CodeDeliveryPending) {
		t.Fatalf("want %s, got %s", CodeDeliveryPending, view.DeliveryErrorCode)
	}
	if view.CardSecret != "" {
		t.Fatal("交付异常不得返回卡密")
	}
}

func TestQueryOrderPaidWithCardsDeliversEncryptedCard(t *testing.T) {
	svc, store, adapter := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "q3"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	adapter.queried = &UpstreamOrder{UpstreamOrderNo: "UP-100", State: StatePaid}
	adapter.cards = []Card{{Secret: "CARD-PLAINTEXT-1234"}}

	view, err := svc.QueryOrder(context.Background(), 42, order.OrderNo)
	if err != nil {
		t.Fatalf("查单失败: %v", err)
	}
	if view.DisplayStatus != DisplayDelivered {
		t.Fatalf("want delivered, got %s", view.DisplayStatus)
	}
	if view.CardSecret != "CARD-PLAINTEXT-1234" {
		t.Fatalf("本人查询应返回明文卡密, got %q", view.CardSecret)
	}
	if view.RedeemPath != "/redeem" {
		t.Fatalf("必须引导到本站兑换入口, got %q", view.RedeemPath)
	}
	stored := store.orders[order.OrderNo]
	if stored.CardSecret == "" || strings.Contains(stored.CardSecret, "CARD-PLAINTEXT") {
		t.Fatalf("卡密必须加密落库, got %q", stored.CardSecret)
	}
}

func TestQueryOrderRepeatReturnsSameCards(t *testing.T) {
	svc, _, adapter := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "q4"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	adapter.queried = &UpstreamOrder{UpstreamOrderNo: "UP-100", State: StatePaid}
	adapter.cards = []Card{{Secret: "CARD-A"}}

	first, err := svc.QueryOrder(context.Background(), 42, order.OrderNo)
	if err != nil {
		t.Fatalf("首次查单失败: %v", err)
	}
	// 上游后续不再返回卡密：本站必须仍然展示已交付的同一批卡密（PRD-03 §2.6）。
	adapter.cards = nil
	second, err := svc.GetOrder(context.Background(), 42, order.OrderNo)
	if err != nil {
		t.Fatalf("再次查询失败: %v", err)
	}
	if first.CardSecret != second.CardSecret || second.CardSecret != "CARD-A" {
		t.Fatalf("重复查询必须返回同一批卡密: %q vs %q", first.CardSecret, second.CardSecret)
	}
}

func TestQueryOrderFailureKeepsPaidStatus(t *testing.T) {
	svc, store, adapter := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "q5"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	adapter.queried = &UpstreamOrder{UpstreamOrderNo: "UP-100", State: StatePaid}
	adapter.cards = []Card{{Secret: "CARD-PAID"}}
	if _, err := svc.QueryOrder(context.Background(), 42, order.OrderNo); err != nil {
		t.Fatalf("首次查单失败: %v", err)
	}
	if got := store.orders[order.OrderNo].DisplayStatus; got != DisplayDelivered {
		t.Fatalf("已支付且有卡密应为 delivered, got %s", got)
	}

	// 之后上游查询失败：绝不能把已支付结果改回未支付（PRD-03 §2.5 / AC-B7）。
	adapter.queryErr = NewError(CodeUpstreamUnavailable, "上游不可用", nil)
	if _, err := svc.QueryOrder(context.Background(), 42, order.OrderNo); err == nil {
		t.Fatal("上游失败时必须返回错误")
	}
	view, err := svc.GetOrder(context.Background(), 42, order.OrderNo)
	if err != nil {
		t.Fatalf("读取订单失败: %v", err)
	}
	if view.DisplayStatus != DisplayDelivered {
		t.Fatalf("查询失败不得改写已支付结果, got %s", view.DisplayStatus)
	}
	if view.CardSecret != "CARD-PAID" {
		t.Fatalf("查询失败不得丢失已交付卡密, got %q", view.CardSecret)
	}
}

func TestGetOrderIsOwnerScoped(t *testing.T) {
	svc, _, _ := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "own"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	if _, err := svc.GetOrder(context.Background(), 43, order.OrderNo); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("他人订单必须返回 not found（不区分存在性）, got %v", err)
	}
	if _, err := svc.QueryOrder(context.Background(), 43, order.OrderNo); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("他人订单查单必须返回 not found, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 支付指引白标守卫
// ---------------------------------------------------------------------------

func TestGuardPaymentInstructionBlocksNonAllowlistedRedirect(t *testing.T) {
	cfg := configuredConfig("https://upstream.invalid")
	cfg.RedirectHostAllowlist = []string{"pay.example.com"}
	svc := NewService(cfg, nil, nil, nil)

	blocked := svc.guardPaymentInstruction(PaymentInstruction{Kind: PayRedirect, RedirectURL: "https://upstream-brand.example.com/checkout"})
	if blocked.Kind != PayNone {
		t.Fatalf("未在白名单内的跳转目标必须被降级为 none, got %s", blocked.Kind)
	}

	allowed := svc.guardPaymentInstruction(PaymentInstruction{Kind: PayRedirect, RedirectURL: "https://pay.example.com/checkout"})
	if allowed.Kind != PayRedirect {
		t.Fatalf("白名单内的跳转目标应放行, got %s", allowed.Kind)
	}

	emptyAllowlist := NewService(configuredConfig("https://upstream.invalid"), nil, nil, nil)
	if got := emptyAllowlist.guardPaymentInstruction(PaymentInstruction{Kind: PayRedirect, RedirectURL: "https://pay.example.com/x"}); got.Kind != PayNone {
		t.Fatalf("白名单为空时必须 fail closed, got %s", got.Kind)
	}
}

// ---------------------------------------------------------------------------
// 回调处理：未验签不得改状态
// ---------------------------------------------------------------------------

func TestApplyCallbackDoesNotChangeOrderWithoutVerification(t *testing.T) {
	svc, store, _ := newOrderService(t)
	order, err := svc.CreateOrder(context.Background(), 42, CreateOrderRequest{ProductID: "sku-1", Quantity: 1, IdempotencyKey: "cb"})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	store.orders[order.OrderNo].UpstreamOrderNo = "UP-100"

	decision := svc.ApplyCallback(context.Background(), http.Header{}, []byte(`{"status":"paid"}`))
	if decision.Accepted {
		t.Fatal("未验签的回调绝不能接受")
	}
	if got := store.orders[order.OrderNo].DisplayStatus; got != DisplayPending {
		t.Fatalf("未验签回调不得改变订单状态, got %s", got)
	}
}
