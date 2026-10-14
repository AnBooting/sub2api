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

// Responses handles OpenAI Responses API endpoint for Anthropic platform groups.
// POST /v1/responses
// This converts Responses API requests to Anthropic format, forwards to Anthropic
// upstream, and converts responses back to Responses format.
func (h *GatewayHandler) Responses(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.responsesErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.responsesErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// Read request body
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.responsesErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	if len(body) == 0 {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	setOpsRequestContext(c, "", false)

	// Validate JSON
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	// Extract model and stream using gjson (like OpenAI handler)
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	bindRequestedReasoningEffort(c, body, reqModel)
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformResolved(c, apiKey, reqModel) {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by composite groups")
		return
	}
	if rejectSystemOneOnlyPlatform(c, apiKey, h.responsesErrorResponse) {
		return
	}
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.responsesErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))
	requestCtx := c.Request.Context()
	// 定价上下文无条件装配：/v1/responses 是 token 计费端点，声明生图工具的
	// 混合请求同样按 token 计费（外加图片部分），其 token 利润保护不因请求体
	// 里的任何工具声明（含 Codex 被动 image_gen namespace）而关闭。生图意图
	// 仅用于能力路由与图片计费；独立图片/视频端点才在利润门范围之外。
	requestCtx, pricingAt := service.WithGatewayTokenRequestPricing(requestCtx)
	if service.IsImageGenerationIntentForPlatform("/v1/responses", reqModel, body, openAICompatibleRequestPlatform(c.Request.Context(), apiKey)) {
		requestCtx = service.WithOpenAIImageGenerationIntent(requestCtx)
	}
	c.Request = c.Request.WithContext(requestCtx)

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(requestCtx, apiKey.GroupID, reqModel)

	// Claude Code only restriction: /v1/responses is never a Claude Code
	// endpoint. With a fallback group the request continues and account selection
	// (checkClaudeCodeRestriction) schedules it in the fallback group; without one
	// it is rejected here.
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly && apiKey.Group.FallbackGroupID == nil {
		h.responsesErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}

	// C-1 请求锁价（共享入口切片 / Anthropic 平台上的 /v1/responses）。
	// 位置在"协议/平台/参数"拒绝之后，与 Messages 参考入口一致：
	//   - 仅非 composite 分组启用（composite/HA 的成员分组绑定属 S5 边界）；
	//   - 本入口没有 warmup/intercept/mock 本机分支（全量走真实上游），因此只排除
	//     composite；
	//   - 其余请求的缺价模型在上游请求前即 503 pricing_unavailable；
	//   - 渠道映射以请求建立时冻结的 bundle 为准，覆盖上面解析出的 live 映射。
	// F7 修复：Antigravity 兼容出站按转换后的 Claude thinking 选档位变体
	// （antigravity_gateway_compat.go:215-227），冻结侧必须拿到同一档位，否则
	// reasoning.effort=medium + 裸 gemini 名时冻结值停在默认 high、实际出站是 medium
	// → 等值核对失败 → 用户 200 但整笔零计费。走与出站相同的转换链推导，保证同源。
	{
		thinkingEnabled, thinkingLevel := service.ResponsesThinkingPreferenceFromBody(body)
		c.Request = c.Request.WithContext(service.WithGatewayRequestThinkingPreference(
			c.Request.Context(), thinkingEnabled, thinkingLevel))
	}

	lockedResponsesEligible := apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite
	lockedResponses, lockedResponsesOK := prepareSharedGatewayLockedBillingWithErrorWriter(
		c, h.gatewayService, apiKey, reqModel, body, reqLog, lockedResponsesEligible, false, writeResponsesGatewayLockedBillingError,
	)
	if !lockedResponsesOK {
		return
	}
	channelMapping = lockedResponses.apply(channelMapping)
	// 锁价 helper 把 bundle 写进 c.Request 的 context；本入口后续以 requestCtx 变量
	// 传递上下文，必须同步，否则冻结 bundle 不会进入选号/Forward/worker。
	requestCtx = c.Request.Context()

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.responsesSecurityAuditError(c, decision)
		return
	}

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestBilling := service.GatewayRequestBillingFromContext(requestCtx)
	var normalIDs []int64
	var haIDs []int64
	if lockedResponses.active {
		normalIDs = requestBilling.NormalGroupIDs()
		// PRD-04: HA candidates are part of the same frozen bundle and are only
		// reachable after every normal candidate is exhausted on a retryable error.
		haIDs = requestBilling.HAGroupIDs()
	}
	groupPlan := newNormalGroupPlan(normalIDs, haIDs)
	groupChainActive := groupPlan.active()
	originalAPIKey := apiKey
	crossResponses := chatCrossFamilyChain(apiKey)
	groupIndex := 0
	if groupChainActive {
		var admitted bool
		apiKey, subscription, admitted = h.authorizeSharedResponsesGroup(c, originalAPIKey, normalIDs[0], normalIDs, body, crossResponses)
		if !admitted {
			return
		}
		requestCtx = sharedResponsesExecutionContext(requestCtx, apiKey)
		c.Request = c.Request.WithContext(requestCtx)
	}
	// Keep an unreserved base for switching financial identities. A nil reservation
	// does NOT mask a previous reservation in the context helper.
	reservationBase := requestCtx

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.responses.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	if err := h.billingCacheService.CheckBillingEligibility(requestCtx, apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(requestCtx, apiKey)); err != nil {
		reqLog.Info("gateway.responses.billing_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.responsesErrorResponse(c, status, code, message)
		return
	}

	// 余额模式在途预留：防止并发请求在预检时看到同一份余额而集体透支。
	inflightRelease, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, tokenInflightEstimate(reqModel, body))
	if err != nil {
		reqLog.Info("gateway.responses.inflight_reservation_rejected", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.responsesErrorResponse(c, status, code, message)
		return
	}
	defer func() { inflightRelease() }()
	requestCtx = c.Request.Context()

	// Parse request for session hash
	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "responses")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: bodyRef}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)

	// TS-02 S3b：Responses 文本入口在 failover 循环前挂载单逻辑请求预算。
	// clientDeadline 本期不读，传零值；预算耗尽由 HandleSelectionExhausted /
	// HandleFailoverError 统一走 503 + no_available_group。
	maxAttemptSends, budgetWindow := h.gatewayService.RequestAttemptBudgetLimits(requestCtx)
	budgetCtx, attemptBudget := service.EnsureRequestAttemptBudget(requestCtx, maxAttemptSends, budgetWindow, time.Time{})
	requestCtx = budgetCtx
	c.Request = c.Request.WithContext(requestCtx)

	// 3. Account selection + failover loop
	fs := NewFailoverState(h.maxAccountSwitches, false)
	reservationBase = service.WithRequestAttemptBudget(reservationBase, service.RequestAttemptBudgetFromContext(requestCtx))
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
		// PRD-04: entering the HA phase requires a retryable failure class. Normal
		// chain advancement keeps its existing behaviour.
		nextID, nextIsHA, hasNext := groupPlan.peek(fs.LastFailoverErr == nil || fs.LastFailoverErr.ShouldRetryNextAccount())
		if !hasNext {
			return normalGroupUnavailable
		}
		if failoverClientGone(c) {
			return normalGroupTerminal
		}
		if requestAttemptBudgetExhausted(requestCtx, "responses_group_advance") || service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != semanticStart {
			return normalGroupUnavailable
		}
		if fs.LastFailoverErr != nil && !fs.LastFailoverErr.ShouldRetryNextAccount() {
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
		nextKey, nextSub, admitted := h.authorizeSharedResponsesGroupCandidate(c, originalAPIKey, nextID, nextIsHA, normalIDs, haIDs, body, crossResponses)
		if !admitted {
			return normalGroupTerminal
		}
		nextMapping, mappingErr := requestBilling.MappingFor(nextID)
		if mappingErr != nil {
			writeResponsesGatewayLockedBillingError(c, reqLog, "gateway.responses.group_mapping_rejected", reqModel, mappingErr)
			return normalGroupTerminal
		}
		// No usage worker was submitted on the failed attempts. Release the old
		// handler reference once, then reserve with the destination billing identity.
		inflightRelease()
		inflightRelease = inflightNoop
		nextCtx := sharedResponsesExecutionContext(reservationBase, nextKey)
		if crossResponses && nextKey.Group.Platform == service.PlatformOpenAI {
			if h.openAIGatewayService == nil {
				h.responsesErrorResponse(c, 503, "no_available_group", "OpenAI service unavailable")
				return normalGroupTerminal
			}
			nextCtx, _ = h.openAIGatewayService.WithOpenAIRequestPricingContext(nextCtx, nextKey.GroupID)
		}
		c.Request = c.Request.WithContext(nextCtx)
		if eligibilityErr := h.billingCacheService.CheckFinancialEligibility(nextCtx, nextKey.User, nextKey, nextKey.Group, nextSub, service.QuotaPlatform(nextCtx, nextKey)); eligibilityErr != nil {
			status, code, message, _ := billingErrorDetails(eligibilityErr)
			h.responsesErrorResponse(c, status, code, message)
			return normalGroupTerminal
		}
		if rpmErr := h.billingCacheService.CheckGroupRPMEligibility(nextCtx, nextKey.User, nextKey.Group); rpmErr != nil {
			status, code, message, retryAfter := billingErrorDetails(rpmErr)
			if retryAfter > 0 {
				c.Header("Retry-After", strconv.Itoa(retryAfter))
			}
			h.responsesErrorResponse(c, status, code, message)
			return normalGroupTerminal
		}
		var reserveErr error
		estimator := inflightReservationEstimator(h.gatewayService)
		if crossResponses {
			estimator = chatTargetEstimator(nextKey.Group.Platform, h.gatewayService, h.openAIGatewayService)
		}
		inflightRelease, reserveErr = reserveInflightBalance(c, h.billingCacheService, estimator, nextKey, nextSub, tokenInflightEstimate(reqModel, body))
		if reserveErr != nil {
			status, code, message, _ := billingErrorDetails(reserveErr)
			h.responsesErrorResponse(c, status, code, message)
			return normalGroupTerminal
		}
		groupPlan.commit()
		groupIndex++
		apiKey, subscription, channelMapping = nextKey, nextSub, nextMapping
		lockedResponses.groupID, lockedResponses.mapping = nextID, nextMapping
		requestCtx = c.Request.Context()
		return normalGroupAdvanced
	}

	for {
		if failoverClientGone(c) {
			return
		}
		var selection *service.AccountSelectionResult
		var err error
		if crossResponses && apiKey.Group.Platform == service.PlatformOpenAI {
			previous := gjson.GetBytes(body, "previous_response_id").String()
			if previous != "" {
				owned, e := h.openAIGatewayService.ValidateOpenAIHTTPResponseOwner(requestCtx, derefGroupID(apiKey.GroupID), previous, apiKey.UserID, apiKey.ID)
				if e != nil || !owned {
					h.responsesErrorResponse(c, 400, "invalid_request_error", "previous_response_id is not available for this user")
					return
				}
			}
			service.SetOpenAIHTTPResponseOwner(c, apiKey.UserID, apiKey.ID)
			selection, _, err = h.openAIGatewayService.SelectAccountWithSchedulerForCapability(requestCtx, apiKey.GroupID, gjson.GetBytes(body, "previous_response_id").String(), sessionHash, openAIChannelForwardModel(channelMapping, reqModel), fs.FailedAccountIDs, service.OpenAIUpstreamTransportAny, service.OpenAIEndpointCapabilityResponses, isOpenAILegacyCompactPath(c), false, true, service.PlatformOpenAI)
		} else {
			selection, err = h.gatewayService.SelectAccountWithLoadAwareness(requestCtx, apiKey.GroupID, sessionHash, reqModel, fs.FailedAccountIDs, "", 0)
		}
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("gateway.responses.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, effectiveAPIKeyPlatform(c, apiKey))
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
				h.responsesErrorResponse(c, cls.Status, cls.ErrType, message)
				return
			}
			// Natural pool exhaustion is not the same as MaxSwitches exhaustion.
			// Only a previously granted retry permit can cross a group boundary.
			if errors.Is(err, service.ErrNoAvailableAccounts) && fs.LastFailoverErr != nil && fs.LastFailoverErr.ShouldRetryNextAccount() {
				switch advanceGroup() {
				case normalGroupAdvanced:
					continue
				case normalGroupTerminal:
					return
				}
			}
			action := fs.HandleSelectionExhausted(requestCtx)
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				failoverClientGone(c)
				return
			default:
				if fs.LastFailoverErr != nil {
					h.handleResponsesFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
				} else {
					h.responsesErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
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
				h.responsesErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
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
				reqLog.Warn("gateway.responses.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
		}
		// 终检与准入后绑定必须使用选号结果携带的门：门安装在调度栈的局部
		// ctx 上（composite/fallback 还可能解析出与入口分组不同的门），直接用
		// requestCtx 会退化为空操作。
		admissionCtx := service.ContextWithSelectionProfitGate(requestCtx, selection)
		var latest *service.Account
		var vetoed bool
		var reason string
		if crossResponses && account.Platform == service.PlatformOpenAI {
			latest, vetoed, reason = h.openAIGatewayService.ProfitControlVetoLatest(admissionCtx, account)
		} else {
			latest, vetoed, reason = h.gatewayService.GatewayProfitControlVetoLatest(admissionCtx, account)
		}
		if vetoed {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			reqLog.Debug("gateway.responses.account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			if fs.RecordProfitVeto(account.ID) == FailoverExhausted {
				reqLog.Warn("gateway.responses.profit_veto_attempts_exhausted", zap.Int("profit_veto_count", fs.ProfitVetoCount()))
				h.responsesErrorResponse(c, http.StatusServiceUnavailable, "api_error", profitVetoExhaustedMessage)
				return
			}
			continue
		}
		account = latest
		selection.Account = latest
		if selection.ProfitGateActive() && account.Platform != service.PlatformOpenAI {
			if err := h.gatewayService.BindStickySessionAfterProfitAdmission(admissionCtx, apiKey.GroupID, sessionHash, account.ID); err != nil {
				reqLog.Warn("gateway.responses.bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		if selection.ProfitGateActive() && account.Platform == service.PlatformOpenAI {
			if e := h.openAIGatewayService.BindStickySessionAfterProfitAdmission(admissionCtx, apiKey.GroupID, sessionHash, account.ID); e != nil {
				reqLog.Warn("responses_bridge.sticky_bind_failed", zap.Error(e))
			}
		}
		// C-1 锁价绑定：账号终检之后、Forward 之前把实际执行账号写入冻结 bundle。
		// 绑定失败（账号不在冻结映射表内/冻结价缺失）即 503，不转发、不扣费；
		// failover 换号后重新绑定，价格仍取自同一份请求级 bundle。
		if bound, bindErr := bindSharedGatewayLockedBillingAccount(c, lockedResponses, derefGroupID(apiKey.GroupID), account.ID, reqModel, reqLog); !bound {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			status, code, message, _ := billingErrorDetails(bindErr)
			h.responsesErrorResponse(c, status, code, message)
			return
		}
		// 绑定同样只写 c.Request；本入口 Forward/worker 走 requestCtx 变量，必须再同步
		// 一次，否则冻结的账号执行事实传不到转发与计费消费层。
		requestCtx = c.Request.Context()
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		pendingSwitchPermit = false
		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		usedBeforeForward := attemptBudget.Used()
		if crossResponses {
			decision := responsesBridgeAssess(requestCtx, derefGroupID(apiKey.GroupID), account, forwardBody, isOpenAILegacyCompactPath(c), isOpenAIRemoteCompactionV2Request(body), h.openAIGatewayService)
			if decision.Class != chatBridgeCompatible {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				if decision.Class == chatBridgeIllegal {
					h.responsesErrorResponse(c, 400, "invalid_request_error", decision.Reason)
					return
				}
				if fs.SwitchCount >= fs.MaxSwitches {
					h.responsesErrorResponse(c, 503, "no_available_group", "No compatible Responses target")
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
		if crossResponses && account.Platform == service.PlatformOpenAI {
			openAIResult, err = h.openAIGatewayService.Forward(requestCtx, c, account, forwardBody)
		} else if shouldUseAntigravityCompat(account) {
			if h.antigravityGatewayService == nil {
				h.responsesErrorResponse(c, http.StatusBadGateway, "upstream_error", "Antigravity compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
			setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
			result, err = h.antigravityGatewayService.ForwardAsResponses(requestCtx, c, account, forwardBody, parsedReq)
		} else {
			result, err = h.gatewayService.ForwardAsResponses(requestCtx, c, account, forwardBody, parsedReq)
		}
		// Count identities only after a real send. Selection, preflight rejection and
		// exhausted-budget attempts cannot fabricate account failover. Hidden
		// same-account retries increase Used(), not the number of identities.
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
			stampOpenAIRequestedReasoningEffort(openAIResult, c)
			input := chatOpenAIUsageSnapshot(c, openAIResult, apiKey, subscription, account, reqModel, channelMapping, pricingAt, attemptBudget.Used(), sentDifferentAccount, h.apiKeyService)
			input.NativeCompactionV2 = isOpenAIRemoteCompactionV2Request(body)
			input.RequestPayloadHash = service.HashUsageRequestPayload(body)
			h.submitUsageRecordTask(requestCtx, func(ctx context.Context) {
				if e := h.openAIGatewayService.RecordUsage(ctx, input); e != nil {
					reqLog.Error("responses_bridge.record_usage_failed", zap.Error(e))
				}
			})
			if err == nil {
				h.openAIGatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, reqModel, false, openAIResult), true, openAIResult.FirstTokenMs)
			} else if !gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err) {
				h.ensureForwardErrorResponse(c, streamStarted)
			}
			return
		}
		submitSharedResponsesUsage := func() {
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
			if lockedResponses.active && requestBilling != nil {
				if requested := requestBilling.RequestedGroupID(); requested != nil && *requested > 0 && lockedResponses.groupID > 0 {
					failover := service.RoutingFailoverNone
					if *requested != lockedResponses.groupID {
						failover = service.RoutingFailoverGroup
					} else if sentDifferentAccount {
						failover = service.RoutingFailoverAccount
					}
					routingUsage = &service.RoutingUsageMetadata{RequestedGroupID: *requested, ServedGroupID: lockedResponses.groupID, Failover: failover, AttemptCount: attemptBudget.Used()}
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
					reqLog.Error("gateway.responses.record_usage_failed",
						zap.Int64("account_id", account.ID),
						zap.Error(err),
					)
				}
			})
		}
		if result != nil {
			submitSharedResponsesUsage()
			if err != nil && !gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err) {
				h.ensureForwardErrorResponse(c, streamStarted)
			}
			return
		}

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				// Can't failover if streaming content already sent
				if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != semanticStart {
					h.handleResponsesFailoverExhausted(c, failoverErr, true)
					return
				}
				previousSwitchCount := fs.SwitchCount
				action := fs.HandleFailoverError(requestCtx, h.gatewayService, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
				pendingSwitchPermit = action == FailoverContinue && fs.SwitchCount > previousSwitchCount
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					h.handleResponsesFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
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
			reqLog.Error("gateway.responses.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			return
		}

		return
	}
}

// responsesErrorResponse writes an error in OpenAI Responses API format.
func (h *GatewayHandler) responsesErrorResponse(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

// handleResponsesFailoverExhausted writes a failover-exhausted error in Responses format.
func (h *GatewayHandler) handleResponsesFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if lastErr != nil {
		copyFailoverRetryAfter(c, lastErr.ResponseHeaders)
	}
	if lastErr != nil && lastErr.Reason == service.RequestAttemptBudgetReason {
		if streamStarted {
			service.MarkOpsStreamError(c, requestAttemptBudgetExhaustedCode, strings.TrimSpace(lastErr.ClientMessage), http.StatusServiceUnavailable)
			if c != nil && c.Writer != nil && (c.Writer.Size() <= 0 || gatewayStreamHasOnlyHeartbeats(c)) {
				writeResponsesFailedSSE(c, requestAttemptBudgetExhaustedCode, "", strings.TrimSpace(lastErr.ClientMessage))
			}
		} else {
			h.responsesErrorResponse(c, http.StatusServiceUnavailable, requestAttemptBudgetExhaustedCode, strings.TrimSpace(lastErr.ClientMessage))
		}
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	status, code, message := statusCode, "server_error", "All available accounts exhausted"
	if lastErr != nil && lastErr.IsCredentialFailure() {
		status, message = credentialFailoverClientResponse(lastErr)
	} else if lastErr != nil && lastErr.IsOpenAICapacityShed() && strings.TrimSpace(lastErr.ClientMessage) != "" {
		status = lastErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		message = lastErr.ClientMessage
	} else if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		status, code, message = http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage()
	} else if lastErr != nil && statusCode == http.StatusTooManyRequests {
		status, code, message = http.StatusTooManyRequests, "rate_limit_error", "All available accounts are currently rate-limited. Please retry later."
	}
	if streamStarted {
		// A slot-wait heartbeat commits HTTP 200 before any upstream response.
		// In that case a terminal frame is still required; once any semantic or
		// official terminal bytes exist, preserve them without appending a second
		// generic response.failed.
		service.MarkOpsStreamError(c, code, message, status)
		if c != nil && c.Writer != nil && (c.Writer.Size() <= 0 || gatewayStreamHasOnlyHeartbeats(c)) {
			writeResponsesFailedSSE(c, code, "", message)
		}
		return
	}
	h.responsesErrorResponse(c, status, code, message)
}
