//go:build unit

package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIWSTurnResumeThreeTurns(t *testing.T) { runWSTurnResume(t, "success") }
func TestOpenAIWSTurnResumeDenied(t *testing.T) {
	for _, mode := range []string{"previous", "visible", "uncertain", "passthrough", "deadline"} {
		t.Run(mode, func(t *testing.T) { runWSTurnResume(t, mode) })
	}
}
func runWSTurnResume(t *testing.T, mode string) {
	gin.SetMode(gin.TestMode)

	var mutatePrice func()
	firstHitCh := make(chan []byte, 2)
	secondHitCh := make(chan []byte, 2)

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
		_ = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_turn1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		cancelWrite()
		_, payload, readErr = conn.Read(r.Context())
		if readErr == nil {
			firstHitCh <- payload
		}
		if mutatePrice != nil {
			mutatePrice()
		}
		if mode == "deadline" {
			timer := time.NewTimer(120 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
		}
		if mode == "uncertain" {
			return
		}
		if mode == "visible" {
			_ = conn.Write(r.Context(), coderws.MessageText, []byte(`{"type":"response.output_text.delta","delta":"visible"}`))
		}
		writeCtx, cancelWrite = context.WithTimeout(r.Context(), 3*time.Second)
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
		_, third, e := conn.Read(r.Context())
		if e == nil {
			secondHitCh <- third
			_ = conn.Write(r.Context(), coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_turn3","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		}
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
				"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModeCtxPool,
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
				"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModeCtxPool,
			},
		},
	}

	if mode == "passthrough" {
		for i := range accounts {
			accounts[i].Extra["openai_apikey_responses_websockets_v2_mode"] = service.OpenAIWSIngressModePassthrough
		}
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
	if mode == "deadline" {
		cfg.Gateway.OpenAIWS.RetryTotalBudgetMS = 60
	}

	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	rateLimitSvc := service.NewRateLimitService(accountRepo, nil, cfg, nil, nil)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	// 锁价冻结表按调度快照解析账号→上游模型，缺快照会在上游前拒绝（与 WS usage
	// harness 同口径）。
	schedulerSvc := service.NewSchedulerSnapshotService(
		&fakeSchedulerCache{accounts: []*service.Account{&accounts[0], &accounts[1]}}, nil, nil, nil, cfg,
	)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 3)}
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
		cfg:                 cfg,
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
	mutatePrice = func() {
		*apiKey.Group.ModelPricing[0].InputPrice = 10e-6
		*apiKey.Group.ModelPricing[0].OutputPrice = 20e-6
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
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"first","stream":false}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 5*time.Second)
	_, event, err := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	require.Equal(t, "resp_turn1", gjson.GetBytes(event, "response.id").String())
	second := `{"type":"response.create","model":"gpt-5.1","input":"second","stream":false}`
	if mode == "previous" {
		second = `{"type":"response.create","model":"gpt-5.1","input":"second","previous_response_id":"resp_turn1","stream":false}`
	}
	require.NoError(t, clientConn.Write(context.Background(), coderws.MessageText, []byte(second)))
	if mode != "success" {
		for i := 0; i < 4; i++ {
			readCtx, cancelRead = context.WithTimeout(context.Background(), 4*time.Second)
			_, _, e := clientConn.Read(readCtx)
			cancelRead()
			if e != nil {
				break
			}
		}
		require.Empty(t, secondHitCh, "unsafe later turn must not reach B")
		return
	}
	readCtx, cancelRead = context.WithTimeout(context.Background(), 5*time.Second)
	_, event, err = clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "resp_ws_failover_ok", gjson.GetBytes(event, "response.id").String())
	require.NoError(t, clientConn.Write(context.Background(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"third","stream":false}`)))
	readCtx, cancelRead = context.WithTimeout(context.Background(), 5*time.Second)
	_, event, err = clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "resp_turn3", gjson.GetBytes(event, "response.id").String())

	for i, want := range []string{"first", "second"} {
		select {
		case payload := <-firstHitCh:
			require.Equal(t, want, gjson.GetBytes(payload, "input").String(), i)
		case <-time.After(time.Second):
			t.Fatal("missing A frame")
		}
	}
	for _, want := range []string{"second", "third"} {
		select {
		case payload := <-secondHitCh:
			require.Equal(t, want, gjson.GetBytes(payload, "input").String())
		case <-time.After(time.Second):
			t.Fatal("missing B frame")
		}
	}
	var ids []string
	for i, want := range []int{1, 2, 1} {
		select {
		case row := <-usageRepo.created:
			kind := service.RoutingFailoverNone
			if i == 1 {
				kind = service.RoutingFailoverAccount
			}
			assertResponsesRouting(t, row, 4202, 4202, kind, want)
			ids = append(ids, row.RequestID)
			expected := 3e-6
			if i == 2 {
				expected = 30e-6
			}
			require.InDelta(t, expected, row.TotalCost, 1e-12)
		case <-time.After(time.Second):
			t.Fatal("missing turn usage")
		}
	}
	require.NotEqual(t, ids[0], ids[1])
	require.NotEqual(t, ids[1], ids[2])
}
