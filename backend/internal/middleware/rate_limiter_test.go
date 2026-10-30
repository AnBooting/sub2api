package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWindowTTLMillis(t *testing.T) {
	require.Equal(t, int64(1), windowTTLMillis(500*time.Microsecond))
	require.Equal(t, int64(1), windowTTLMillis(1500*time.Microsecond))
	require.Equal(t, int64(2), windowTTLMillis(2500*time.Microsecond))
}

func TestRateLimiterFailureModes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	limiter := NewRateLimiter(rdb)

	failOpenRouter := gin.New()
	failOpenRouter.Use(limiter.Limit("test", 1, time.Second))
	failOpenRouter.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	failOpenRouter.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	failCloseRouter := gin.New()
	failCloseRouter.Use(limiter.LimitWithOptions("test", 1, time.Second, RateLimitOptions{
		FailureMode: RateLimitFailClose,
	}))
	failCloseRouter.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	failCloseRouter.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

func TestRateLimiterDifferentIPsIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	callCounts := make(map[string]int64)
	originalRun := rateLimitRun
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		callCounts[key]++
		return callCounts[key], false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))

	router := gin.New()
	router.Use(limiter.Limit("api", 1, time.Second))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 第一个 IP 的请求应通过
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code, "第一个 IP 的第一次请求应通过")

	// 第二个 IP 的请求应独立通过（不受第一个 IP 的计数影响）
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "10.0.0.2:5678"
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "第二个 IP 的第一次请求应独立通过")

	// 第一个 IP 的第二次请求应被限流
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "10.0.0.1:1234"
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusTooManyRequests, rec3.Code, "第一个 IP 的第二次请求应被限流")
}

func TestRateLimiterAllow(t *testing.T) {
	originalRun := rateLimitRun
	var gotKey string
	count := int64(0)
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		gotKey = key
		count++
		return count, false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	// PTTL 走真实客户端（不可达地址）→ 失败后 RetryAfter 应回退为完整窗口
	limiter := NewRateLimiter(redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	}))

	res, err := limiter.Allow(context.Background(), "panel:global:user:42", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Equal(t, int64(1), res.Count)
	require.Zero(t, res.RetryAfter)
	require.Equal(t, "rate_limit:panel:global:user:42", gotKey)

	res, err = limiter.Allow(context.Background(), "panel:global:user:42", 1, time.Minute)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Equal(t, int64(2), res.Count)
	require.Equal(t, time.Minute, res.RetryAfter)
}

func TestRateLimiterHonorsForwardedIPSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	callCounts := make(map[string]int64)
	originalRun := rateLimitRun
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		callCounts[key]++
		return callCounts[key], false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))

	router := gin.New()
	// 模拟 SessionBindingContext：开启转发 IP 兼容模式快照
	router.Use(func(c *gin.Context) {
		ippkg.SetForwardedIPSettings(c, true, nil)
		c.Next()
	})
	router.Use(limiter.Limit("fwd", 1, time.Second))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	send := func(xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		// 所有请求都来自同一个反代地址
		req.RemoteAddr = "127.0.0.1:5678"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// 反代后两个不同的真实客户端应各自独立计数，不因共享代理地址被合并限流
	require.Equal(t, http.StatusOK, send("198.51.100.1"))
	require.Equal(t, http.StatusOK, send("198.51.100.2"))
	// 同一真实客户端第二次请求应被限流
	require.Equal(t, http.StatusTooManyRequests, send("198.51.100.1"))

	require.Contains(t, callCounts, "rate_limit:fwd:198.51.100.1")
	require.Contains(t, callCounts, "rate_limit:fwd:198.51.100.2")
}

func TestRateLimiterSuccessAndLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	counts := []int64{1, 2}
	callIndex := 0
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		if callIndex >= len(counts) {
			return counts[len(counts)-1], false, nil
		}
		value := counts[callIndex]
		callIndex++
		return value, false, nil
	}
	t.Cleanup(func() {
		rateLimitRun = originalRun
	})

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))

	router := gin.New()
	router.Use(limiter.Limit("test", 1, time.Second))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)

	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

// ---- 账号维度（失败计数模式）：主防线，且不得变成 account lockout DoS ----

// newFailureModeTestLimiter 使用 miniredis：失败计数与锁定断言需要真实的
// GET/INCR/PEXPIRE/DEL 语义，不能用不可达地址（那样每个调用都报错）。
func newFailureModeTestLimiter(t *testing.T) *RateLimiter {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRateLimiter(rdb)
}

func newAccountFailureTestRouter(t *testing.T, limiter *RateLimiter, handler gin.HandlerFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(limiter.LimitWithOptions("auth-login", 100, time.Minute, RateLimitOptions{
		FailureMode:        RateLimitFailClose,
		AccountKey:         AccountKeyFromJSONBodyField("email", nil),
		FailureOnlyAccount: true,
	}))
	router.POST("/login", handler)
	return router
}

