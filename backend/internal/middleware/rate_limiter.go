package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimitFailureMode Redis 故障策略
type RateLimitFailureMode int

const (
	RateLimitFailOpen RateLimitFailureMode = iota
	RateLimitFailClose
)

// RateLimitOptions 限流可选配置
type RateLimitOptions struct {
	FailureMode RateLimitFailureMode
	// AccountKey 返回本次请求的账号维度标识（通常来自请求体里的邮箱/手机号）。
	// 返回空串表示该请求没有账号维度。
	//
	// 账号维度是主防线：即使部署侧客户端 IP 解析坍缩或可被伪造，按账号计数
	// 仍然生效；IP 维度只作为次防线。
	AccountKey func(*gin.Context) string
	// FailureOnlyAccount 为 true 时，账号维度不再按「请求次数」计数，而是按
	// 「失败次数」计数：中间件只做只读的锁定检查，由 handler 在判定凭据失败后
	// 调用 RecordAccountFailure 递增、在成功时调用 ResetAccountFailure 清零。
	//
	// 这是针对 account lockout DoS 的必需设计：若按请求次数计数，攻击者只需用
	// 受害者邮箱发 N 次失败即可把受害者锁在窗口外；改为失败计数后，受害者本人
	// 一次成功登录就会清零，攻击者无法维持锁定。
	FailureOnlyAccount bool
	// AccountLimit 是账号维度使用的独立阈值；<=0 表示与 limit 相同。
	//
	// 这是一个**有意的可用性/安全性取舍**：按账号限流在阻断定向爆破的同时，
	// 也允许攻击者用受害者邮箱反复失败把受害者锁在窗口外（account lockout）。
	// 因此账号阈值必须**显著高于**该端点的 IP 阈值，绝不更严：IP 维度是「同一
	// 出口的滥用预算」，账号维度只是「针对单个身份的爆破预算」。具体取值集中在
	// server/routes/auth.go 的 authAccount*RateLimit 常量里，按线上观测调整。
	AccountLimit int
	// SkipPrivateClientIP 为 true 时，客户端地址不是全局单播地址（回环/内网/
	// 链路本地）的请求不参与 IP 维度计数。用于公开接口避免把整条反代链路合并
	// 进同一个桶；账号维度不受此开关影响。
	SkipPrivateClientIP bool
}

var rateLimitScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
local repaired = 0
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
elseif ttl == -1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  repaired = 1
end
return {current, repaired}
`)

// rateLimitRun 允许测试覆写脚本执行逻辑
var rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
	values, err := rateLimitScript.Run(ctx, client, []string{key}, windowMillis).Slice()
	if err != nil {
		return 0, false, err
	}
	if len(values) < 2 {
		return 0, false, fmt.Errorf("rate limit script returned %d values", len(values))
	}
	count, err := parseInt64(values[0])
	if err != nil {
		return 0, false, err
	}
	repaired, err := parseInt64(values[1])
	if err != nil {
		return 0, false, err
	}
	return count, repaired == 1, nil
}

// RateLimiter Redis 速率限制器
type RateLimiter struct {
	redis  *redis.Client
	prefix string
}

// NewRateLimiter 创建速率限制器实例
func NewRateLimiter(redisClient *redis.Client) *RateLimiter {
	return &RateLimiter{
		redis:  redisClient,
		prefix: "rate_limit:",
	}
}

// AllowResult 单次固定窗口限流判定结果。
type AllowResult struct {
	// Allowed 是否放行
	Allowed bool
	// Count 当前窗口内累计请求数（含本次）
	Count int64
	// RetryAfter 超限时距窗口重置的剩余时间（尽力而为；PTTL 不可用时回退为完整窗口）
	RetryAfter time.Duration
}

// Allow 对给定 key（不含 "rate_limit:" 前缀）执行一次固定窗口计数判定。
// 供需要自定义限流维度（如按用户 ID）的调用方使用；Redis 错误由调用方决定 fail-open/close。
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (AllowResult, error) {
	redisKey := r.prefix + key
	windowMillis := windowTTLMillis(window)

	count, repaired, err := rateLimitRun(ctx, r.redis, redisKey, windowMillis)
	if err != nil {
		return AllowResult{}, err
	}
	if repaired {
		log.Printf("[RateLimit] ttl repaired: key=%s window_ms=%d", redisKey, windowMillis)
	}

	result := AllowResult{Allowed: count <= int64(limit), Count: count}
	if !result.Allowed {
		result.RetryAfter = window
		if ttl, ttlErr := r.redis.PTTL(ctx, redisKey).Result(); ttlErr == nil && ttl > 0 {
			result.RetryAfter = ttl
		}
	}
	return result, nil
}

// maxAccountKeyBodyBytes 是提取账号维度时愿意读取的请求体上限。
// 超限请求不参与账号维度计数（回落 IP 维度），避免为构造限流键把大 body
// 整个读进内存。登录/注册/发码/优惠码请求体远小于该值。
const maxAccountKeyBodyBytes = 64 << 10

// AccountKeyFromJSONBodyField 返回一个从 JSON 请求体指定字段取账号标识的
// AccountKey 实现。字段值先经 normalize（可为 nil）归一，再取 sha256 前 16 位
// 十六进制，因此 Redis 键里不会出现邮箱等明文 PII。
//
// normalize 用于让同一身份的不同写法落在同一个桶（邮箱别名/点号变体等）。
// 调用方应传入仓库既有的归一化函数（service.NormalizeEmailForAliasDedup），
// 不要各自再写一套，否则攻击者可以用 victim+1@ 之类的别名绕过账号维度。
//
// 读取请求体后用 httputil.PrereadBody 回填，下游 handler 仍可正常绑定。
// 请求体不是合法 JSON、字段缺失、字段不是字符串或体积超限时返回空串。
func AccountKeyFromJSONBodyField(field string, normalize func(string) string) func(*gin.Context) string {
	return AccountKeyFromJSONBodyFields([]string{field}, normalize)
}

// AccountKeyFromJSONBodyFields 是 AccountKeyFromJSONBodyField 的多字段版本：
// 按顺序取第一个存在的字符串字段。用于同一端点存在多种身份载体的场景，
// 例如 /auth/login/2fa 的请求体里只有 temp_token（没有邮箱），
// 按 temp_token 计数即可把一次 2FA 会话内对 6 位动态码的爆破限制住。
func AccountKeyFromJSONBodyFields(fields []string, normalize func(string) string) func(*gin.Context) string {
	normalizedFields := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			normalizedFields = append(normalizedFields, trimmed)
		}
	}
	if len(normalizedFields) == 0 {
		return nil
	}
	return func(c *gin.Context) string {
		if c == nil || c.Request == nil || c.Request.Body == nil {
			return ""
		}
		if c.Request.ContentLength > maxAccountKeyBodyBytes {
			// 不读也不回填：下游读取时仍会看到原始 Body。
			return ""
		}
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			return ""
		}
		// 无论解析是否成功都必须回填，否则下游 handler 读不到请求体。
		c.Request.Body = httputil.NewPrereadBody(body)
		if len(body) == 0 || len(body) > maxAccountKeyBodyBytes {
			return ""
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return ""
		}
		for _, field := range normalizedFields {
			raw, ok := payload[field].(string)
			if !ok || strings.TrimSpace(raw) == "" {
				continue
			}
			if normalize != nil {
				raw = normalize(raw)
			}
			return HashAccountKey(raw)
		}
		return ""
	}
}

// HashAccountKey 把已经归一化的账号标识做不可逆摘要，供限流键使用。
// 返回值为 sha256 十六进制前 16 位；输入为空时返回空串。
func HashAccountKey(account string) string {
	normalized := strings.ToLower(strings.TrimSpace(account))
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])[:16]
}

const accountFailureContextKey = "sub2api.rate_limit.account_recorder"

// rateLimitRecorder 返回一个把失败/成功记到账号桶的闭包，交给 handler 调用。
func rateLimitRecorder(r *RateLimiter, key, account string) func(failed bool) {
	return func(failed bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		if failed {
			err = r.RecordAccountFailure(ctx, key, account)
		} else {
			err = r.ResetAccountFailure(ctx, key, account)
		}
		if err != nil {
			log.Printf("[RateLimit] account failure accounting failed: key=%s failed=%t err=%v", r.prefix+key, failed, err)
		}
	}
}

// RecordAccountOutcome 记录本次请求的账号维度结局。
// failed=true 表示凭据校验失败（递增失败计数），false 表示成功（清零）。
// 未启用 FailureOnlyAccount 的端点上调用它是空操作。
func RecordAccountOutcome(c *gin.Context, failed bool) {
	if c == nil {
		return
	}
	value, exists := c.Get(accountFailureContextKey)
	if !exists {
		return
	}
	recorder, ok := value.(func(failed bool))
	if !ok || recorder == nil {
		return
	}
	recorder(failed)
}

// AccountFailureScope 是中间件为本请求记录账号失败计数所需的上下文。
type AccountFailureScope struct {
	// Key 是限流中间件的端点键（如 "auth-login"）。
	Key string
	// Account 是归一化后做摘要的账号标签。
	Account string
	// Window 是计数窗口。
	Window time.Duration
}

const accountFailureScopeContextKey = "sub2api.rate_limit.account_scope"

// AccountFailureScopeFromContext 返回本请求的账号失败计数作用域。
// 未启用 FailureOnlyAccount 或取不到账号时 ok=false。
func AccountFailureScopeFromContext(c *gin.Context) (AccountFailureScope, bool) {
	if c == nil {
		return AccountFailureScope{}, false
	}
	value, exists := c.Get(accountFailureScopeContextKey)
	if !exists {
		return AccountFailureScope{}, false
	}
	scope, valid := value.(AccountFailureScope)
	if !valid || scope.Account == "" {
		return AccountFailureScope{}, false
	}
	return scope, true
}

// AccountFailureLabel 返回中间件为本请求解析出的账号标签（已做过不可逆摘要）。
// 未启用失败计数模式时返回空串。
func AccountFailureLabel(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get(accountFailureScopeContextKey)
	if !ok {
		return ""
	}
	label, _ := value.(string)
	return label
}

// accountFailureKey 返回账号失败计数使用的键（不含 "rate_limit:" 前缀）。
func accountFailureKey(key, account string) string {
	return key + ":acct:" + account
}

// 账号失败计数阈值与分级锁定时长。
//
// 取舍说明（account lockout DoS）：
//   - 账号桶**只统计失败**：受害者正常登录（成功）不消耗额度，成功后计数清零。
//   - 锁定采用**短窗口 + 分级**：5 次失败锁 60s，10 次锁 5min，20 次锁 15min。
//     不做更长的锁，是因为锁越久越像攻击者手里的杠杆；短窗口 + 成功后清零已经
//     足以把爆破成本抬高到无利可图，同时把对受害者的骚扰限制在"每次 60s"粒度。
//   - 残余代价（如实承认）：攻击者仍可以每 60s 投入 5 次失败来持续骚扰某个账号，
//     也可以让受害者在 60s 内无法登录。这是一种**有界的、可自愈的**干扰，
//     而不是可无限维持的锁死：没有攻击者持续投入时，窗口到期自动解锁。
const (
	accountFailureLockThreshold = 5
	accountFailureShortWindow   = 60 * time.Second
	accountFailureMediumWindow  = 5 * time.Minute
	accountFailureLongWindow    = 15 * time.Minute
	accountFailureMediumTier    = 10
	accountFailureLongTier      = 20
)

// accountFailureTierWindow 返回该失败次数对应的计数窗口（同时也是锁定时长上限）。
func accountFailureTierWindow(failures int64) time.Duration {
	switch {
	case failures >= accountFailureLongTier:
		return accountFailureLongWindow
	case failures >= accountFailureMediumTier:
		return accountFailureMediumWindow
	default:
		return accountFailureShortWindow
	}
}

// accountFailureScript 递增失败计数，并按新的失败次数重置 TTL（分级窗口）。
// 返回 {count, pttlMillis}。
var accountFailureScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
local short = tonumber(ARGV[1])
local medium = tonumber(ARGV[2])
local long = tonumber(ARGV[3])
local mediumTier = tonumber(ARGV[4])
local longTier = tonumber(ARGV[5])
local ttl = short
if current >= longTier then
  ttl = long
elseif current >= mediumTier then
  ttl = medium
end
redis.call('PEXPIRE', KEYS[1], ttl)
return {current, ttl}
`)

