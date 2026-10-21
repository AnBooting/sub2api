package yunmao

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SecretEncryptor 与 service.SecretEncryptor 结构一致。
//
// 刻意在包内重新声明而不是直接引用 service 包：本包会被 handler 层直接依赖，
// 重新声明可以避免 service ↔ yunmao 的循环依赖，同时让单测注入内存加密器。
// repository.NewAESEncryptor 的返回值天然满足本接口（鸭子类型）。
type SecretEncryptor interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// Service 是白标充值中心的业务编排层。
//
// 职责边界（PRD-03）：
//   - 只做「展示映射 + 最小订单关联 + 卡密加密存取」；
//   - 不核算收款主体、不做退款、不做资金清结算、不自动入账；
//   - 支付结果的唯一可信来源是「上游回调验签通过」或「服务端主动查单」。
type Service struct {
	adapter   Adapter
	verifier  CallbackVerifier
	store     OrderStore
	encryptor SecretEncryptor
	cfg       Config

	now        func() time.Time
	newOrderNo func() (string, error)
}

// Option 覆盖 Service 的可注入部件（测试用）。
type Option func(*Service)

// WithClock 注入时钟。
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithOrderNoGenerator 注入订单号生成器。
func WithOrderNoGenerator(gen func() (string, error)) Option {
	return func(s *Service) {
		if gen != nil {
			s.newOrderNo = gen
		}
	}
}

// WithCallbackVerifier 替换回调验签器（默认是 fail closed 的 PendingCallbackVerifier）。
func WithCallbackVerifier(verifier CallbackVerifier) Option {
	return func(s *Service) {
		if verifier != nil {
			s.verifier = verifier
		}
	}
}

// NewService 构造业务服务。
//
// adapter / store 可以为 nil：此时所有需要它们的操作都会 fail closed，
// 而不是 panic 或静默成功——这样「未配置」与「配置错误」都能被明确观察到。
func NewService(cfg Config, adapter Adapter, store OrderStore, encryptor SecretEncryptor, opts ...Option) *Service {
	service := &Service{
		adapter:    adapter,
		verifier:   PendingCallbackVerifier{},
		store:      store,
		encryptor:  encryptor,
		cfg:        cfg,
		now:        time.Now,
		newOrderNo: defaultOrderNo,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

// defaultOrderNo 生成本站可追踪订单号。
//
// 前缀刻意使用中性字符（RC = recharge），不含任何上游品牌，符合白标要求（PRD-03 FE-04）。
func defaultOrderNo() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", NewError(CodeUpstreamRejected, "订单号生成失败", err)
	}
	return "RC" + time.Now().UTC().Format("20060102150405") + strings.ToUpper(hex.EncodeToString(buf)), nil
}

// ---------------------------------------------------------------------------
// 商品目录
// ---------------------------------------------------------------------------

// CatalogState 是商品目录的对外状态。
type CatalogState struct {
	// Configured 表示上游是否已配置。false 时前端必须展示「未配置」而不是空商品列表。
	Configured bool
	// Available 表示当前是否允许下单（已配置 且 最近一次同步成功 且 有商品）。
	Available bool
	// Products 是最近一次成功同步的商品快照；同步失败时**保留**（PRD-03 BE-08）。
	Products []Product
	// SyncedAt 最近一次成功同步时间；零值表示从未成功同步。
	SyncedAt time.Time
	// SyncFailedAt 最近一次同步失败时间；nil 表示当前无失败标记。
	SyncFailedAt *time.Time
	// SyncErrorCode 最近一次同步失败的稳定错误码。
	SyncErrorCode ErrorCode
	// UnavailableReason 是给前端展示的稳定错误码（空串表示可用）。
	UnavailableReason ErrorCode
}

