package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API endpoint for Anthropic platform groups.
// POST /v1/chat/completions
// This converts Chat Completions requests to Anthropic format (via Responses format chain),
// forwards to Anthropic upstream, and converts responses back to Chat Completions format.
func (h *GatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// Read request body
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.chatCompletionsErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	bindRequestedReasoningEffort(c, body, reqModel)
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformResolved(c, apiKey, reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by composite groups")
		return
	}
	if rejectSystemOneOnlyPlatform(c, apiKey, h.chatCompletionsErrorResponse) {
		return
	}
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	pricingCtx, pricingAt := service.WithGatewayTokenRequestPricing(c.Request.Context())
	c.Request = c.Request.WithContext(pricingCtx)

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	// Claude Code only restriction: /v1/chat/completions is never a Claude Code
	// endpoint. With a fallback group the request continues and account selection
	// (checkClaudeCodeRestriction) schedules it in the fallback group; without one
	// it is rejected here.
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly && apiKey.Group.FallbackGroupID == nil {
		h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}

	// C-1 请求锁价（共享入口切片 / 非 OpenAI 平台的 Chat Completions）。
	// 位置在"参数/协议/平台"拒绝之后，与 Messages 参考入口一致：
	//   - 仅非 composite 分组启用（composite/HA 的成员分组绑定属 S5 边界）；
	//   - 本入口没有 warmup/intercept/mock 本机分支（全量走真实上游），因此只排除
	//     composite，不做额外的 mock 豁免；
	//   - 其余请求的缺价模型在上游请求前即 503 pricing_unavailable；
	//   - 渠道映射以请求建立时冻结的 bundle 为准，覆盖上面解析出的 live 映射。
	// F7 修复：Antigravity 兼容出站按转换后的 Claude thinking 选档位变体
	// （antigravity_gateway_compat.go:215-227），冻结侧必须拿到同一档位，否则
	// reasoning_effort=medium + 裸 gemini 名时冻结值停在默认 high、实际出站是 medium
	// → 等值核对失败 → 用户 200 但整笔零计费。走与出站相同的转换链推导，保证同源。
	{
		thinkingEnabled, thinkingLevel := service.ChatCompletionsThinkingPreferenceFromBody(body)
		c.Request = c.Request.WithContext(service.WithGatewayRequestThinkingPreference(
			c.Request.Context(), thinkingEnabled, thinkingLevel))
	}

	lockedCCBilling := apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite
	lockedCC, lockedCCOK := prepareSharedGatewayLockedBillingWithErrorWriter(
		c, h.gatewayService, apiKey, reqModel, body, reqLog, lockedCCBilling, false, writeOpenAIGatewayLockedBillingError,
	)
	if !lockedCCOK {
		return
	}
	channelMapping = lockedCC.apply(channelMapping)

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestBilling := service.GatewayRequestBillingFromContext(c.Request.Context())
	var normalIDs []int64
	var haIDs []int64
	if lockedCC.active {
		normalIDs = requestBilling.NormalGroupIDs()
		// PRD-04: HA candidates come from the same frozen bundle and are only
		// reachable after every normal candidate is exhausted on a retryable error.
		haIDs = requestBilling.HAGroupIDs()
	}
	// The plan owns normal-then-HA ordering; both lists are frozen and never
	// reordered here.
	groupPlan := newNormalGroupPlan(normalIDs, haIDs)
	groupChainActive := groupPlan.active()
	originalAPIKey := apiKey
	crossChat := chatCrossFamilyChain(apiKey)
	// authorizeChatCandidate selects the normal or HA gate for the candidate's phase.
	// Both phases share the platform, allowlist, Claude-Code and subscription policy.
	authorizeChatCandidate := func(original *service.APIKey, id int64, isHA bool) (*service.APIKey, *service.UserSubscription, bool) {
		if crossChat {
			// The frozen allow-set must match the phase: a normal candidate is
			// authorized against the normal chain, an HA candidate against the HA set.
			allowSet := normalIDs
			if isHA {
				allowSet = haIDs
			}
			return authorizeChatExecutionGroupCandidate(c, h.apiKeyService, h.subscriptionService, original, id, allowSet, reqModel, func(status int, code, msg string) { h.chatCompletionsErrorResponse(c, status, code, msg) }, true, isHA)
		}
		return h.authorizeSharedTextGroupPolicyWith(c, original, id, normalIDs, haIDs, isHA, body, true, false, h.chatCompletionsErrorResponse)
	}
	groupIndex := 0
	if groupChainActive {
		var admitted bool
		// Start from the plan's first candidate rather than normalIDs[0]: a chain may
		// hold only HA links, and the phase must match what the plan serves.
		startID, startIsHA, hasStart := groupPlan.first()
		if !hasStart {
			h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "no_available_group", "No group candidate available for this request")
			return
		}
		apiKey, subscription, admitted = authorizeChatCandidate(originalAPIKey, startID, startIsHA)
		if !admitted {
			return
		}
		c.Request = c.Request.WithContext(sharedResponsesExecutionContext(c.Request.Context(), apiKey))
	}
	// Keep an unreserved base for switching financial identities. A nil reservation
	// does NOT mask a previous reservation in the context helper.
	reservationBase := c.Request.Context()

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.cc.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("gateway.cc.billing_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.chatCompletionsErrorResponse(c, status, code, message)
		return
	}

	// 余额模式在途预留：防止并发请求在预检时看到同一份余额而集体透支。
	inflightRelease, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(reqModel, body))
	if err != nil {
		reqLog.Info("gateway.cc.inflight_reservation_rejected", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.chatCompletionsErrorResponse(c, status, code, message)
		return
	}
	defer func() { inflightRelease() }()

	// Parse request for session hash
	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "chat_completions")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: bodyRef}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	groupPlatform := effectiveAPIKeyPlatform(c, apiKey)
	selectionSessionHash := sessionHash
	if groupPlatform == service.PlatformGemini && selectionSessionHash != "" {
		selectionSessionHash = "gemini:" + selectionSessionHash
	}

	// TS-02 S3b：Chat Completions 文本入口在 failover 循环前挂载单逻辑请求预算。
	// clientDeadline 本期不读，传零值；预算耗尽由 HandleSelectionExhausted /
	// HandleFailoverError 统一走 503 + no_available_group。
	maxAttemptSends, budgetWindow := h.gatewayService.RequestAttemptBudgetLimits(c.Request.Context())
	budgetCtx, attemptBudget := service.EnsureRequestAttemptBudget(c.Request.Context(), maxAttemptSends, budgetWindow, time.Time{})
	c.Request = c.Request.WithContext(budgetCtx)

	// 3. Account selection + failover loop
	fs := NewFailoverState(h.maxAccountSwitches, false)
	if groupPlatform == service.PlatformGemini {
		fs = NewFailoverState(h.maxAccountSwitchesGemini, false)
	}
	reservationBase = service.WithRequestAttemptBudget(reservationBase, service.RequestAttemptBudgetFromContext(c.Request.Context()))
	// A recoverable account failure obtains its switch permit BEFORE the next
	// selection. Reuse that permit if its pool is empty; never charge it twice.
	pendingSwitchPermit := false
	var firstSentAccountID int64
	sentDifferentAccount := false
	semanticStart := service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
	advanceGroup := func() normalGroupAdvance {
		if !groupChainActive {
			return normalGroupUnavailable
		}
		if failoverClientGone(c) {
			return normalGroupTerminal
		}
		if requestAttemptBudgetExhausted(c.Request.Context(), "cc_group_advance") || service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != semanticStart {
			return normalGroupUnavailable
		}
		if fs.LastFailoverErr != nil && !fs.LastFailoverErr.ShouldRetryNextAccount() {
			return normalGroupUnavailable
		}
		nextID, nextIsHA, hasNext := groupPlan.peek(fs.LastFailoverErr == nil || fs.LastFailoverErr.ShouldRetryNextAccount())
		if !hasNext {
			return normalGroupUnavailable
		}
		nextKey, nextSub, admitted := authorizeChatCandidate(originalAPIKey, nextID, nextIsHA)
		if !admitted {
			return normalGroupTerminal
		}
		// Tighten to the destination platform BEFORE using any transition permit.
		targetLimit := h.maxAccountSwitches
		if nextKey.Group.Platform == service.PlatformGemini {
			targetLimit = h.maxAccountSwitchesGemini
		}
		if targetLimit < fs.MaxSwitches {
			fs.MaxSwitches = targetLimit
		}
		if fs.SwitchCount > fs.MaxSwitches {
			return normalGroupUnavailable
		}
		// Group boundaries also spend the request-wide smaller switch allowance,
		// even for an empty pool. Unlike this allowance, attempt budget counts
		// only actual upstream sends. A permit granted by A can enter B once.
		if !pendingSwitchPermit {
			if fs.SwitchCount >= fs.MaxSwitches {
				return normalGroupUnavailable
			}
			fs.SwitchCount++
		}
		pendingSwitchPermit = false
		nextMapping, mappingErr := requestBilling.MappingFor(nextID)
		if mappingErr != nil {
			writeOpenAIGatewayLockedBillingError(c, reqLog, "gateway.cc.group_mapping_rejected", reqModel, mappingErr)
			return normalGroupTerminal
		}
		// No usage worker was submitted on the failed attempts. Release the old
		// handler reference once, then reserve with the destination billing identity.
		inflightRelease()
		inflightRelease = inflightNoop
		nextCtx := sharedResponsesExecutionContext(reservationBase, nextKey)
		if crossChat && nextKey.Group.Platform == service.PlatformOpenAI {
			if h.openAIGatewayService == nil {
				h.chatCompletionsErrorResponse(c, 503, "no_available_group", "OpenAI service unavailable")
				return normalGroupTerminal
			}
			nextCtx, _ = h.openAIGatewayService.WithOpenAIRequestPricingContext(nextCtx, nextKey.GroupID)
		}
		c.Request = c.Request.WithContext(nextCtx)
		if eligibilityErr := h.billingCacheService.CheckFinancialEligibility(nextCtx, nextKey.User, nextKey, nextKey.Group, nextSub, service.QuotaPlatform(nextCtx, nextKey)); eligibilityErr != nil {
			status, code, message, _ := billingErrorDetails(eligibilityErr)
			h.chatCompletionsErrorResponse(c, status, code, message)
			return normalGroupTerminal
		}
		if rpmErr := h.billingCacheService.CheckGroupRPMEligibility(nextCtx, nextKey.User, nextKey.Group); rpmErr != nil {
			status, code, message, retryAfter := billingErrorDetails(rpmErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.chatCompletionsErrorResponse(c, status, code, message)
			return normalGroupTerminal
		}
		var reserveErr error
		estimator := inflightReservationEstimator(h.gatewayService)
		if crossChat {
			estimator = chatTargetEstimator(nextKey.Group.Platform, h.gatewayService, h.openAIGatewayService)
		}
		inflightRelease, reserveErr = reserveInflightBalance(c, h.billingCacheService, estimator, nextKey, nextSub, tokenInflightEstimate(reqModel, body))
		if reserveErr != nil {
			status, code, message, _ := billingErrorDetails(reserveErr)
			h.chatCompletionsErrorResponse(c, status, code, message)
			return normalGroupTerminal
		}
		// Commit only after every guard passed, so a rejected attempt never skips a
		// candidate.
		groupPlan.commit()
		groupIndex++
		apiKey, subscription, channelMapping = nextKey, nextSub, nextMapping
		lockedCC.groupID, lockedCC.mapping = nextID, nextMapping
		groupPlatform = nextKey.Group.Platform
		selectionSessionHash = sessionHash
		if groupPlatform == service.PlatformGemini && sessionHash != "" {
			selectionSessionHash = "gemini:" + sessionHash
		}
		return normalGroupAdvanced
	}

	for {
		if failoverClientGone(c) {
			return
		}
		var selection *service.AccountSelectionResult
		var err error
		if crossChat && groupPlatform == service.PlatformOpenAI {
			selection, _, err = h.openAIGatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), apiKey.GroupID, "", selectionSessionHash, openAIChannelForwardModel(channelMapping, reqModel), fs.FailedAccountIDs, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityChatCompletions, false, false, true, groupPlatform)
		} else {
			selection, err = h.gatewayService.SelectAccountWithLoadAwareness(c.Request.Context(), apiKey.GroupID, selectionSessionHash, reqModel, fs.FailedAccountIDs, "", 0)
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("gateway.cc.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, groupPlatform)
				cls = classifySelectionFailureError(err, cls)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				if !cls.ModelNotFound && errors.Is(err, service.ErrNoAvailableAccounts) {
					switch advanceGroup() {
					case normalGroupAdvanced:
						continue
					case normalGroupTerminal:
						return
					}
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available accounts: " + err.Error()
				}
				h.chatCompletionsErrorResponse(c, cls.Status, cls.ErrType, message)
				return
			}
			if errors.Is(err, service.ErrNoAvailableAccounts) && fs.LastFailoverErr != nil && fs.LastFailoverErr.ShouldRetryNextAccount() {
				switch advanceGroup() {
				case normalGroupAdvanced:
					continue
				case normalGroupTerminal:
					return
				}
			}
			action := fs.HandleSelectionExhausted(c.Request.Context())
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				failoverClientGone(c)
				return
			default:
				if fs.LastFailoverErr != nil {
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
				} else {
					h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
				}
				return
			}
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// 4. Acquire account concurrency slot
		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				selection.WaitPlan.Timeout,
				reqStream,
				&streamStarted,
			)
			if err != nil {
				reqLog.Warn("gateway.cc.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
		}
		// 终检与准入后绑定使用选号结果携带的门（见 responses 同名注释）。
		admissionCtx := service.ContextWithSelectionProfitGate(c.Request.Context(), selection)
		var latest *service.Account
		var vetoed bool
		var reason string
		if crossChat && account.Platform == service.PlatformOpenAI {
			latest, vetoed, reason = h.openAIGatewayService.ProfitControlVetoLatest(admissionCtx, account)
		} else {
			latest, vetoed, reason = h.gatewayService.GatewayProfitControlVetoLatest(admissionCtx, account)
		}
		if vetoed {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			reqLog.Debug("gateway.cc.account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			if fs.RecordProfitVeto(account.ID) == FailoverExhausted {
				reqLog.Warn("gateway.cc.profit_veto_attempts_exhausted", zap.Int("profit_veto_count", fs.ProfitVetoCount()))
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", profitVetoExhaustedMessage)
				return
			}
			continue
		}
		account = latest
		selection.Account = latest
		// Exactly one binder must run per admitted account. The bridge path
		// (crossChat on an OpenAI account) goes through the OpenAI service because
		// that is the service that selected the account; every other combination —
		// including a plain OpenAI group where crossChat is false — keeps the
		// original shared-gateway bind. Excluding the whole OpenAI platform here
		// left that last quadrant with no sticky binding at all, so a session lost
		// its affinity after the first admitted request.
		bindViaOpenAIService := chatStickyBinderViaOpenAIService(crossChat, account)
		if selection.ProfitGateActive() && !bindViaOpenAIService {
			if err := h.gatewayService.BindStickySessionAfterProfitAdmission(admissionCtx, apiKey.GroupID, selectionSessionHash, account.ID); err != nil {
				reqLog.Warn("gateway.cc.bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		if bindViaOpenAIService && selection.ProfitGateActive() {
			if e := h.openAIGatewayService.BindStickySessionAfterProfitAdmission(admissionCtx, apiKey.GroupID, selectionSessionHash, account.ID); e != nil {
				reqLog.Warn("chat_bridge.sticky_bind_failed", zap.Error(e))
			}
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		if groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}

		// C-1 锁价绑定：账号终检与平台一致性过滤之后、Forward 之前，把实际执行账号
		// 写入冻结 bundle。绑定失败（账号不在冻结映射表内/冻结价缺失）即 503，不转发、
		// 不扣费；failover 换号后重新绑定，价格仍取自同一份请求级 bundle。
		if bound, bindErr := bindSharedGatewayLockedBillingAccount(c, lockedCC, derefGroupID(apiKey.GroupID), account.ID, reqModel, reqLog); !bound {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			status, code, message, _ := billingErrorDetails(bindErr)
			h.chatCompletionsErrorResponse(c, status, code, message)
			return
		}

		pendingSwitchPermit = false
		usedBeforeForward := attemptBudget.Used()
		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		if crossChat {
			decision := chatBridgeAssess(c.Request.Context(), derefGroupID(apiKey.GroupID), account, forwardBody, "", h.openAIGatewayService)
			if decision.Class != chatBridgeCompatible {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				if decision.Class == chatBridgeIllegal {
					h.chatCompletionsErrorResponse(c, 400, "invalid_request_error", decision.Reason)
					return
				}
				if fs.SwitchCount >= fs.MaxSwitches {
					h.chatCompletionsErrorResponse(c, 503, "no_available_group", "No compatible Chat target")
					return
				}
				fs.FailedAccountIDs[account.ID] = struct{}{}
				fs.SwitchCount++
				fs.LastFailoverErr = &service.UpstreamFailoverError{StatusCode: 503}
				pendingSwitchPermit = true
				continue
			}
		}
		var openAIResult *service.OpenAIForwardResult
		var result *service.ForwardResult
		setActualUpstreamEndpoint(c, "")
		if crossChat && account.Platform == service.PlatformOpenAI {
			openAIResult, err = h.openAIGatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, h.openAIGatewayService.ExtractSessionID(c, body), "")
		} else if account.Platform == service.PlatformGemini {
			if h.geminiCompatService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Gemini compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			result, err = h.geminiCompatService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody)
		} else if shouldUseAntigravityCompat(account) {
			if h.antigravityGatewayService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Antigravity compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
			result, err = h.antigravityGatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		} else {
			result, err = h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, parsedReq)
		}
		if attemptBudget.Used() > usedBeforeForward {
			if firstSentAccountID == 0 {
				firstSentAccountID = account.ID
			} else if firstSentAccountID != account.ID {
				sentDifferentAccount = true
			}
		}

		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}

		if openAIResult != nil {
			if err == nil {
				h.openAIGatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, openAIResult), true, openAIResult.FirstTokenMs)
			}
			stampOpenAIRequestedReasoningEffort(openAIResult, c)
			input := chatOpenAIUsageSnapshot(c, openAIResult, apiKey, subscription, account, reqModel, channelMapping, pricingAt, attemptBudget.Used(), sentDifferentAccount, h.apiKeyService)
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if e := h.openAIGatewayService.RecordUsage(ctx, input); e != nil {
					reqLog.Error("chat_bridge.record_usage_failed", zap.Error(e))
				}
			})
			if err != nil && !gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err) {
				h.ensureForwardErrorResponse(c, streamStarted)
			}
			return
		}
		if result != nil && err != nil {
			stampForwardRequestedReasoningEffort(result, service.RequestedReasoningEffortFromContext(c.Request.Context()))
			input := chatSharedUsageSnapshot(c, result, apiKey, subscription, account, reqModel, body, channelMapping, pricingAt, attemptBudget.Used(), sentDifferentAccount, h.apiKeyService)
			h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
				if e := h.gatewayService.RecordUsage(ctx, input); e != nil {
					reqLog.Error("chat_bridge.record_usage_failed", zap.Error(e))
				}
			})
			if !gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err) {
				h.ensureForwardErrorResponse(c, streamStarted)
			}
			return
		}
		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != semanticStart {
					h.handleCCFailoverExhausted(c, failoverErr, true)
					return
				}
				previousSwitchCount := fs.SwitchCount
				action := fs.HandleFailoverError(c.Request.Context(), h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
				pendingSwitchPermit = action == FailoverContinue && fs.SwitchCount > previousSwitchCount
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
					return
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
			}
			upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
			}
			reqLog.Error("gateway.cc.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			return
		}

		// 6. Record usage
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)
		stampForwardRequestedReasoningEffort(result, service.RequestedReasoningEffortFromContext(c.Request.Context()))
		usageFields := clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel)
		servedKey, servedSubscription := apiKey, subscription
		var routingUsage *service.RoutingUsageMetadata
		// Scope zero (truly ungrouped keys) has no positive group identity. Keep
		// its historical NULLs rather than invent IDs or fail successful billing.
		if lockedCC.active && requestBilling != nil {
			if requested := requestBilling.RequestedGroupID(); requested != nil && *requested > 0 && lockedCC.groupID > 0 {
				routingUsage = &service.RoutingUsageMetadata{
					RequestedGroupID: *requested,
					ServedGroupID:    lockedCC.groupID,
					Failover:         classifyGroupFailover(*requested, lockedCC.groupID, requestBilling.HAGroupIDs(), sentDifferentAccount),
					AttemptCount:     attemptBudget.Used(),
				}
			}
		}
		// All C6 scalars above are captured before the task is queued: the worker
		// must not read a later budget value, Gin context, or mutable group cursor.
		h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
				Result:             result,
				RoutingUsage:       routingUsage,
				QuotaPlatform:      quotaPlatform,
				APIKey:             servedKey,
				User:               servedKey.User,
				Account:            account,
				Subscription:       servedSubscription,
				PricingAt:          pricingAt,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      h.apiKeyService,
				SessionID:          sessionID,
				ChannelUsageFields: usageFields,
			}); err != nil {
				reqLog.Error("gateway.cc.record_usage_failed",
					zap.Int64("account_id", account.ID),
					zap.Error(err),
				)
			}
		})
		return
	}
}