func postAccountLogin(router *gin.Engine, email, outcome string) *httptest.ResponseRecorder {
	body := `{"email":"` + email + `"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.50:5000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	_ = outcome
	return rec
}

// 关键验收：走**真实中间件**的请求不按次数消耗账号额度——只有 handler 判定失败
// 才记账。这是 account lockout DoS 的防线：若中间件"查一次就算一次"，正常用户
// 反复打开登录页就会把自己锁死。
func TestAccountFailureLockIsFailureOnlyAndSelfHealing(t *testing.T) {
	limiter := newFailureModeTestLimiter(t)
	ctx := context.Background()
	account := HashAccountKey("victim@example.com")

	// handler 不记账（例如请求体校验失败、既不是凭据错误也不是成功登录）：
	// 这类请求绝不能消耗账号额度。
	router := newAccountFailureTestRouter(t, limiter, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	for i := 0; i < 20; i++ {
		rec := postAccountLogin(router, "victim@example.com", "")
		require.Equal(t, http.StatusOK, rec.Code, "第 %d 次请求必须放行（中间件不得计数）", i+1)
	}

	count, err := limiter.AccountFailureCount(ctx, "auth-login", account)
	require.NoError(t, err)
	require.Zero(t, count, "中间件不得按请求次数递增账号桶")

	// 只读查锁同样不产生计数。
	for i := 0; i < 20; i++ {
		locked, _, lockErr := limiter.AccountFailureLock(ctx, "auth-login", account)
		require.NoError(t, lockErr)
		require.False(t, locked)
	}
	count, err = limiter.AccountFailureCount(ctx, "auth-login", account)
	require.NoError(t, err)
	require.Zero(t, count, "只读查锁不得产生计数")
}

func TestAccountFailureLockThresholdAndReset(t *testing.T) {
	limiter := newFailureModeTestLimiter(t)

	ctx := context.Background()
	account := HashAccountKey("victim@example.com")

	for i := 0; i < accountFailureLockThreshold; i++ {
		locked, _, err := limiter.AccountFailureLock(ctx, "auth-login", account)
		require.NoError(t, err)
		require.False(t, locked, "第 %d 次失败之前不应锁定", i+1)
		require.NoError(t, limiter.RecordAccountFailure(ctx, "auth-login", account))
	}

	locked, retryAfter, err := limiter.AccountFailureLock(ctx, "auth-login", account)
	require.NoError(t, err)
	require.True(t, locked, "达到阈值必须锁定")
	require.Greater(t, retryAfter, time.Duration(0))
	require.LessOrEqual(t, retryAfter, accountFailureShortWindow)

	// 成功登录清零，攻击者无法维持锁定。
	require.NoError(t, limiter.ResetAccountFailure(ctx, "auth-login", account))
	locked, _, err = limiter.AccountFailureLock(ctx, "auth-login", account)
	require.NoError(t, err)
	require.False(t, locked)
}

func TestAccountFailureLockTiersEscalate(t *testing.T) {
	limiter := newFailureModeTestLimiter(t)

	ctx := context.Background()
	account := HashAccountKey("victim2@example.com")

	require.Equal(t, accountFailureShortWindow, accountFailureTierWindow(accountFailureLockThreshold))
	require.Equal(t, accountFailureMediumWindow, accountFailureTierWindow(accountFailureMediumTier))
	require.Equal(t, accountFailureLongWindow, accountFailureTierWindow(accountFailureLongTier))

	for i := 0; i < accountFailureLongTier; i++ {
		require.NoError(t, limiter.RecordAccountFailure(ctx, "auth-login", account))
	}
	// 达到最高档后剩余 TTL 应接近长窗口
	_, retryAfter, err := limiter.AccountFailureLock(ctx, "auth-login", account)
	require.NoError(t, err)
	require.Greater(t, retryAfter, accountFailureMediumWindow)
}

// 中间件只在锁定时拦截，且拦的是 429（不是 401，避免误导为密码错误）。
func TestAccountFailureMiddlewareAbortsWith429(t *testing.T) {
	limiter := newFailureModeTestLimiter(t)

	account := HashAccountKey("victim3@example.com")
	for i := 0; i < accountFailureLockThreshold; i++ {
		require.NoError(t, limiter.RecordAccountFailure(context.Background(), "auth-login", account))
	}

	router := newAccountFailureTestRouter(t, limiter, func(c *gin.Context) { c.Status(http.StatusOK) })
	rec := postAccountLogin(router, "victim3@example.com", "")
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

// 未锁定时，中间件把记账闭包交给 handler；handler 记 1 次失败后计数 +1。
func TestAccountFailureOutcomeIsRecordedThroughContext(t *testing.T) {
	limiter := newFailureModeTestLimiter(t)

	router := newAccountFailureTestRouter(t, limiter, func(c *gin.Context) {
		_, ok := AccountFailureScopeFromContext(c)
		require.True(t, ok, "中间件必须把账号作用域交给 handler")
		RecordAccountOutcome(c, true)
		c.Status(http.StatusUnauthorized)
	})

	rec := postAccountLogin(router, "victim4@example.com", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	count, err := limiter.AccountFailureCount(context.Background(), "auth-login", HashAccountKey("victim4@example.com"))
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

// 不同邮箱互不干扰。
func TestAccountFailureCountersAreIsolatedPerAccount(t *testing.T) {
	limiter := newFailureModeTestLimiter(t)

	ctx := context.Background()
	for i := 0; i < accountFailureLockThreshold; i++ {
		require.NoError(t, limiter.RecordAccountFailure(ctx, "auth-login", HashAccountKey("a@example.com")))
	}
	locked, _, err := limiter.AccountFailureLock(ctx, "auth-login", HashAccountKey("a@example.com"))
	require.NoError(t, err)
	require.True(t, locked)

	locked, _, err = limiter.AccountFailureLock(ctx, "auth-login", HashAccountKey("b@example.com"))
	require.NoError(t, err)
	require.False(t, locked, "其它账号不得受影响")
}

// 账号键只暴露 sha256 前缀，不得把邮箱明文写进 Redis 键。
func TestAccountKeyFromJSONBodyFieldHashesEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	var got string
	router.POST("/login", func(c *gin.Context) {
		got = AccountKeyFromJSONBodyField("email", nil)(c)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"Victim@Example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.50:5000"
	router.ServeHTTP(httptest.NewRecorder(), req)

	require.Len(t, got, 16)
	require.NotContains(t, strings.ToLower(got), "victim")
	require.NotContains(t, got, "@")

	// 大小写归一：同一账号的不同写法必须落在同一个桶。
	sum := sha256.Sum256([]byte("victim@example.com"))
	require.Equal(t, hex.EncodeToString(sum[:])[:16], got)
	require.Equal(t, got, HashAccountKey("victim@example.com"))
}

// 请求体非法/字段缺失/字段非字符串时不做账号维度计数，且仍要回填请求体。
func TestAccountKeyFromJSONBodyFieldRejectsNonStringOrMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []string{
		"",
		"not-json",
		"{}",
		`{"email":123}`,
		`{"email":null}`,
		`{"other":"a@example.com"}`,
	}
	for _, body := range cases {
		router := gin.New()
		router.POST("/login", func(c *gin.Context) {
			require.Empty(t, AccountKeyFromJSONBodyField("email", nil)(c), "body=%q", body)
			raw, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(raw), "请求体必须被回填")
			c.Status(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.50:5000"
		router.ServeHTTP(httptest.NewRecorder(), req)
	}
}

// 读取账号键不得吞掉请求体，下游 handler 必须还能正常绑定。
func TestRateLimiterAccountKeyPreservesRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		return 1, false, nil
	}
	t.Cleanup(func() { rateLimitRun = originalRun })

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
	router := gin.New()
	router.Use(limiter.LimitWithOptions("auth-login", 10, time.Minute, RateLimitOptions{
		AccountKey: AccountKeyFromJSONBodyField("email", nil),
	}))
	router.POST("/login", func(c *gin.Context) {
		var payload struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": payload.Email, "password": payload.Password})
	})

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"a@example.com","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.50:5000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"email":"a@example.com","password":"secret"}`, rec.Body.String())
}

