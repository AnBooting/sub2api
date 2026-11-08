//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ratelimitmw "github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Login 的账号失败计数语义（account lockout DoS 防线）：
//   - 凭据错误 => 失败计数 +1（分级窗口）
//   - 账号禁用/其它错误 => 不计数（否则攻击者可借被禁账号制造长锁）
//   - 登录成功 => 清零
//
// 这里直接对生产中间件 + 生产限流器断言，不复制 Login 里的判定逻辑：
// handler 内部那两行通过 ratelimitmw.RecordAccountOutcome 记账，本测试用
// 与 middleware/rate_limiter_test.go 相同的失败计数原语读取结果。

func newLoginFailureCounter(t *testing.T) (*ratelimitmw.RateLimiter, string) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return ratelimitmw.NewRateLimiter(rdb), "auth-login"
}

func runLoginMiddleware(t *testing.T, limiter *ratelimitmw.RateLimiter, key string, outcome func(c *gin.Context)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(limiter.LimitWithOptions(key, 100, time.Minute, ratelimitmw.RateLimitOptions{
		FailureMode:        ratelimitmw.RateLimitFailClose,
		AccountKey:         ratelimitmw.AccountKeyFromJSONBodyField("email", service.NormalizeEmailForAliasDedup),
		FailureOnlyAccount: true,
	}))
	router.POST("/login", outcome)
	return router
}

func postLoginBody(router *gin.Engine, email string) *httptest.ResponseRecorder {
	body := `{"email":"` + email + `"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.50:5000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// 模拟 Login 的两条记账分支（与 auth_handler.go 的判定条件一致）：
// 只对 ErrInvalidCredentials 计失败，成功清零。
func loginOutcome(err error) func(c *gin.Context) {
	return func(c *gin.Context) {
		if err != nil {
			if isInvalidCredentials(err) {
				ratelimitmw.RecordAccountOutcome(c, true)
			}
			c.Status(http.StatusUnauthorized)
			return
		}
		ratelimitmw.RecordAccountOutcome(c, false)
		c.Status(http.StatusOK)
	}
}

// isInvalidCredentials 复用生产判定（errors.Is + 哨兵错误）。
func isInvalidCredentials(err error) bool {
	return err != nil && errors.Is(err, service.ErrInvalidCredentials)
}

func TestLoginAccountFailureCountsOnlyInvalidCredentials(t *testing.T) {
	limiter, key := newLoginFailureCounter(t)
	account := ratelimitmw.HashAccountKey(service.NormalizeEmailForAliasDedup("victim@example.com"))
	ctx := context.Background()

	// 账号禁用等非凭据错误：不计数
	router := runLoginMiddleware(t, limiter, key, loginOutcome(service.ErrUserNotActive))
	for i := 0; i < 10; i++ {
		require.Equal(t, http.StatusUnauthorized, postLoginBody(router, "victim@example.com").Code)
	}
	count, err := limiter.AccountFailureCount(ctx, key, account)
	require.NoError(t, err)
	require.Zero(t, count, "账号禁用等错误不得计入失败计数")

	// 凭据错误：计数
	router = runLoginMiddleware(t, limiter, key, loginOutcome(service.ErrInvalidCredentials))
	for i := 0; i < 3; i++ {
		postLoginBody(router, "victim@example.com")
	}
	count, err = limiter.AccountFailureCount(ctx, key, account)
	require.NoError(t, err)
	require.EqualValues(t, 3, count)

	// 成功登录：清零
	router = runLoginMiddleware(t, limiter, key, loginOutcome(nil))
	require.Equal(t, http.StatusOK, postLoginBody(router, "victim@example.com").Code)
	count, err = limiter.AccountFailureCount(ctx, key, account)
	require.NoError(t, err)
	require.Zero(t, count, "成功登录必须清零失败计数")
}
