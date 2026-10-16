//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// wsGroupCase configures the two/three-group normal chain fixture.
type wsGroupCase struct {
	maxSwitches    int
	secondPlatform string
	secondEmpty    bool
	firstPayload   string
}

type wsGroupResult struct {
	upstreamFrames [][]byte
	usageRows      []*service.UsageLog
	closeReason    string
	closed         bool
}

// runWSGroupCase drives the real ResponsesWebSocket handler with a normal group
// chain where group A has no schedulable account. It returns the frames the
// healthy upstream received and the usage rows recorded.
func runWSGroupCase(t *testing.T, spec wsGroupCase) wsGroupResult {
	t.Helper()
	gin.SetMode(gin.TestMode)

	frames := make(chan []byte, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, payload, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr == nil {
			frames <- payload
		}
		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		_ = conn.Write(writeCtx, coderws.MessageText, []byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_group_ok\",\"model\":\"gpt-5.1\",\"usage\":{\"input_tokens\":1000,\"output_tokens\":500}}}"))
		cancelWrite()
		_ = conn.Close(coderws.StatusNormalClosure, "done")
	}))
	t.Cleanup(upstream.Close)

	const (
		groupA = int64(8801)
		groupB = int64(8802)
		groupC = int64(8803)
	)
	groupPlatform := service.PlatformOpenAI
	pricing := []service.ChannelModelPricing{wsFixtureTokenPricing([]string{"gpt-5.1"}, 1e-6, 2e-6)}
	aGroup := &service.Group{ID: groupA, Platform: groupPlatform, Status: service.StatusActive, RateMultiplier: 1, ModelPricing: pricing}
	bGroup := &service.Group{ID: groupB, Platform: groupPlatform, Status: service.StatusActive, RateMultiplier: 1, ModelPricing: pricing}
	cGroup := &service.Group{ID: groupC, Platform: groupPlatform, Status: service.StatusActive, RateMultiplier: 1, ModelPricing: pricing}
	if spec.secondPlatform != "" {
		bGroup.Platform = spec.secondPlatform
	}
	groupIDs := []int64{groupA, groupB}
	groupsByID := map[int64]*service.Group{groupA: aGroup, groupB: bGroup}
	if spec.secondEmpty {
		groupIDs = append(groupIDs, groupC)
		groupsByID[groupC] = cGroup
	}

	newAccount := func(id, groupID int64, schedulable bool, priority int) *service.Account {
		return &service.Account{
			ID: id, Name: "ws-group-" + string(rune('a'+priority)), Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: schedulable, Concurrency: 1, Priority: priority,
			AccountGroups: []service.AccountGroup{{AccountID: id, GroupID: groupID}},
			Credentials:   map[string]any{"api_key": "fixture-not-a-secret", "base_url": upstream.URL},
			Extra:         map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeCtxPool},
		}
	}
	accounts := []*service.Account{newAccount(groupA, groupA, false, 1)}
	if spec.secondEmpty {
		accounts = append(accounts, newAccount(groupB, groupB, false, 2), newAccount(groupC, groupC, true, 3))
	} else {
		accounts = append(accounts, newAccount(groupB, groupB, true, 2))
	}

	cfg := &config.Config{}
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

	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{}
	for _, account := range accounts {
		accountRepo.accounts = append(accountRepo.accounts, *account)
	}
	rateLimitSvc := service.NewRateLimitService(accountRepo, nil, cfg, nil, nil)
	billingCacheSvc := service.NewBillingCacheService(&responsesAccountingFunds{lockedHTTPFunds: &lockedHTTPFunds{balance: 100}}, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheSvc.Stop)
	schedulerSvc := service.NewSchedulerSnapshotService(
		&groupScopedSchedulerCache{fakeSchedulerCache: &fakeSchedulerCache{accounts: accounts}}, nil, nil, nil, cfg,
	)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
	ledger := &lockedHTTPLedger{balance: 100, seen: map[string]bool{}}
	platforms := map[int64]string{}
	for id, group := range groupsByID {
		platforms[id] = group.Platform
	}
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{groupPlatforms: platforms}, nil, nil, nil, nil)
	billing := service.NewBillingService(cfg, nil)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo, usageRepo, ledger, nil, nil, &lockedHTTPRate{rate: 1}, nil, cfg, schedulerSvc, nil, billing, rateLimitSvc, billingCacheSvc,
		nil, &service.DeferredService{}, nil, nil, service.NewModelPricingResolver(channels, billing), channels, nil, nil, nil,
	)

	user := &service.User{ID: 7701, Status: service.StatusActive}
	groupAID := groupA
	links := []service.APIKeyGroupLink{{GroupID: groupA, Role: service.APIKeyGroupRoleNormal, Group: aGroup}}
	for i, id := range groupIDs[1:] {
		links = append(links, service.APIKeyGroupLink{GroupID: id, Role: service.APIKeyGroupRoleNormal, Position: i + 1, Group: groupsByID[id]})
	}
	key := &service.APIKey{
		ID: 8800, UserID: user.ID, User: user, GroupID: &groupAID, Status: service.StatusActive,
		Groups: links, Group: aGroup,
	}
	groups := &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: groupsByID}

	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := &OpenAIGatewayHandler{
		cfg:                 cfg,
		gatewayService:      gatewaySvc,
		billingCacheService: billingCacheSvc,
		apiKeyService:       service.NewAPIKeyService(&responsesChainKeyRepo{key: key}, &responsesChainUserRepo{user: user}, groups, nil, nil, nil, cfg),
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
		maxAccountSwitches:  spec.maxSwitches,
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	t.Cleanup(handlerServer.Close)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", &coderws.DialOptions{CompressionMode: coderws.CompressionContextTakeover})
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	payload := spec.firstPayload
	if payload == "" {
		payload = "{\"type\":\"response.create\",\"model\":\"gpt-5.1\",\"input\":\"hello\",\"stream\":false}"
	}
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, clientConn.Write(writeCtx, coderws.MessageText, []byte(payload)))
	cancelWrite()

	result := wsGroupResult{}
	readCtx, cancelRead := context.WithTimeout(context.Background(), 5*time.Second)
	_, event, readErr := clientConn.Read(readCtx)
	cancelRead()
	if readErr == nil {
		require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), string(event))
		require.Equal(t, "resp_group_ok", gjson.GetBytes(event, "response.id").String())
		_ = clientConn.Close(coderws.StatusNormalClosure, "done")
		select {
		case frame := <-frames:
			result.upstreamFrames = append(result.upstreamFrames, frame)
		case <-time.After(2 * time.Second):
		}
		for {
			select {
			case row := <-usageRepo.created:
				result.usageRows = append(result.usageRows, row)
				continue
			case <-time.After(2 * time.Second):
			}
			break
		}
		return result
	}
	result.closed = true
	var closeErr coderws.CloseError
	if errors.As(readErr, &closeErr) {
		result.closeReason = closeErr.Reason
	}
	select {
	case frame := <-frames:
		result.upstreamFrames = append(result.upstreamFrames, frame)
	case <-time.After(time.Second):
	}
	return result
}