// SkipPrivateClientIP：非公网客户端地址不参与 IP 维度计数，但账号维度仍然生效。
func TestRateLimiterSkipPrivateClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	counts := make(map[string]int64)
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		counts[key]++
		return counts[key], false, nil
	}
	t.Cleanup(func() { rateLimitRun = originalRun })

	send := func(router *gin.Engine, remoteAddr, email string) int {
		req := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader("{\"email\":\""+email+"\"}"))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	// 账号维度可用时：私网地址不再单独开桶（避免整条反代链路坍缩），
	// 由账号维度承担计数，因此不同账号共享同一私网出口不会被误伤。
	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
	withAccount := gin.New()
	withAccount.Use(limiter.LimitWithOptions("k", 1, time.Minute, RateLimitOptions{
		SkipPrivateClientIP: true,
		AccountKey:          AccountKeyFromJSONBodyField("email", nil),
	}))
	withAccount.POST("/t", func(c *gin.Context) { c.Status(http.StatusOK) })

	require.Equal(t, http.StatusOK, send(withAccount, "10.1.2.3:5000", "a@example.com"))
	require.Equal(t, http.StatusOK, send(withAccount, "10.1.2.3:5000", "b@example.com"),
		"私网出口下不同账号必须各自独立计数")
	require.Equal(t, http.StatusTooManyRequests, send(withAccount, "10.1.2.3:5000", "a@example.com"),
		"同一账号第二次必须被账号维度拦下")
	for key := range counts {
		require.NotContains(t, key, "10.1.2.3", "私网地址不应单独产生 IP 维度计数: %v", counts)
	}

	// 公网地址：即使账号维度可用，IP 维度仍要计数（次防线保留）。
	clear(counts)
	require.Equal(t, http.StatusOK, send(withAccount, "203.0.113.9:5000", "c@example.com"))
	require.Equal(t, http.StatusTooManyRequests, send(withAccount, "203.0.113.9:5000", "d@example.com"),
		"公网地址仍按 IP 计数（次防线）")
}

