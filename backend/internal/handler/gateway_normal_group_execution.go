package handler

import (
	"context"
	"github.com/tidwall/gjson"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// normalGroupAdvance distinguishes a terminal admission response from no remaining
// candidate. It never interprets an account retry-limit exit as pool exhaustion.
type normalGroupAdvance uint8

const (
	normalGroupUnavailable normalGroupAdvance = iota
	normalGroupAdvanced
	normalGroupTerminal
)

// authorizeSharedResponsesGroup checks live execution policy without refreshing
// the immutable request pricing bundle. Only shared Responses forwarding families
// are supported by this thin slice; other families must not be mis-forwarded.
func (h *GatewayHandler) authorizeSharedResponsesGroup(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte, crossFamily ...bool) (*service.APIKey, *service.UserSubscription, bool) {
	if len(crossFamily) > 0 && crossFamily[0] {
		return authorizeChatExecutionGroup(c, h.apiKeyService, h.subscriptionService, original, groupID, normalIDs, gjson.GetBytes(body, "model").String(), func(status int, code, msg string) { h.responsesErrorResponse(c, status, code, msg) }, true)
	}
	return h.authorizeSharedTextGroup(c, original, groupID, normalIDs, body, false, h.responsesErrorResponse)
}

func (h *GatewayHandler) authorizeSharedTextGroup(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte, chat bool, writeError func(*gin.Context, int, string, string), nativeModel ...string) (*service.APIKey, *service.UserSubscription, bool) {
	return h.authorizeSharedTextGroupPolicy(c, original, groupID, normalIDs, body, chat, false, writeError, nativeModel...)
}

func (h *GatewayHandler) authorizeSharedMessagesGroup(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte) (*service.APIKey, *service.UserSubscription, bool) {
	return h.authorizeSharedTextGroupPolicy(c, original, groupID, normalIDs, body, false, true, h.errorResponse)
}

func (h *GatewayHandler) authorizeSharedTextGroupPolicy(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte, chat, messages bool, writeError func(*gin.Context, int, string, string), nativeModel ...string) (*service.APIKey, *service.UserSubscription, bool) {
	fail := func(err error) (*service.APIKey, *service.UserSubscription, bool) {
		status, code, message, _ := billingErrorDetails(err)
		writeError(c, status, code, message)
		return nil, nil, false
	}
	if h.apiKeyService == nil {
		return fail(service.ErrBillingServiceUnavailable)
	}
	key, sub, err := h.apiKeyService.AuthorizeNormalGroupExecution(c.Request.Context(), original.ID, original.UserID, groupID, normalIDs)
	if err != nil {
		return fail(err)
	}
	if len(nativeModel) > 0 && key.Group.Platform != service.PlatformGemini && key.Group.Platform != service.PlatformAntigravity {
		writeError(c, http.StatusServiceUnavailable, "no_available_group", "Group does not support Gemini native requests")
		return nil, nil, false
	}
	if len(nativeModel) == 0 && key.Group.Platform != service.PlatformAnthropic && key.Group.Platform != service.PlatformAntigravity && !((chat || messages) && key.Group.Platform == service.PlatformGemini) {
		writeError(c, http.StatusServiceUnavailable, "no_available_group", "This group forwarding family is not supported by the requested endpoint")
		return nil, nil, false
	}
	// Never let the scheduler silently substitute a Claude-Code-only fallback group
	// outside this request's explicit, frozen candidate chain.
	if key.Group.ClaudeCodeOnly && !(messages && service.IsClaudeCodeClient(c.Request.Context())) {
		writeError(c, http.StatusForbidden, "permission_error", "This group is restricted to Claude Code clients (/v1/messages only)")
		return nil, nil, false
	}
	path := "/v1/responses"
	if chat {
		path = "/v1/chat/completions"
	}
	if messages {
		path = "/v1/messages"
	}
	models := requestmodel.FromBodyCandidates(path, "application/json", body)
	if len(nativeModel) > 0 {
		models = nativeModel
	}
	for _, model := range models {
		if !key.Group.ModelAllowlist.Allows(model) {
			writeError(c, http.StatusNotFound, "model_not_allowed", "Model is not available for this group")
			return nil, nil, false
		}
	}
	if sub != nil {
		if h.subscriptionService == nil {
			return fail(service.ErrBillingServiceUnavailable)
		}
		maintenance, validationErr := h.subscriptionService.ValidateAndCheckLimits(sub, key.Group)
		if validationErr != nil {
			return fail(validationErr)
		}
		if maintenance {
			sub, err = h.subscriptionService.EnsureWindowMaintenance(c.Request.Context(), sub)
			if err != nil {
				return fail(err)
			}
			if sub == nil || sub.UserID != key.UserID || sub.GroupID != key.Group.ID {
				return fail(service.ErrSubscriptionInvalid)
			}
			_, validationErr = h.subscriptionService.ValidateAndCheckLimits(sub, key.Group)
		}
		if validationErr != nil {
			return fail(validationErr)
		}
	}
	return key, sub, true
}

func sharedResponsesExecutionContext(base context.Context, key *service.APIKey) context.Context {
	ctx := context.WithValue(base, ctxkey.Group, key.Group)
	ctx = context.WithValue(ctx, ctxkey.ResolvedTargetPlatform, "")
	return context.WithValue(ctx, ctxkey.ForcePlatform, "")
}
