package service

import (
	"context"
	"fmt"
	"strings"
)

// lockedSharedGatewayUsageMediaError 报告共享计费链路上本切片不覆盖的按次/媒体形态。
// 锁价消费者覆盖 token/image；搜索叠加与语音一旦混入，宁可 fail-closed
// 也不能按 token 价静默错价（这些形态另由媒体/非文本切片处理）。
//
// 这是纵深防御：入口仍须在上游前检查具体图片尺寸的冻结报价；本守卫禁止
// 将未接入的搜索与音频维度静默当作 token 或图片记账。
//
// 注意：共享的 ForwardResult 只有 ImageCount / SearchCount / AudioUsage 三个媒体维度；
// 没有 VideoCount / WebSearchCalls（那是 OpenAIForwardResult 的字段），所以视频与
// 网页搜索在这里天然不可见。
func lockedSharedGatewayUsageMediaError(result *ForwardResult) error {
	if result == nil {
		return nil
	}
	switch {
	case result.ImageCount < 0:
		return fmt.Errorf("%w: locked shared billing rejects negative image count (image_count=%d)", ErrModelPricingUnavailable, result.ImageCount)
	case result.SearchCount != 0:
		return fmt.Errorf("%w: locked shared billing does not cover search surcharges (search_count=%d)", ErrModelPricingUnavailable, result.SearchCount)
	case result.AudioUsage != nil:
		return fmt.Errorf("%w: locked shared billing does not cover audio usage (mode=%q)", ErrModelPricingUnavailable, result.AudioUsage.Mode)
	}
	return nil
}

// lockedPricingIsChannelPriced 与 legacy 的 baselineChannelPriced 同口径：解析结果来自
// 管理员配置的分组价或渠道价（而非全局价格表/兜底价）。
func lockedPricingIsChannelPriced(quote *LockedRequestPricing) bool {
	if quote == nil {
		return false
	}
	source := quote.Source()
	return source == PricingSourceGroup || source == PricingSourceChannel
}