// SyncCatalog 执行一次商品同步。
//
// fail closed 语义：未配置时直接返回 CodeNotConfigured，**不写入失败标记、不清空商品**；
// 同步失败时记录失败标记并保留上一次成功数据（PRD-03 §2.8 / BE-08）。
func (s *Service) SyncCatalog(ctx context.Context) (CatalogState, error) {
	if err := s.requireAdapter(); err != nil {
		return s.CatalogState(ctx), err
	}
	products, err := s.adapter.ListProducts(ctx)
	if err != nil {
		code := CodeOf(err)
		if code == "" {
			code = CodeUpstreamUnavailable
		}
		if s.store != nil {
			_ = s.store.MarkCatalogSyncFailure(ctx, code, s.now())
		}
		return s.CatalogState(ctx), err
	}
	if s.store == nil {
		return CatalogState{Configured: true}, NewError(CodeNotConfigured, "订单存储未配置", nil)
	}
	if err := s.store.SaveCatalog(ctx, products, s.now()); err != nil {
		return s.CatalogState(ctx), err
	}
	return s.CatalogState(ctx), nil
}

// CatalogState 读取当前目录状态（不发起任何上游调用）。
func (s *Service) CatalogState(ctx context.Context) CatalogState {
	state := CatalogState{Configured: s.configured()}
	if s.store == nil {
		state.UnavailableReason = CodeNotConfigured
		return state
	}
	snapshot, err := s.store.LoadCatalog(ctx)
	if err != nil {
		state.UnavailableReason = CodeUpstreamUnavailable
		return state
	}
	if snapshot == nil {
		if state.Configured {
			state.UnavailableReason = CodeProductSyncUnavailable
		} else {
			state.UnavailableReason = CodeNotConfigured
		}
		return state
	}
	state.Products = snapshot.Products
	state.SyncedAt = snapshot.SyncedAt
	state.SyncFailedAt = snapshot.SyncFailedAt
	state.SyncErrorCode = ErrorCode(snapshot.SyncErrorCode)
	switch {
	case !state.Configured:
		state.UnavailableReason = CodeNotConfigured
	case snapshot.SyncFailedAt != nil:
		// 同步失败：商品仍展示，但暂停新订单（PRD-03 §2.8）。
		state.UnavailableReason = CodeProductSyncUnavailable
	case len(snapshot.Products) == 0:
		state.UnavailableReason = CodeProductSyncUnavailable
	default:
		state.Available = true
	}
	return state
}

// ---------------------------------------------------------------------------
// 下单
// ---------------------------------------------------------------------------