// accountFailureRun 允许测试覆写失败计数脚本执行逻辑。
var accountFailureRun = func(ctx context.Context, client *redis.Client, key string, args [5]int64) (int64, error) {
	values, err := accountFailureScript.Run(ctx, client, []string{key},
		args[0], args[1], args[2], args[3], args[4]).Slice()
	if err != nil {
		return 0, err
	}
	if len(values) < 1 {
		return 0, fmt.Errorf("account failure script returned %d values", len(values))
	}
	return parseInt64(values[0])
}

// AccountFailureCount 返回账号当前的失败次数（只读，不递增，不续期）。
func (r *RateLimiter) AccountFailureCount(ctx context.Context, key, account string) (int64, error) {
	value, err := r.redis.Get(ctx, r.prefix+accountFailureKey(key, account)).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return value, nil
}

// AccountFailureLock 判断账号当前是否处于锁定状态。
// 锁定条件是失败次数达到 accountFailureLockThreshold；retryAfter 取计数键剩余
// TTL（窗口到期即自动解锁，无需额外清理）。
func (r *RateLimiter) AccountFailureLock(ctx context.Context, key, account string) (bool, time.Duration, error) {
	redisKey := r.prefix + accountFailureKey(key, account)
	count, err := r.redis.Get(ctx, redisKey).Int64()
	if err != nil {
		if err == redis.Nil {
			return false, 0, nil
		}
		return false, 0, err
	}
	if count < accountFailureLockThreshold {
		return false, 0, nil
	}
	retryAfter := accountFailureTierWindow(count)
	if ttl, ttlErr := r.redis.PTTL(ctx, redisKey).Result(); ttlErr == nil && ttl > 0 {
		retryAfter = ttl
	}
	return true, retryAfter, nil
}

