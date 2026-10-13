package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// gatewayLockedBilling 是共享网关入口（Anthropic /v1/messages，以及后续 Chat
// Completions / Gemini 切片）的锁价准入结果。
type gatewayLockedBilling struct {
	// active 为 true 表示本次请求已进入锁价路径：ctx 上已有冻结 bundle，
	// 渠道映射与账号绑定事实必须来自该 bundle，而不是 live 查询。
	active bool
	// mapping 是请求建立时冻结的渠道映射（BillingModelSource/ChannelID/映射模型）。
	mapping service.ChannelMappingResult
	// groupID 是冻结候选分组的键，绑定/消费事实与该键一致。
	groupID int64
}

// prepareSharedGatewayLockedBilling 冻结一次共享网关请求的定价与渠道映射。
//
// 语义（与 OpenAI Responses 的 lockedText 准入同源）：
//   - eligible=false（composite / 本机 mock 拦截等不产生上游发送与扣费的转发分支）
//     时返回 active=false，调用方保持旧 live 行为；
//   - 其余分组的缺价模型必须在上游请求前拒绝（ErrModelPricingUnavailable →
//     503 pricing_unavailable），不是零价放行、也不回落兜底价；
//   - 失败时本函数已经写好错误响应并返回 ok=false，调用方直接 return。
//
// apiKey/model/body 由调用方给出（body 用于 token 估算），因此后续 Chat
// Completions / Gemini 入口可复用同一 helper，无需复制 Prepare+Validate+
// MappingFor+错误映射。
func prepareSharedGatewayLockedBilling(
	c *gin.Context,
	gatewayService *service.GatewayService,
	apiKey *service.APIKey,
	model string,
	body []byte,
	reqLog *zap.Logger,
	eligible bool,
) (gatewayLockedBilling, bool) {
	if !eligible || c == nil || c.Request == nil || gatewayService == nil || apiKey == nil {
		return gatewayLockedBilling{}, true
	}
	priceRequest := tokenInflightEstimate(model, body)
	prepared, requestBilling, priceErr := gatewayService.PrepareRequestBilling(c.Request.Context(), apiKey, priceRequest)
	if priceErr == nil {
		priceErr = gatewayService.ValidateRequestPricing(prepared, apiKey, priceRequest)
	}
	if priceErr != nil {
		return gatewayLockedBilling{}, writeSharedGatewayLockedBillingError(c, reqLog, "gateway.locked_pricing_rejected", model, priceErr)
	}
	groupID := int64(0)
	if apiKey.GroupID != nil {
		groupID = *apiKey.GroupID
	}
	mapping, mappingErr := requestBilling.MappingFor(groupID)
	if mappingErr != nil {
		return gatewayLockedBilling{}, writeSharedGatewayLockedBillingError(c, reqLog, "gateway.locked_pricing_mapping_rejected", model, mappingErr)
	}
	c.Request = c.Request.WithContext(prepared)
	return gatewayLockedBilling{active: true, mapping: mapping, groupID: groupID}, true
}

// refreezeSharedGatewayLockedBilling 为"同请求内显式切换分组"重新冻结定价与映射。
//
// Messages 的 prompt-too-long fallback 会切到另一个分组：旧 bundle 的候选分组里
// 根本没有 fallback 分组，继续复用它会让绑定失败（或更糟，绑到旧价）。因此这里
// 必须走 RefreezeRequestBilling 重新取价，而不是复用旧 bundle。
//
// 返回值 ok=false 时本函数已写好错误响应。
func refreezeSharedGatewayLockedBilling(
	c *gin.Context,
	gatewayService *service.GatewayService,
	apiKey *service.APIKey,
	model string,
	body []byte,
	reqLog *zap.Logger,
) (gatewayLockedBilling, bool) {
	if c == nil || c.Request == nil || gatewayService == nil || apiKey == nil {
		return gatewayLockedBilling{}, true
	}
	priceRequest := tokenInflightEstimate(model, body)
	refrozen, requestBilling, priceErr := gatewayService.RefreezeRequestBilling(c.Request.Context(), apiKey, priceRequest)
	if priceErr == nil {
		priceErr = gatewayService.ValidateRequestPricing(refrozen, apiKey, priceRequest)
	}
	if priceErr != nil {
		return gatewayLockedBilling{}, writeSharedGatewayLockedBillingError(c, reqLog, "gateway.locked_pricing_refreeze_rejected", model, priceErr)
	}
	groupID := int64(0)
	if apiKey.GroupID != nil {
		groupID = *apiKey.GroupID
	}
	mapping, mappingErr := requestBilling.MappingFor(groupID)
	if mappingErr != nil {
		return gatewayLockedBilling{}, writeSharedGatewayLockedBillingError(c, reqLog, "gateway.locked_pricing_refreeze_mapping_rejected", model, mappingErr)
	}
	c.Request = c.Request.WithContext(refrozen)
	return gatewayLockedBilling{active: true, mapping: mapping, groupID: groupID}, true
}

// writeSharedGatewayLockedBillingError 把锁价准入失败映射成统一的 503 error 体，
// 与 GatewayHandler.errorResponse 的输出形状一致（error.type 即 billingErrorDetails
// 的 code，例如 pricing_unavailable）。返回 false 便于调用点直接判失败返回。
func writeSharedGatewayLockedBillingError(c *gin.Context, reqLog *zap.Logger, event, model string, err error) bool {
	if reqLog != nil {
		reqLog.Warn(event, zap.String("model", model), zap.Error(err))
	}
	status, code, message, _ := billingErrorDetails(err)
	c.JSON(status, gin.H{
		"type":  "error",
		"error": gin.H{"type": code, "message": message},
	})
	return false
}

// bindSharedGatewayLockedBillingAccount 把最终选中的账号绑定到冻结 bundle。
//
// 必须放在账号终检之后、Forward 之前：绑定失败（账号不在冻结映射表内、冻结价缺失）
// 即 503 pricing_unavailable，不转发、不扣费。
func bindSharedGatewayLockedBillingAccount(
	c *gin.Context,
	locked gatewayLockedBilling,
	groupID int64,
	accountID int64,
	model string,
	reqLog *zap.Logger,
) (bool, error) {
	if !locked.active || c == nil || c.Request == nil {
		return true, nil
	}
	bound, bindErr := service.WithGatewayRequestBillingAccount(c.Request.Context(), groupID, accountID)
	if bindErr != nil {
		if reqLog != nil {
			reqLog.Warn("gateway.locked_pricing_account_bind_rejected",
				zap.String("model", model),
				zap.Int64("group_id", groupID),
				zap.Int64("account_id", accountID),
				zap.Error(bindErr))
		}
		return false, bindErr
	}
	c.Request = c.Request.WithContext(bound)
	return true, nil
}

// apply 在锁价激活时用冻结映射覆盖 live 渠道映射。
func (l gatewayLockedBilling) apply(mapping service.ChannelMappingResult) service.ChannelMappingResult {
	if l.active {
		return l.mapping
	}
	return mapping
}
