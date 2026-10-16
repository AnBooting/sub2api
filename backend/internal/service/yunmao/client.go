package yunmao

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// maxResponseBytes 限制上游响应体大小，避免异常上游打爆内存。
const maxResponseBytes = 1 << 20 // 1 MiB

// RequestBuilder 是「把本站请求翻译成上游报文」的槽位。
//
// 为什么单独抽出来：上游的路径、方法、请求体字段名与鉴权头写法都属于**接口文档未提供的
// 信息**。这里不猜，而是把「报文构造」做成可替换实现：
//   - 文档到位后写一个实现（或直接改 DefaultRequestBuilder 的 TODO 处）；
//   - 调用方（Service / Handler / 前端）完全不受影响。
type RequestBuilder interface {
	BuildListProducts(ctx context.Context, cfg Config) (*http.Request, error)
	BuildCreateOrder(ctx context.Context, cfg Config, req CreateOrderRequest) (*http.Request, error)
	BuildQueryOrder(ctx context.Context, cfg Config, upstreamOrderNo string) (*http.Request, error)
	BuildFetchCards(ctx context.Context, cfg Config, upstreamOrderNo string) (*http.Request, error)
}

// ResponseDecoder 是「把上游响应解码成本站语义」的槽位。
type ResponseDecoder interface {
	DecodeProducts(ctx context.Context, body []byte) ([]Product, error)
	DecodeOrder(ctx context.Context, body []byte) (*UpstreamOrder, error)
	DecodeCards(ctx context.Context, body []byte) ([]Card, error)
}

// DefaultRequestBuilder 使用本站字段名构造最小 JSON 报文。
//
// TODO(云猫文档)：拿到接口文档后必须核对并替换以下内容，替换前该实现只能配合
// 兼容本站字段名的上游/网关使用，不能假定它已经对接成功：
//   - HTTP 方法（当前统一 POST）；
//   - 请求体字段名（当前为 product_id / quantity / idempotency_key / buyer_ref / order_no）；
//   - 鉴权头写法（当前 X-App-Id / X-App-Secret）；
//   - 成功/失败的业务码约定（当前只按 HTTP 状态码判定）。
type DefaultRequestBuilder struct{}

func (DefaultRequestBuilder) BuildListProducts(_ context.Context, cfg Config) (*http.Request, error) {
	return newJSONRequest(cfg, cfg.Endpoints.ListProducts, map[string]any{})
}

func (DefaultRequestBuilder) BuildCreateOrder(_ context.Context, cfg Config, req CreateOrderRequest) (*http.Request, error) {
	return newJSONRequest(cfg, cfg.Endpoints.CreateOrder, map[string]any{
		"product_id":      req.ProductID,
		"quantity":        req.Quantity,
		"idempotency_key": req.IdempotencyKey,
		"buyer_ref":       req.BuyerRef,
	})
}

func (DefaultRequestBuilder) BuildQueryOrder(_ context.Context, cfg Config, upstreamOrderNo string) (*http.Request, error) {
	return newJSONRequest(cfg, cfg.Endpoints.QueryOrder, map[string]any{"order_no": upstreamOrderNo})
}

func (DefaultRequestBuilder) BuildFetchCards(_ context.Context, cfg Config, upstreamOrderNo string) (*http.Request, error) {
	return newJSONRequest(cfg, cfg.Endpoints.FetchCards, map[string]any{"order_no": upstreamOrderNo})
}

