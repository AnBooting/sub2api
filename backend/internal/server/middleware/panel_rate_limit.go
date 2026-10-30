package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// panelRateLimitWindow 面板限流固定窗口时长（所有档位均按每分钟计数）。
const panelRateLimitWindow = time.Minute

// collapsedPrivateIPBucketMultiplier 是「客户端地址解析坍缩为私网/回环地址」时
// 公开接口限流桶的放宽倍数。
//
// 为什么需要放宽：未配置 server.trusted_proxies 时，安全客户端 IP 解析只能拿到
// 反代的内部地址（127.0.0.1 / docker bridge / k8s pod 网段）。按该地址计数会把
// 整条反代链路后面的**所有**真实用户合并进同一个桶——若沿用每客户端阈值
// （默认 300 RPM），一个中等规模实例的 /api/v1/settings/public 等匿名接口
// 很容易把全局桶打满，造成全站大面积 429。
//
// 为什么是 10 倍：默认阈值 300 RPM 对应「单客户端」的宽松上限；一个典型反代后
// 同一时刻活跃的并发客户端在数百量级，匿名公开接口的常态请求率约为每个活跃
// 客户端每分钟 1~2 次（页面加载时的设置/公告拉取），因此 3000 RPM 足以覆盖
// 数百个活跃客户端而不误拦，同时仍能把脚本级刷接口（数千 RPM 以上）挡下来。
// 该倍数只作用于「坍缩桶」，公网地址的精确计数完全不受影响。
const collapsedPrivateIPBucketMultiplier = 10

// panelRateLimitCollapsedIPWarned 保证"限流因 IP 不可路由而坍缩到放宽桶"的告警只打印一次，
// 避免每请求刷屏。
var panelRateLimitCollapsedIPWarned atomic.Bool

// panelRateLimitAllower 抽象底层限流原语，便于单测注入。
type panelRateLimitAllower interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (middleware.AllowResult, error)
}

// PanelRateLimiter 面板（管理面 /api/v1）API 限流器。
//
// 设计要点：
//   - 认证接口按「用户 ID」维度计数：与客户端 IP 完全无关，反向代理/共享出口
//     （所有请求源 IP 坍缩为 127.0.0.1 等）不会互相误伤。
//   - 公开接口按安全客户端 IP 计数：仅统计全局单播地址，回环/内网/链路本地
//     地址（反代内部转发地址）直接跳过，避免误拦整条反代链路的流量。
//   - 配置走进程内缓存（60s TTL），热路径零 DB 访问。
//   - Redis 异常一律 fail-open：限流是保护措施，不能反过来把面板打挂。
type PanelRateLimiter struct {
	limiter        panelRateLimitAllower
	settingService *service.SettingService
	// redis 仅用于 RedeemGuard 的「失败次数」计数（需要「先读后写」，而
	// RateLimiter.Allow 只提供「自增并判定」语义，成功兑换不应消耗失败额度）。
	// 为 nil 时失败次数这一层自动跳过（见 RedeemGuard 注释）。
	redis *redis.Client
}

// NewPanelRateLimiter 创建面板限流器。
func NewPanelRateLimiter(redisClient *redis.Client, settingService *service.SettingService) *PanelRateLimiter {
	return &PanelRateLimiter{
		limiter:        middleware.NewRateLimiter(redisClient),
		settingService: settingService,
		redis:          redisClient,
	}
}

// Global 认证面板接口的全局按用户限流（宽松档，覆盖所有登录后端点）。
func (p *PanelRateLimiter) Global() gin.HandlerFunc {
	return p.userScoped("global", func(s service.PanelRateLimitSettings) int { return s.UserRPM })
}

// Heavy 重查询接口的按用户限流（严格档，覆盖 usage/dashboard 等聚合统计端点）。
// 与 Global 叠加计数：一次重查询同时消耗两档额度。
func (p *PanelRateLimiter) Heavy() gin.HandlerFunc {
	return p.userScoped("heavy", func(s service.PanelRateLimitSettings) int { return s.HeavyRPM })
}