// OrderView 是订单的对外视图。
//
// 白标约束：不含上游订单号、不含上游域名、不含凭据。卡密明文只在「请求者是订单所属用户
// 且订单已交付」时填充（PRD-03 §2.7 / AC-B3）。
type OrderView struct {
	OrderNo       string
	ProductID     string
	ProductName   string
	Quantity      int
	AmountCNY     float64
	DisplayStatus DisplayStatus
	CardSecret    string
	CardHint      string
	// RedeemPath 是本站现有的兑换入口路径，前端据此引导用户兑换（PRD-03 FE-02）。
	RedeemPath string
	// DeliveryErrorCode 是交付异常时的稳定错误码（仅本人可见）。
	DeliveryErrorCode string
	Payment           PaymentInstruction
	PaidAt            *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateOrder 下单。
//
// 顺序（每一步失败都 fail closed，不落库、不下发假数据）：
//  1. 适配层已配置；
//  2. 幂等键非空且未被复用（重复点击返回同一条映射，PRD-03 BE-04 / AC-B2）；
//  3. 商品目录同步健康（同步失败 → product_sync_unavailable，PRD-03 BE-08）；
//  4. 商品存在且可购；
//  5. 调用上游下单；
//  6. 写入本站最小映射。
func (s *Service) CreateOrder(ctx context.Context, userID int64, req CreateOrderRequest) (*OrderView, error) {
	if err := s.requireAdapter(); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, NewError(CodeNotConfigured, "订单存储未配置", nil)
	}
	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, NewError(CodeUpstreamRejected, "缺少请求标识", nil)
	}
	if req.Quantity < 1 {
		return nil, NewError(CodeUpstreamRejected, "购买数量非法", nil)
	}
	productID := strings.TrimSpace(req.ProductID)
	if productID == "" {
		return nil, NewError(CodeProductNotFound, "商品不存在或已下架", nil)
	}

	if existing, err := s.store.FindByIdempotencyKey(ctx, userID, idempotencyKey); err == nil && existing != nil {
		if existing.ProductID != productID || existing.Quantity != req.Quantity {
			return nil, ErrIdempotencyConflict
		}
		return s.toView(ctx, existing, userID)
	} else if err != nil && !errors.Is(err, ErrOrderNotFound) {
		return nil, err
	}

	catalog := s.CatalogState(ctx)
	if !catalog.Configured {
		return nil, notConfiguredError("白标充值服务未配置")
	}
	if !catalog.Available {
		return nil, NewError(CodeProductSyncUnavailable, "商品服务暂时不可用，请稍后重试", nil)
	}
	product, found := findProduct(catalog.Products, productID)
	if !found || !product.Purchasable() {
		return nil, NewError(CodeProductNotFound, "商品不存在或已下架", nil)
	}

	upstreamOrder, err := s.adapter.CreateOrder(ctx, CreateOrderRequest{
		ProductID:      productID,
		Quantity:       req.Quantity,
		IdempotencyKey: idempotencyKey,
		BuyerRef:       req.BuyerRef,
	})
	if err != nil {
		return nil, err
	}
	if upstreamOrder == nil || strings.TrimSpace(upstreamOrder.UpstreamOrderNo) == "" {
		return nil, NewError(CodeUpstreamResponseInvalid, "上游未返回订单号", nil)
	}

	orderNo, err := s.newOrderNo()
	if err != nil {
		return nil, err
	}
	snapshot, err := json.Marshal(map[string]any{
		"id":                 product.ID,
		"name":               product.Name,
		"description":        product.Description,
		"price_cny":          product.PriceCNY,
		"original_price_cny": product.OriginalPriceCNY,
		"credit_per_cny":     product.CreditPerCNY,
	})
	if err != nil {
		return nil, NewError(CodeUpstreamRejected, "商品快照序列化失败", err)
	}

	// 支付指引只允许白名单结构透传；跳转目标必须过白标校验（见 guardPaymentInstruction）。
	payment := s.guardPaymentInstruction(upstreamOrder.Payment)
	amount := product.PriceCNY * float64(req.Quantity)
	record, err := s.store.Create(ctx, CreateOrderRecord{
		OrderNo:         orderNo,
		UserID:          userID,
		IdempotencyKey:  idempotencyKey,
		UpstreamOrderNo: upstreamOrder.UpstreamOrderNo,
		ProductID:       productID,
		ProductSnapshot: snapshot,
		AmountCNY:       amount,
		Quantity:        req.Quantity,
		DisplayStatus:   displayStatusFor(upstreamOrder.State),
	})
	if err != nil {
		// 并发下的幂等键冲突：回读既有映射，保证「重复点击只有一条映射」。
		if errors.Is(err, ErrIdempotencyConflict) {
			if existing, findErr := s.store.FindByIdempotencyKey(ctx, userID, idempotencyKey); findErr == nil && existing != nil {
				return s.toView(ctx, existing, userID)
			}
		}
		return nil, err
	}

	// 上游下单即已支付（少见但合法）时立刻结算，避免卡在 pending。
	// 结算失败（加密槽位未配置/加密失败）时不能把 record 覆盖成 nil：
	// 订单已落库，应返回已持久化状态而不是误报 ErrOrderNotFound。
	if record.DisplayStatus != DisplayPending {
		if settled, settleErr := s.settleFromUpstream(ctx, record, upstreamOrder); settleErr == nil && settled != nil {
			record = settled
		}
	}
	view, err := s.toView(ctx, record, userID)
	if err != nil {
		return nil, err
	}
	view.Payment = payment
	return view, nil
}

