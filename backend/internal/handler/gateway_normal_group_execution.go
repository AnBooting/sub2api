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

// normalGroupPlan walks this request's frozen normal group chain first and only
// then the user-authorized HA candidates. Both lists come from the original
// request billing bundle and are never expanded or reordered here.
//
// PRD-04: HA is reachable ONLY after every normal candidate is exhausted AND the
// caller reports a retryable (site-side recoverable) failure. A client error,
// insufficient balance, policy rejection or uncertain execution never opens HA.
type normalGroupPlan struct {
	normalIDs []int64
	haIDs     []int64
	index     int
}

func newNormalGroupPlan(normalIDs, haIDs []int64) *normalGroupPlan {
	return &normalGroupPlan{normalIDs: append([]int64(nil), normalIDs...), haIDs: append([]int64(nil), haIDs...)}
}

// active reports whether this request has any group boundary to cross at all.
func (p *normalGroupPlan) active() bool {
	if p == nil {
		return false
	}
	return len(p.normalIDs)+len(p.haIDs) > 1
}

// current returns the serving group id and whether it is an HA candidate.
func (p *normalGroupPlan) current() (int64, bool) {
	if p == nil || p.index >= len(p.normalIDs)+len(p.haIDs) {
		return 0, false
	}
	if p.index < len(p.normalIDs) {
		return p.normalIDs[p.index], false
	}
	return p.haIDs[p.index-len(p.normalIDs)], true
}

// peek returns the next candidate WITHOUT consuming it. Callers run every guard
// first and only then call commit, so a rejected attempt never skips a candidate.
// retryable must be true to cross from the normal chain into the HA phase.
func (p *normalGroupPlan) peek(retryable bool) (int64, bool, bool) {
	if p == nil {
		return 0, false, false
	}
	next := p.index + 1
	if next >= len(p.normalIDs)+len(p.haIDs) {
		return 0, false, false
	}
	// Crossing from the normal chain into HA requires a retryable failure class.
	if next >= len(p.normalIDs) && !retryable {
		return 0, false, false
	}
	if next < len(p.normalIDs) {
		return p.normalIDs[next], false, true
	}
	return p.haIDs[next-len(p.normalIDs)], true, true
}

// commit consumes the candidate returned by the matching peek.
func (p *normalGroupPlan) commit() {
	if p == nil {
		return
	}
	if p.index < len(p.normalIDs)+len(p.haIDs) {
		p.index++
	}
}

// authorizeSharedResponsesGroup checks live execution policy without refreshing
// the immutable request pricing bundle. Only shared Responses forwarding families
// are supported by this thin slice; other families must not be mis-forwarded.
func (h *GatewayHandler) authorizeSharedResponsesGroup(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte, crossFamily ...bool) (*service.APIKey, *service.UserSubscription, bool) {
	if len(crossFamily) > 0 && crossFamily[0] {
		return authorizeChatExecutionGroup(c, h.apiKeyService, h.subscriptionService, original, groupID, normalIDs, gjson.GetBytes(body, "model").String(), func(status int, code, msg string) { h.responsesErrorResponse(c, status, code, msg) }, true)
	}
	return h.authorizeSharedTextGroup(c, original, groupID, normalIDs, body, false, h.responsesErrorResponse)
}

// authorizeSharedResponsesGroupCandidate routes to the normal or HA gate depending
// on which phase of the frozen candidate plan this group came from. Every
// platform/model/subscription policy check is shared, so HA cannot bypass them.
func (h *GatewayHandler) authorizeSharedResponsesGroupCandidate(c *gin.Context, original *service.APIKey, groupID int64, isHA bool, normalIDs, haIDs []int64, body []byte, crossFamily ...bool) (*service.APIKey, *service.UserSubscription, bool) {
	if !isHA {
		return h.authorizeSharedResponsesGroup(c, original, groupID, normalIDs, body, crossFamily...)
	}
	if len(crossFamily) > 0 && crossFamily[0] {
		return authorizeChatExecutionGroupCandidate(c, h.apiKeyService, h.subscriptionService, original, groupID, haIDs, gjson.GetBytes(body, "model").String(), func(status int, code, msg string) { h.responsesErrorResponse(c, status, code, msg) }, true, true)
	}
	return h.authorizeSharedTextGroupPolicyWith(c, original, groupID, normalIDs, haIDs, true, body, false, false, h.responsesErrorResponse)
}

func (h *GatewayHandler) authorizeSharedTextGroup(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte, chat bool, writeError func(*gin.Context, int, string, string), nativeModel ...string) (*service.APIKey, *service.UserSubscription, bool) {
	return h.authorizeSharedTextGroupPolicy(c, original, groupID, normalIDs, body, chat, false, writeError, nativeModel...)
}

func (h *GatewayHandler) authorizeSharedMessagesGroup(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte) (*service.APIKey, *service.UserSubscription, bool) {
	return h.authorizeSharedTextGroupPolicy(c, original, groupID, normalIDs, body, false, true, h.errorResponse)
}

func (h *GatewayHandler) authorizeSharedTextGroupPolicy(c *gin.Context, original *service.APIKey, groupID int64, normalIDs []int64, body []byte, chat, messages bool, writeError func(*gin.Context, int, string, string), nativeModel ...string) (*service.APIKey, *service.UserSubscription, bool) {
	return h.authorizeSharedTextGroupPolicyWith(c, original, groupID, normalIDs, nil, false, body, chat, messages, writeError, nativeModel...)
}

// authorizeSharedTextGroupPolicyWith is the single policy surface for both normal
// and user-authorized HA candidates. isHA selects the HA authorization gate; every
// platform, model-allowlist, Claude-Code and subscription-window check below is
// identical for both, so a fallback group cannot bypass them.
func (h *GatewayHandler) authorizeSharedTextGroupPolicyWith(c *gin.Context, original *service.APIKey, groupID int64, normalIDs, haIDs []int64, isHA bool, body []byte, chat, messages bool, writeError func(*gin.Context, int, string, string), nativeModel ...string) (*service.APIKey, *service.UserSubscription, bool) {
	fail := func(err error) (*service.APIKey, *service.UserSubscription, bool) {
		status, code, message, _ := billingErrorDetails(err)
		writeError(c, status, code, message)
		return nil, nil, false
	}
	if h.apiKeyService == nil {
		return fail(service.ErrBillingServiceUnavailable)
	}
	var key *service.APIKey
	var sub *service.UserSubscription
	var err error
	if isHA {
		key, sub, err = h.apiKeyService.AuthorizeHAGroupExecution(c.Request.Context(), original.ID, original.UserID, groupID, haIDs)
	} else {
		key, sub, err = h.apiKeyService.AuthorizeNormalGroupExecution(c.Request.Context(), original.ID, original.UserID, groupID, normalIDs)
	}
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
