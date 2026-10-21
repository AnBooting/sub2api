package yunmao

import (
	"context"
	"net/http"
	"time"
)

// ---------------------------------------------------------------------------
// 商品 / 订单 / 卡密 —— 白标展示模型
// ---------------------------------------------------------------------------

// Product 是商品快照（本站只展示，不重复录入价格或权益）。
//
// 白标约束：本结构只会进入本站 API 响应，因此**不得**包含上游品牌、店铺标识、
// 上游商品页链接或任何上游域名（PRD-03 FE-04 / AC-F1）。
type Product struct {
	// ID 是上游商品标识，作为本站商品目录的稳定主键。
	ID string
	// Name / Description 由上游提供，本站不自行改写权益描述。
	Name        string
	Description string
	// PriceCNY 是上游给出的 CNY 售价；本站不重算手续费、不二次定价（PRD-03 §2.1）。
	PriceCNY float64
	// OriginalPriceCNY 是划线价，nil 表示无。
	OriginalPriceCNY *float64
	// Stock 为剩余可购数量；nil 表示上游未提供（此时不得据此判定可购）。
	Stock *int
	// Listed 为上游上下架状态。
	Listed bool
	// CreditPerCNY 是本站展示的兑换比例（¥1 = 1 Credit，PRD-03 §2.1）。
	CreditPerCNY float64
	// SyncedAt 是这条快照的同步时间。
	SyncedAt time.Time
}

// Purchasable 表示该商品当前是否允许下单。
//
// 判定只依赖「已上架 + 有库存或库存未知」，真正的下单放行还要求商品目录同步健康
// （见 Service.ProductCatalogState）。
func (p Product) Purchasable() bool {
	if !p.Listed {
		return false
	}
	if p.Stock == nil {
		return true
	}
	return *p.Stock > 0
}

// DisplayStatus 是本站对上游支付/交付结果的**展示映射**，不是独立支付状态机。
//
// 取值与迁移 256 的 CHECK 约束一一对应（PRD-03 §2、共享契约 order_delivery_pending）。
type DisplayStatus string

const (
	// DisplayPending 待支付。
	DisplayPending DisplayStatus = "pending"
	// DisplayPaid 上游已支付，卡密尚未就绪或尚未拉取。
	DisplayPaid DisplayStatus = "paid"
	// DisplayDeliveryPending 已支付但卡密获取失败（PRD-03 §2.5 的「交付异常」）。
	DisplayDeliveryPending DisplayStatus = "delivery_pending"
	// DisplayDelivered 卡密已就绪，可展示与复制。
	DisplayDelivered DisplayStatus = "delivered"
)

// Valid 判断展示状态是否为合法取值。
func (s DisplayStatus) Valid() bool {
	switch s {
	case DisplayPending, DisplayPaid, DisplayDeliveryPending, DisplayDelivered:
		return true
	default:
		return false
	}
}

// Card 是一张卡密。
//
// 明文只允许在「服务端向所属用户返回」这一条路径上出现；落库必须加密，
// 日志与错误信息中永远不出现（PRD-03 §2.7 / BE-06）。
type Card struct {
	// Secret 是卡密明文，仅在内存中短暂存在。
	Secret string
	// Hint 是脱敏提示（例如尾号），可安全落库并用于列表展示。
	Hint string
}

// UpstreamOrderState 是上游订单状态的归一化取值。
//
// 适配层负责把上游的原始状态字符串映射到这里；本站不自己发明支付状态。
type UpstreamOrderState string

const (
	// StateUnknown 上游返回了无法识别的状态：按「未知」处理，不当作已支付。
	StateUnknown UpstreamOrderState = "unknown"
	// StateUnpaid 未支付。
	StateUnpaid UpstreamOrderState = "unpaid"
	// StatePaid 已支付。
	StatePaid UpstreamOrderState = "paid"
	// StateDelivered 已支付且卡密已交付。
	StateDelivered UpstreamOrderState = "delivered"
	// StateClosed 已关闭 / 已取消 / 已退款（本站只做展示映射，不建退款系统）。
	StateClosed UpstreamOrderState = "closed"
)

