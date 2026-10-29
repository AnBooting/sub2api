package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	coderws "github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	defaultLiveMaxSessionDuration = time.Hour
	liveLeaseRefreshInterval      = 20 * time.Second
	liveRedisOperationTimeout     = 3 * time.Second
	liveClosedRecordTTL           = 24 * time.Hour
	liveObserverPollInterval      = 250 * time.Millisecond
	liveObserverStoreRetryLimit   = 5
	liveUpstreamBodyLimit         = 2 << 20
	// liveSettlementRetryAttempts 是 observer 对 pending 结算的自动重试次数。
	liveSettlementRetryAttempts = 4
)

// liveObserverStoreRetryInterval 是 var 以便测试缩短 store 报错的重试等待。
var liveObserverStoreRetryInterval = time.Second

// liveSettlementRetryInterval 是 var 以便测试缩短等待；逐步退避到分钟级，
// 覆盖数据库/网络短时故障，又不至于在小时级故障里刷屏。
var liveSettlementRetryInterval = func(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 5 * time.Second
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

var (
	chatGPTLiveCallsURL        = "https://chatgpt.com/backend-api/codex/realtime/calls?intent=quicksilver&architecture=avas"
	chatGPTLiveSidebandBaseURL = "wss://chatgpt.com/backend-api/codex"
)

type liveFrameConn interface {
	ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error)
	WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error
	Close() error
}

func liveSidebandReadError(err error) error {
	if coderws.CloseStatus(err) == coderws.StatusNormalClosure {
		return ErrLiveCallNotFound
	}
	return err
}

func hashLiveCallID(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return hex.EncodeToString(sum[:])
}

func liveGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func liveOptionalID(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	result := value
	return &result
}

func (s *OpenAIGatewayService) liveStore() (LiveCallStore, error) {
	if s == nil || s.cache == nil {
		return nil, ErrLiveUnavailable
	}
	store, ok := s.cache.(LiveCallStore)
	if !ok {
		return nil, ErrLiveUnavailable
	}
	return store, nil
}

func (s *OpenAIGatewayService) liveConcurrencyCache() (LiveConcurrencyCache, error) {
	if s == nil || s.concurrencyService == nil || s.concurrencyService.cache == nil {
		return nil, ErrLiveUnavailable
	}
	cache, ok := s.concurrencyService.cache.(LiveConcurrencyCache)
	if !ok {
		return nil, ErrLiveUnavailable
	}
	return cache, nil
}

func (s *OpenAIGatewayService) liveMaxSessionDuration() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.Live.MaxSessionDurationSeconds > 0 {
		return time.Duration(s.cfg.Gateway.Live.MaxSessionDurationSeconds) * time.Second
	}
	return defaultLiveMaxSessionDuration
}

func ValidateLiveCallRequest(request *LiveCallRequest) error {
	if request == nil || strings.TrimSpace(request.SDP) == "" {
		return errors.New("sdp is required")
	}
	if len(request.Session) == 0 || !json.Valid(request.Session) {
		return errors.New("session must be valid JSON")
	}
	var sessionObject map[string]json.RawMessage
	if err := json.Unmarshal(request.Session, &sessionObject); err != nil {
		return errors.New("session must be a JSON object")
	}
	if sessionObject == nil {
		return errors.New("session must be a JSON object")
	}
	return nil
}

// CreateLiveCall 创建 Frameless 会话。调用方须在调用期间持有普通用户槽位；
// 调度器持有的普通账号槽位会被同一个 Live 租约原子接替。
func (s *OpenAIGatewayService) CreateLiveCall(
	ctx context.Context,
	request *LiveCallRequest,
	identity LiveCallIdentity,
	userMaxConcurrency int,
) (*LiveCallCreated, error) {
	if err := ValidateLiveCallRequest(request); err != nil {
		return nil, err
	}
	store, err := s.liveStore()
	if err != nil {
		return nil, err
	}
	liveCache, err := s.liveConcurrencyCache()
	if err != nil {
		return nil, err
	}
	attestation, attestationCiphertext, err := s.prepareLiveAttestation(ctx)
	if err != nil {
		return nil, err
	}

	excluded := make(map[int64]struct{})
	// Live 按通话时长计费，不属于 token 利润门的语义范围：显式豁免，避免
	// 防御性装门按文本 D 过滤 Live 账号池且门与计费时刻不同源。
	ctx = WithOpenAIProfitControlSuppressed(ctx)
	var lastErr error
	for attempt := 0; attempt <= 3; attempt++ {
		selection, _, selectErr := s.SelectAccountWithSchedulerForCapability(
			ctx,
			identity.GroupID,
			"",
			uuid.NewString(),
			"",
			excluded,
			OpenAIUpstreamTransportHTTPSSE,
			OpenAIEndpointCapabilityLive,
			false,
			false,
			false,
		)
		if selectErr != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, selectErr
		}
		if selection == nil || selection.Account == nil || !selection.Acquired {
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			return nil, ErrLiveConcurrencyFull
		}

		account := selection.Account
		leaseID := generateRequestID()
		acquired, acquireErr := liveCache.AcquireLiveLease(
			ctx,
			account.ID,
			account.Concurrency,
			identity.UserID,
			userMaxConcurrency,
			identity.APIKeyID,
			leaseID,
			true,
		)
		if acquireErr != nil || !acquired {
			selection.ReleaseFunc()
			if acquireErr != nil {
				return nil, acquireErr
			}
			return nil, ErrLiveConcurrencyFull
		}

		created, createErr := s.createUpstreamLiveCall(ctx, account, request, attestation)
		selection.ReleaseFunc()
		if createErr != nil {
			s.releaseLiveLease(account.ID, identity.UserID, identity.APIKeyID, leaseID)
			if !s.shouldFailoverLiveCreateError(account, createErr) {
				return nil, createErr
			}
			excluded[account.ID] = struct{}{}
			lastErr = createErr
			continue
		}

		now := time.Now()
		model := strings.TrimSpace(gjson.GetBytes(request.Session, "model").String())
		if model == "" {
			model = "gpt-live"
		}
		record := &LiveCallRecord{
			CallID:          created.CallID,
			CallHash:        hashLiveCallID(created.CallID),
			AccountID:       account.ID,
			APIKeyID:        identity.APIKeyID,
			UserID:          identity.UserID,
			GroupID:         liveGroupID(identity.GroupID),
			SubscriptionID:  liveGroupID(identity.SubscriptionID),
			LeaseID:         leaseID,
			Model:           model,
			CreatedAt:       now,
			ExpiresAt:       now.Add(s.liveMaxSessionDuration()),
			Controller:      LiveControllerPending,
			UserAgent:       identity.UserAgent,
			IPAddress:       identity.IPAddress,
			InboundEndpoint: identity.InboundEndpoint,
			// 开播即冻结单价与计费策略：结算在会话关闭时发生，期间改价或改限额
			// 不得影响本次会话，observer 回读与失败重试也只能使用这份快照。
			RealtimePricePerMin:   identity.RealtimePricePerMin,
			BillingPolicy:         identity.BillingPolicy,
			AttestationCiphertext: attestationCiphertext,
		}
		mappingTTL := s.liveMaxSessionDuration() + 5*time.Minute
		if saveErr := store.SaveLiveCall(ctx, record, mappingTTL); saveErr != nil {
			s.releaseLiveLease(account.ID, identity.UserID, identity.APIKeyID, leaseID)
			return nil, fmt.Errorf("save live call mapping: %w", saveErr)
		}
		created.Account = account
		go s.observeLiveCall(record)
		return created, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrLiveUnavailable
}