// RecordAccountFailure 递增账号失败计数（分级窗口）。
// 只应在 handler 判定凭据失败之后调用。
func (r *RateLimiter) RecordAccountFailure(ctx context.Context, key, account string) error {
	if strings.TrimSpace(account) == "" {
		return nil
	}
	args := [5]int64{
		accountFailureShortWindow.Milliseconds(),
		accountFailureMediumWindow.Milliseconds(),
		accountFailureLongWindow.Milliseconds(),
		accountFailureMediumTier,
		accountFailureLongTier,
	}
	_, err := accountFailureRun(ctx, r.redis, r.prefix+accountFailureKey(key, account), args)
	return err
}

// ResetAccountFailure 清除账号失败计数（登录成功后调用）。
func (r *RateLimiter) ResetAccountFailure(ctx context.Context, key, account string) error {
	if strings.TrimSpace(account) == "" {
		return nil
	}
	return r.redis.Del(ctx, r.prefix+accountFailureKey(key, account)).Err()
}

// clientIPForRateLimit 返回 IP 维度限流使用的客户端地址。
// 与审计日志/会话绑定/API Key IP ACL 共用同一套安全客户端 IP 解析
// （SessionBindingContext 快照：兼容开关开启时信任反代转发头，关闭时走
// server.trusted_proxies 可信链）。避免默认反代部署下 Gin ClientIP 恒等于
// 代理地址、所有用户坍缩进同一个限流桶造成整体误拦截。
func clientIPForRateLimit(c *gin.Context) string {
	// 取「最保守」的客户端地址：GetClientIP 会按请求快照选择解析路径
	// （兼容开关开启 => 受信对端闸门之后的转发头；关闭 => server.trusted_proxies
	// 可信链），并且在拿不到对端时回落到 gin 的结果。
	//
	// 这里不能用 c.ClientIP() 兜底：gin.New() 默认信任所有对端（0.0.0.0/0），
	// 任何漏调 SetTrustedProxies 的入口都会让原始 X-Real-IP 直接决定限流桶，
	// 正是 F-01 的绕过面。
	if resolved := ippkg.GetClientIP(c); resolved != "" {
		return resolved
	}
	return c.ClientIP()
}

// Limit 返回速率限制中间件
// key: 限制类型标识
// limit: 时间窗口内最大请求数
// window: 时间窗口
func (r *RateLimiter) Limit(key string, limit int, window time.Duration) gin.HandlerFunc {
	return r.LimitWithOptions(key, limit, window, RateLimitOptions{})
}