// 空池 A 必须把本 turn 推进到普通组链下一组 B：B 真实收到当前 canonical 首帧，
// 只落一笔账，C6 记录 requested=A / served=B / failover=group / attempts=1。
func TestOpenAIWSGroupFailoverEmptyPoolAdvances(t *testing.T) {
	got := runWSGroupCase(t, wsGroupCase{maxSwitches: 3})
	require.Len(t, got.upstreamFrames, 1, "B must receive the replayed turn frame")
	require.Equal(t, "hello", gjson.GetBytes(got.upstreamFrames[0], "input").String())
	require.Len(t, got.usageRows, 1, "exactly one billed turn")
	assertResponsesRouting(t, got.usageRows[0], 8801, 8802, service.RoutingFailoverGroup, 1)
}

// 同 turn 可连续推进多组：A 空 → B 空 → C 服务，仍只耗一个 turn 预算、只落一笔账。
func TestOpenAIWSGroupFailoverRepeatsWithinTurn(t *testing.T) {
	got := runWSGroupCase(t, wsGroupCase{maxSwitches: 3, secondEmpty: true})
	require.Len(t, got.upstreamFrames, 1, "C must receive the replayed turn frame")
	require.Len(t, got.usageRows, 1, "exactly one billed turn for the whole chain")
	assertResponsesRouting(t, got.usageRows[0], 8801, 8803, service.RoutingFailoverGroup, 1)
}

// maxAccountSwitches=0 时不得换组、不得发上游、不得落账；关闭语义为"无可用账号"。
func TestOpenAIWSGroupFailoverMaxZero(t *testing.T) {
	got := runWSGroupCase(t, wsGroupCase{maxSwitches: 0})
	require.True(t, got.closed, "connection must close when the group switch budget is zero")
	require.Equal(t, "no available account", got.closeReason)
	require.Empty(t, got.upstreamFrames)
	require.Empty(t, got.usageRows)
}

// 目标分组平台不受 WS 转发族支持时是策略拒绝，不得静默换协议，也不得降级 passthrough。
func TestOpenAIWSGroupFailoverRejectsUnsupportedPlatform(t *testing.T) {
	got := runWSGroupCase(t, wsGroupCase{maxSwitches: 3, secondPlatform: service.PlatformGemini})
	require.True(t, got.closed)
	require.Equal(t, "no available account", got.closeReason)
	require.Empty(t, got.upstreamFrames)
	require.Empty(t, got.usageRows)
}

// previous/tool continuation 命名空间不可移植到新分组时拒绝重放：0 send、0 账。
func TestOpenAIWSGroupFailoverRejectsNonPortableContinuation(t *testing.T) {
	payload := "{\"type\":\"response.create\",\"model\":\"gpt-5.1\",\"previous_response_id\":\"resp_prev\",\"input\":[{\"type\":\"function_call_output\",\"call_id\":\"call_missing\",\"output\":\"ok\"}],\"stream\":false}"
	got := runWSGroupCase(t, wsGroupCase{maxSwitches: 3, firstPayload: payload})
	require.True(t, got.closed)
	require.Equal(t, "no available account", got.closeReason)
	require.Empty(t, got.upstreamFrames, "non-portable continuation must not be replayed to another group")
	require.Empty(t, got.usageRows)
}