func (s *OpenAIGatewayService) shouldFailoverLiveCreateError(account *Account, err error) bool {
	var upstreamErr *UpstreamFailoverError
	if !errors.As(err, &upstreamErr) {
		// 凭证读取和网络传输错误都可能只影响当前账号或代理。
		return true
	}
	return s.shouldFailoverOpenAIUpstreamResponse(account,
		upstreamErr.StatusCode,
		"",
		upstreamErr.ResponseBody,
	)
}

func (s *OpenAIGatewayService) createUpstreamLiveCall(
	ctx context.Context,
	account *Account,
	request *LiveCallRequest,
	attestation string,
) (*LiveCallCreated, error) {
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		logLiveCreateStageFailure(ctx, account.ID, "access_token", err)
		return nil, err
	}
	body, err := json.Marshal(struct {
		SDP     string          `json:"sdp"`
		Session json.RawMessage `json:"session"`
	}{
		SDP:     request.SDP,
		Session: request.Session,
	})
	if err != nil {
		return nil, err
	}
	reqCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))
	upstreamReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, chatGPTLiveCallsURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		logLiveCreateStageFailure(ctx, account.ID, "authentication_headers", err)
		return nil, err
	}
	for key, values := range authHeaders {
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}
	upstreamReq.Host = "chatgpt.com"
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, upstreamReq.Header, account); err != nil {
		logLiveCreateStageFailure(ctx, account.ID, "account_headers", err)
		return nil, err
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", "application/sdp")
	upstreamReq.Header.Set(liveAttestationHeader, attestation)
	applyLiveUpstreamIdentityHeaders(upstreamReq.Header)

	resp, err := s.doOpenAIUpstream(upstreamReq, resolveAccountProxyURL(account), account)
	if err != nil {
		logLiveCreateStageFailure(ctx, account.ID, "upstream_transport", err)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, liveUpstreamBodyLimit+1))
	if readErr != nil {
		return nil, readErr
	}
	if len(responseBody) > liveUpstreamBodyLimit {
		return nil, errors.New("live upstream response is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logLiveUpstreamFailure(ctx, account.ID, resp.StatusCode, resp.Header, responseBody)
		return nil, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    responseBody,
			ResponseHeaders: resp.Header.Clone(),
		}
	}
	callID, err := liveCallIDFromLocation(resp.Header.Get("Location"))
	if err != nil {
		return nil, err
	}
	return &LiveCallCreated{
		SDP:      responseBody,
		CallID:   callID,
		Location: resp.Header.Get("Location"),
	}, nil
}

func logLiveCreateStageFailure(ctx context.Context, accountID int64, stage string, err error) {
	logger.FromContext(ctx).Warn(
		"OpenAI Live 创建阶段失败",
		zap.Int64("account_id", accountID),
		zap.String("stage", stage),
		zap.String("error_type", fmt.Sprintf("%T", err)),
	)
}

func logLiveUpstreamFailure(
	ctx context.Context,
	accountID int64,
	statusCode int,
	headers http.Header,
	body []byte,
) {
	errorType := strings.TrimSpace(gjson.GetBytes(body, "error.type").String())
	errorCode := strings.TrimSpace(gjson.GetBytes(body, "error.code").String())
	errorMessage := strings.TrimSpace(gjson.GetBytes(body, "error.message").String())
	if errorType == "" {
		errorType = strings.TrimSpace(gjson.GetBytes(body, "type").String())
	}
	if errorCode == "" {
		errorCode = strings.TrimSpace(gjson.GetBytes(body, "code").String())
	}
	if errorMessage == "" {
		errorMessage = strings.TrimSpace(gjson.GetBytes(body, "message").String())
	}
	if errorMessage == "" {
		errorMessage = strings.TrimSpace(gjson.GetBytes(body, "detail").String())
	}

	logger.FromContext(ctx).Warn(
		"OpenAI Live 上游拒绝请求",
		zap.Int64("account_id", accountID),
		zap.Int("upstream_status_code", statusCode),
		zap.String("upstream_error_type", truncateOpenAIWSLogValue(errorType, 120)),
		zap.String("upstream_error_code", truncateOpenAIWSLogValue(errorCode, 120)),
		zap.String("upstream_error_message", truncateOpenAIWSLogValue(errorMessage, 300)),
		zap.String("upstream_content_type", truncateOpenAIWSLogValue(headers.Get("Content-Type"), 120)),
		zap.String("upstream_server", truncateOpenAIWSLogValue(headers.Get("Server"), 120)),
		zap.String("upstream_cf_mitigated", truncateOpenAIWSLogValue(headers.Get("Cf-Mitigated"), 120)),
		zap.String("upstream_cf_ray", truncateOpenAIWSLogValue(headers.Get("Cf-Ray"), 120)),
		zap.String("upstream_request_id", truncateOpenAIWSLogValue(headers.Get("X-Request-Id"), 120)),
	)
}

func liveCallIDFromLocation(location string) (string, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return "", errors.New("live upstream response has no Location")
	}
	parsed, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("parse live Location: %w", err)
	}
	callID := strings.TrimSpace(path.Base(strings.TrimSuffix(parsed.Path, "/")))
	if callID == "" || callID == "." || callID == "codex" {
		return "", errors.New("live upstream Location has no call id")
	}
	return callID, nil
}

func applyLiveUpstreamIdentityHeaders(headers http.Header) {
	headers.Set("OpenAI-Alpha", "quicksilver=v2")
	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)
	if strings.TrimSpace(headers.Get("session-id")) == "" {
		headers.Set("session-id", uuid.NewString())
	}
	if strings.TrimSpace(headers.Get("thread-id")) == "" {
		headers.Set("thread-id", uuid.NewString())
	}
	// Realtime/Live 不使用 Responses 的实验头。
	headers.Del("OpenAI-Beta")
}

func (s *OpenAIGatewayService) liveSidebandHeaders(
	ctx context.Context,
	account *Account,
	record *LiveCallRecord,
) (http.Header, error) {
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	headers, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return nil, err
	}
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, headers, account); err != nil {
		return nil, err
	}
	attestation, err := s.decryptLiveAttestation(record)
	if err != nil {
		return nil, err
	}
	headers.Set(liveAttestationHeader, attestation)
	applyLiveUpstreamIdentityHeaders(headers)
	return headers, nil
}

func (s *OpenAIGatewayService) dialLiveSideband(ctx context.Context, record *LiveCallRecord) (liveFrameConn, error) {
	account, err := s.accountRepo.GetByID(ctx, record.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive) {
		return nil, ErrLiveUnavailable
	}
	headers, err := s.liveSidebandHeaders(ctx, account, record)
	if err != nil {
		return nil, err
	}
	target := strings.TrimRight(chatGPTLiveSidebandBaseURL, "/") + "/" + url.PathEscape(record.CallID)
	conn, status, _, err := s.getOpenAIWSPassthroughDialer().Dial(ctx, target, headers, resolveAccountProxyURL(account))
	if err != nil {
		return nil, fmt.Errorf("dial live sideband (status %d): %w", status, err)
	}
	raw, ok := conn.(liveFrameConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("live sideband transport does not support raw frames")
	}
	return raw, nil
}

func (s *OpenAIGatewayService) GetLiveCallForIdentity(
	ctx context.Context,
	callID string,
	identity LiveCallIdentity,
) (*LiveCallRecord, error) {
	store, err := s.liveStore()
	if err != nil {
		return nil, err
	}
	record, err := store.GetLiveCall(ctx, hashLiveCallID(callID))
	if err != nil {
		return nil, err
	}
	if record.CallID != callID ||
		record.APIKeyID != identity.APIKeyID ||
		record.UserID != identity.UserID ||
		record.GroupID != liveGroupID(identity.GroupID) {
		return nil, ErrLiveIdentityMismatch
	}
	if record.Controller == LiveControllerClosed {
		return nil, ErrLiveCallNotFound
	}
	return record, nil
}