// UpstreamOrder 是上游订单的归一化视图。
type UpstreamOrder struct {
	// UpstreamOrderNo 是上游订单号：只在服务端内部使用，绝不出现在本站 API 响应里。
	UpstreamOrderNo string
	State           UpstreamOrderState
	// UpstreamStatusRaw 是上游原始状态字符串，只用于服务端诊断，不进入响应。
	UpstreamStatusRaw string
	// AmountCNY 为上游给出的实付金额。
	AmountCNY float64
	// Cards 在上游查单/取卡密一并返回时填充；为空表示卡密尚未就绪。
	Cards []Card
	// Payment 是给前端的支付指引（可为空）。
	Payment PaymentInstruction
	// PaidAt 上游支付时间，nil 表示未支付。
	PaidAt *time.Time
	// ExpiresAt 支付过期时间，nil 表示上游未提供。
	ExpiresAt *time.Time
}

// PaymentInstructionKind 描述支付指引的类型。
type PaymentInstructionKind string

const (
	// PayNone 无支付指引（例如上游返回的仍是待支付但未给指引）。
	PayNone PaymentInstructionKind = "none"
	// PayQRCode 展示二维码内容，由用户在自己的支付 App 中完成（PRD-03 §2.3：必要的外部支付动作不算上游页面）。
	PayQRCode PaymentInstructionKind = "qr_code"
	// PayRedirect 跳转到支付渠道页面。跳转目标必须通过白标校验，见 Service.guardPaymentInstruction。
	PayRedirect PaymentInstructionKind = "redirect"
)

// PaymentInstruction 是适配层允许下发给前端的支付指引。
//
// 这是**白名单结构**：上游响应里任何未在此声明的字段都不会到达前端，
// 因此上游页面地址、品牌参数不会被顺手透传（PRD-03 FE-04）。
type PaymentInstruction struct {
	Kind PaymentInstructionKind
	// QRContent 是二维码内容（支付渠道链接或串码），仅在 Kind=PayQRCode 时有意义。
	QRContent string
	// RedirectURL 是支付渠道跳转地址，仅在 Kind=PayRedirect 时有意义；
	// 必须通过白标校验（见 Service 中的守卫）。
	RedirectURL string
	// ExpiresAt 是支付指引的有效期。
	ExpiresAt *time.Time
}

// ---------------------------------------------------------------------------
// 四个固定能力 + 验签能力：适配层契约
// ---------------------------------------------------------------------------

// CreateOrderRequest 是下单入参。
type CreateOrderRequest struct {
	// ProductID 是上游商品标识。
	ProductID string
	// Quantity 是购买数量，必须 >= 1。
	Quantity int
	// IdempotencyKey 是客户端请求 ID：本站据此保证同一请求只映射到一条订单（PRD-03 BE-04）。
	IdempotencyKey string
	// BuyerRef 是本站侧对买家的最小不透明引用（如 "u123"）。
	// 禁止填入邮箱、用户名、手机号等 PII。
	BuyerRef string
}

// Adapter 是上游供应商的适配层契约。
//
// 四个能力（商品列表 / 下单 / 查单 / 取卡密）的**方法签名与返回类型是稳定的**：
// 拿到真实接口文档后只需要新增一个实现并替换装配，调用方（Service / Handler / 前端）
// 不改一行。返回类型刻意只暴露本站语义，避免把上游字段名固化进调用方。
//
// 所有实现都必须遵守 fail closed：
//   - 未配置时返回 CodeNotConfigured，而不是空列表或假数据；
//   - 无法解码的响应返回 CodeUpstreamResponseInvalid，不猜测字段。
type Adapter interface {
	// Name 返回内部供应商标识，仅用于日志与管理面，不得出现在用户可见文案里。
	Name() string
	// Configured 表示凭据与接口地址是否齐备。false 时其余方法必须 fail closed。
	Configured() bool
	// ListProducts 拉取商品列表（商品同步）。
	ListProducts(ctx context.Context) ([]Product, error)
	// CreateOrder 下单。
	CreateOrder(ctx context.Context, req CreateOrderRequest) (*UpstreamOrder, error)
	// QueryOrder 主动查单：支付结果的**唯一可信来源**之一（PRD-03 §2.4 / BE-03）。
	QueryOrder(ctx context.Context, upstreamOrderNo string) (*UpstreamOrder, error)
	// FetchCards 取卡密。
	FetchCards(ctx context.Context, upstreamOrderNo string) ([]Card, error)
}