// 不受信对端轮换 X-Real-IP 时，IP 维度必须落在同一个桶（回归 F-01）。
func TestRateLimiterIgnoresForwardedIPFromUntrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	counts := make(map[string]int64)
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		counts[key]++
		return counts[key], false, nil
	}
	t.Cleanup(func() { rateLimitRun = originalRun })

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ippkg.SetForwardedIPSettings(c, true, nil)
		c.Next()
	})
	router.Use(limiter.LimitWithOptions("auth-login", 3, time.Minute, RateLimitOptions{
		FailureMode: RateLimitFailClose,
	}))
	router.POST("/login", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	codes := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.50:5000"
		req.Header.Set("X-Real-IP", "198.51.100."+strconv.Itoa(10+i))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	require.Equal(t, []int{200, 200, 200, 429, 429}, codes,
		"轮换 X-Real-IP 不得改变 IP 维度分桶: %v", codes)
	require.Len(t, counts, 1, "所有请求必须落在同一个桶: %v", counts)
}

// 归一化钩子：别名/点号变体必须落在同一个桶，否则账号维度可被 aliasing 绕过。
func TestAccountKeyFromJSONBodyFieldAppliesNormalizer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	normalizer := func(email string) string {
		local, domain, ok := strings.Cut(email, "@")
		if !ok {
			return email
		}
		if idx := strings.IndexByte(local, '+'); idx > 0 {
			local = local[:idx]
		}
		return local + "@" + domain
	}
	router := gin.New()
	router.POST("/login", func(c *gin.Context) {
		key := AccountKeyFromJSONBodyField("email", normalizer)(c)
		c.String(http.StatusOK, key)
	})

	keys := make(map[string]struct{})
	for _, email := range []string{"victim@example.com", "victim+1@example.com", "victim+2@example.com"} {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"`+email+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.50:5000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		keys[rec.Body.String()] = struct{}{}
	}
	require.Len(t, keys, 1, "别名变体必须归一到一个账号桶: %v", keys)
}

// 超大 body：不读取、不构造账号键（避免把大 body 读进内存），但请求仍可用。
func TestAccountKeyFromJSONBodyFieldSkipsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	var got string
	router.POST("/login", func(c *gin.Context) {
		got = "sentinel"
		got = AccountKeyFromJSONBodyField("email", nil)(c)
		c.Status(http.StatusOK)
	})

	big := `{"email":"victim@example.com","pad":"` + strings.Repeat("x", maxAccountKeyBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(big))
	req.RemoteAddr = "203.0.113.50:5000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, got, "超大 body 不得用于账号维度键")
}

// SkipPrivateClientIP + 取不到账号维度时，必须回落 IP 维度，不能完全不限流。
func TestRateLimiterNeverAllowsRequestWithZeroDimensions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalRun := rateLimitRun
	counts := make(map[string]int64)
	rateLimitRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64) (int64, bool, error) {
		counts[key]++
		return counts[key], false, nil
	}
	t.Cleanup(func() { rateLimitRun = originalRun })

	limiter := NewRateLimiter(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}))
	router := gin.New()
	router.Use(limiter.LimitWithOptions("k", 1, time.Minute, RateLimitOptions{
		SkipPrivateClientIP: true,
		AccountKey:          AccountKeyFromJSONBodyField("email", nil),
	}))
	router.POST("/t", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 畸形 body => 无账号维度；私网地址 => 本应跳过 IP 维度，但必须回落。
	codes := make([]int, 0, 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader("not-json"))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.1.2.3:5000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	require.Equal(t, []int{200, 429}, codes, "零维度请求必须回落 IP 维度计数: %v | %v", codes, counts)
}