// GetOrder 读取订单（仅本人）。
//
// 不发起上游调用：刷新页面要便宜。恢复查询走 QueryOrder。
func (s *Service) GetOrder(ctx context.Context, userID int64, orderNo string) (*OrderView, error) {
	if s.store == nil {
		return nil, NewError(CodeNotConfigured, "订单存储未配置", nil)
	}
	record, err := s.store.FindByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if record.UserID != userID {
		// 不区分「不存在」与「不属于你」：避免探测他人订单存在性（PRD-03 AC-B3）。
		return nil, ErrOrderNotFound
	}
	return s.toView(ctx, record, userID)
}

// ListOrders 列出当前用户订单（仅本人）。
func (s *Service) ListOrders(ctx context.Context, userID int64, limit int) ([]OrderView, error) {
	if s.store == nil {
		return nil, NewError(CodeNotConfigured, "订单存储未配置", nil)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	records, err := s.store.ListByUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	views := make([]OrderView, 0, len(records))
	for i := range records {
		view, viewErr := s.toView(ctx, &records[i], userID)
		if viewErr != nil {
			return nil, viewErr
		}
		views = append(views, *view)
	}
	return views, nil
}

// QueryOrder 主动查单并结算（支付结果的唯一可信来源之一，PRD-03 §2.4 / BE-03）。
func (s *Service) QueryOrder(ctx context.Context, userID int64, orderNo string) (*OrderView, error) {
	if err := s.requireAdapter(); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, NewError(CodeNotConfigured, "订单存储未配置", nil)
	}
	record, err := s.store.FindByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if record.UserID != userID {
		return nil, ErrOrderNotFound
	}
	if strings.TrimSpace(record.UpstreamOrderNo) == "" {
		return s.toView(ctx, record, userID)
	}

	upstreamOrder, err := s.adapter.QueryOrder(ctx, record.UpstreamOrderNo)
	if err != nil {
		s.recordQueryFailure(ctx, record, err)
		return nil, err
	}
	settled, settleErr := s.settleFromUpstream(ctx, record, upstreamOrder)
	if settleErr != nil {
		s.recordQueryFailure(ctx, record, settleErr)
		return nil, settleErr
	}
	view, viewErr := s.toView(ctx, settled, userID)
	if viewErr != nil {
		return nil, viewErr
	}
	view.Payment = s.guardPaymentInstruction(upstreamOrder.Payment)
	return view, nil
}

// ApplyCallback 处理上游回调。
//
// 红线：验签未通过（或算法未确定）时**绝不改状态**，只返回拒绝原因；
// 调用方必须据此返回 401（PRD-03 接口文档 / AC-B1）。
func (s *Service) ApplyCallback(ctx context.Context, headers http.Header, body []byte) CallbackDecision {
	decision := EvaluateCallback(s.cfg, s.verifier, ctx, headers, body)
	if !decision.Accepted {
		return decision
	}
	if s.store == nil {
		return CallbackDecision{Accepted: false, Code: CodeNotConfigured}
	}
	record, err := s.store.FindByUpstreamOrderNo(ctx, decision.Event.UpstreamOrderNo)
	if err != nil {
		// 未知订单：不改任何状态。Accepted 保持 true（回调本身可信），但调用方按 NotFound 处理。
		decision.Accepted = false
		decision.Code = CodeOrderNotFound
		return decision
	}
	upstreamOrder := &UpstreamOrder{
		UpstreamOrderNo:   decision.Event.UpstreamOrderNo,
		State:             decision.Event.State,
		UpstreamStatusRaw: decision.Event.StatusRaw,
		Cards:             decision.Event.Cards,
		PaidAt:            decision.Event.PaidAt,
	}
	if _, settleErr := s.settleFromUpstream(ctx, record, upstreamOrder); settleErr != nil {
		decision.Accepted = false
		code := CodeOf(settleErr)
		if code == "" {
			code = CodeUpstreamUnavailable
		}
		decision.Code = code
		return decision
	}
	return decision
}