func (p *PanelRateLimiter) userScoped(scope string, limitOf func(service.PanelRateLimitSettings) int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil || p.limiter == nil || p.settingService == nil {
			c.Next()
			return
		}
		settings := p.settingService.GetPanelRateLimitSettingsCached(c.Request.Context())
		if !settings.Enabled {
			c.Next()
			return
		}
		limit := limitOf(settings)
		if limit <= 0 {
			c.Next()
			return
		}
		subject, ok := GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			// 无认证主体（认证中间件缺位时的防御分支）：放行，避免误伤
			c.Next()
			return
		}
		if settings.ExemptAdmin {
			if role, hasRole := GetUserRoleFromContext(c); hasRole && role == service.RoleAdmin {
				c.Next()
				return
			}
		}

		key := "panel:" + scope + ":user:" + strconv.FormatInt(subject.UserID, 10)
		result, err := p.limiter.Allow(c.Request.Context(), key, limit, panelRateLimitWindow)
		if err != nil {
			// fail-open：Redis 异常不阻断面板访问
			slog.Warn("panel rate limit check failed, allowing request", "scope", scope, "error", err)
			c.Next()
			return
		}
		if !result.Allowed {
			abortPanelRateLimited(c, result.RetryAfter)
			return
		}
		c.Next()
	}
}

// PublicIP 无需认证的公开接口按客户端 IP 限流。
// 使用与审计日志/会话绑定一致的安全客户端 IP 解析。
//
// 解析结果为回环/内网/链路本地地址时（即"客户端地址坍缩"：通常是
// server.trusted_proxies 未配置，只能拿到反代的内部地址）**仍然计数**，
// 但改用放宽 collapsedPrivateIPBucketMultiplier 倍的桶：
//   - 不能再像以前那样直接跳过计数。跳过会让公开接口限流在默认部署下静默失效，
//     攻击者只要不触发 trusted_proxies 配置就能无限刷匿名接口（F-02 同类问题）。
//   - 也不能沿用每客户端阈值。坍缩桶是全局共享的，沿用严苛阈值会让整条反代
//     链路后面的所有用户互相消耗额度，造成大面积 429。
//   - 因此是"先判断是否坍缩，再选桶宽"：公网地址的精确计数完全不受放宽影响。
//
// Redis 异常仍 fail-open（限流是保护措施，不能反过来把面板打挂）。
func (p *PanelRateLimiter) PublicIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil || p.limiter == nil || p.settingService == nil {
			c.Next()
			return
		}
		settings := p.settingService.GetPanelRateLimitSettingsCached(c.Request.Context())
		if !settings.Enabled || settings.PublicIPRPM <= 0 {
			c.Next()
			return
		}
		clientIP := SecurityClientIP(c)
		limit := settings.PublicIPRPM
		key := "panel:public:ip:" + clientIP
		if !isPubliclyRoutableClientIP(clientIP) {
			// 坍缩场景：键仍是该私网地址本身（运维可直接从 Redis 看出坍缩来源），
			// 但桶宽放宽，避免全站用户共享一个严苛的全局桶。
			limit *= collapsedPrivateIPBucketMultiplier
			key = "panel:public:ip-collapsed:" + clientIP
			if panelRateLimitCollapsedIPWarned.CompareAndSwap(false, true) {
				slog.Warn("panel public rate limit uses a widened shared bucket because the resolved client IP is not publicly routable; configure server.trusted_proxies to get accurate per-client counting",
					"client_ip", clientIP, "widened_limit", limit)
			}
		}

		result, err := p.limiter.Allow(c.Request.Context(), key, limit, panelRateLimitWindow)
		if err != nil {
			slog.Warn("panel public rate limit check failed, allowing request", "error", err)
			c.Next()
			return
		}
		if !result.Allowed {
			abortPanelRateLimited(c, result.RetryAfter)
			return
		}
		c.Next()
	}
}

