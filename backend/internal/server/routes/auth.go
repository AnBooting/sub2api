package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	// 认证端点的限流阈值。IP 维度是「同一出口的滥用预算」，账号维度是「针对
	// 单个身份的爆破预算」。
	//
	// 权衡：按账号限流会带来 account lockout 风险（攻击者用受害者邮箱反复失败
	// 即可把受害者锁在窗口外），因此账号阈值刻意显著高于 IP 阈值，且只挂在
	// 会泄露/消耗凭据的端点上，绝不给 refresh / logout / OAuth 回调挂账号维度
	// （那些必须保证可用）。调整这里即可改策略，无需改动中间件。
	authLoginIPRateLimit             = 20
	authLoginAccountRateLimit        = 60 // 登录类：只有 3 倍于 IP 阈值才不会被拿来锁号
	authLowFrequencyIPRateLimit      = 5
	authLowFrequencyAccountRateLimit = 10 // 注册/发码：低频操作，误伤面小
	authPromoIPRateLimit             = 10
	authPromoAccountRateLimit        = 10
	// 2FA：账号维度按一次性 temp_token 计，限制同一个 2FA 会话内对 6 位
	// 动态码的爆破次数（10^6 空间，20/min 的 IP 阈值单独不足以形成约束）。
	authTOTPIPRateLimit      = 20
	authTOTPAccountRateLimit = 30
)

// authAccountKey 返回按归一化邮箱计的账号维度键构造器。
// 归一化复用仓库既有的别名口径（service.NormalizeEmailForAliasDedup），
// 因此 victim+1@example.com / victim@googlemail.com 等别名会落在同一个桶，
// 攻击者无法用别名扩展自己的爆破预算，也不会误锁无关账号。
func authAccountKey() func(*gin.Context) string {
	return middleware.AccountKeyFromJSONBodyField("email", service.NormalizeEmailForAliasDedup)
}

// authTOTPAccountKey 按 temp_token 计账号维度。temp_token 已经是随机一次性凭据，
// 这里仍做不可逆摘要，避免把凭据原文写进 Redis 键。
func authTOTPAccountKey() func(*gin.Context) string {
	return middleware.AccountKeyFromJSONBodyFields([]string{"temp_token"}, nil)
}

