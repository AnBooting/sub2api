package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

// authorizeOpenAIResponsesGroup reloads execution policy without refreezing prices.
func (h *OpenAIGatewayHandler) authorizeOpenAIResponsesGroup(c *gin.Context, original *service.APIKey, id int64, normalIDs []int64, model string, streamStarted bool, crossFamily ...bool) (*service.APIKey, *service.UserSubscription, bool) {
	return authorizeChatExecutionGroup(c, h.apiKeyService, h.subscriptionService, original, id, normalIDs, model, func(status int, code, msg string) { h.handleStreamingAwareError(c, status, code, msg, streamStarted) }, len(crossFamily) > 0 && crossFamily[0])
}

func authorizeChatExecutionGroup(c *gin.Context, keys *service.APIKeyService, subscriptions *service.SubscriptionService, original *service.APIKey, id int64, normalIDs []int64, model string, writeError func(int, string, string), chat bool) (*service.APIKey, *service.UserSubscription, bool) {
	fail := func(err error) (*service.APIKey, *service.UserSubscription, bool) {
		status, code, message, _ := billingErrorDetails(err)
		writeError(status, code, message)
		return nil, nil, false
	}
	if keys == nil {
		return fail(service.ErrBillingServiceUnavailable)
	}
	key, sub, err := keys.AuthorizeNormalGroupExecution(c.Request.Context(), original.ID, original.UserID, id, normalIDs)
	if err != nil {
		return fail(err)
	}
	if key.Group.Platform != service.PlatformOpenAI && !(chat && key.Group.Platform == service.PlatformAnthropic) {
		writeError(http.StatusServiceUnavailable, "no_available_group", "This forwarding family is not supported by the requested endpoint")
		return nil, nil, false
	}
	if key.Group.ClaudeCodeOnly {
		writeError(http.StatusForbidden, "permission_error", "This group is restricted to Claude Code clients")
		return nil, nil, false
	}
	if !key.Group.ModelAllowlist.Allows(model) {
		writeError(http.StatusNotFound, "model_not_allowed", "Model is not available for this group")
		return nil, nil, false
	}
	if sub != nil {
		if subscriptions == nil {
			return fail(service.ErrBillingServiceUnavailable)
		}
		maintenance, e := subscriptions.ValidateAndCheckLimits(sub, key.Group)
		if e != nil {
			return fail(e)
		}
		if maintenance {
			sub, e = subscriptions.EnsureWindowMaintenance(c.Request.Context(), sub)
			if e != nil {
				return fail(e)
			}
			if sub == nil || sub.UserID != key.UserID || sub.GroupID != key.Group.ID {
				return fail(service.ErrSubscriptionInvalid)
			}
			_, e = subscriptions.ValidateAndCheckLimits(sub, key.Group)
			if e != nil {
				return fail(e)
			}
		}
	}
	return key, sub, true
}