// ──────────────────────────────────────────────────────────
// 兑换码（POST /api/v1/redeem）专用限流
// ──────────────────────────────────────────────────────────

const (
	// redeemRateLimitPerMinute 单个登录用户每分钟最多发起的兑换尝试数。
	// 兑换码是 16 字节随机串，正常用户每秒点一次都算异常；5/min 足够人工操作，
	// 同时把「高频枚举」压到不可行量级。
	redeemRateLimitPerMinute = 5
	// redeemFailedAttemptLimit 单个用户在窗口内允许的**失败**次数上限。
	//
	// 取值依据（不是随手取的数）：兑换码是「复制粘贴一次性输入」的场景，
	// 正常用户的失败几乎全部来自粘贴时漏字符或码已被别人用掉，实测形态是
	// 失败 1~2 次后成功；10 次已经是正常行为的 5 倍余量，不会让正常用户
	// 误锁。另一侧，10 次/10 分钟把暴力枚举压到每天最多 1440 次尝试，
	// 相对 16 字节（128 bit）随机码空间不可行。
	// 该阈值比 service 层既有的 30 次/10 分钟更严，因此会先触发——这是有意的：
	// 更早收口，同时新层能正确写出 Retry-After（既有 service 层那条 429 缺该头）。
	redeemFailedAttemptLimit = 10
	// redeemFailedAttemptWindow 失败计数窗口。与 repository 层既有的
	// redeem:ratelimit:v2 计数窗口（10 分钟）保持一致。
	redeemFailedAttemptWindow = 10 * time.Minute
)

// RedeemGuard 兑换码接口的专用限流中间件。
//
// 为什么需要独立一层：/api/v1/redeem 只挂在 panelRateLimiter.Global() 之下
// （默认 240 RPM），单用户每分钟仍可枚举 240 个兑换码；线上实测 25 次无效码
// 全部返回 404 REDEEM_CODE_NOT_FOUND、0 次 429，构成可判定的兑换码 oracle
// （「码存在」与「码不存在」的响应可区分，且速率不受限）。
//
// 两层语义（都按**登录用户 ID** 计数，与客户端 IP 无关）：
//
//  1. 请求速率层：每用户每窗口最多 redeemRateLimitPerMinute 次尝试。
//     键 = panel:redeem:user:<userID>。
//  2. 失败次数层：每用户每窗口最多 redeemFailedAttemptLimit 次**失败**尝试。
//     键 = panel:redeem:fail:<userID>，只在 handler 判定失败后由
//     RecordRedeemFailure 累加；成功兑换不累加、也不清零。
//
// 关于「按用户计数会不会让攻击者锁死他人账号」（reviewer 的 DoS 质疑）：
// 不会。账号维度的键是**已认证主体的 userID**，而 JWT 绑定的是攻击者自己的
// 会话——攻击者只能烧自己的额度，无法替别人计数。这与「按请求体里的邮箱计数」
// （攻击者可以用受害者邮箱刷登录接口锁死受害者，见 auth 的账号维度）本质不同。
// 因此这里刻意**不**按兑换码字符串计数：按 code 计数会让不同用户的失败互相
// 影响，且能通过枚举把某个真实码的桶打满，反而制造跨用户 DoS。
//
// 取舍（明确记录）：
//   - 请求速率层用 fail-close：Redis 故障时拒绝，而不是放行。理由是这个端点
//     是当前唯一能在「不需凭据」的前提下批量探测业务数据的入口（凭据是登录态，
//     但兑换码本身是可枚举的资产）；限流失效的后果（oracle 重开）比短暂拒绝
//     兑换严重。其余面板限流仍保持 fail-open。
//   - 失败计数层在 Redis 不可用时**不加拦截**：它只是补充收紧，不能因为读不到
//     计数就把用户永久挡在兑换之外。
//   - 成功兑换不清零失败计数（简单优先）：窗口固定 10 分钟，自然过期；这样
//     实现里没有任何跨窗口状态，也不会出现「用一个有效码反复清零」的绕过面。
//     代价是用户偶发打错几个码后，剩余额度按滑动窗口自然恢复。
func (p *PanelRateLimiter) RedeemGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		subject, ok := GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			// 认证中间件缺位时的防御分支：交给下游（handler 会返回 401）。
			c.Next()
			return
		}
		userKey := strconv.FormatInt(subject.UserID, 10)

		// 第 1 层：请求速率（fail-close）。
		if p != nil && p.limiter != nil {
			result, err := p.limiter.Allow(ctx, "panel:redeem:user:"+userKey, redeemRateLimitPerMinute, panelRateLimitWindow)
			if err != nil {
				slog.Warn("redeem rate limit check failed, rejecting request", "user_id", subject.UserID, "error", err)
				abortPanelRateLimited(c, panelRateLimitWindow)
				return
			}
			if !result.Allowed {
				abortPanelRateLimited(c, result.RetryAfter)
				return
			}
		}

		// 第 2 层：窗口内失败次数（Redis 不可用时不拦截）。
		if p != nil && p.redis != nil {
			key := redeemFailureKey(userKey)
			count, err := p.redis.Get(ctx, key).Int()
			if err == nil && count >= redeemFailedAttemptLimit {
				retryAfter := redeemFailedAttemptWindow
				if ttl, ttlErr := p.redis.TTL(ctx, key).Result(); ttlErr == nil && ttl > 0 {
					retryAfter = ttl
				}
				abortPanelRateLimited(c, retryAfter)
				return
			}
		}

		c.Next()

		// 第 2 层的写入端：只有本次尝试**失败**才累加。判断依据是下游写出的
		// HTTP 状态，而不是 handler 内部的具体错误码——这样新增兑换失败类型
		// 时会自动纳入，无需同步维护一张错误码清单。
		// 401/403（认证/权限）与 429（限流）不算「兑换失败」，不消耗额度。
		if status := c.Writer.Status(); isRedeemFailureStatus(status) {
			p.recordRedeemFailure(ctx, subject.UserID)
		}
	}
}