// CallbackEvent 是已验签回调解码出的统一事件。
type CallbackEvent struct {
	// UpstreamOrderNo 是回调指向的上游订单号。
	UpstreamOrderNo string
	State           UpstreamOrderState
	// StatusRaw 是上游原始状态，仅供服务端诊断。
	StatusRaw string
	// Cards 若上游在回调里直接带回卡密则填充；否则走主动查单。
	Cards []Card
	// PaidAt 上游支付时间。
	PaidAt *time.Time
	// EventID 是上游事件 ID，用于幂等；为空时退化为「订单号 + 状态」幂等。
	EventID string
}

// CallbackVerifier 是回调验签契约（预留）。
//
// 高危红线：签名算法尚未确定时，实现必须返回 CodeCallbackSignatureUnavailable 并拒绝该回调，
// **绝不默认放行**（PRD-03 §2.4、AC-B1：前端成功标记或不可信通知不能被当作支付成功）。
type CallbackVerifier interface {
	// SignatureAlgorithm 返回当前生效的签名算法标识；返回空串表示「未确定」。
	SignatureAlgorithm() string
	// Verify 校验回调可信性。返回 nil 才代表验签通过。
	Verify(ctx context.Context, headers http.Header, body []byte) error
	// Decode 把**已通过验签**的报文解码为统一事件。
	Decode(ctx context.Context, body []byte) (*CallbackEvent, error)
}

// ---------------------------------------------------------------------------
// 配置
// ---------------------------------------------------------------------------

// Config 是适配层的配置槽位。
//
// 凭据只允许来自环境变量 / 配置，**绝不落库明文、不进前端、不进日志**（PRD-03 BE-01、共享契约 C-8）。
// 本结构刻意实现 fmt.Stringer 的替代物（Redacted）以便安全打印。
type Config struct {
	// Enabled 是管理员总开关：false 时一律 fail closed，即使填了地址与凭据。
	Enabled bool
	// BaseURL 是上游接口根地址，例如 https://<host>/api（真实地址等待接口文档）。
	BaseURL string
	// Endpoints 是四个能力的相对路径槽位，等待接口文档填充。
	Endpoints Endpoints
	// Credentials 是凭据槽位。
	Credentials Credentials
	// Timeout 是单次上游调用超时。
	Timeout time.Duration
	// RedirectHostAllowlist 是允许作为支付跳转目标的主机白名单。
	// 为空时任何 PayRedirect 指引都会 fail closed（宁可不跳转，也不把用户送到未知站点）。
	RedirectHostAllowlist []string
}

// Endpoints 是四个能力的相对路径槽位。
//
// 这些路径**不是**对上游接口的猜测，而是必须由运维在配置里显式填写的槽位；
// 未填写的槽位对应能力直接 fail closed。
type Endpoints struct {
	// ListProducts 商品列表。
	ListProducts string
	// CreateOrder 下单。
	CreateOrder string
	// QueryOrder 查单。
	QueryOrder string
	// FetchCards 取卡密。
	FetchCards string
}

// Credentials 是上游凭据槽位。
type Credentials struct {
	// AppID 等字段名是**本站槽位名**，不假设上游字段命名。
	AppID string
	// AppSecret 是密钥；只从环境变量读取。
	AppSecret string
	// Extra 是留给接口文档确认后追加的凭据键（如店铺号、签名盐）。
	Extra map[string]string
}

// Redacted 返回可安全写日志的配置摘要：只保留「有没有配」，不保留值本身。
func (c Config) Redacted() map[string]any {
	return map[string]any{
		"enabled":         c.Enabled,
		"base_url_set":    c.BaseURL != "",
		"app_id_set":      c.Credentials.AppID != "",
		"app_secret_set":  c.Credentials.AppSecret != "",
		"endpoint_count":  c.Endpoints.configuredCount(),
		"redirect_hosts":  len(c.RedirectHostAllowlist),
		"timeout_seconds": int(c.Timeout.Seconds()),
	}
}

func (e Endpoints) configuredCount() int {
	n := 0
	for _, v := range []string{e.ListProducts, e.CreateOrder, e.QueryOrder, e.FetchCards} {
		if v != "" {
			n++
		}
	}
	return n
}

// Complete 表示四个能力槽位是否全部填写。
func (e Endpoints) Complete() bool { return e.configuredCount() == 4 }