// ---------------------------------------------------------------------------
// 内部
// ---------------------------------------------------------------------------

func (s *Service) configured() bool {
	if !s.cfg.Enabled {
		return false
	}
	return ValidateConfig(s.cfg) == nil
}

func (s *Service) requireAdapter() error {
	if s.adapter == nil {
		return notConfiguredError("上游适配层未装配")
	}
	if err := ValidateConfig(s.cfg); err != nil {
		return err
	}
	if !s.adapter.Configured() {
		return notConfiguredError("上游适配层未配置")
	}
	return nil
}

// settleFromUpstream 用上游结果更新本站展示状态。
//
// 状态映射（只映射、不自建状态机）：
//   - unpaid/unknown/closed → 保持 pending（unknown 绝不当作已支付）；
//   - paid → 有卡密则 delivered，否则尝试取卡密，取不到记 delivery_pending；
//   - delivered → 取卡密并落库。
func (s *Service) settleFromUpstream(ctx context.Context, record *OrderRecord, upstreamOrder *UpstreamOrder) (*OrderRecord, error) {
	if record == nil || upstreamOrder == nil {
		return record, nil
	}
	now := s.now()
	patch := OrderPatch{
		UpstreamStatus:  stringPtr(string(upstreamOrder.State)),
		LastQueriedAt:   &now,
		UpstreamOrderNo: stringPtr(upstreamOrder.UpstreamOrderNo),
	}

	switch upstreamOrder.State {
	case StatePaid, StateDelivered:
		if upstreamOrder.PaidAt != nil {
			patch.PaidAt = upstreamOrder.PaidAt
		} else if record.PaidAt == nil {
			patch.PaidAt = &now
		}

		cards := upstreamOrder.Cards
		if len(cards) == 0 {
			// adapter 可为 nil（NewService 允许），此时无法补取卡密，
			// 必须显式返回交付异常而不是空指针 panic。
			if s.adapter == nil {
				status := DisplayDeliveryPending
				code := string(CodeDeliveryPending)
				patch.DisplayStatus = &status
				patch.LastErrorCode = &code
				return s.applyPatch(ctx, record, patch)
			}
			fetched, fetchErr := s.adapter.FetchCards(ctx, record.UpstreamOrderNo)
			if fetchErr != nil {
				// 已支付但卡密取不到：交付异常，明确错误码 + 保留可追踪订单号（PRD-03 §2.5）。
				status := DisplayDeliveryPending
				code := string(CodeDeliveryPending)
				attempts := record.DeliveryAttempts + 1
				patch.DisplayStatus = &status
				patch.LastErrorCode = &code
				patch.DeliveryAttempts = &attempts
				return s.applyPatch(ctx, record, patch)
			}
			cards = fetched
		}
		if len(cards) == 0 {
			status := DisplayDeliveryPending
			code := string(CodeDeliveryPending)
			patch.DisplayStatus = &status
			patch.LastErrorCode = &code
			return s.applyPatch(ctx, record, patch)
		}
		if s.encryptor == nil {
			return nil, NewError(CodeDeliveryUnavailable, "卡密加密槽位未配置", nil)
		}
		encrypted, encryptErr := s.encryptor.Encrypt(cards[0].Secret)
		if encryptErr != nil {
			return nil, NewError(CodeDeliveryUnavailable, "卡密加密失败", encryptErr)
		}
		hint := cards[0].Hint
		if hint == "" {
			hint = maskSecret(cards[0].Secret)
		}
		status := DisplayDelivered
		deliveredAt := now
		patch.DisplayStatus = &status
		patch.CardSecret = &encrypted
		patch.CardHint = &hint
		patch.DeliveredAt = &deliveredAt
		patch.LastErrorCode = stringPtr("")
		return s.applyPatch(ctx, record, patch)
	default:
		// unpaid / unknown / closed：保持展示状态不变，只记录上游状态与查询时间。
		return s.applyPatch(ctx, record, patch)
	}
}