// isRedeemFailureStatus 判断该状态码是否代表「一次失败的兑换尝试」。
// 404 REDEEM_CODE_NOT_FOUND / 409 REDEEM_CODE_USED|EXPIRED / 400 参数非法
// 都计入；认证与限流类状态码不计入（它们不是「猜码」行为的结果）。
func isRedeemFailureStatus(status int) bool {
	if status < 400 || status >= 500 {
		return false
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return false
	}
	return true
}

// recordRedeemFailure 累加一次失败尝试；Redis 不可用时静默降级（不阻断业务）。
func (p *PanelRateLimiter) recordRedeemFailure(ctx context.Context, userID int64) {
	if p == nil || p.redis == nil || userID <= 0 {
		return
	}
	key := redeemFailureKey(strconv.FormatInt(userID, 10))
	pipe := p.redis.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, redeemFailedAttemptWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("record redeem failure count failed", "user_id", userID, "error", err)
	}
}

func redeemFailureKey(userKey string) string {
	return "panel:redeem:fail:" + userKey
}

// isPubliclyRoutableClientIP 判断地址是否为可作为限流依据的全局单播地址。
// 回环、RFC1918/ULA 内网、链路本地与未指定地址返回 false。
func isPubliclyRoutableClientIP(clientIP string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return ip.IsGlobalUnicast()
}

func abortPanelRateLimited(c *gin.Context, retryAfter time.Duration) {
	if retryAfter <= 0 {
		retryAfter = panelRateLimitWindow
	}
	seconds := int64(retryAfter / time.Second)
	if retryAfter%time.Second > 0 {
		seconds++
	}
	c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	AbortWithError(c, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please slow down and try again later")
}
