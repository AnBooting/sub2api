//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	ratelimitmw "github.com/Wei-Shaw/sub2api/internal/middleware"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// 这些用例走真实的 RegisterAuthRoutes + 真实 Redis 协议（miniredis），
// 只把 handler 换成空实现：限流是中间件职责，断言的是「请求有没有被放行到 handler」。

func newAccountRateLimitRouter(t *testing.T, rdb *redis.Client) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// handler 是零值（内部依赖为 nil），未被限流放行的请求会在 handler 内 panic；
	// 用 Recovery 把它转成 500。于是「非 429」= 请求已通过限流层。
	router.Use(gin.Recovery())
	v1 := router.Group("/api/v1")

	RegisterAuthRoutes(
		v1,
		&handler.Handlers{
			Auth:    &handler.AuthHandler{},
			Setting: &handler.SettingHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		rdb,
		nil,
		nil,
	)
	return router
}

func postLoginBody(router *gin.Engine, remoteAddr, realIP, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	if realIP != "" {
		req.Header.Set("X-Real-IP", realIP)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func loginBody(email string) string {
	return `{"email":"` + email + `","password":"wrong"}`
}

// F-01 线上复现手法的路由级回归：轮换 X-Real-IP 不能绕过 /auth/login 的 IP 维度限流。
// 前 20 次放行到 handler，第 21 次起 429；轮换头不改变任何一次判定。
func TestAuthLoginRateLimitCannotBeBypassedByRotatingForwardedHeader(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	router := newAccountRateLimitRouter(t, rdb)

	codes := make([]int, 0, 25)
	for i := 0; i < 25; i++ {
		// 每请求轮换一个公网 X-Real-IP（线上 25 次全部 401、0 次 429 的手法）
		rec := postLoginBody(router, "203.0.113.50:5000", "198.51.100."+itoa(10+i), loginBody("nobody@example.invalid"))
		codes = append(codes, rec.Code)
	}

	require.NotEqual(t, http.StatusTooManyRequests, codes[19], "前 20 次应放行到 handler: %v", codes)
	require.Equal(t, http.StatusTooManyRequests, codes[20], "第 21 次必须被拦下（线上绕过点）: %v", codes)
	require.Equal(t, http.StatusTooManyRequests, codes[24])
}

// 账号维度只统计**失败**：单纯重复请求同一邮箱（handler 未记账）不得触发账号锁，
// 否则攻击者只需反复请求受害者邮箱就能把受害者锁死（account lockout DoS）。
func TestAuthLoginAccountDimensionNeverLocksByRequestCount(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	router := newAccountRateLimitRouter(t, rdb)

	for i := 0; i < authLoginAccountRateLimit+10; i++ {
		// 每次换来源地址，让 IP 维度不成为干扰因素
		rec := postLoginBody(router, "198.51."+itoa(1+i%250)+".7:5000", "", loginBody("victim@example.com"))
		require.NotEqual(t, http.StatusTooManyRequests, rec.Code,
			"第 %d 次请求不得因账号维度被拦（失败计数模式）", i+1)
	}
}

// 账号桶确实由失败计数驱动：handler 记 1 次失败后计数 +1（与 middleware 包同一套原语）。
func TestAuthLoginAccountFailureCounterIsWired(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	limiter := ratelimitmw.NewRateLimiter(rdb)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(limiter.LimitWithOptions("auth-login", authLoginIPRateLimit, time.Minute, ratelimitmw.RateLimitOptions{
		FailureMode:        ratelimitmw.RateLimitFailClose,
		AccountKey:         authAccountKey(),
		FailureOnlyAccount: true,
	}))
	router.POST("/login", func(c *gin.Context) {
		ratelimitmw.RecordAccountOutcome(c, true)
		c.Status(http.StatusUnauthorized)
	})

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginBody("victim@example.com")))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:5000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())

	count, err := limiter.AccountFailureCount(t.Context(), "auth-login",
		ratelimitmw.HashAccountKey(service.NormalizeEmailForAliasDedup("victim@example.com")))
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