// ProxyLiveSideband 让认证后的客户端接管控制连接；媒体始终不经过这里。
func (s *OpenAIGatewayService) ProxyLiveSideband(
	ctx context.Context,
	record *LiveCallRecord,
	downstream *coderws.Conn,
) error {
	if record == nil || downstream == nil {
		return ErrLiveCallNotFound
	}
	store, err := s.liveStore()
	if err != nil {
		return err
	}
	owner := uuid.NewString()
	claimed, err := store.ClaimLiveController(ctx, record.CallHash, LiveControllerProxy, owner)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrLiveControllerChanged
	}

	// observer 轮询到接管状态后会关闭旧控制连接；同一个 call 可重新加入。
	time.Sleep(liveObserverPollInterval)
	upstream, err := s.dialLiveSideband(ctx, record)
	if err != nil {
		_, _ = store.ReleaseLiveController(context.Background(), record.CallHash, owner)
		go s.observeLiveCall(record)
		return err
	}
	defer func() { _ = upstream.Close() }()
	downstream.SetReadLimit(openAIWSMessageReadLimitBytes)

	proxyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	go func() {
		for {
			messageType, payload, readErr := downstream.Read(proxyCtx)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if writeErr := upstream.WriteFrame(proxyCtx, messageType, payload); writeErr != nil {
				errCh <- writeErr
				return
			}
		}
	}()
	go func() {
		for {
			messageType, payload, readErr := upstream.ReadFrame(proxyCtx)
			if readErr != nil {
				errCh <- liveSidebandReadError(readErr)
				return
			}
			if writeErr := downstream.Write(proxyCtx, messageType, payload); writeErr != nil {
				errCh <- writeErr
				return
			}
			if messageType == coderws.MessageText {
				eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
				if eventType == "session.closed" || eventType == "session.ended" {
					errCh <- ErrLiveCallNotFound
					return
				}
			}
		}
	}()

	runErr := s.runLiveController(proxyCtx, record, upstream, errCh)
	cancel()
	_, _ = store.ReleaseLiveController(context.Background(), record.CallHash, owner)
	if liveSessionEnded(runErr) || !time.Now().Before(record.ExpiresAt) {
		s.finalizeLiveCall(record)
		go s.retryPendingLiveSettlement(record)
		return runErr
	}
	go s.observeLiveCall(record)
	return runErr
}

// liveSessionEnded 判断控制连接的退出原因是否意味着会话已终结（应 finalize：写
// usage log 并释放租约），而不是可以交给 observer 重连的临时错误。
//
// ErrLiveUnavailable 在控制循环里只会来自租约续租失败。RefreshLiveLease 的 Lua 在
// leaseID 被 GC 后不会重新写入，重连也拿不回并发槽 —— 若按临时错误重试，会话会以
// 约 1 秒一轮的节奏空转到 ExpiresAt，期间持着上游连接却不计入任何并发限制。
func liveSessionEnded(err error) bool {
	return errors.Is(err, ErrLiveCallNotFound) ||
		errors.Is(err, ErrLiveUnavailable) ||
		errors.Is(err, context.DeadlineExceeded)
}

func (s *OpenAIGatewayService) runLiveController(
	ctx context.Context,
	record *LiveCallRecord,
	upstream liveFrameConn,
	errCh <-chan error,
) error {
	refreshTicker := time.NewTicker(liveLeaseRefreshInterval)
	defer refreshTicker.Stop()
	maxTimer := time.NewTimer(time.Until(record.ExpiresAt))
	defer maxTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case err := <-errCh:
			return err
		case <-maxTimer.C:
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = upstream.WriteFrame(closeCtx, coderws.MessageText, []byte(`{"type":"session.close"}`))
			cancel()
			return context.DeadlineExceeded
		case <-refreshTicker.C:
			if !s.refreshLiveLease(record) {
				return ErrLiveUnavailable
			}
		}
	}
}

func (s *OpenAIGatewayService) observeLiveCall(record *LiveCallRecord) {
	if record == nil {
		return
	}
	store, err := s.liveStore()
	if err != nil {
		return
	}
	owner := uuid.NewString()
	claimed, claimErr := store.ClaimLiveController(context.Background(), record.CallHash, LiveControllerObserver, owner)
	if claimErr != nil {
		// store 报错时无法确认控制权归属，不能静默退出：若 claim 实际已生效而
		// observer 消失，租约与 usage log 都会丢。兜底 finalize 是幂等的，即使
		// 控制权在他人手上也只会在到期后落一次库。
		s.finalizeLiveCallAfterExpiry(record)
		return
	}
	if !claimed {
		return
	}
	storeErrStreak := 0
	for {
		latest, getErr := store.GetLiveCall(context.Background(), record.CallHash)
		if getErr != nil {
			// 记录已被清理（closed TTL 到期）不是故障，直接退出。
			if errors.Is(getErr, ErrLiveCallNotFound) {
				return
			}
			// store 抖动不等于控制权被接管：有限次重试；仍失败则按
			// record.ExpiresAt 兜底 finalize，保证 usage log 与租约释放不丢。
			storeErrStreak++
			if storeErrStreak >= liveObserverStoreRetryLimit {
				s.finalizeLiveCallAfterExpiry(record)
				return
			}
			time.Sleep(liveObserverStoreRetryInterval)
			continue
		}
		storeErrStreak = 0
		record = latest
		if record.Controller != LiveControllerObserver {
			// 控制权已交还（会话被 finalize）。若结算仍处于 pending（财务仓储
			// 临时故障），由 observer 在关闭 TTL 内自动重试，避免只依赖人工补账。
			s.retryPendingLiveSettlement(record)
			return
		}
		if !time.Now().Before(record.ExpiresAt) {
			s.finalizeLiveCall(record)
			// 本 observer 负责收尾：结算若因账本故障停在 pending，必须在这里接上自动
			// 重试。少了这一步，observer 退出后没有任何人会再重试这笔结算，记录只能
			// 等 TTL 过期被清掉（钱就收不回来了）。
			s.retryPendingLiveSettlement(record)
			return
		}
		upstream, dialErr := s.dialLiveSideband(context.Background(), record)
		if dialErr != nil {
			if !s.waitForLiveObserverRetry(record) {
				return
			}
			continue
		}
		runErr := s.runLiveObserverConnection(record, upstream)
		_ = upstream.Close()
		if errors.Is(runErr, ErrLiveControllerChanged) {
			return
		}
		if liveSessionEnded(runErr) {
			s.finalizeLiveCall(record)
			// 同上：observer 自己结束会话时，pending 结算的责任必须交出去重试。
			s.retryPendingLiveSettlement(record)
			return
		}
		if !s.waitForLiveObserverRetry(record) {
			return
		}
	}
}