// RegisterAuthRoutes 注册认证相关路由
func RegisterAuthRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth servermiddleware.JWTAuthMiddleware,
	auditLog servermiddleware.AuditLogMiddleware,
	redisClient *redis.Client,
	settingService *service.SettingService,
	panelRateLimiter *servermiddleware.PanelRateLimiter,
) {
	// 创建速率限制器
	rateLimiter := middleware.NewRateLimiter(redisClient)

	// 公开接口
	auth := v1.Group("/auth")
	auth.Use(servermiddleware.BackendModeAuthGuard(settingService))
	// 认证事件（登录/注册/2FA/token 刷新失败）入审计
	auth.Use(gin.HandlerFunc(auditLog))
	{
		// 注册/登录/2FA/验证码发送均属于高风险入口，增加服务端兜底限流（Redis 故障时 fail-close）
		auth.POST("/register", rateLimiter.LimitWithOptions("auth-register", authLowFrequencyIPRateLimit, time.Minute, middleware.RateLimitOptions{
			FailureMode:  middleware.RateLimitFailClose,
			AccountKey:   authAccountKey(),
			AccountLimit: authLowFrequencyAccountRateLimit,
		}), h.Auth.Register)
		// /auth/login 的账号维度只统计**失败**（FailureOnlyAccount）：中间件只做
		// 只读查锁，handler 在凭据错误后递增、成功后清零。按请求次数计数会让攻击者
		// 用受害者邮箱发几次失败就把受害者锁死（account lockout DoS）。
		auth.POST("/login", rateLimiter.LimitWithOptions("auth-login", authLoginIPRateLimit, time.Minute, middleware.RateLimitOptions{
			FailureMode:        middleware.RateLimitFailClose,
			AccountKey:         authAccountKey(),
			AccountLimit:       authLoginAccountRateLimit,
			FailureOnlyAccount: true,
		}), h.Auth.Login)
		auth.POST("/login/2fa", rateLimiter.LimitWithOptions("auth-login-2fa", authTOTPIPRateLimit, time.Minute, middleware.RateLimitOptions{
			FailureMode:  middleware.RateLimitFailClose,
			AccountKey:   authTOTPAccountKey(),
			AccountLimit: authTOTPAccountRateLimit,
		}), h.Auth.Login2FA)
		auth.POST("/passkey/login/begin", rateLimiter.LimitWithOptions("passkey-login-begin", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Passkey.BeginLogin)
		auth.POST("/passkey/login/finish", rateLimiter.LimitWithOptions("passkey-login-finish", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Passkey.FinishLogin)
		auth.POST("/send-verify-code", rateLimiter.LimitWithOptions("auth-send-verify-code", authLowFrequencyIPRateLimit, time.Minute, middleware.RateLimitOptions{
			FailureMode:  middleware.RateLimitFailClose,
			AccountKey:   authAccountKey(),
			AccountLimit: authLowFrequencyAccountRateLimit,
		}), h.Auth.SendVerifyCode)
		// Token刷新接口添加速率限制：每分钟最多 30 次（Redis 故障时 fail-close）
		auth.POST("/refresh", rateLimiter.LimitWithOptions("refresh-token", 30, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.RefreshToken)
		// 登出接口（公开，允许未认证用户调用以撤销Refresh Token）
		auth.POST("/logout", h.Auth.Logout)
		// 优惠码验证接口添加速率限制：每分钟最多 10 次（Redis 故障时 fail-close）
		auth.POST("/validate-promo-code", rateLimiter.LimitWithOptions("validate-promo", authPromoIPRateLimit, time.Minute, middleware.RateLimitOptions{
			FailureMode:  middleware.RateLimitFailClose,
			AccountKey:   authAccountKey(),
			AccountLimit: authPromoAccountRateLimit,
		}), h.Auth.ValidatePromoCode)
		// 邀请码验证接口添加速率限制：每分钟最多 10 次（Redis 故障时 fail-close）
		auth.POST("/validate-invitation-code", rateLimiter.LimitWithOptions("validate-invitation", 10, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.ValidateInvitationCode)
		// 忘记密码接口添加速率限制：每分钟最多 5 次（Redis 故障时 fail-close）
		auth.POST("/forgot-password", rateLimiter.LimitWithOptions("forgot-password", 5, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.ForgotPassword)
		// 重置密码接口添加速率限制：每分钟最多 10 次（Redis 故障时 fail-close）
		auth.POST("/reset-password", rateLimiter.LimitWithOptions("reset-password", 10, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.ResetPassword)
		auth.GET("/oauth/linuxdo/start", h.Auth.LinuxDoOAuthStart)
		auth.POST("/oauth/linuxdo/start", rateLimiter.LimitWithOptions("oauth-linuxdo-start", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.LinuxDoOAuthStart)
		auth.GET("/oauth/github/start", h.Auth.GitHubOAuthStart)
		auth.POST("/oauth/github/start", rateLimiter.LimitWithOptions("oauth-github-start", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.GitHubOAuthStart)
		auth.GET("/oauth/github/callback", h.Auth.GitHubOAuthCallback)
		auth.POST("/oauth/github/complete-registration",
			rateLimiter.LimitWithOptions("oauth-github-complete", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CompleteGitHubOAuthRegistration,
		)
		auth.GET("/oauth/google/start", h.Auth.GoogleOAuthStart)
		auth.POST("/oauth/google/start", rateLimiter.LimitWithOptions("oauth-google-start", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.GoogleOAuthStart)
		auth.GET("/oauth/google/callback", h.Auth.GoogleOAuthCallback)
		auth.POST("/oauth/google/complete-registration",
			rateLimiter.LimitWithOptions("oauth-google-complete", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CompleteGoogleOAuthRegistration,
		)
		auth.GET("/oauth/linuxdo/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.LinuxDoOAuthStart(c)
		})
		auth.GET("/oauth/linuxdo/callback", h.Auth.LinuxDoOAuthCallback)
		auth.GET("/oauth/wechat/start", h.Auth.WeChatOAuthStart)
		auth.POST("/oauth/wechat/start", rateLimiter.LimitWithOptions("oauth-wechat-start", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.WeChatOAuthStart)
		auth.GET("/oauth/wechat/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.WeChatOAuthStart(c)
		})
		auth.GET("/oauth/wechat/callback", h.Auth.WeChatOAuthCallback)
		auth.GET("/oauth/wechat/payment/start", h.Auth.WeChatPaymentOAuthStart)
		auth.GET("/oauth/wechat/payment/callback", h.Auth.WeChatPaymentOAuthCallback)
		auth.POST("/oauth/pending/exchange",
			rateLimiter.LimitWithOptions("oauth-pending-exchange", 20, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.ExchangePendingOAuthCompletion,
		)
		auth.POST("/oauth/pending/send-verify-code",
			rateLimiter.LimitWithOptions("oauth-pending-send-verify-code", 5, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.SendPendingOAuthVerifyCode,
		)
		auth.POST("/oauth/pending/create-account",
			rateLimiter.LimitWithOptions("oauth-pending-create-account", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CreatePendingOAuthAccount,
		)
		auth.POST("/oauth/pending/bind-login",
			rateLimiter.LimitWithOptions("oauth-pending-bind-login", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.BindPendingOAuthLogin,
		)
		auth.POST("/oauth/linuxdo/complete-registration",
			rateLimiter.LimitWithOptions("oauth-linuxdo-complete", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CompleteLinuxDoOAuthRegistration,
		)
		auth.POST("/oauth/linuxdo/bind-login",
			rateLimiter.LimitWithOptions("oauth-linuxdo-bind-login", 20, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.BindLinuxDoOAuthLogin,
		)
		auth.POST("/oauth/linuxdo/create-account",
			rateLimiter.LimitWithOptions("oauth-linuxdo-create-account", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CreateLinuxDoOAuthAccount,
		)
		auth.POST("/oauth/wechat/complete-registration",
			rateLimiter.LimitWithOptions("oauth-wechat-complete", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CompleteWeChatOAuthRegistration,
		)
		auth.POST("/oauth/wechat/bind-login",
			rateLimiter.LimitWithOptions("oauth-wechat-bind-login", 20, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.BindWeChatOAuthLogin,
		)
		auth.POST("/oauth/wechat/create-account",
			rateLimiter.LimitWithOptions("oauth-wechat-create-account", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CreateWeChatOAuthAccount,
		)
		auth.GET("/oauth/oidc/start", h.Auth.OIDCOAuthStart)
		auth.POST("/oauth/oidc/start", rateLimiter.LimitWithOptions("oauth-oidc-start", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.OIDCOAuthStart)
		auth.GET("/oauth/oidc/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.OIDCOAuthStart(c)
		})
		auth.GET("/oauth/oidc/callback", h.Auth.OIDCOAuthCallback)
		auth.POST("/oauth/oidc/complete-registration",
			rateLimiter.LimitWithOptions("oauth-oidc-complete", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CompleteOIDCOAuthRegistration,
		)
		auth.POST("/oauth/oidc/bind-login",
			rateLimiter.LimitWithOptions("oauth-oidc-bind-login", 20, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.BindOIDCOAuthLogin,
		)
		auth.POST("/oauth/oidc/create-account",
			rateLimiter.LimitWithOptions("oauth-oidc-create-account", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CreateOIDCOAuthAccount,
		)
		auth.GET("/oauth/dingtalk/start", h.Auth.DingTalkOAuthStart)
		auth.POST("/oauth/dingtalk/start", rateLimiter.LimitWithOptions("oauth-dingtalk-start", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.DingTalkOAuthStart)
		auth.GET("/oauth/dingtalk/bind/start", func(c *gin.Context) {
			query := c.Request.URL.Query()
			query.Set("intent", "bind_current_user")
			c.Request.URL.RawQuery = query.Encode()
			h.Auth.DingTalkOAuthStart(c)
		})
		auth.GET("/oauth/dingtalk/callback", h.Auth.DingTalkOAuthCallback)
		auth.POST("/oauth/dingtalk/complete-registration",
			rateLimiter.LimitWithOptions("oauth-dingtalk-complete", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CompleteDingTalkOAuthRegistration,
		)
		auth.POST("/oauth/dingtalk/bind-login",
			rateLimiter.LimitWithOptions("oauth-dingtalk-bind-login", 20, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.BindDingTalkOAuthLogin,
		)
		auth.POST("/oauth/dingtalk/create-account",
			rateLimiter.LimitWithOptions("oauth-dingtalk-create-account", 10, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Auth.CreateDingTalkOAuthAccount,
		)
	}

	// 公开设置（无需认证）：每次请求都会查询 DB，按客户端 IP 兜底限流，
	// 防止匿名高频刷接口打爆数据库（反代内部地址会被自动跳过，不会误伤）。
	settings := v1.Group("/settings")
	settings.Use(panelRateLimiter.PublicIP())
	{
		settings.GET("/public", h.Setting.GetPublicSettings)
		settings.GET("/email-unsubscribe", h.Setting.UnsubscribeNotificationEmail)
	}

	// 需要认证的当前用户信息
	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(servermiddleware.BackendModeUserGuard(settingService))
	// 面板全局按用户限流
	authenticated.Use(panelRateLimiter.Global())
	{
		authenticated.GET("/auth/me", h.Auth.GetCurrentUser)
		// 撤销所有会话（需要认证）
		authenticated.POST("/auth/revoke-all-sessions", h.Auth.RevokeAllSessions)
		authenticated.POST("/auth/oauth/bind-token", h.Auth.PrepareOAuthBindAccessTokenCookie)
	}
}