// 别名变体必须落在同一个账号桶：否则攻击者用 victim+1@ 就能扩预算。
func TestAuthAccountKeyFoldsEmailAliases(t *testing.T) {
	key := authAccountKey()
	require.NotNil(t, key)

	keys := make(map[string]struct{})
	for _, email := range []string{
		"victim@gmail.com",
		"victim+1@gmail.com",
		"v.i.c.t.i.m@gmail.com",
		"victim@googlemail.com",
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(loginBody(email)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.9:5000"
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		keys[key(c)] = struct{}{}
	}
	require.Len(t, keys, 1, "Gmail 点号/加号别名与 googlemail.com 必须归一: %v", keys)
}

// 阈值次序：账号维度不得比 IP 维度更严，否则等于给攻击者一把锁号工具。
func TestAuthAccountRateLimitsAreLooserThanIPRateLimits(t *testing.T) {
	require.Greater(t, authLoginAccountRateLimit, authLoginIPRateLimit,
		"登录：账号阈值必须显著高于 IP 阈值（account lockout 风险）")
	require.GreaterOrEqual(t, authLowFrequencyAccountRateLimit, authLowFrequencyIPRateLimit,
		"注册/发码：账号阈值不得低于 IP 阈值")
	require.GreaterOrEqual(t, authPromoAccountRateLimit, authPromoIPRateLimit)
}

type accountLockProbeRepo struct {
	service.UserRepository
	attempts int
}

func (r *accountLockProbeRepo) GetByEmail(context.Context, string) (*service.User, error) {
	r.attempts++
	return nil, service.ErrUserNotFound
}

func TestAuthLoginLockedAccountHonorsGinCaseAndRejectsBypasses(t *testing.T) {
	cases := []struct {
		name, body string
		want       int
	}{
		{"uppercase_still_locked", `{"Email":"victim@example.com","password":"wrong"}`, http.StatusTooManyRequests},
		{"mixed_case_decoy", `{"email":"decoy@example.com","Email":"victim@example.com","password":"wrong"}`, http.StatusBadRequest},
		{"duplicate_field", `{"email":"victim@example.com","email":"victim@example.com","password":"wrong"}`, http.StatusBadRequest},
		{"trailing_json", `{"email":"victim@example.com","password":"wrong"} {}`, http.StatusBadRequest},
		{"oversize", `{"email":"victim@example.com","password":"wrong","pad":"` + strings.Repeat("x", 65536) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			repo := &accountLockProbeRepo{}
			authService := service.NewAuthService(nil, repo, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil)
			h := handler.NewAuthHandler(&config.Config{}, authService, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			RegisterAuthRoutes(router.Group("/api/v1"), &handler.Handlers{Auth: h, Setting: &handler.SettingHandler{}},
				servermiddleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
				servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), rdb, nil, nil)
			for i := 0; i < 5; i++ {
				require.Equal(t, http.StatusUnauthorized, postLoginBody(router, "203.0.113.50:5000", "", loginBody("victim@example.com")).Code)
			}
			require.Equal(t, http.StatusTooManyRequests, postLoginBody(router, "203.0.113.50:5000", "", loginBody("victim@example.com")).Code)
			attempts := repo.attempts
			rec := postLoginBody(router, "203.0.113.50:5000", "", tc.body)
			require.Equal(t, tc.want, rec.Code)
			require.Equal(t, attempts, repo.attempts, "rejected or locked request must not reach credential service")
		})
	}
}

func TestAuthAccountJSONRoutesRejectInvalidAndOversizedBeforeHandler(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newAccountRateLimitRouter(t, rdb)
	cases := []struct {
		path, body string
		want       int
	}{
		{"/api/v1/auth/register", `{"email":"a@example.com","Email":"b@example.com"}`, 400},
		{"/api/v1/auth/send-verify-code", `{"email":"a@example.com"} {}`, 400},
		{"/api/v1/auth/login/2fa", `{"temp_token":"a","Temp_Token":"b"}`, 400},
		{"/api/v1/auth/validate-promo-code", `{"code":"a","Code":"b"}`, 400},
		{"/api/v1/auth/validate-promo-code", `{"code":"a","pad":"` + strings.Repeat("x", 65536) + `"}`, 413},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = -1
		req.RemoteAddr = "203.0.113.50:5000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tc.want, rec.Code, tc.path)
	}
}

func TestAuthPromoRateLimitUsesCode(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	router := newAccountRateLimitRouter(t, rdb)
	for i := 0; i < authPromoAccountRateLimit; i++ {
		body := `{"code":"same-code"}`
		if i%2 == 1 {
			body = `{"Code":"same-code"}`
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate-promo-code", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113." + itoa(20+i) + ":5000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.NotEqual(t, http.StatusTooManyRequests, rec.Code, "request %d", i)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/validate-promo-code", strings.NewReader(`{"code":"same-code"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.99:5000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