// calculateLockedSharedGatewayUsageCost 是共享网关（Anthropic /v1/messages，以及后续
// Chat Completions 入口）的锁价消费分支，语义与 OpenAI 侧 locked 分支对齐：
//
//   - 计费模型、渠道价、分组价、倍率与定价时刻全部取自请求建立时冻结的 bundle，
//     不再回源查实时分组/渠道/目录；
//   - 实际账号与冻结的账号事实必须一致，实际出站模型必须等于该账号的冻结模型，
//     否则 fail-closed（ErrModelPricingUnavailable），绝不落到 live 兜底价；
//   - response_model 计费源（管理员在渠道上开启）必须先过两条守卫：响应模型在流中
//     自相矛盾（UpstreamResponseModelConflict）直接拒绝；采纳响应模型时沿用 legacy 的
//     responseModelBillingAdoptable 口径（不得比冻结基线更贵 / 不得把计费归零 /
//     不得跨定价源切换），基线是同一冻结 bundle 下 mapping.MappedModel 的冻结价；
//   - 显式零价视为「有价」，由 quote.Availability() 判定，不在这里放宽；
//   - 仅 token/image：搜索/语音及未知计费模式拒绝；图像计数与尺寸只使用冻结价卡。
//
// 返回值中的 quote 供调用方取冻结倍率，scope.BillingModel 用于 usage_log 的计费基准。
func (s *GatewayService) calculateLockedSharedGatewayUsageCost(
	ctx context.Context,
	requestPricing *RequestBillingContext,
	result *ForwardResult,
	account *Account,
) (*CostBreakdown, *LockedRequestPricing, PriceScope, error) {
	if requestPricing == nil {
		return nil, nil, PriceScope{}, fmt.Errorf("%w: frozen request pricing bundle is missing", ErrModelPricingUnavailable)
	}
	if result == nil || account == nil {
		return nil, nil, PriceScope{}, fmt.Errorf("%w: locked usage input is incomplete", ErrModelPricingUnavailable)
	}
	if err := lockedSharedGatewayUsageMediaError(result); err != nil {
		return nil, nil, PriceScope{}, err
	}
	facts, known := gatewayRequestBillingAccountFromContext(ctx)
	if !known || facts.accountID != account.ID {
		return nil, nil, PriceScope{}, fmt.Errorf("%w: missing actual account execution facts", ErrModelPricingUnavailable)
	}
	expected, _, modelErr := frozenGatewayRequestAccountModel(ctx, account.ID)
	if modelErr != nil {
		return nil, nil, PriceScope{}, modelErr
	}
	if strings.TrimSpace(result.UpstreamModel) != expected {
		return nil, nil, PriceScope{}, fmt.Errorf("%w: actual upstream model differs from the frozen account model", ErrModelPricingUnavailable)
	}
	mapping, mappingErr := requestPricing.MappingFor(facts.groupID)
	if mappingErr != nil {
		return nil, nil, PriceScope{}, mappingErr
	}
	// response_model 计费源下，流内自相矛盾的模型声明不可作为计费事实：与 OpenAI 锁价
	// 分支同语义，在取 scope 之前就 fail-closed，而不是挑其中一个模型去计价。
	if mapping.BillingModelSource == BillingModelSourceResponse && result.UpstreamResponseModelConflict {
		return nil, nil, PriceScope{}, fmt.Errorf("%w: conflicting upstream response model", ErrModelPricingUnavailable)
	}
	scope, scopeErr := requestPricing.ScopeFor(facts.groupID, result.UpstreamModel, result.UpstreamResponseModel)
	if scopeErr != nil {
		return nil, nil, PriceScope{}, scopeErr
	}
	quote, quoteErr := requestPricing.PricingFor(scope)
	if quoteErr != nil {
		return nil, nil, PriceScope{}, quoteErr
	}
	if err := quote.Availability(); err != nil {
		return nil, nil, PriceScope{}, err
	}
	tokens := UsageTokens{
		InputTokens:           result.Usage.InputTokens,
		OutputTokens:          result.Usage.OutputTokens,
		CacheCreationTokens:   result.Usage.CacheCreationInputTokens,
		CacheReadTokens:       result.Usage.CacheReadInputTokens,
		CacheCreation5mTokens: result.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens: result.Usage.CacheCreation1hTokens,
		ImageOutputTokens:     result.Usage.ImageOutputTokens,
	}
	costInput := CostInput{
		SelectedAccountID: account.ID,
		Tokens:            tokens,
		ServiceTier:       optionalStringValue(result.ServiceTier),
		ReasoningEffort:   optionalStringValue(result.ReasoningEffort),
		// 显式上下文缓存的存储量：随请求冻结的价卡计价（上游 0.2.17 维度），
		// 缺存储单价时由冻结 quote fail-closed。
		CacheStorageTokenHours: result.CacheStorageTokenHours,
		// 纯存储量请求不是一次模型请求（与 live 路径 isCacheStorageOnlyResult 同口径）。
		CacheStorageOnly: isCacheStorageOnlyResult(result),
	}
	cost, costErr := calculateLockedSharedQuoteCost(quote, result, costInput)
	if costErr != nil {
		return nil, nil, PriceScope{}, costErr
	}
	// response_model 采纳准入：ScopeFor 已经按响应模型选了 scope，这里再用"冻结基线
	// （渠道映射模型）的价"做三道不变式校验，与 legacy 的 responseModelBillingAdoptable
	// 完全同口径。不满足即静默回落基线，绝不因为上游自报的名字而抬高或清零费用。
	if mapping.BillingModelSource == BillingModelSourceResponse {
		baselineModel := strings.TrimSpace(mapping.MappedModel)
		if baselineModel != "" && !strings.EqualFold(baselineModel, strings.TrimSpace(scope.BillingModel)) {
			baselineScope := PriceScope{GroupID: facts.groupID, BillingModel: baselineModel, ChannelID: mapping.ChannelID}
			if baselineQuote, baselineErr := requestPricing.PricingFor(baselineScope); baselineErr == nil && baselineQuote.Availability() == nil {
				if baselineCost, baselineCostErr := calculateLockedSharedQuoteCost(baselineQuote, result, costInput); baselineCostErr == nil {
					if !responseModelBillingAdoptable(baselineCost, cost, lockedPricingIsChannelPriced(baselineQuote), lockedPricingIsChannelPriced(quote)) {
						cost = baselineCost
						quote = baselineQuote
						scope = baselineScope
					}
				} else if result.ImageCount > 0 {
					return nil, nil, PriceScope{}, fmt.Errorf("%w: image response baseline cannot be calculated: %v", ErrModelPricingUnavailable, baselineCostErr)
				}
			} else if result.ImageCount > 0 {
				return nil, nil, PriceScope{}, fmt.Errorf("%w: image response baseline is unavailable", ErrModelPricingUnavailable)
			}
		}
	}
	return cost, quote, scope, nil
}