// LimitWithOptions 返回速率限制中间件（带可选配置）
func (r *RateLimiter) LimitWithOptions(key string, limit int, window time.Duration, opts RateLimitOptions) gin.HandlerFunc {
	failureMode := opts.FailureMode
	if failureMode != RateLimitFailClose {
		failureMode = RateLimitFailOpen
	}

	return func(c *gin.Context) {
		if opts.FailureOnlyAccount && opts.AccountKey != nil {
			account := strings.TrimSpace(opts.AccountKey(c))
			if account != "" {
				// 只读查锁，绝不在这里 INCR：否则正常用户每次登录都会消耗额度，
				// 攻击者只需拿受害者邮箱发几次失败就能把受害者长期锁死。
				locked, retryAfter, err := r.AccountFailureLock(c.Request.Context(), key, account)
				if err != nil {
					log.Printf("[RateLimit] redis error: key=%s mode=%s err=%v", r.prefix+key, failureModeLabel(failureMode), err)
					if failureMode == RateLimitFailClose {
						abortRateLimit(c, window)
						return
					}
				} else if locked {
					// 429 + Retry-After：不要返回 401，那会误导成「密码错」。
					abortRateLimit(c, retryAfter)
					return
				}
				// 把账号标签 + 记账闭包暴露给 handler：
				//   失败 => INCR（分级窗口）；成功 => DEL 清零。
				c.Set(accountFailureScopeContextKey, AccountFailureScope{Key: key, Account: account, Window: window})
				c.Set(accountFailureContextKey, rateLimitRecorder(r, key, account))
			}
		}

		dimensions := rateLimitDimensions(c, key, limit, opts)
		for _, dimension := range dimensions {
			result, err := r.Allow(c.Request.Context(), dimension.key, dimension.limit, window)
			if err != nil {
				log.Printf("[RateLimit] redis error: key=%s mode=%s err=%v", r.prefix+dimension.key, failureModeLabel(failureMode), err)
				if failureMode == RateLimitFailClose {
					abortRateLimit(c, window)
					return
				}
				// Redis 错误时放行，避免影响正常服务
				c.Next()
				return
			}

			// 超过限制
			if !result.Allowed {
				abortRateLimit(c, result.RetryAfter)
				return
			}
		}

		c.Next()
	}
}

// rateLimitDimensions 返回本次请求需要逐个判定的限流键。
//
// 顺序固定为「账号维度 → IP 维度」：账号是主防线，先判账号可以让被针对的账号
// 在 IP 维度耗尽预算之前就被拦下，避免攻击者用同一账号把自己的 IP 桶打满后
// 影响同出口的其它用户。
type rateLimitDimension struct {
	key   string
	limit int
}

func rateLimitDimensions(c *gin.Context, key string, limit int, opts RateLimitOptions) []rateLimitDimension {
	dimensions := make([]rateLimitDimension, 0, 2)
	if opts.AccountKey != nil && !opts.FailureOnlyAccount {
		if account := strings.TrimSpace(opts.AccountKey(c)); account != "" {
			accountLimit := opts.AccountLimit
			if accountLimit <= 0 {
				accountLimit = limit
			}
			dimensions = append(dimensions, rateLimitDimension{key: key + ":acct:" + account, limit: accountLimit})
		}
	}
	clientIP := clientIPForRateLimit(c)
	if opts.SkipPrivateClientIP && !isPubliclyRoutableIP(clientIP) && len(dimensions) > 0 {
		// 私网地址不单独开桶（避免整条反代链路坍缩），但只有在账号维度已经
		// 生效时才允许跳过 IP 维度。否则（畸形 body、取不到账号）必须回落
		// IP 维度：宁可让同一出口共用一个桶，也绝不允许某条请求完全不计数。
		return dimensions
	}
	return append(dimensions, rateLimitDimension{key: key + ":" + clientIP, limit: limit})
}

// isPubliclyRoutableIP 判断地址是否为可作为限流依据的全局单播地址。
func isPubliclyRoutableIP(clientIP string) bool {
	parsed := net.ParseIP(strings.TrimSpace(clientIP))
	if parsed == nil {
		return false
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsUnspecified() ||
		parsed.IsLinkLocalUnicast() || parsed.IsLinkLocalMulticast() {
		return false
	}
	return parsed.IsGlobalUnicast()
}

func windowTTLMillis(window time.Duration) int64 {
	ttl := window.Milliseconds()
	if ttl < 1 {
		return 1
	}
	return ttl
}

func abortRateLimit(c *gin.Context, retryAfter time.Duration) {
	if retryAfter > 0 {
		seconds := int64(retryAfter / time.Second)
		if retryAfter%time.Second > 0 {
			seconds++
		}
		c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	}
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"error":   "rate limit exceeded",
		"message": "Too many requests, please try again later",
	})
}

func failureModeLabel(mode RateLimitFailureMode) string {
	if mode == RateLimitFailClose {
		return "fail-close"
	}
	return "fail-open"
}

func parseInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unexpected value type %T", value)
	}
}
