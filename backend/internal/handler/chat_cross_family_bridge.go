package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"time"
)

// chatBridgeAcquire owns one attempt, never a retry loop.
func chatBridgeAcquire(c *gin.Context, helper *ConcurrencyHelper, shared *service.GatewayService, openAI *service.OpenAIGatewayService, selection *service.AccountSelectionResult, stream bool, started *bool) (func(), bool, error) {
	release := selection.ReleaseFunc
	if !selection.Acquired {
		if selection.WaitPlan == nil {
			return nil, false, service.ErrNoAvailableAccounts
		}
		var err error
		release, err = helper.AcquireAccountSlotWithWaitTimeout(c, selection.Account.ID, selection.WaitPlan.MaxConcurrency, selection.WaitPlan.Timeout, stream, started)
		if err != nil {
			return nil, false, err
		}
	}
	ctx := service.ContextWithSelectionProfitGate(c.Request.Context(), selection)
	var latest *service.Account
	var veto bool
	if selection.Account.Platform == service.PlatformOpenAI {
		latest, veto, _ = openAI.ProfitControlVetoLatest(ctx, selection.Account)
	} else {
		latest, veto, _ = shared.GatewayProfitControlVetoLatest(ctx, selection.Account)
	}
	if veto || latest == nil {
		if release != nil {
			release()
		}
		return nil, true, nil
	}
	selection.Account = latest
	return wrapReleaseOnDone(c.Request.Context(), release), false, nil
}
func chatBridgeRouting(ctx context.Context, key *service.APIKey, used int, different bool) *service.RoutingUsageMetadata {
	b := service.GatewayRequestBillingFromContext(ctx)
	if b == nil || b.RequestedGroupID() == nil {
		return nil
	}
	kind := service.RoutingFailoverNone
	requested := *b.RequestedGroupID()
	served := derefGroupID(key.GroupID)
	if requested != served {
		kind = service.RoutingFailoverGroup
	} else if different {
		kind = service.RoutingFailoverAccount
	}
	return &service.RoutingUsageMetadata{RequestedGroupID: requested, ServedGroupID: served, Failover: kind, AttemptCount: used}
}
func chatSharedUsageSnapshot(c *gin.Context, result *service.ForwardResult, key *service.APIKey, sub *service.UserSubscription, account *service.Account, model string, body []byte, mapping service.ChannelMappingResult, pricingAt time.Time, used int, different bool, keys *service.APIKeyService) *service.RecordUsageInput {
	return &service.RecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, Subscription: sub, PricingAt: pricingAt, RoutingUsage: chatBridgeRouting(c.Request.Context(), key, used, different), QuotaPlatform: service.QuotaPlatform(c.Request.Context(), key), InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: GetUpstreamEndpoint(c, account.Platform), UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c), RequestPayloadHash: service.HashUsageRequestPayload(body), APIKeyService: keys, SessionID: service.ExtractClientSessionID(c), ChannelUsageFields: clientRequestedUsageFields(c, mapping, model, result.UpstreamModel)}
}

func chatOpenAIUsageSnapshot(c *gin.Context, result *service.OpenAIForwardResult, key *service.APIKey, sub *service.UserSubscription, account *service.Account, model string, mapping service.ChannelMappingResult, pricingAt time.Time, used int, different bool, keys *service.APIKeyService) *service.OpenAIRecordUsageInput {
	return &service.OpenAIRecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, Subscription: sub, PricingAt: pricingAt, RoutingUsage: chatBridgeRouting(c.Request.Context(), key, used, different), QuotaPlatform: service.QuotaPlatform(c.Request.Context(), key), InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: resolveOpenAIUpstreamEndpoint(c, account, result), UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c), APIKeyService: keys, SessionID: service.ExtractClientSessionID(c), ChannelUsageFields: clientRequestedUsageFields(c, mapping, model, result.UpstreamModel), CyberBlocked: service.GetOpsCyberPolicy(c) != nil}
}

// Only an explicit normal chain spanning these families enables the bridge.
func chatCrossFamilyChain(key *service.APIKey) bool {
	if key == nil {
		return false
	}
	openAI, anthropic := false, false
	for _, item := range key.Groups {
		if item.Role != "normal" || item.Group == nil {
			continue
		}
		switch item.Group.Platform {
		case service.PlatformOpenAI:
			openAI = true
		case service.PlatformAnthropic:
			anthropic = true
		}
	}
	return openAI && anthropic
}
func chatTargetEstimator(platform string, shared *service.GatewayService, openAI *service.OpenAIGatewayService) inflightReservationEstimator {
	if platform == service.PlatformAnthropic {
		return shared
	}
	return openAI
}
func chatBridgeAssess(ctx context.Context, groupID int64, account *service.Account, body []byte, defaultMappedModel string, openAI *service.OpenAIGatewayService) chatBridgeCapabilityDecision {
	bundle := service.GatewayRequestBillingFromContext(ctx)
	if bundle == nil {
		return chatBridgeDecision(chatBridgeUnsupported, "model", "missing frozen pricing")
	}
	// Account binding has already validated group/model before this preflight.
	model, err := bundle.UpstreamModelFor(groupID, account.ID)
	if err != nil {
		return chatBridgeDecision(chatBridgeUnsupported, "model", "missing frozen account model")
	}
	protocol := chatBridgeAnthropic
	if account.Platform == service.PlatformOpenAI {
		if openAI == nil {
			return chatBridgeDecision(chatBridgeUnsupported, "protocol", "service unavailable")
		}
		switch openAI.ChatUpstreamProtocol(ctx, account, body, defaultMappedModel) {
		case service.APIProtocolChatCompletions:
			protocol = chatBridgeRawChat
		case service.APIProtocolResponses:
			protocol = chatBridgeResponses
		case service.APIProtocolAnthropic:
			protocol = chatBridgeAnthropic
		default:
			return chatBridgeDecision(chatBridgeUnsupported, "protocol", "unsupported protocol")
		}
	} else if account.Platform != service.PlatformAnthropic || account.Type != service.AccountTypeAPIKey {
		return chatBridgeDecision(chatBridgeUnsupported, "protocol", "unsupported account family")
	}
	return assessChatBridgeCapability(body, model, protocol)
}
