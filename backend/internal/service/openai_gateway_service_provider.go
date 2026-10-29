package service

import "github.com/Wei-Shaw/sub2api/internal/config"

// ProvideOpenAIGatewayService 组装 OpenAI 网关，并显式注入 Live 结算所需的
// APIKeyService（密钥额度/窗口限额计量与鉴权缓存失效）。
// 不把第 23 个依赖塞进 NewOpenAIGatewayService：该构造函数被大量测试装配按位置
// 引用，加宽参数会迫使几十处测试同步修改；生产只需在 wire 装配层注入一次。
func ProvideOpenAIGatewayService(
	accountRepo AccountRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	openAITokenProvider *OpenAITokenProvider,
	grokTokenProvider *GrokTokenProvider,
	resolver *ModelPricingResolver,
	channelService *ChannelService,
	balanceNotifyService *BalanceNotifyService,
	settingService *SettingService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	apiKeyService *APIKeyService,
	opsRepo OpsRepository,
) *OpenAIGatewayService {
	gateway := NewOpenAIGatewayService(
		accountRepo,
		usageLogRepo,
		usageBillingRepo,
		userRepo,
		userSubRepo,
		userGroupRateRepo,
		cache,
		cfg,
		schedulerSnapshot,
		concurrencyService,
		billingService,
		rateLimitService,
		billingCacheService,
		httpUpstream,
		deferredService,
		openAITokenProvider,
		grokTokenProvider,
		resolver,
		channelService,
		balanceNotifyService,
		settingService,
		userPlatformQuotaRepo,
	)
	gateway.SetAPIKeyService(apiKeyService)
	// Live 结算失败审计走 ops 错误日志（独立于 usage_logs，不参与用量汇总）。
	gateway.SetOpsRepository(opsRepo)
	return gateway
}