func (s *Service) applyPatch(ctx context.Context, record *OrderRecord, patch OrderPatch) (*OrderRecord, error) {
	if s.store == nil {
		return record, nil
	}
	updated, err := s.store.Update(ctx, record.OrderNo, patch)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return record, nil
	}
	return updated, nil
}

func (s *Service) recordQueryFailure(ctx context.Context, record *OrderRecord, err error) {
	if s.store == nil || record == nil {
		return
	}
	code := CodeOf(err)
	if code == "" {
		code = CodeUpstreamUnavailable
	}
	now := s.now()
	attempts := record.DeliveryAttempts + 1
	_, _ = s.store.Update(ctx, record.OrderNo, OrderPatch{
		LastErrorCode:    stringPtr(string(code)),
		LastQueriedAt:    &now,
		DeliveryAttempts: &attempts,
	})
}

// toView 组装对外视图；卡密只对订单所属用户解密。
func (s *Service) toView(ctx context.Context, record *OrderRecord, requesterID int64) (*OrderView, error) {
	if record == nil {
		return nil, ErrOrderNotFound
	}
	view := &OrderView{
		OrderNo:       record.OrderNo,
		ProductID:     record.ProductID,
		ProductName:   productNameFromSnapshot(record.ProductSnapshot),
		Quantity:      record.Quantity,
		AmountCNY:     record.AmountCNY,
		DisplayStatus: record.DisplayStatus,
		CardHint:      record.CardHint,
		RedeemPath:    "/redeem",
		PaidAt:        record.PaidAt,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
	}
	if record.LastErrorCode != "" && record.DisplayStatus == DisplayDeliveryPending {
		view.DeliveryErrorCode = record.LastErrorCode
	}
	if record.UserID != requesterID {
		// 双重保险：仓储层已做归属校验，这里再拦一次。
		return view, nil
	}
	if record.CardSecret != "" && record.DisplayStatus == DisplayDelivered {
		if s.encryptor == nil {
			return view, nil
		}
		plaintext, err := s.encryptor.Decrypt(record.CardSecret)
		if err != nil {
			// 解密失败不回退明文、不返回空卡密：标记交付异常并保留可追踪订单号。
			view.DisplayStatus = DisplayDeliveryPending
			view.DeliveryErrorCode = string(CodeDeliveryUnavailable)
			return view, nil
		}
		view.CardSecret = plaintext
	}
	_ = ctx
	return view, nil
}

// guardPaymentInstruction 校验上游给出的支付指引是否可以安全下发。
//
// fail closed：PayRedirect 的目标主机不在白名单时降级为 PayNone，
// 宁可不跳转，也不把用户送到未知/上游品牌站点（PRD-03 FE-04 / §2.3）。
func (s *Service) guardPaymentInstruction(instruction PaymentInstruction) PaymentInstruction {
	if instruction.Kind != PayRedirect {
		return instruction
	}
	parsed, err := url.Parse(strings.TrimSpace(instruction.RedirectURL))
	if err != nil || parsed.Host == "" {
		return PaymentInstruction{Kind: PayNone}
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return PaymentInstruction{Kind: PayNone}
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range s.cfg.RedirectHostAllowlist {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return instruction
		}
	}
	return PaymentInstruction{Kind: PayNone}
}

func displayStatusFor(state UpstreamOrderState) DisplayStatus {
	switch state {
	case StatePaid:
		return DisplayPaid
	case StateDelivered:
		return DisplayPaid
	default:
		return DisplayPending
	}
}

func findProduct(products []Product, productID string) (Product, bool) {
	for _, product := range products {
		if product.ID == productID {
			return product, true
		}
	}
	return Product{}, false
}

func productNameFromSnapshot(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var decoded struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	return decoded.Name
}

// maskSecret 生成脱敏提示：只保留末 4 位。
func maskSecret(secret string) string {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return "****"
	}
	return "****" + trimmed[len(trimmed)-4:]
}

func stringPtr(value string) *string { return &value }