func (s *OpenAIGatewayService) runLiveObserverConnection(record *LiveCallRecord, upstream liveFrameConn) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frameCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			messageType, payload, err := upstream.ReadFrame(ctx)
			if err != nil {
				select {
				case errCh <- liveSidebandReadError(err):
				case <-ctx.Done():
				}
				return
			}
			if messageType == coderws.MessageText {
				select {
				case frameCh <- payload:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	refreshTicker := time.NewTicker(liveLeaseRefreshInterval)
	defer refreshTicker.Stop()
	controllerTicker := time.NewTicker(liveObserverPollInterval)
	defer controllerTicker.Stop()
	maxTimer := time.NewTimer(time.Until(record.ExpiresAt))
	defer maxTimer.Stop()
	store, _ := s.liveStore()
	for {
		select {
		case payload := <-frameCh:
			eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
			if eventType == "session.closed" || eventType == "session.ended" {
				return ErrLiveCallNotFound
			}
		case err := <-errCh:
			return err
		case <-controllerTicker.C:
			controller, err := store.GetLiveController(context.Background(), record.CallHash)
			if err != nil {
				return err
			}
			if controller != LiveControllerObserver {
				return ErrLiveControllerChanged
			}
		case <-refreshTicker.C:
			if !s.refreshLiveLease(record) {
				return ErrLiveUnavailable
			}
		case <-maxTimer.C:
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = upstream.WriteFrame(closeCtx, coderws.MessageText, []byte(`{"type":"session.close"}`))
			closeCancel()
			return context.DeadlineExceeded
		}
	}
}

func (s *OpenAIGatewayService) waitForLiveObserverRetry(record *LiveCallRecord) bool {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	<-timer.C
	store, err := s.liveStore()
	if err != nil {
		return false
	}
	controller, getErr := store.GetLiveController(context.Background(), record.CallHash)
	if getErr != nil && !errors.Is(getErr, ErrLiveCallNotFound) {
		// store 报错不等于控制权被接管：返回 true 交回 observeLiveCall 循环顶部，
		// 由它对 store 故障做有限次重试与 ExpiresAt 兜底 finalize。在这里返回
		// false 会让 Redis 抖动时会话静默结束、不留记录。
		return true
	}
	// 过期不在此处判定：返回 true 让调用方回到循环顶部的过期分支，由它 finalize
	// （写 usage log + 释放租约）。在这里直接返回 false 会让会话静默结束、不留记录。
	return getErr == nil && controller == LiveControllerObserver
}

func liveSettlementNeedsRetry(state *LiveSettlement) bool {
	return state != nil && (state.Status == LiveSettlementPending || (state.Status == LiveSettlementSettled && !state.UsageLogWritten))
}

// retryPendingLiveSettlement 自动重试待结算的 Live 会话。
// 待结算和已结算但日志未确认的记录均可重试；金额复用权威冻结。
func (s *OpenAIGatewayService) retryPendingLiveSettlement(record *LiveCallRecord) {
	if record == nil || record.settlementIdentityRejected {
		return
	}
	store, err := s.liveStore()
	if err != nil {
		return
	}
	// 规则 A（必须在回读存储**之前**取）：本进程内已经冻结过就沿用那一份。
	//
	// 检查点写失败时（Redis 写不进去），存储里根本没有 Settlement —— 若先回读、
	// 看到 Settlement 为空就返回，那么「检查点与账本同时失败」这个正是要救的场景
	// 永远走不到重试分支，待收费用只能等短 TTL 过期丢掉。所以顺序必须是
	// 「先取内存冻结 → 再回读存储 → 两者都没有才放弃」。
	var frozen *LiveSettlement
	if liveCallHasCompleteSnapshot(record) && liveSettlementNeedsRetry(record.Settlement) {
		frozen = record.Settlement
	}
	readCtx, readCancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	latest, getErr := store.GetLiveCall(readCtx, record.CallHash)
	readCancel()
	if getErr != nil {
		// 回读失败不等于「没有待结算」：这次可能只是一次瞬时抖动。
		//
		// 三处容错点（入口 / 循环头 / 循环尾）必须一致，都不能因为一次读失败
		// 就放弃。入口有界回读：已有冻结时保留主循环的退避预算，无冻结时
		// 使用入口退避；全都失败时，只有内存里还握着冻结金额（检查点写失败
		// 场景）才值得继续，否则无从判断这笔结算是否存在。
		if errors.Is(getErr, ErrLiveCallNotFound) {
			return
		}
		latest = nil
		for entryAttempt := 0; entryAttempt < liveSettlementRetryAttempts; entryAttempt++ {
			// A retained checkpoint still has the main retry budget below.
			if frozen == nil {
				time.Sleep(liveSettlementRetryInterval(entryAttempt))
			}
			retryCtx, retryCancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
			retried, retryErr := store.GetLiveCall(retryCtx, record.CallHash)
			retryCancel()
			if retryErr == nil {
				latest = retried
				break
			}
			if errors.Is(retryErr, ErrLiveCallNotFound) {
				return
			}
			logger.LegacyPrintf("service.openai_live",
				"Live 待结算入口回读失败，退避重试 call_hash=%s attempt=%d err=%v",
				record.CallHash, entryAttempt, retryErr)
		}
		if latest == nil {
			if frozen == nil {
				return
			}
			latest = &LiveCallRecord{CallHash: record.CallHash, Settlement: frozen}
		}
	}
	if latest == nil {
		if frozen == nil {
			return
		}
		latest = &LiveCallRecord{CallHash: record.CallHash, Settlement: frozen}
	}
	if latest.Settlement == nil && frozen != nil {
		if !sameLiveCallBillingSnapshot(record, latest) {
			rejectLiveSettlementCandidate(record)
			return
		}
		latest.Settlement = frozen
	}
	if !liveSettlementNeedsRetry(latest.Settlement) {
		return
	}
	for attempt := 0; attempt < liveSettlementRetryAttempts; attempt++ {
		time.Sleep(liveSettlementRetryInterval(attempt))
		latest, getErr := store.GetLiveCall(context.Background(), record.CallHash)
		if getErr != nil {
			// 一次瞬时读失败不等于「这笔结算不存在」：此刻手上还握着 pending 的冻结
			// 金额，也还有剩余预算。直接 return 会让 observer 永久退出这条重试链，
			// 钱只能等 TTL 过期丢掉。只有「记录明确不存在」才值得放弃。
			if errors.Is(getErr, ErrLiveCallNotFound) {
				return
			}
			logger.LegacyPrintf("service.openai_live",
				"Live 待结算重试回读失败，保留冻结金额与剩余预算 call_hash=%s attempt=%d err=%v",
				record.CallHash, attempt, getErr)
			s.finalizeLiveCallWithRead(record, store, nil, getErr)
			if record.settlementIdentityRejected {
				return
			}
			frozen = record.Settlement
			if frozen != nil && !liveSettlementNeedsRetry(frozen) {
				return
			}
			continue
		}
		if latest == nil {
			// store 返回 (nil, nil) 不是正常契约，但不能因此 panic：
			// 入口对 nil 有守卫，循环里必须同样处理。
			if frozen == nil {
				return
			}
			latest = &LiveCallRecord{CallHash: record.CallHash, Settlement: frozen}
		}
		if latest.Settlement == nil && frozen != nil {
			if !sameLiveCallBillingSnapshot(record, latest) {
				rejectLiveSettlementCandidate(record)
				return
			}
			latest.Settlement = frozen
		}
		if !liveSettlementNeedsRetry(latest.Settlement) {
			return
		}
		s.finalizeLiveCall(latest)
		if latest.Settlement != nil {
			frozen = latest.Settlement
		}
		// finalizeLiveCall 内部会重新读取记录，这里必须回读才能看到最新状态，
		// 否则会话已结算后仍会多睡一整轮退避。
		//
		// 退出判据必须与入口一致：检查点写不进 Redis 时存储里永远没有 Settlement，
		// 若这里按「存储为空就退出」判断，重试预算会从 5 次塌缩成 2 次——而
		// 「检查点写失败」正是这条重试存在的原因。因此存储为空但内存还有冻结时，
		// 继续用内存冻结判断：只有它不再是 pending（已结算）才真正退出。
		settled, getErr := store.GetLiveCall(context.Background(), record.CallHash)
		if getErr != nil {
			if frozen == nil {
				return
			}
			continue
		}
		if settled == nil || settled.Settlement == nil {
			if frozen == nil {
				return
			}
			continue
		}
		if !liveSettlementNeedsRetry(settled.Settlement) {
			return
		}
	}
}

// finalizeLiveCallAfterExpiry 在 observer 无法继续观察时等待会话到期后尝试结算。
// 关闭和金额须由原子 Begin 确认，后续使用 CAS 推进并有限重试；持续故障或
// 进程退出可能导致结算、用量日志或租约释放未完成。
func (s *OpenAIGatewayService) finalizeLiveCallAfterExpiry(record *LiveCallRecord) {
	if record == nil {
		return
	}
	if wait := time.Until(record.ExpiresAt); wait > 0 {
		time.Sleep(wait)
	}
	s.finalizeLiveCall(record)
	// 兜底 finalize 同样可能留下 pending（账本临时故障）：把重试责任交出去。
	s.retryPendingLiveSettlement(record)
}

func (s *OpenAIGatewayService) refreshLiveLease(record *LiveCallRecord) bool {
	cache, err := s.liveConcurrencyCache()
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	defer cancel()
	refreshed, err := cache.RefreshLiveLease(ctx, record.AccountID, record.UserID, record.APIKeyID, record.LeaseID)
	return err == nil && refreshed
}

func (s *OpenAIGatewayService) releaseLiveLease(accountID, userID, apiKeyID int64, leaseID string) {
	cache, err := s.liveConcurrencyCache()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	defer cancel()
	_ = cache.ReleaseLiveLease(ctx, accountID, userID, apiKeyID, leaseID)
}

const (
	liveSettlementModeStandard      = "standard"
	liveSettlementModeSimpleSkip    = "simple_skip"
	liveSettlementModeSimpleKeyOnly = "simple_key_only"
)

func (s *OpenAIGatewayService) liveSettlementMode(record *LiveCallRecord) string {
	apiKey, _ := liveBillingAPIKey(record)
	skip, keyOnly := s.liveSettlementPolicy(apiKey)
	if skip {
		return liveSettlementModeSimpleSkip
	}
	if keyOnly {
		return liveSettlementModeSimpleKeyOnly
	}
	return liveSettlementModeStandard
}

func liveCallHasCompleteSnapshot(record *LiveCallRecord) bool {
	return record != nil && record.CallID != "" && record.CallHash == hashLiveCallID(record.CallID) &&
		record.UserID > 0 && record.AccountID > 0 && record.APIKeyID > 0 && record.LeaseID != "" &&
		record.Model != "" && record.CreatedAt.UnixMilli() > 0 && record.ExpiresAt.UnixMilli() > 0
}

// A local checkpoint may only be reused with the exact immutable billing
// snapshot from which it was derived. Compare timestamps at Redis precision.
func sameLiveCallBillingSnapshot(a, b *LiveCallRecord) bool {
	if !liveCallHasCompleteSnapshot(a) || !liveCallHasCompleteSnapshot(b) {
		return false
	}
	if a.CallID != b.CallID || a.CallHash != b.CallHash || a.UserID != b.UserID || a.AccountID != b.AccountID || a.APIKeyID != b.APIKeyID || a.GroupID != b.GroupID || a.SubscriptionID != b.SubscriptionID || a.LeaseID != b.LeaseID || a.Model != b.Model || a.CreatedAt.UnixMilli() != b.CreatedAt.UnixMilli() || a.ExpiresAt.UnixMilli() != b.ExpiresAt.UnixMilli() || a.UserAgent != b.UserAgent || a.IPAddress != b.IPAddress || a.InboundEndpoint != b.InboundEndpoint {
		return false
	}
	if (a.RealtimePricePerMin == nil) != (b.RealtimePricePerMin == nil) || (a.BillingPolicy == nil) != (b.BillingPolicy == nil) {
		return false
	}
	if a.RealtimePricePerMin != nil && *a.RealtimePricePerMin != *b.RealtimePricePerMin {
		return false
	}
	return a.BillingPolicy == nil || *a.BillingPolicy == *b.BillingPolicy
}

func rejectLiveSettlementCandidate(record *LiveCallRecord) {
	if record == nil {
		return
	}
	record.Settlement = nil
	record.settlementIdentityRejected = true
	logger.FromContext(context.Background()).Error("Live settlement snapshot mismatch; manual review required", zap.String("call_hash", record.CallHash))
}

func (s *OpenAIGatewayService) finalizeLiveCall(record *LiveCallRecord) {
	if record == nil || record.CallHash == "" || record.settlementIdentityRejected {
		return
	}
	store, err := s.liveStore()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	current, getErr := store.GetLiveCall(ctx, record.CallHash)
	cancel()
	s.finalizeLiveCallWithRead(record, store, current, getErr)
}

func (s *OpenAIGatewayService) finalizeLiveCallWithRead(record *LiveCallRecord, store LiveCallStore, current *LiveCallRecord, getErr error) {
	if errors.Is(getErr, ErrLiveCallNotFound) {
		return
	}
	verified := getErr != nil || current == nil
	if verified {
		if !liveCallHasCompleteSnapshot(record) || s.usageLogRepo == nil {
			return
		}
		copy := *record
		current = &copy
	}
	if s.usageLogRepo == nil {
		ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		first, err := store.MarkLiveCallClosed(ctx, current.CallHash, liveClosedRecordTTL)
		cancel()
		if err == nil && first {
			s.releaseLiveLease(current.AccountID, current.UserID, current.APIKeyID, current.LeaseID)
		}
		return
	}
	if current.Settlement != nil {
		state := *current.Settlement
		current.Settlement = &state
	} else if record.Settlement != nil {
		if !sameLiveCallBillingSnapshot(record, current) {
			rejectLiveSettlementCandidate(record)
			return
		}
		state := *record.Settlement
		current.Settlement = &state
	}
	if current.Settlement == nil {
		current.Settlement = s.freezeLiveSettlement(current)
	}
	if current.Settlement.SettlementMode == "" {
		current.Settlement.SettlementMode = s.liveSettlementMode(current)
	}
	record.Settlement = current.Settlement

	begin := func(ctx context.Context) (*LiveSettlement, string, bool, error) {
		if verified {
			return store.BeginLiveSettlementVerified(ctx, current, current.Settlement, liveClosedRecordTTL)
		}
		return store.BeginLiveSettlement(ctx, current.CallHash, current.Settlement, liveClosedRecordTTL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	state, token, _, beginErr := begin(ctx)
	cancel()
	uncertain := beginErr != nil && !errors.Is(beginErr, ErrLiveCallNotFound) && !errors.Is(beginErr, ErrLiveIdentityMismatch)
	if uncertain {
		// The first command may have committed despite a lost reply. Repeating
		// this atomic operation returns the existing winner, not a new amount.
		ctx, cancel = context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		state, token, _, beginErr = begin(ctx)
		cancel()
	}
	if errors.Is(beginErr, ErrLiveIdentityMismatch) {
		rejectLiveSettlementCandidate(record)
		return
	}
	if beginErr != nil || state == nil {
		logger.FromContext(context.Background()).Warn("Live settlement checkpoint unavailable; billing deferred", zap.String("call_hash", current.CallHash), zap.Error(beginErr))
		return
	}
	// The close is confirmed. ZREM is idempotent for this unique lease, so
	// even another process can recover a release after the closer crashed.
	s.releaseLiveLease(current.AccountID, current.UserID, current.APIKeyID, current.LeaseID)
	current.Controller = LiveControllerClosed
	current.ControllerOwner = ""
	current.Settlement = state
	defer func() { record.Settlement = current.Settlement }()

	// Legacy checkpoints have no mode. Establish it with CAS before any
	// billing or log write so competing instances use the same decision.
	if state.SettlementMode == "" {
		next := *state
		next.SettlementMode = s.liveSettlementMode(current)
		var ok bool
		token, ok = s.persistLiveSettlement(current, token, &next)
		if !ok {
			return
		}
		state = current.Settlement
	}
	if state.SettlementMode != liveSettlementModeStandard && state.SettlementMode != liveSettlementModeSimpleSkip && state.SettlementMode != liveSettlementModeSimpleKeyOnly {
		logger.FromContext(context.Background()).Warn("Unknown Live settlement mode", zap.String("call_hash", current.CallHash))
		return
	}
	actualCost := state.ActualCost
	if state.SettlementMode == liveSettlementModeSimpleSkip {
		actualCost = 0
	}
	if state.Status == LiveSettlementSettled {
		if !state.UsageLogWritten && s.writeLiveUsageLog(liveUsageLog(current, state, actualCost)) {
			next := *state
			next.UsageLogWritten = true
			s.persistLiveSettlement(current, token, &next)
		}
		return
	}
	if state.Status != LiveSettlementPending {
		return
	}
	if state.SettlementMode == liveSettlementModeSimpleSkip {
		next := *state
		next.UsageLogWritten = s.writeLiveUsageLog(liveUsageLog(current, state, 0))
		next.Status = LiveSettlementSettled
		s.persistLiveSettlement(current, token, &next)
		return
	}

	apiKey, policy := liveBillingAPIKey(current)
	cost := &CostBreakdown{TotalCost: state.TotalCost, ActualCost: state.ActualCost, BillingMode: state.BillingMode}
	usageLog := liveUsageLog(current, state, state.ActualCost)
	if err := s.settleLiveCallUsage(current, usageLog, cost, apiKey, policy, state.SettlementMode == liveSettlementModeSimpleKeyOnly); err != nil {
		auditToken, auditNeeded := s.recordLiveSettlementFailure(current, token)
		logger.FromContext(context.Background()).Warn("Live 会话扣费失败：保留待结算状态，等待重试",
			zap.Int64("user_id", current.UserID), zap.Int64("api_key_id", current.APIKeyID),
			zap.String("call_hash", current.CallHash), zap.Int("attempts", current.Settlement.Attempts), zap.Error(err))
		if auditNeeded {
			s.tryLiveSettlementPendingAudit(current, auditToken, err)
		}
		return
	}
	next := *state
	next.UsageLogWritten = s.writeLiveUsageLog(usageLog)
	next.Status = LiveSettlementSettled
	s.persistLiveSettlement(current, token, &next)
}

// liveSettlementDurationCapMs 是单次会话可计费的最大时长（毫秒）。
//
// 口径：锚在**会话自己的历史事实**上——ExpiresAt 是会话开始时记下的到期时间
// （= CreatedAt + 当时的会话上限），不会因为后来把配置调小而被追溯性地削掉已发生的
// 用量时长。因此上限取 ExpiresAt - CreatedAt；只有历史遗留记录缺 ExpiresAt 时才回落到
// 配置的最大会话时长（再没有就是配置默认值）。
//
// 用途：冻结检查点写失败后只能重新冻结时（跨进程），时长必须被这个上限封住——故障再久，
// 用户也只为整个会话上限付费。
func (s *OpenAIGatewayService) liveSettlementDurationCapMs(record *LiveCallRecord) int {
	if record == nil {
		return int(s.liveMaxSessionDuration().Milliseconds())
	}
	if d := int(record.ExpiresAt.Sub(record.CreatedAt).Milliseconds()); d > 0 {
		return d
	}
	return int(s.liveMaxSessionDuration().Milliseconds())
}

// freezeLiveSettlement 冻结本次会话的时长与金额（含缺价/零费用情形）。
func (s *OpenAIGatewayService) freezeLiveSettlement(record *LiveCallRecord) *LiveSettlement {
	duration := int(time.Since(record.CreatedAt).Milliseconds())
	if duration < 0 {
		duration = 0
	}
	// 规则 B（跨进程/只能从存储重新冻结）：时长必须封顶到会话自身的上限
	// （ExpiresAt - CreatedAt，历史事实）。用户不该为"会话结束之后的故障时长"付费——
	// 故障再久，计费时长也有界。
	if max := s.liveSettlementDurationCapMs(record); duration > max {
		duration = max
	}
	state := &LiveSettlement{Status: LiveSettlementSettled, DurationMs: duration, FrozenAt: time.Now()}
	cost, err := s.liveSessionCost(record, duration)
	if err != nil || cost == nil {
		logger.FromContext(context.Background()).Warn(
			"Live 会话结算缺少可用价格：按未计费落库",
			zap.Int64("user_id", record.UserID),
			zap.Int64("group_id", record.GroupID),
			zap.Int("duration_ms", duration),
			zap.Error(err),
		)
		return state
	}
	state.TotalCost = cost.TotalCost
	state.ActualCost = cost.ActualCost
	state.BillingMode = cost.BillingMode
	if cost.ActualCost > 0 {
		state.Status = LiveSettlementPending
	}
	return state
}

// recordLiveSettlementFailure increments only after a confirmed CAS. A write
// error is ambiguous and must not be replayed as another failure.
func (s *OpenAIGatewayService) recordLiveSettlementFailure(record *LiveCallRecord, token string) (string, bool) {
	store, err := s.liveStore()
	if err != nil || record == nil || record.Settlement == nil {
		return "", false
	}
	original := *record.Settlement
	current := original
	for attempt := 0; attempt < 8; attempt++ {
		if current.Status != LiveSettlementPending {
			return "", false
		}
		next := current
		next.Attempts++
		ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		swapped, swapErr := store.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &next, liveClosedRecordTTL)
		cancel()
		if swapErr == nil && swapped {
			data, marshalErr := json.Marshal(&next)
			if marshalErr != nil {
				return "", true
			}
			record.Settlement = &next
			return string(data), true
		}
		if swapErr != nil {
			logger.FromContext(context.Background()).Warn("Live failure checkpoint result uncertain", zap.String("call_hash", record.CallHash), zap.Error(swapErr))
		}
		ctx, cancel = context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		latest, latestToken, first, readErr := store.BeginLiveSettlement(ctx, record.CallHash, &current, liveClosedRecordTTL)
		cancel()
		if readErr != nil || latest == nil {
			// Without a readable checkpoint, retain a best-effort SQL alert.
			return "", true
		}
		if first {
			s.releaseLiveLease(record.AccountID, record.UserID, record.APIKeyID, record.LeaseID)
		}
		record.Settlement = latest
		if latest.Status != LiveSettlementPending || latest.TotalCost != original.TotalCost || latest.ActualCost != original.ActualCost || latest.BillingMode != original.BillingMode || latest.SettlementMode != original.SettlementMode || latest.DurationMs != original.DurationMs || !latest.FrozenAt.Equal(original.FrozenAt) {
			return "", false
		}
		if swapErr != nil {
			return latestToken, true
		}
		// A conflict is definite: the attempted increment did not happen.
		current = *latest
		token = latestToken
	}
	return token, true
}

// The claim limits normal concurrent SQL inserts. Redis and SQL do not share a
// transaction, so an uncertain response or crash can still duplicate or lose
// the best-effort alert. A failed insert releases the claim for a later retry.
func (s *OpenAIGatewayService) tryLiveSettlementPendingAudit(record *LiveCallRecord, token string, cause error) {
	if s == nil || s.opsRepo == nil || record == nil || record.Settlement == nil || record.Settlement.Status != LiveSettlementPending {
		return
	}
	store, err := s.liveStore()
	if err != nil || token == "" {
		s.recordLiveSettlementPendingAudit(record, record.Settlement, cause)
		return
	}
	owner := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	claimed, claimErr := store.ClaimLiveSettlementAudit(ctx, record.CallHash, owner, 10*time.Second)
	cancel()
	if claimErr != nil && !errors.Is(claimErr, ErrLiveSettlementInvalid) && !errors.Is(claimErr, ErrLiveCallNotFound) {
		// The same owner may safely retry after a lost claim response.
		ctx, cancel = context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		claimed, claimErr = store.ClaimLiveSettlementAudit(ctx, record.CallHash, owner, 10*time.Second)
		cancel()
	}
	if claimErr != nil {
		logger.FromContext(context.Background()).Warn("Live audit claim unavailable", zap.String("call_hash", record.CallHash), zap.Error(claimErr))
		if !errors.Is(claimErr, ErrLiveSettlementInvalid) && !errors.Is(claimErr, ErrLiveCallNotFound) {
			// Redis did not confirm a claim; preserve an alert without promising deduplication.
			s.recordLiveSettlementPendingAudit(record, record.Settlement, cause)
		}
		return
	}
	if !claimed {
		return
	}
	written := s.recordLiveSettlementPendingAudit(record, record.Settlement, cause)
	ctx, cancel = context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	_, finishErr := store.FinishLiveSettlementAudit(ctx, record.CallHash, owner, written)
	cancel()
	if finishErr != nil {
		logger.FromContext(context.Background()).Warn("Live audit claim completion uncertain", zap.String("call_hash", record.CallHash), zap.Error(finishErr))
	}
}

// persistLiveSettlement advances a confirmed checkpoint without allowing a
// stale retry to replace the frozen amount or regress settled/logged state.
func (s *OpenAIGatewayService) persistLiveSettlement(record *LiveCallRecord, token string, proposed *LiveSettlement) (string, bool) {
	store, err := s.liveStore()
	if err != nil || record == nil || proposed == nil {
		return token, false
	}
	desired := *proposed
	for attempt := 0; attempt < 8; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		swapped, swapErr := store.CompareAndSwapLiveSettlement(ctx, record.CallHash, token, &desired, liveClosedRecordTTL)
		cancel()
		if swapErr == nil && swapped {
			encoded, err := json.Marshal(&desired)
			if err != nil {
				return token, false
			}
			state := desired
			record.Settlement = &state
			return string(encoded), true
		}
		if errors.Is(swapErr, ErrLiveCallNotFound) {
			return token, false
		}
		// A rejected or ambiguous CAS is resolved by reading the authoritative
		// checkpoint through the idempotent atomic begin operation.
		ctx, cancel = context.WithTimeout(context.Background(), liveRedisOperationTimeout)
		current, newToken, first, readErr := store.BeginLiveSettlement(ctx, record.CallHash, &desired, liveClosedRecordTTL)
		cancel()
		if readErr != nil || current == nil {
			logger.FromContext(context.Background()).Warn("Live 结算状态持久化失败", zap.String("call_hash", record.CallHash), zap.Error(readErr))
			return token, false
		}
		if first {
			s.releaseLiveLease(record.AccountID, record.UserID, record.APIKeyID, record.LeaseID)
		}
		record.Settlement = current
		token = newToken
		if current.TotalCost != desired.TotalCost || current.ActualCost != desired.ActualCost ||
			current.BillingMode != desired.BillingMode || current.DurationMs != desired.DurationMs || !current.FrozenAt.Equal(desired.FrozenAt) {
			return token, false
		}
		if current.SettlementMode != "" && desired.SettlementMode != current.SettlementMode {
			return token, true
		}
		if desired.SettlementMode == "" {
			desired.SettlementMode = current.SettlementMode
		}
		if current.Status == LiveSettlementSettled {
			desired.Status = LiveSettlementSettled
		}
		desired.UsageLogWritten = desired.UsageLogWritten || current.UsageLogWritten
		if desired.Attempts < current.Attempts {
			desired.Attempts = current.Attempts
		}
	}
	return token, false
}

// liveBillingAPIKey 用开播时冻结的策略构造最小 APIKey；会话创建时未冻结策略的
// 旧记录按零值处理（不做密钥限额计量），但余额/订阅结算不受影响。
func liveBillingAPIKey(record *LiveCallRecord) (*APIKey, *LiveBillingPolicy) {
	policy := record.BillingPolicy
	if policy == nil {
		policy = &LiveBillingPolicy{}
	}
	apiKey := &APIKey{
		ID:          record.APIKeyID,
		Quota:       policy.APIKeyQuota,
		RateLimit5h: policy.RateLimit5h,
		RateLimit1d: policy.RateLimit1d,
		RateLimit7d: policy.RateLimit7d,
	}
	if record.GroupID > 0 {
		groupID := record.GroupID
		apiKey.GroupID = &groupID
	}
	return apiKey, policy
}

// liveSettlementPolicy 与请求路径的简易模式语义保持一致：
// 简易模式且未开启密钥窗口限额时，不扣余额/订阅，只记录用量。
func (s *OpenAIGatewayService) liveSettlementPolicy(apiKey *APIKey) (skip bool, simpleModeKeyRateLimitOnly bool) {
	if s == nil || s.cfg == nil || s.cfg.RunMode != config.RunModeSimple {
		return false, false
	}
	if simpleModeKeyRateLimitBillingEnabled(s.cfg, apiKey) {
		return false, true
	}
	return true, false
}

// writeLiveUsageLog 写入 Live 会话的用量行，返回是否已确认落库。
//
// 与通用 writeUsageLogBestEffort 的差别：这里需要"金额是否真的持久化"的结论——
// 只有确认落库，调用方才能把 UsageLogWritten 置真（置早了就再也不会补写，出现
// "余额已扣、用量行金额为 0 或缺失"的账实不符）。
//
// 写入方式沿用共享助手（best-effort 入队 + 失败后同步兜底，保留"已扣费就必须有
// usage_log"的既有契约），只多返回一个"是否已确认落库"的结论。
func (s *OpenAIGatewayService) writeLiveUsageLog(usageLog *UsageLog) bool {
	if s == nil || s.usageLogRepo == nil || usageLog == nil {
		return false
	}
	return writeUsageLogBestEffort(context.Background(), s.usageLogRepo, usageLog, "service.openai_live")
}

// liveSettlementPendingPublicMessage 是「结算未完成」对用户可见的唯一说明。
//
// ops_error_logs **不是管理员专用表**：OpsService.ListUserErrorRequests 按当前用户
// 查同一张表，ToUserErrorRequest 把 error_message 原样下发给前端（500 也正好通过
// status >= 400 的守卫）。账本错误的原文常带内网地址、表名与 SQL 片段，写进
// error_message 就等于把内部拓扑展示给普通用户。
//
// 因此 error_message 只能是这条**固定的公开说明**：不含内部细节、不随错误变化，
// 前端按稳定的错误码（error_phase=internal → code=internal_error）判定，不解析文案。
//
// 文案**不能承诺「会自动重试」**：重试只在 observer 还活着且预算未耗尽时进行
// （liveSettlementRetryAttempts=4，退避合计约 7.5 分钟），预算耗尽后没有任何后台
// sweeper 接手，这笔结算会停在 pending 直到记录 TTL 过期。承诺自动重试会把
// 「泄露内部细节」换成「给用户一个不成立的承诺」。
const liveSettlementPendingPublicMessage = "Settlement is temporarily unavailable. Please contact support if this persists."

// recordLiveSettlementPendingAudit 记录一次「结算未完成」的审计事件。
//
// 它刻意**不写 usage_logs**：那张表是请求数、目录费用、账号成本与会话时长的单一
// 事实源，汇总 SQL 直接按 total_cost / duration_ms 求和，多一条行就会把一次会话
// 的费用与时长重复计入。失败诊断属于排障信息，放 ops 错误日志更合适，也不参与
// 任何用量汇总。
//
// 落库口径必须与 OpsService.RecordError 对齐，否则这条记录「写了但看不见」：
//   - CreatedAt 必须显式填（InsertErrorLog 不像批量接口那样兜底，零值会落成
//     0001-01-01，时间窗查询查不到、retention 下一轮就删掉）；
//   - StatusCode 必须 >= 400：管理端错误列表的守卫是
//     (COALESCE(status_code,0) >= 400 OR error_type='cyber_policy')，NULL 会被挡掉；
//   - ErrorPhase 必须在管理端枚举内（request/auth/routing/upstream/network/internal），
//     所以用 internal，具体原因放 ErrorType；
//   - Severity 用表内既有的 P0-P3 分类，warning 不在其中。
//
// best-effort：ops 未装配或写入失败都只记日志，绝不影响结算正确性；插入带超时，
// 避免数据库卡住时把 observer goroutine 一起挂住。
func (s *OpenAIGatewayService) recordLiveSettlementPendingAudit(record *LiveCallRecord, state *LiveSettlement, cause error) bool {
	if s == nil || s.opsRepo == nil || record == nil {
		return false
	}
	attempts := 0
	if state != nil {
		attempts = state.Attempts
	}
	requestType := int16(RequestTypeLive)
	userID, apiKeyID, accountID := record.UserID, record.APIKeyID, record.AccountID
	statusCode := http.StatusInternalServerError
	ctx, cancel := context.WithTimeout(context.Background(), liveRedisOperationTimeout)
	defer cancel()
	// The claim, rather than the attempt count, controls duplicate SQL writes.
	if attempts < 1 {
		attempts = 1
	}
	// 管理员侧摘要与完整诊断：尝试次数是排障的第一分诊信息，cause 原文
	// （内网地址 / 表名 / SQL 片段）是第二层，两者都只进管理员字段。
	adminSummary := fmt.Sprintf("live settlement pending after %d attempts", attempts)
	adminDetail := ""
	if cause != nil {
		adminDetail = cause.Error()
	}
	input := &OpsInsertErrorLogInput{
		RequestID:   record.CallHash,
		UserID:      &userID,
		APIKeyID:    &apiKeyID,
		AccountID:   &accountID,
		Platform:    PlatformOpenAI,
		Model:       record.Model,
		RequestType: &requestType,
		CreatedAt:   time.Now(),
		ErrorPhase:  "internal",
		ErrorType:   "live_settlement_pending",
		Severity:    "P2",
		StatusCode:  statusCode,
		// 用户可见：固定公开说明。绝不写 cause。
		ErrorMessage: liveSettlementPendingPublicMessage,
		// 管理员可见：完整诊断。用户投影不含这两个字段。
		UpstreamErrorMessage: &adminSummary,
		// 这是业务侧暂时不可用（内部账本故障），不是网关自身故障。
		// 置 true 让运营看板的 SLA 判据（status>=400 AND NOT is_business_limited）
		// 不把它算成服务端错误——否则一次内部账本故障会按重试次数拉低 SLA。
		IsBusinessLimited: true,
	}
	if adminDetail != "" {
		input.UpstreamErrorDetail = &adminDetail
	}
	// 结构化日志：管理员侧的第二个排障入口（审计写入失败时同样保留）。
	logger.FromContext(context.Background()).Warn(
		"Live 会话结算未完成：完整诊断只落管理员字段",
		zap.Int64("user_id", record.UserID),
		zap.Int64("api_key_id", record.APIKeyID),
		zap.String("call_hash", record.CallHash),
		zap.Int("attempts", attempts),
		zap.Error(cause),
	)
	_, err := s.opsRepo.InsertErrorLog(ctx, input)
	if err != nil {
		logger.LegacyPrintf("service.openai_live",
			"live settlement audit unavailable call_hash=%s attempts=%d err=%v", record.CallHash, attempts, err)
		return false
	}
	return true
}

func liveUsageLog(record *LiveCallRecord, state *LiveSettlement, actualCost float64) *UsageLog {
	billingType := int8(BillingTypeBalance)
	if record.SubscriptionID > 0 {
		billingType = BillingTypeSubscription
	}
	duration := state.DurationMs
	userAgent := record.UserAgent
	ipAddress := record.IPAddress
	inboundEndpoint := record.InboundEndpoint
	upstreamEndpoint := "/backend-api/codex/realtime/calls"
	log := &UsageLog{
		UserID:           record.UserID,
		APIKeyID:         record.APIKeyID,
		AccountID:        record.AccountID,
		RequestID:        record.CallHash,
		Model:            record.Model,
		RequestedModel:   record.Model,
		GroupID:          liveOptionalID(record.GroupID),
		SubscriptionID:   liveOptionalID(record.SubscriptionID),
		RateMultiplier:   1,
		BillingType:      billingType,
		RequestType:      RequestTypeLive,
		TotalCost:        state.TotalCost,
		ActualCost:       actualCost,
		DurationMs:       &duration,
		UserAgent:        &userAgent,
		IPAddress:        &ipAddress,
		InboundEndpoint:  &inboundEndpoint,
		UpstreamEndpoint: &upstreamEndpoint,
		CreatedAt:        record.CreatedAt,
	}
	if state.BillingMode != "" {
		billingMode := state.BillingMode
		log.BillingMode = &billingMode
	}
	return log
}

// settleLiveCallUsage 把冻结费用写入统一计费账本。request_id 使用 CallHash，
// 与 usage_logs 唯一键一致，重复结算由账本去重，不会重复扣费。
func (s *OpenAIGatewayService) settleLiveCallUsage(record *LiveCallRecord, usageLog *UsageLog, cost *CostBreakdown, apiKey *APIKey, policy *LiveBillingPolicy, simpleModeKeyRateLimitOnly bool) error {
	if s == nil || record == nil || usageLog == nil || cost == nil || cost.ActualCost <= 0 {
		return nil
	}
	if s.usageBillingRepo == nil {
		return nil
	}
	isSubscription := record.SubscriptionID > 0 && !simpleModeKeyRateLimitOnly
	var subscription *UserSubscription
	if isSubscription {
		subscription = &UserSubscription{ID: record.SubscriptionID}
	}
	platform := ""
	if policy != nil {
		platform = policy.GroupPlatform
	}
	_, err := applyUsageBilling(context.Background(), record.CallHash, usageLog, &postUsageBillingParams{
		Cost:                       cost,
		User:                       &User{ID: record.UserID},
		APIKey:                     apiKey,
		Account:                    &Account{ID: record.AccountID},
		Subscription:               subscription,
		IsSubscriptionBill:         isSubscription,
		APIKeyService:              s.apiKeyService,
		SimpleModeKeyRateLimitOnly: simpleModeKeyRateLimitOnly,
		Platform:                   platform,
	}, s.billingDeps(), s.usageBillingRepo)
	return err
}

// liveSessionCost 用开播时冻结的单价结算本次 Live 会话。
// 返回错误表示该分组未配置单价：调用方必须告警且不得伪造零成本账单。
func (s *OpenAIGatewayService) liveSessionCost(record *LiveCallRecord, durationMs int) (*CostBreakdown, error) {
	if s.billingService == nil {
		return nil, ErrLivePricingUnavailable
	}
	return s.billingService.CalculateLiveSessionCost(durationMs, record.RealtimePricePerMin, 1)
}