func newJSONRequest(cfg Config, path string, payload map[string]any) (*http.Request, error) {
	endpoint, err := cfg.resolveEndpoint(path)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, NewError(CodeUpstreamRejected, "请求体构造失败", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, NewError(CodeUpstreamRejected, "请求构造失败", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// 凭据只出现在服务端出站请求头里：不落库、不回显、不进日志。
	req.Header.Set("X-App-Id", cfg.Credentials.AppID)
	req.Header.Set("X-App-Secret", cfg.Credentials.AppSecret)
	for key, value := range cfg.Credentials.Extra {
		req.Header.Set("X-Credential-"+strings.ToUpper(key), value)
	}
	return req, nil
}

// PendingResponseDecoder 是等待接口文档期间的**占位解码器**：一律 fail closed。
//
// 它存在的意义是让「未确定的上游响应结构」变成一个显式、可测试、可替换的失败点，
// 而不是在代码里悄悄猜字段名。TODO(云猫文档)：文档到位后实现真实解码并替换装配。
type PendingResponseDecoder struct{}

func (PendingResponseDecoder) DecodeProducts(context.Context, []byte) ([]Product, error) {
	return nil, errDecoderPending()
}

func (PendingResponseDecoder) DecodeOrder(context.Context, []byte) (*UpstreamOrder, error) {
	return nil, errDecoderPending()
}

func (PendingResponseDecoder) DecodeCards(context.Context, []byte) ([]Card, error) {
	return nil, errDecoderPending()
}

func errDecoderPending() error {
	return NewError(CodeUpstreamResponseInvalid, "上游响应解码器未实现（等待上游接口文档）", nil)
}

// HTTPAdapter 是 Adapter 的真实实现：可配置 base URL + 端点槽位 + 凭据槽位。
//
// 行为约定：
//   - Configured()==false 时四个能力全部 fail closed，且**不发起任何网络请求**；
//   - 单个能力对应的路径槽位缺失时，只有该能力 fail closed；
//   - 所有错误都是本站语义，绝不携带上游响应原文或凭据。
type HTTPAdapter struct {
	cfg      Config
	client   *http.Client
	requests RequestBuilder
	decoder  ResponseDecoder
}

// HTTPAdapterOption 用于覆盖适配层的可替换部件。
type HTTPAdapterOption func(*HTTPAdapter)

// WithRequestBuilder 替换报文构造实现。
func WithRequestBuilder(builder RequestBuilder) HTTPAdapterOption {
	return func(a *HTTPAdapter) {
		if builder != nil {
			a.requests = builder
		}
	}
}

// WithResponseDecoder 替换响应解码实现。
func WithResponseDecoder(decoder ResponseDecoder) HTTPAdapterOption {
	return func(a *HTTPAdapter) {
		if decoder != nil {
			a.decoder = decoder
		}
	}
}

// WithHTTPClient 注入 HTTP 客户端（测试用）。
func WithHTTPClient(client *http.Client) HTTPAdapterOption {
	return func(a *HTTPAdapter) {
		if client != nil {
			a.client = client
		}
	}
}

// refuseCrossHostRedirect rejects any redirect that leaves the original host.
//
// Same-host redirects are allowed (they cannot move the credential headers to a
// different principal). A cross-host hop returns an explicit error so the adapter
// reports a local upstream failure instead of sending credentials onward.
func refuseCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return fmt.Errorf("refusing cross-host redirect to %q", req.URL.Host)
	}
	if len(via) >= 5 {
		return errors.New("stopped after 5 redirects")
	}
	return nil
}

// NewHTTPAdapter 构造真实适配层。注意：构造成功不代表已配置——
// 未配置时所有调用都会返回 CodeNotConfigured。
func NewHTTPAdapter(cfg Config, opts ...HTTPAdapterOption) *HTTPAdapter {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	adapter := &HTTPAdapter{
		cfg: cfg,
		// CheckRedirect pins every outbound call to the configured host. The
		// credentials travel in custom X-App-Id / X-App-Secret / X-Credential-*
		// headers, and Go's client only strips Authorization/Cookie on a redirect,
		// so a cross-host 302 (from the upstream or an injected middlebox) would
		// otherwise replay the AppSecret to a third party. Refusing the redirect
		// fails closed instead of leaking.
		client:   &http.Client{Timeout: timeout, CheckRedirect: refuseCrossHostRedirect},
		requests: DefaultRequestBuilder{},
		decoder:  PendingResponseDecoder{},
	}
	for _, opt := range opts {
		opt(adapter)
	}
	return adapter
}

// Name 返回内部供应商标识：只用于服务端日志与诊断，不得出现在用户可见文案里。
func (a *HTTPAdapter) Name() string { return "upstream-shop" }

