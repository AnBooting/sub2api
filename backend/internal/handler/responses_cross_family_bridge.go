package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Inspect canonical input before any account-specific attempt transformation.
func responsesBridgeAssess(ctx context.Context, groupID int64, account *service.Account, canonical []byte, legacy, native bool, openAI *service.OpenAIGatewayService) chatBridgeCapabilityDecision {
	no := func(field, reason string) chatBridgeCapabilityDecision {
		return chatBridgeDecision(chatBridgeUnsupported, field, reason)
	}
	b := service.GatewayRequestBillingFromContext(ctx)
	if b == nil {
		return no("model", "missing frozen pricing")
	}
	model, err := b.UpstreamModelFor(groupID, account.ID)
	if err != nil {
		return no("model", "missing frozen account model")
	}
	route := responsesBridgeRouteFlags{LegacyCompact: legacy, NativeV2: native}
	protocol := chatBridgeAnthropic
	if account.Platform == service.PlatformOpenAI && account.Type == service.AccountTypeAPIKey {
		if openAI == nil {
			return no("protocol", "service unavailable")
		}
		actual, e := openAI.ResponsesUpstreamProtocol(ctx, account, canonical)
		if e != nil {
			return no("model", "unbound frozen account")
		}
		switch actual {
		case service.APIProtocolResponses:
			protocol = chatBridgeResponses
			route.SupportsResponses = true
			route.SupportsLegacyCompact = account.AllowsOpenAICompact()
			route.SupportsNativeV2 = true
			route.HTTPContinuationSupported = true
		case service.APIProtocolAnthropic:
			protocol = chatBridgeAnthropic
		default:
			protocol = chatBridgeRawChat
		}
	} else if account.Platform != service.PlatformAnthropic || account.Type != service.AccountTypeAPIKey {
		return no("protocol", "unsupported account family")
	}
	return assessResponsesBridgeCapability(canonical, model, protocol, route)
}
