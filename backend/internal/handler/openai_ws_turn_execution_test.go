//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIWSTurnExecutionQueuedSnapshot(t *testing.T) {
	f, _ := newOpenAIResponsesChainFixture(t)
	ctx, _, err := f.handler.gatewayService.RefreezeRequestBilling(context.Background(), f.key, service.InflightEstimateRequest{Model: "client-model", Kind: service.InflightEstimateToken})
	require.NoError(t, err)
	budget := service.NewRequestAttemptBudget(time.Now(), 16, 600*time.Second, time.Time{})
	ctx = service.WithRequestAttemptBudget(ctx, budget)
	require.NoError(t, budget.AllowSend())
	require.NoError(t, budget.AllowSend())
	at := time.Now()
	sub := &service.UserSubscription{ID: 101, GroupID: 71}
	e := newOpenAIWSTurnExecution(ctx, f.key, sub, f.accounts[0], at)
	var slots openAIWSTurnExecutions
	slots.set(1, e)
	input := slots.get(1, openAIWSTurnExecution{}).snapshotUsage(&service.OpenAIForwardResult{})
	pool := newUsageRecordTestPool(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	result := make(chan *service.OpenAIRecordUsageInput, 1)
	pool.Submit(func(context.Context) { result <- input })
	require.NoError(t, budget.AllowSend())
	*f.key.GroupID = 999
	f.key.User.ID = 999
	f.accounts[0].ID = 999
	sub.ID = 999
	slots.set(2, newOpenAIWSTurnExecution(context.Background(), f.key, sub, f.accounts[0], at.Add(time.Hour)))
	close(release)
	pool.Stop()
	got := <-result
	require.Equal(t, 2, got.RoutingUsage.AttemptCount)
	require.Equal(t, int64(71), got.RoutingUsage.ServedGroupID)
	require.Equal(t, int64(71), *got.APIKey.GroupID)
	require.NotEqual(t, int64(999), got.Account.ID)
	require.Equal(t, int64(101), got.Subscription.ID)
	require.Equal(t, at, got.PricingAt)
	require.Equal(t, at, slots.get(1, openAIWSTurnExecution{}).pricingAt)
}
func TestOpenAIWSTurnExecutionQuotaSnapshot(t *testing.T) {
	for _, forced := range []bool{false, true} {
		t.Run(fmt.Sprintf("forced_%t", forced), func(t *testing.T) {
			key := &service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI}}
			ctx := context.Background()
			want := service.PlatformOpenAI
			if forced {
				ctx = context.WithValue(ctx, ctxkey.ForcePlatform, service.PlatformAntigravity)
				want = service.PlatformAntigravity
			}
			var turns openAIWSTurnExecutions
			turns.set(1, newOpenAIWSTurnExecution(ctx, key, nil, nil, time.Now()))
			key.Group.Platform = service.PlatformAnthropic
			next := context.WithValue(context.Background(), ctxkey.ForcePlatform, service.PlatformGemini)
			turns.set(2, newOpenAIWSTurnExecution(next, key, nil, nil, time.Now()))
			first := turns.get(1, openAIWSTurnExecution{}).snapshotUsage(nil)
			second := turns.get(2, openAIWSTurnExecution{}).snapshotUsage(nil)
			pool := newUsageRecordTestPool(t)
			release := make(chan struct{})
			entered := make(chan struct{})
			pool.Submit(func(context.Context) { close(entered); <-release })
			<-entered
			got := make(chan string, 2)
			pool.Submit(func(context.Context) { got <- first.QuotaPlatform; got <- second.QuotaPlatform })
			key.Group.Platform = service.PlatformComposite
			ctx = next
			turns.complete(1)
			turns.complete(2)
			close(release)
			pool.Stop()
			require.Equal(t, want, <-got)
			require.Equal(t, service.PlatformGemini, <-got)
		})
	}
	require.Empty(t, newOpenAIWSTurnExecution(nil, nil, nil, nil, time.Time{}).snapshotUsage(nil).QuotaPlatform)
	key := &service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI}}
	require.Equal(t, service.PlatformOpenAI, newOpenAIWSTurnExecution(nil, key, nil, nil, time.Time{}).snapshotUsage(nil).QuotaPlatform)
	key.Group.Platform = service.PlatformComposite
	require.Empty(t, newOpenAIWSTurnExecution(nil, key, nil, nil, time.Time{}).snapshotUsage(nil).QuotaPlatform)
}
func TestOpenAIWSTurnExecutionOffset(t *testing.T) {
	var seen []int
	budget := service.NewRequestAttemptBudget(time.Now(), 16, 600*time.Second, time.Time{})
	ctx := service.WithRequestAttemptBudget(context.Background(), budget)
	hooks := &service.OpenAIWSIngressHooks{LogicalTurnOffset: 4, BeforeTurn: func(turn int) error { seen = append(seen, turn); return nil }, TurnContext: func(turn int) context.Context { seen = append(seen, turn); return ctx }, AfterTurn: func(turn int, _ *service.OpenAIForwardResult, _ error) { seen = append(seen, turn) }}
	mapped := hooks.WithLogicalTurnOffset()
	require.NoError(t, mapped.BeforeTurn(1))
	require.Same(t, budget, service.RequestAttemptBudgetFromContext(mapped.TurnContext(1)))
	mapped.AfterTurn(1, nil, nil)
	require.Equal(t, []int{5, 5, 5}, seen)
	require.Same(t, mapped, mapped.WithLogicalTurnOffset())
	require.Equal(t, 4, hooks.LogicalTurnOffset)
	empty := &service.OpenAIWSIngressHooks{}
	require.Same(t, empty, empty.WithLogicalTurnOffset())
}
func runWSTurnRetryQueuedCase(t *testing.T, tc openAIResponsesWSUsageLogCase) openAIResponsesWSUsageLogResult {
	t.Helper()
	gin.SetMode(gin.TestMode)

	turnCount := 1
	if strings.TrimSpace(tc.midPayload) != "" {
		turnCount++
	}
	if strings.TrimSpace(tc.secondPayload) != "" {
		turnCount++
	}
	upstreamPayloadCh := make(chan []byte, turnCount+1)
	upstreamErrCh := make(chan error, 1)
	var channelSvc *service.ChannelService
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tc.accountPlatform == service.PlatformGrok {
			payload, err := io.ReadAll(r.Body)
			if err != nil {
				upstreamErrCh <- err
				return
			}
			upstreamPayloadCh <- payload
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_grok_test\",\"model\":%q,\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n", gjson.GetBytes(payload, "model").String())
			return
		}
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{
			CompressionMode: coderws.CompressionContextTakeover,
		})
		if err != nil {
			upstreamErrCh <- err
			return
		}
		defer func() {
			_ = conn.CloseNow()
		}()

		for turn := 1; turn <= turnCount; turn++ {
			readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
			msgType, payload, readErr := conn.Read(readCtx)
			cancelRead()
			if readErr != nil {
				upstreamErrCh <- readErr
				return
			}
			if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
				upstreamErrCh <- errors.New("unexpected upstream websocket message type")
				return
			}
			upstreamPayloadCh <- payload
			if turn == 1 {
				if e := conn.Write(r.Context(), coderws.MessageText, []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"unknown_parameter","param":"max_output_tokens","message":"Unknown parameter: max_output_tokens"}}`)); e != nil {
					upstreamErrCh <- e
					return
				}
				_, retry, e := conn.Read(r.Context())
				if e != nil {
					upstreamErrCh <- e
					return
				}
				upstreamPayloadCh <- retry
			}
			if turn == 1 && tc.afterFirstUpstreamRequest != nil {
				if callbackErr := tc.afterFirstUpstreamRequest(channelSvc); callbackErr != nil {
					upstreamErrCh <- callbackErr
					return
				}
			}

			response := fmt.Sprintf(
				`{"type":"response.completed","response":{"id":"resp_usage_e2e_%d","model":%q,"usage":{"input_tokens":2,"output_tokens":1}}}`,
				turn,
				gjson.GetBytes(payload, "model").String(),
			)
			writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
			writeErr := conn.Write(writeCtx, coderws.MessageText, []byte(response))
			cancelWrite()
			if writeErr != nil {
				upstreamErrCh <- writeErr
				return
			}
		}
		upstreamErrCh <- nil
	}))
	defer upstreamServer.Close()

	groupID := int64(4201)
	account := service.Account{
		ID:          9901,
		Name:        "openai-ws-passthrough-usage-e2e",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":       "sk-test",
			"base_url":      upstreamServer.URL,
			"model_mapping": tc.accountModelMapping,
		},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
		},
	}
	if tc.accountPlatform != "" {
		account.Platform = tc.accountPlatform
	}
	account.RateMultiplier = tc.accountRateMultiplier
	if strings.TrimSpace(tc.ingressMode) != "" {
		account.Extra["openai_apikey_responses_websockets_v2_mode"] = tc.ingressMode
	}
	// 用例可显式模拟"WS passthrough ingress + 账号未标 passthrough + 非空
	// model_mapping"的生产风险组合；默认仍按该组合的真实默认值（账号不额外标记），
	// 冻结口径由 RequireWSIngressModel 与 ingress 谓词对齐，不依赖账号标记。
	if tc.accountPassthroughFlag {
		account.Extra["openai_passthrough"] = true
	}

	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, turnCount)}

	if len(tc.channelMapping) > 0 || len(tc.channelModelPricing) > 0 {
		channelSvc = service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
			channels: []service.Channel{{
				ID:                 7701,
				Name:               "openai-ws-e2e-channel",
				Status:             service.StatusActive,
				GroupIDs:           []int64{groupID},
				ModelMapping:       map[string]map[string]string{service.PlatformOpenAI: tc.channelMapping},
				BillingModelSource: tc.billingModelSource,
				ModelPricing:       tc.channelModelPricing,
			}},
			groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
		}, nil, nil, nil, nil)
	}

	var keyRepo service.APIKeyRepository
	if tc.simpleModeRejectAtRead > 0 {
		cfg.SimpleModeKeyRateLimitEnabled = true
		keyRepo = &simpleModeWSRateLimitRepo{rejectAt: tc.simpleModeRejectAtRead}
	}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, keyRepo, nil, nil, cfg, nil)
	t.Cleanup(billingCacheSvc.Stop)
	// C1 锁价：冻结表按调度快照解析账号→上游模型，缺快照会让账号绑定失败；
	// 定价解析器与转发共用同一渠道/计费服务，冻结模型才与实际出站同源。
	billingSvc := service.NewBillingService(cfg, nil)
	schedulerSvc := service.NewSchedulerSnapshotService(
		&fakeSchedulerCache{accounts: []*service.Account{&account}},
		nil,
		nil,
		nil,
		cfg,
	)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo,
		usageRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		schedulerSvc,
		nil,
		billingSvc,
		nil,
		billingCacheSvc,
		&compositeWSHTTPUpstream{},
		&service.DeferredService{},
		nil,
		nil,
		service.NewModelPricingResolver(channelSvc, billingSvc),
		channelSvc,
		nil,
		nil,
		nil, // userPlatformQuotaRepo
	)

	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	}
	h := &OpenAIGatewayHandler{
		cfg:                 cfg,
		compositeResolver:   tc.compositeResolver,
		gatewayService:      gatewaySvc,
		billingCacheService: billingCacheSvc,
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
	}

	pool := newUsageRecordTestPool(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	pool.Submit(func(context.Context) { close(entered); <-release })
	<-entered
	h.usageRecordWorkerPool = pool
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	apiKey := &service.APIKey{
		ID:      1801,
		GroupID: &groupID,
		User:    &service.User{ID: 1701, Status: service.StatusActive},
	}
	// 真实入口的分组由认证中间件挂载；锁价冻结表以 apiKey.Group 为候选分组
	// 快照来源，缺分组快照会在准入阶段 fail-closed。这里补一个最小 OpenAI
	// 分组（无渠道/分组定价 → 走内置兜底价），与生产认证后的形态一致。
	apiKey.Group = &service.Group{
		ID:             groupID,
		Platform:       service.PlatformOpenAI,
		Status:         service.StatusActive,
		RateMultiplier: 1,
		// 测试 fixture 定价（非产品价）：gpt-5.1 不在内置可识别价表内，锁价准入
		// 会在上游前拒绝。仅补齐该用例族的定价，倍率/金额断言语义不变。
		ModelPricing: []service.ChannelModelPricing{
			wsFixtureTokenPricing([]string{"gpt-5.1"}, 1e-6, 2e-6),
		},
	}
	if tc.apiKeyService != nil {
		h.apiKeyService = tc.apiKeyService
		authKey, err := tc.apiKeyService.GetByKey(context.Background(), tc.apiKeyCredential)
		require.NoError(t, err)
		apiKey = authKey
	}
	if tc.simpleModeRejectAtRead > 0 {
		apiKey.RateLimit5h = 1
	}
	if tc.group != nil {
		apiKey.Group = tc.group
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		if tc.apiKeyService != nil && apiKey.Group != nil {
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, apiKey.Group))
		}
		c.Next()
	})
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()

	headers := http.Header{}
	if tc.userAgent != nil {
		headers.Set("User-Agent", *tc.userAgent)
	}
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(
		dialCtx,
		"ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses",
		&coderws.DialOptions{HTTPHeader: headers, CompressionMode: coderws.CompressionContextTakeover},
	)
	cancelDial()
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(tc.firstPayload))
	cancelWrite()
	require.NoError(t, err)

	if tc.closeReason == "" {
		tc.closeReason = "not available for this group"
	}
	if tc.closeStatus == 0 {
		tc.closeStatus = coderws.StatusPolicyViolation
	}
	if tc.firstFrameCloseExpected {
		readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
		_, _, readErr := clientConn.Read(readCtx)
		cancelRead()
		require.Error(t, readErr, "first frame should have been rejected with a close")
		var closeErr coderws.CloseError
		require.ErrorAs(t, readErr, &closeErr)
		status := tc.closeStatus
		if status == 0 {
			status = coderws.StatusPolicyViolation
		}
		require.Equal(t, status, closeErr.Code)
		reason := tc.closeReason
		if reason == "" {
			reason = "not available for this group"
		}
		require.Contains(t, closeErr.Reason, reason)
		require.Empty(t, upstreamPayloadCh, "rejected first frame must not reach upstream")
		_ = clientConn.CloseNow()
		return openAIResponsesWSUsageLogResult{}
	}

	clientEvents := make([][]byte, 0, turnCount)
	readCompleted := func() {
		readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
		_, event, readErr := clientConn.Read(readCtx)
		cancelRead()
		require.NoError(t, readErr)
		require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
		clientEvents = append(clientEvents, append([]byte(nil), event...))
	}
	readCompleted()
	if tc.midPayload != "" {
		writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
		err = clientConn.Write(writeCtx, coderws.MessageText, []byte(tc.midPayload))
		cancelWrite()
		require.NoError(t, err)
		readCompleted()
	}
	if strings.TrimSpace(tc.secondPayload) != "" && (turnCount >= 2) {
		writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
		err = clientConn.Write(writeCtx, coderws.MessageText, []byte(tc.secondPayload))
		cancelWrite()
		require.NoError(t, err)
		if tc.secondTurnCloseExpected {
			readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
			_, _, readErr := clientConn.Read(readCtx)
			cancelRead()
			require.Error(t, readErr, "second turn should have been rejected with a close")
			var closeErr coderws.CloseError
			require.ErrorAs(t, readErr, &closeErr)
			status := tc.closeStatus
			if status == 0 {
				status = coderws.StatusPolicyViolation
			}
			require.Equal(t, status, closeErr.Code)
			reason := tc.closeReason
			if reason == "" {
				reason = "not available for this group"
			}
			require.Contains(t, closeErr.Reason, reason)
			require.Len(t, upstreamPayloadCh, turnCount-1, "rejected turn must not reach upstream")
			_ = clientConn.CloseNow()
			return openAIResponsesWSUsageLogResult{}
		}
		readCompleted()
	}
	_ = clientConn.Close(coderws.StatusNormalClosure, "done")

	require.Empty(t, usageRepo.created, "both real WS turns wait behind queued worker")
	apiKey.User.ID = 999
	apiKey.Group.RateMultiplier = 99
	close(release)
	released = true
	pool.Stop()
	usageLogs := make([]*service.UsageLog, 0, turnCount)
	for len(usageLogs) < turnCount {
		select {
		case usageLog := <-usageRepo.created:
			require.NotNil(t, usageLog)
			usageLogs = append(usageLogs, usageLog)
		case <-time.After(3 * time.Second):
			t.Fatal("等待 WebSocket usage log 写入超时")
		}
	}

	upstreamPayloads := make([][]byte, 0, turnCount)
	for len(upstreamPayloads) < turnCount+1 {
		select {
		case payload := <-upstreamPayloadCh:
			upstreamPayloads = append(upstreamPayloads, payload)
		case <-time.After(3 * time.Second):
			t.Fatal("等待上游 WebSocket 请求帧超时")
		}
	}

	if tc.accountPlatform == service.PlatformGrok {
		upstreamErrCh <- nil
	}
	select {
	case upstreamErr := <-upstreamErrCh:
		require.NoError(t, upstreamErr)
	case <-time.After(3 * time.Second):
		t.Fatal("等待上游 WebSocket 结束超时")
	}

	return openAIResponsesWSUsageLogResult{
		log:                  usageLogs[0],
		logs:                 usageLogs,
		upstreamFirstPayload: upstreamPayloads[0],
		upstreamPayloads:     upstreamPayloads,
		clientEvents:         clientEvents,
	}
}
func TestOpenAIWSTurnExecutionHiddenQueued(t *testing.T) {
	repo := newWSGroupPricingAPIKeyRepoStub(3)
	keys := newWSGroupPricingAPIKeyService(repo)
	got := runWSTurnRetryQueuedCase(t, openAIResponsesWSUsageLogCase{firstPayload: `{"type":"response.create","model":"gpt-5.6-sol","max_output_tokens":64}`, secondPayload: wsGroupPricingSecond, ingressMode: service.OpenAIWSIngressModeCtxPool, apiKeyService: keys, apiKeyCredential: wsGroupPricingKey, afterFirstUpstreamRequest: func(*service.ChannelService) error {
		return repo.adminUpdate(keys, func(_ *service.APIKey, g *service.Group) { g.RateMultiplier = 0.3 })
	}})
	require.Len(t, got.upstreamPayloads, 3)
	require.Len(t, got.logs, 2)
	assertResponsesRouting(t, got.logs[0], 4201, 4201, service.RoutingFailoverNone, 2)
	assertResponsesRouting(t, got.logs[1], 4201, 4201, service.RoutingFailoverNone, 1)
	require.NotEqual(t, got.logs[0].RequestID, got.logs[1].RequestID)
	require.InDelta(t, 3, got.logs[0].RateMultiplier, 1e-12)
	require.InDelta(t, 0.3, got.logs[1].RateMultiplier, 1e-12)
}
func TestOpenAIWSTurnExecutionAccountFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	firstHitCh := make(chan []byte, 1)
	secondHitCh := make(chan []byte, 1)

	firstUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, payload, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr == nil {
			firstHitCh <- payload
		}

		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		_ = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"error","error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"The usage limit has been reached"}}`))
		cancelWrite()
	}))
	defer firstUpstream.Close()

	secondUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, payload, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr == nil {
			secondHitCh <- payload
		}

		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		_ = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_ws_failover_ok","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		cancelWrite()
		_ = conn.Close(coderws.StatusNormalClosure, "done")
	}))
	defer secondUpstream.Close()

	groupID := int64(4202)
	accounts := []service.Account{
		{
			ID:          9902,
			Name:        "openai-ws-rate-limited",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			Credentials: map[string]any{
				"api_key":  "sk-first",
				"base_url": firstUpstream.URL,
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
				"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
			},
		},
		{
			ID:          9903,
			Name:        "openai-ws-healthy",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    2,
			Credentials: map[string]any{
				"api_key":  "sk-second",
				"base_url": secondUpstream.URL,
			},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
				"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
			},
		},
	}

	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.MaxAccountSwitches = 3

	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	rateLimitSvc := service.NewRateLimitService(accountRepo, nil, cfg, nil, nil)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	// 锁价冻结表按调度快照解析账号→上游模型，缺快照会在上游前拒绝（与 WS usage
	// harness 同口径）。
	schedulerSvc := service.NewSchedulerSnapshotService(
		&fakeSchedulerCache{accounts: []*service.Account{&accounts[0], &accounts[1]}}, nil, nil, nil, cfg,
	)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
	t.Cleanup(billingCacheSvc.Stop)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo,
		usageRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		schedulerSvc,
		nil,
		service.NewBillingService(cfg, nil),
		rateLimitSvc,
		billingCacheSvc,
		nil,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	cache := &concurrencyCacheMock{
		acquireUserSlotFn: func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
			return true, nil
		},
	}
	h := &OpenAIGatewayHandler{
		gatewayService:      gatewaySvc,
		billingCacheService: billingCacheSvc,
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
		maxAccountSwitches:  3,
	}

	apiKey := &service.APIKey{
		ID:      1802,
		GroupID: &groupID,
		User:    &service.User{ID: 1702, Status: service.StatusActive},
		// 测试 fixture 定价（非产品价）：gpt-5.1 无内置可识别价，锁价准入会拒绝。
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive,
			ModelPricing: []service.ChannelModelPricing{wsFixtureTokenPricing([]string{"gpt-5.1"}, 1e-6, 2e-6)}},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(
		dialCtx,
		"ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses",
		&coderws.DialOptions{CompressionMode: coderws.CompressionContextTakeover},
	)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 5*time.Second)
	_, event, err := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	require.Equal(t, "resp_ws_failover_ok", gjson.GetBytes(event, "response.id").String())

	select {
	case <-firstHitCh:
	case <-time.After(3 * time.Second):
		t.Fatal("等待第一个上游收到首帧超时")
	}
	select {
	case <-secondHitCh:
	case <-time.After(3 * time.Second):
		t.Fatal("等待第二个上游收到重放首帧超时")
	}
	require.Equal(t, []int64{int64(9902)}, accountRepo.rateLimitedIDs)
	select {
	case row := <-usageRepo.created:
		assertResponsesRouting(t, row, 4202, 4202, service.RoutingFailoverAccount, 2)
	case <-time.After(3 * time.Second):
		t.Fatal("missing failover usage")
	}
}
func TestOpenAIWSTurnExecutionC6(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			repo := newWSGroupPricingAPIKeyRepoStub(3)
			keys := newWSGroupPricingAPIKeyService(repo)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode, apiKeyService: keys, apiKeyCredential: wsGroupPricingKey, afterFirstUpstreamRequest: func(*service.ChannelService) error {
				return repo.adminUpdate(keys, func(_ *service.APIKey, g *service.Group) { g.RateMultiplier = 0.3 })
			}})
			require.Len(t, got.logs, 2)
			require.NotEqual(t, got.logs[0].RequestID, got.logs[1].RequestID)
			for _, row := range got.logs {
				assertResponsesRouting(t, row, wsGroupPricingGroupID, wsGroupPricingGroupID, service.RoutingFailoverNone, 1)
			}
			require.InDelta(t, 3, got.logs[0].RateMultiplier, 1e-12)
			require.InDelta(t, 0.3, got.logs[1].RateMultiplier, 1e-12)
		})
	}
}