// Config 返回当前配置副本（凭据字段仍在其中，禁止直接序列化到日志）。
func (a *HTTPAdapter) Config() Config { return a.cfg }

// Configured 见 Adapter 契约。
func (a *HTTPAdapter) Configured() bool { return ValidateConfig(a.cfg) == nil }

// ListProducts 见 Adapter 契约。
func (a *HTTPAdapter) ListProducts(ctx context.Context) ([]Product, error) {
	req, err := a.requests.BuildListProducts(ctx, a.cfg)
	if err != nil {
		return nil, err
	}
	body, err := a.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return a.decoder.DecodeProducts(ctx, body)
}

// CreateOrder 见 Adapter 契约。
func (a *HTTPAdapter) CreateOrder(ctx context.Context, create CreateOrderRequest) (*UpstreamOrder, error) {
	if strings.TrimSpace(create.ProductID) == "" {
		return nil, NewError(CodeProductNotFound, "商品不存在或已下架", nil)
	}
	if create.Quantity < 1 {
		return nil, NewError(CodeUpstreamRejected, "购买数量非法", nil)
	}
	req, err := a.requests.BuildCreateOrder(ctx, a.cfg, create)
	if err != nil {
		return nil, err
	}
	body, err := a.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return a.decoder.DecodeOrder(ctx, body)
}

// QueryOrder 见 Adapter 契约。
func (a *HTTPAdapter) QueryOrder(ctx context.Context, upstreamOrderNo string) (*UpstreamOrder, error) {
	req, err := a.requests.BuildQueryOrder(ctx, a.cfg, upstreamOrderNo)
	if err != nil {
		return nil, err
	}
	body, err := a.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return a.decoder.DecodeOrder(ctx, body)
}

// FetchCards 见 Adapter 契约。
func (a *HTTPAdapter) FetchCards(ctx context.Context, upstreamOrderNo string) ([]Card, error) {
	req, err := a.requests.BuildFetchCards(ctx, a.cfg, upstreamOrderNo)
	if err != nil {
		return nil, err
	}
	body, err := a.do(ctx, req)
	if err != nil {
		return nil, err
	}
	return a.decoder.DecodeCards(ctx, body)
}

// do 执行出站请求。
//
// 红线：这里构造的任何错误都不得包含上游响应原文或凭据；上游返回的错误信息
// 只保留「HTTP 状态码」，正文一律丢弃（PRD-03 §2.7 / AC-B3）。
func (a *HTTPAdapter) do(ctx context.Context, req *http.Request) ([]byte, error) {
	if !a.Configured() {
		return nil, ValidateConfig(a.cfg)
	}
	if a.client == nil {
		return nil, NewError(CodeUpstreamUnavailable, "上游客户端不可用", nil)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		// 只保留错误类型，不拼入 URL（URL 可能带 query 凭据）与上游原文。
		return nil, NewError(CodeUpstreamUnavailable, "上游请求失败", sanitizeTransportError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return nil, NewError(CodeUpstreamRejected, fmt.Sprintf("上游鉴权失败（HTTP %d）", resp.StatusCode), nil)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return nil, NewError(CodeUpstreamUnavailable, fmt.Sprintf("上游暂时不可用（HTTP %d）", resp.StatusCode), nil)
		default:
			return nil, NewError(CodeUpstreamRejected, fmt.Sprintf("上游拒绝了本次调用（HTTP %d）", resp.StatusCode), nil)
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, NewError(CodeUpstreamUnavailable, "上游响应读取失败", sanitizeTransportError(err))
	}
	if len(body) > maxResponseBytes {
		return nil, NewError(CodeUpstreamResponseInvalid, "上游响应体过大", nil)
	}
	return body, nil
}

// sanitizeTransportError 只保留错误的**类型**，丢掉可能含 URL/凭据的文本。
func sanitizeTransportError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case isTimeout(err):
		return fmt.Errorf("upstream timeout")
	case strings.Contains(err.Error(), "connection refused"):
		return fmt.Errorf("upstream connection refused")
	default:
		return fmt.Errorf("upstream transport error")
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded")
}
