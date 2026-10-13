package service

import (
	"context"
	"fmt"
	"strings"
)

// lockedSharedGatewayUsageMediaError 报告共享计费链路上本切片不覆盖的按次/媒体形态。
// 锁价消费者只实现了纯 token 计费；图片、搜索叠加与语音一旦混入，宁可 fail-closed
// 也不能按 token 价静默错价（这些形态另由媒体/非文本切片处理）。
//
// 注意：共享的 ForwardResult 只有 ImageCount / SearchCount / AudioUsage 三个媒体维度；
// 没有 VideoCount / WebSearchCalls（那是 OpenAIForwardResult 的字段），所以视频与
// 网页搜索在这里天然不可见，不会绕过守卫。
func lockedSharedGatewayUsageMediaError(result *ForwardResult) error {
	if result == nil {
		return nil
	}
	switch {
	case result.ImageCount != 0:
		return fmt.Errorf("%w: locked shared billing does not cover image results (image_count=%d)", ErrModelPricingUnavailable, result.ImageCount)
	case result.SearchCount != 0:
		return fmt.Errorf("%w: locked shared billing does not cover search surcharges (search_count=%d)", ErrModelPricingUnavailable, result.SearchCount)
	case result.AudioUsage != nil:
		return fmt.Errorf("%w: locked shared billing does not cover audio usage (mode=%q)", ErrModelPricingUnavailable, result.AudioUsage.Mode)
	}
	return nil
}

// calculateLockedSharedGatewayUsageCost 是共享网关（Anthropic /v1/messages 及后续
// Chat Completions / Gemini 入口）的锁价消费分支，语义与 OpenAI 侧 locked 分支对齐：
//
//   - 计费模型、渠道价、分组价、倍率与定价时刻全部取自请求建立时冻结的 bundle，
//     不再回源查实时分组/渠道/目录；
//   - 实际账号与冻结的账号事实必须一致，实际出站模型必须等于该账号的冻结模型，
//     否则 fail-closed（ErrModelPricingUnavailable），绝不落到 live 兜底价；
//   - 显式零价视为「有价」，由 quote.Availability() 判定，不在这里放宽；
//   - 纯 token 守卫：媒体/搜索/语音形态整笔拒绝。
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
	cost, costErr := quote.Calculate(CostInput{
		SelectedAccountID: account.ID,
		Tokens:            tokens,
		ServiceTier:       optionalStringValue(result.ServiceTier),
		ReasoningEffort:   optionalStringValue(result.ReasoningEffort),
	})
	if costErr != nil {
		return nil, nil, PriceScope{}, costErr
	}
	return cost, quote, scope, nil
}
