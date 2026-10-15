package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API requests.
// POST /v1/chat/completions
func (h *OpenAIGatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		logRequestBodyReadFailure(reqLog, c.Request, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	if cappedBody, changed, err := applyOpenAIReasoningEffortPolicyForRequest(c, apiKey, body); err != nil {
		respondOpenAIReasoningEffortPolicyError(c, err, h.errorResponse)
		return
	} else if changed {
		body = cappedBody
	}
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if _, err := service.ValidateOpenAIServiceTierField(body); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if h.rejectIfCyberSessionBlocked(c, apiKey, body, reqModel, cyberBlockFormatChat) {
		return
	}

	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	crossChat := chatCrossFamilyChain(apiKey)
	authorizeChatCandidate := func(original *service.APIKey, id int64, isHA bool, normalIDs, haIDs []int64) (*service.APIKey, *service.UserSubscription, bool) {
		ids := normalIDs
		if isHA {
			ids = haIDs
		}
		return authorizeChatExecutionGroupCandidate(c, h.apiKeyService, h.subscriptionService, original, id, ids, reqModel, func(status int, code, msg string) { h.handleStreamingAwareError(c, status, code, msg, streamStarted) }, crossChat, isHA)
	}

	// 解析渠道级模型映射。限制检查（ResolveChannelMappingAndRestrict）继续保留；
	// 锁价激活时随后用请求建立时冻结的 bundle 覆盖它，与 Responses 参考实现同口径。
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	// C-1 请求锁价（共享入口切片 / OpenAI 兼容分支）。位置在协议/平台/参数拒绝之后、
	// 选号之前：
	//   - 口径与 OpenAI Responses 参考入口一致：仅 PlatformOpenAI（普通 OpenAI
	//     文本请求）进入锁价；grok/国产等其它 OpenAI 兼容平台保持既有行为；
	//   - composite/HA 的成员分组绑定属 S5 边界，同样不进入锁价；
	//   - 缺价模型在上游请求前即 503 pricing_unavailable；
	//   - 与既有 WithOpenAIRequestPricingContext 共存：后者的 pricingAt/profit gate 在
	//     bundle 已存在时会自动取 bundle.StartedAt()，不会覆盖锁价时刻。
	lockedOAIEligible := requestPlatform == service.PlatformOpenAI
	lockedOAI, lockedOAIOK := prepareSharedGatewayLockedBillingWithErrorWriter(
		c, h.gatewayService, apiKey, reqModel, body, reqLog, lockedOAIEligible, false, writeOpenAIGatewayLockedBillingError,
	)
	if !lockedOAIOK {
		return
	}
	channelMapping = lockedOAI.apply(channelMapping)
	forwardModel := openAIChannelForwardModel(channelMapping, reqModel)

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestBilling := service.GatewayRequestBillingFromContext(c.Request.Context())
	if requestBilling != nil && len(requestBilling.NormalGroupIDs())+len(requestBilling.HAGroupIDs()) > 1 {
		var admitted bool
		// Start from the plan's first candidate rather than NormalGroupIDs()[0]: an
		// HA-only authorized chain must authorize its own first candidate.
		startID, startIsHA, hasStart := newNormalGroupPlan(requestBilling.NormalGroupIDs(), requestBilling.HAGroupIDs()).first()
		if hasStart {
			apiKey, subscription, admitted = authorizeChatCandidate(apiKey, startID, startIsHA, requestBilling.NormalGroupIDs(), requestBilling.HAGroupIDs())
			if !admitted {
				return
			}
			c.Request = c.Request.WithContext(sharedResponsesExecutionContext(c.Request.Context(), apiKey))
		}
	}

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_chat_completions.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	// 余额模式在途预留：防止并发请求在预检时看到同一份余额而集体透支。
	reservationBase := c.Request.Context()
	inflightRelease, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(reqModel, body))
	if err != nil {
		reqLog.Info("openai_chat_completions.inflight_reservation_rejected", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}
	defer func() { inflightRelease() }()

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState

	// 分组利润控制：chat completions 文本入口请求级装门并固定 pricingAt。
	ccPricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(ccPricingCtx)

	// TS-02 S3b：OpenAI Chat Completions 文本入口在 failover 循环前挂载单逻辑
	// 请求预算。clientDeadline 本期不读，传零值；预算耗尽在下方同账号重试/换号
	// 分支复用 503 + no_available_group 出口。
	maxAttemptSends, budgetWindow := h.gatewayService.RequestAttemptBudgetLimits(c.Request.Context())
	budgetCtx, requestBudget := service.EnsureRequestAttemptBudget(c.Request.Context(), maxAttemptSends, budgetWindow, time.Time{})
	c.Request = c.Request.WithContext(budgetCtx)
	reservationBase = service.WithRequestAttemptBudget(reservationBase, requestBudget)
	var normalIDs []int64
	var haIDs []int64
	if requestBilling != nil {
		normalIDs = requestBilling.NormalGroupIDs()
		haIDs = requestBilling.HAGroupIDs()
	}
	// PRD-04 AC-B2: this entry must reach the user's authorized fallback groups too.
	// normalGroupPlan owns the ordering rule (every normal candidate first, HA only
	// after a retryable failure) and is shared with the messages/responses/Gemini
	// entries, so the OpenAI-native paths cannot drift from them.
	groupPlan := newNormalGroupPlan(normalIDs, haIDs)
	pendingSwitchPermit := false
	firstSentAccountID := int64(0)
	sentDifferentAccount := false
	initialSemanticSize := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
	advanceNormalGroup := func() normalGroupAdvance {
		if !groupPlan.active() || requestBudget.Exhausted() || !openAIRequestAllowsFailoverReplay(c) || service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != initialSemanticSize {
			return normalGroupUnavailable
		}
		if lastFailoverErr != nil && !lastFailoverErr.ShouldRetryNextAccount() {
			return normalGroupUnavailable
		}
		if !pendingSwitchPermit {
			if switchCount >= maxAccountSwitches {
				return normalGroupUnavailable
			}
			switchCount++
		}
		pendingSwitchPermit = false
		// retryable=true: the guard above already returned when lastFailoverErr is
		// non-nil and not retryable, so reaching here means either there is no prior
		// failure or it is recoverable. Passing the condition again would be a
		// tautology that hides which guard actually enforces the PRD-04 rule.
		nextID, nextIsHA, hasNext := groupPlan.peek(true)
		if !hasNext {
			return normalGroupUnavailable
		}
		nextKey, nextSub, ok := authorizeChatCandidate(apiKey, nextID, nextIsHA, normalIDs, haIDs)
		if !ok {
			return normalGroupTerminal
		}
		nextMapping, e := requestBilling.MappingFor(nextID)
		if e != nil {
			status, code, msg, _ := billingErrorDetails(e)
			h.handleStreamingAwareError(c, status, code, msg, streamStarted)
			return normalGroupTerminal
		}
		inflightRelease()
		inflightRelease = inflightNoop
		nextCtx := sharedResponsesExecutionContext(reservationBase, nextKey)
		nextCtx, _ = h.gatewayService.WithOpenAIRequestPricingContext(nextCtx, nextKey.GroupID)
		c.Request = c.Request.WithContext(nextCtx)
		for _, check := range []func() error{func() error {
			return h.billingCacheService.CheckFinancialEligibility(nextCtx, nextKey.User, nextKey, nextKey.Group, nextSub, service.QuotaPlatform(nextCtx, nextKey))
		}, func() error {
			return h.billingCacheService.CheckGroupRPMEligibility(nextCtx, nextKey.User, nextKey.Group)
		}} {
			if e := check(); e != nil {
				status, code, msg, retryAfter := billingErrorDetails(e)
				if retryAfter > 0 {
					c.Header("Retry-After", strconv.Itoa(retryAfter))
				}
				h.handleStreamingAwareError(c, status, code, msg, streamStarted)
				return normalGroupTerminal
			}
		}
		var eReserve error
		inflightRelease, eReserve = reserveInflightBalance(c, h.billingCacheService, chatTargetEstimator(nextKey.Group.Platform, h.sharedGatewayService, h.gatewayService), nextKey, nextSub, tokenInflightEstimate(reqModel, body))
		if eReserve != nil {
			status, code, msg, _ := billingErrorDetails(eReserve)
			h.handleStreamingAwareError(c, status, code, msg, streamStarted)
			return normalGroupTerminal
		}
		// Commit only after every guard passed, so a rejected attempt never skips a
		// candidate.
		groupPlan.commit()
		apiKey, subscription, channelMapping = nextKey, nextSub, nextMapping
		requestPlatform = nextKey.Group.Platform
		lockedOAI.groupID, lockedOAI.mapping = nextID, nextMapping
		forwardModel = openAIChannelForwardModel(nextMapping, reqModel)
		sessionHash = h.gatewayService.GenerateSessionHash(c, body)
		return normalGroupAdvanced
	}

	for {
		if !openAIRequestAllowsFailoverReplay(c) {
			return
		}
		if failoverClientGone(c) {
			return
		}
		reqLog.Debug("openai_chat_completions.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		var selection *service.AccountSelectionResult
		var err error
		if requestPlatform == service.PlatformAnthropic && crossChat {
			if h.sharedGatewayService == nil {
				h.handleStreamingAwareError(c, 503, "no_available_group", "Shared service unavailable", streamStarted)
				return
			}
			selection, err = h.sharedGatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, sessionHash, forwardModel, failedAccountIDs, "", 0)
		} else {
			selection, _, err = h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), apiKey.GroupID, "", sessionHash, forwardModel, failedAccountIDs, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, requestPlatform)
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai_chat_completions.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai_chat_completions.account_select_failed",
				zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if errors.Is(err, service.ErrNoAvailableAccounts) {
				switch advanceNormalGroup() {
				case normalGroupAdvanced:
					continue
				case normalGroupTerminal:
					return
				}
			}
			if len(failedAccountIDs) == 0 {
				cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
				cls = classifySelectionFailureError(err, cls)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return
			} else {
				if lastFailoverErr != nil {
					h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
				} else {
					h.handleStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
				}
				return
			}
		}
		if selection == nil || selection.Account == nil {
			switch advanceNormalGroup() {
			case normalGroupAdvanced:
				continue
			case normalGroupTerminal:
				return
			}
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		account := selection.Account
		pendingSwitchPermit = false
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_chat_completions.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// C-1 锁价绑定：账号选定后、取槽与 Forward 之前把实际执行账号写入冻结
		// bundle。绑定失败（账号不在冻结映射表内/冻结价缺失）即 503，不转发、不扣费；
		// failover 换号后重新绑定，价格仍取自同一份请求级 bundle。
		if bound, bindErr := bindSharedGatewayLockedBillingAccount(c, lockedOAI, derefGroupID(apiKey.GroupID), account.ID, reqModel, reqLog); !bound {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			status, code, message, _ := billingErrorDetails(bindErr)
			h.handleStreamingAwareError(c, status, code, message, streamStarted)
			return
		}

		var accountReleaseFunc func()
		slotResult := openAISlotAcquireOK
		if crossChat && account.Platform == service.PlatformAnthropic {
			var veto bool
			var e error
			accountReleaseFunc, veto, e = chatBridgeAcquire(c, h.concurrencyHelper, h.sharedGatewayService, h.gatewayService, selection, reqStream, &streamStarted)
			if e != nil {
				h.handleStreamingAwareError(c, 503, "no_available_group", "Account admission failed", streamStarted)
				return
			}
			if veto {
				slotResult = openAISlotAcquireProfitVetoed
			}
			account = selection.Account
		} else {
			accountReleaseFunc, slotResult = h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
		}
		if slotResult == openAISlotAcquireProfitVetoed {
			// 利润终检否决：排除该账号重新选号；否决次数达上限则按无可用账号终止。
			if !recordOpenAIProfitVeto(failedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
			return
		}

		if crossChat && account.Platform == service.PlatformAnthropic && selection.ProfitGateActive() {
			if e := h.sharedGatewayService.BindStickySessionAfterProfitAdmission(service.ContextWithSelectionProfitGate(c.Request.Context(), selection), apiKey.GroupID, sessionHash, account.ID); e != nil {
				reqLog.Warn("chat_bridge.sticky_bind_failed", zap.Error(e))
			}
		}
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		if crossChat {
			decision := chatBridgeAssess(c.Request.Context(), derefGroupID(apiKey.GroupID), account, forwardBody, "", h.gatewayService)
			if decision.Class != chatBridgeCompatible {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				if decision.Class == chatBridgeIllegal {
					h.handleStreamingAwareError(c, 400, "invalid_request_error", decision.Reason, streamStarted)
					return
				}
				if switchCount >= maxAccountSwitches {
					h.handleStreamingAwareError(c, 503, "no_available_group", "No compatible Chat target", streamStarted)
					return
				}
				failedAccountIDs[account.ID] = struct{}{}
				switchCount++
				pendingSwitchPermit = true
				continue
			}
		}
		writerSizeBeforeForward := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
		usedBeforeForward := requestBudget.Used()
		var sharedResult *service.ForwardResult
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			if crossChat && account.Platform == service.PlatformAnthropic {
				var e error
				sharedResult, e = h.sharedGatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, nil)
				return nil, e
			}
			return h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, promptCacheKey, "")
		}()
		if requestBudget.Used() > usedBeforeForward {
			if firstSentAccountID == 0 {
				firstSentAccountID = account.ID
			} else if firstSentAccountID != account.ID {
				sentDifferentAccount = true
			}
		}
		var cyberBlockBodyChat []byte
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockBodyChat = body
		}
		h.recordCyberPolicyIfMarked(c, apiKey, account, subscription, reqModel, err != nil, cyberBlockBodyChat, clientRequestedUsageFields(c, channelMapping, reqModel, ""), service.HashUsageRequestPayload(body))

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		// #5148 对齐：错误返回携带的部分 result（流中断前上游已计量的 usage）照常
		// 入账；failover 错误恒定 result=nil，不会重复计费。
		submitChatUsage := func(res *service.OpenAIForwardResult) {
			if res == nil {
				return
			}
			stampOpenAIRequestedReasoningEffort(res, c)
			userAgent := c.GetHeader("User-Agent")
			clientIP := ip.GetClientIP(c)
			inboundEndpoint := GetInboundEndpoint(c)
			upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, res)
			quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
			sessionID := service.ExtractClientSessionID(c)
			cyberBlocked := service.GetOpsCyberPolicy(c) != nil
			usageFields := clientRequestedUsageFields(c, channelMapping, reqModel, res.UpstreamModel)
			servedKey, servedSub, servedAccount := apiKey, subscription, account
			var routingUsage *service.RoutingUsageMetadata
			if requestBilling != nil {
				if requested := requestBilling.RequestedGroupID(); requested != nil && *requested > 0 && derefGroupID(apiKey.GroupID) > 0 {
					routingUsage = &service.RoutingUsageMetadata{
						RequestedGroupID: *requested,
						ServedGroupID:    derefGroupID(apiKey.GroupID),
						// C-6/PRD-04 BE-05: use the shared classifier so an HA-served
						// request is recorded as failover=ha rather than a plain group
						// switch. This entry can now reach HA, so it must attribute it.
						Failover:     classifyGroupFailover(*requested, derefGroupID(apiKey.GroupID), requestBilling.HAGroupIDs(), sentDifferentAccount),
						AttemptCount: requestBudget.Used(),
					}
				}
			}
			h.submitOpenAIUsageRecordTask(c.Request.Context(), res, func(ctx context.Context) {
				if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
					Result:             res,
					RoutingUsage:       routingUsage,
					APIKey:             servedKey,
					User:               servedKey.User,
					Account:            servedAccount,
					Subscription:       servedSub,
					InboundEndpoint:    inboundEndpoint,
					UpstreamEndpoint:   upstreamEndpoint,
					UserAgent:          userAgent,
					IPAddress:          clientIP,
					APIKeyService:      h.apiKeyService,
					QuotaPlatform:      quotaPlatform,
					SessionID:          sessionID,
					ChannelUsageFields: usageFields,
					PricingAt:          pricingAt,
					CyberBlocked:       cyberBlocked,
				}); err != nil {
					logger.L().With(
						zap.String("component", "handler.openai_gateway.chat_completions"),
						zap.Int64("user_id", subject.UserID),
						zap.Int64("api_key_id", servedKey.ID),
						zap.Any("group_id", servedKey.GroupID),
						zap.String("model", reqModel),
						zap.Int64("account_id", servedAccount.ID),
					).Error("openai_chat_completions.record_usage_failed", zap.Error(err))
				}
			})
		}
		if sharedResult != nil {
			stampForwardRequestedReasoningEffort(sharedResult, service.RequestedReasoningEffortFromContext(c.Request.Context()))
			input := chatSharedUsageSnapshot(c, sharedResult, apiKey, subscription, account, reqModel, body, channelMapping, pricingAt, requestBudget.Used(), sentDifferentAccount, h.apiKeyService)
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if e := h.sharedGatewayService.RecordUsage(ctx, input); e != nil {
					reqLog.Error("chat_bridge.record_usage_failed", zap.Error(e))
				}
			})
			if err != nil {
				h.ensureOpenAIStreamReadErrorResponse(c, err, streamStarted)
			}
			return
		}
		if err != nil {
			if result != nil {
				h.ensureOpenAIStreamReadErrorResponse(c, err, streamStarted)
				submitChatUsage(result)
				reqLog.Warn("openai_chat_completions.forward_partial_error", zap.Int64("account_id", account.ID), zap.Error(err))
				return
			}

			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if failoverClientGone(c) {
					reqLog.Info("openai_chat_completions.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				if !openAIForwardMayFailover(c, writerSizeBeforeForward, failoverErr) {
					// TS-02：预算耗尽是本站策略终止，账号健康且未被调用，
					// 不得据此记录账号健康失败（会污染账号调度分数）。
					if failoverErr.Reason != service.RequestAttemptBudgetReason {
						if account.Platform == service.PlatformOpenAI {
							h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						}
					}
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				if account.Platform == service.PlatformOpenAI && failoverErr.ShouldReportAccountScheduleFailure() {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, nil), false, nil, err)
				}
				if !failoverErr.ShouldRetryNextAccount() {
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				// Pool mode: retry on the same account
				if failoverErr.RetryableOnSameAccount {
					retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
					if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
						// TS-02 S3b：预算耗尽则不再重试，复用耗尽出口
						// （503 / no_available_group）。本次 failover 的真实上游错误
						// 已在上面上报；这里只终止下一次尝试，不产生新的账号切换。
						if requestBudget.Exhausted() {
							h.handleFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), streamStarted)
							return
						}
						sameAccountRetryCount[account.ID]++
						retryDelay := clampFailoverDelayToBudget(c.Request.Context(), sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID]))
						reqLog.Warn("openai_chat_completions.pool_mode_same_account_retry",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
							zap.Int("retry_limit", retryLimit),
							zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							zap.Duration("retry_delay", retryDelay),
						)
						select {
						case <-c.Request.Context().Done():
							return
						case <-time.After(retryDelay):
						}
						continue
					}
				}
				// TS-02 S3b：预算耗尽则不再换号，复用耗尽出口
				// （503 / no_available_group）。放在 RecordOpenAIAccountSwitch 之前：
				// 被预算挡下的尝试不产生账号切换。
				if requestBudget.Exhausted() {
					h.handleFailoverExhausted(c, service.NewRequestAttemptBudgetFailoverError(), streamStarted)
					return
				}
				h.gatewayService.RecordOpenAIAccountSwitch()
				failedAccountIDs[account.ID] = struct{}{}
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				switchCount++
				pendingSwitchPermit = true
				if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
					h.handleFailoverExhausted(c, failoverErr, streamStarted)
					return
				}
				reqLog.Warn("openai_chat_completions.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				continue
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, nil), false, nil, err)
			upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureOpenAIStreamReadErrorResponse(c, err, streamStarted)
				if !wroteFallback {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
			}
			reqLog.Warn("openai_chat_completions.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			submitChatUsage(result)
			return
		}
		if result != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, result), true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, result), true, nil)
		}

		submitChatUsage(result)
		reqLog.Debug("openai_chat_completions.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

// resolveOpenAIUpstreamEndpoint returns the actual upstream endpoint for an
// OpenAI-compatible account. A forwarding result is authoritative because a
// single inbound route may choose raw Chat or a Responses bridge at runtime.
// The account-based derivation remains as a fallback for existing callers and
// forwarding paths that do not report their endpoint yet.
func resolveOpenAIUpstreamEndpoint(c *gin.Context, account *service.Account, result *service.OpenAIForwardResult) string {
	if result != nil {
		if endpoint := strings.TrimSpace(result.UpstreamEndpoint); endpoint != "" {
			return endpoint
		}
	}
	if endpoint := service.GetActualOpenAIUpstreamEndpoint(c); endpoint != "" {
		return endpoint
	}
	if account != nil && account.Type == service.AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return EndpointChatCompletions
	}
	return GetUpstreamEndpoint(c, account.Platform)
}