// chatCompletionsErrorResponse writes an error in OpenAI Chat Completions format.
func (h *GatewayHandler) chatCompletionsErrorResponse(c *gin.Context, status int, errType, message string) {
	h.chatCompletionsErrorResponseWithCode(c, status, errType, "", message)
}

// chatCompletionsErrorResponseWithCode is chatCompletionsErrorResponse plus an
// optional protocol-level error code (C-7 / TS-02 budget exits use no_available_group).
func (h *GatewayHandler) chatCompletionsErrorResponseWithCode(c *gin.Context, status int, errType, code, message string) {
	errorObject := gin.H{"type": errType, "message": message}
	if code != "" {
		errorObject["code"] = code
	}
	c.JSON(status, gin.H{
		"error": errorObject,
	})
}

// handleCCFailoverExhausted writes a failover-exhausted error in CC format.
func (h *GatewayHandler) handleCCFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return
	}
	if lastErr != nil {
		copyFailoverRetryAfter(c, lastErr.ResponseHeaders)
	}
	if lastErr != nil && lastErr.Reason == service.RequestAttemptBudgetReason {
		h.chatCompletionsErrorResponseWithCode(c, http.StatusServiceUnavailable, "api_error", requestAttemptBudgetExhaustedCode, strings.TrimSpace(lastErr.ClientMessage))
		return
	}
	if lastErr != nil && lastErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(lastErr)
		h.chatCompletionsErrorResponse(c, status, "server_error", message)
		return
	}
	if lastErr != nil && lastErr.IsOpenAICapacityShed() && strings.TrimSpace(lastErr.ClientMessage) != "" {
		status := lastErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		h.chatCompletionsErrorResponse(c, status, "server_error", lastErr.ClientMessage)
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage())
		return
	}
	h.chatCompletionsErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
