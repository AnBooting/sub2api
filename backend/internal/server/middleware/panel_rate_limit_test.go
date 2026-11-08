package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// panelRateLimitStubRepo 内存版 SettingRepository，仅覆盖本测试用到的方法。
type panelRateLimitStubRepo struct {
	mu     sync.Mutex
	values map[string]string
}

func (r *panelRateLimitStubRepo) Get(_ context.Context, key string) (*service.Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return nil, service.ErrSettingNotFound
	}
	return &service.Setting{Key: key, Value: value}, nil
}

func (r *panelRateLimitStubRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *panelRateLimitStubRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = make(map[string]string)
	}
	r.values[key] = value
	return nil
}

func (r *panelRateLimitStubRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *panelRateLimitStubRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *panelRateLimitStubRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *panelRateLimitStubRepo) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
	return nil
}

// fakePanelAllower 内存计数版限流原语。
type fakePanelAllower struct {
	mu     sync.Mutex
	counts map[string]int64
	err    error
}

func (f *fakePanelAllower) Allow(_ context.Context, key string, limit int, window time.Duration) (middleware.AllowResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return middleware.AllowResult{}, f.err
	}
	if f.counts == nil {
		f.counts = make(map[string]int64)
	}
	f.counts[key]++
	count := f.counts[key]
	result := middleware.AllowResult{Allowed: count <= int64(limit), Count: count}
	if !result.Allowed {
		result.RetryAfter = window
	}
	return result, nil
}

func newPanelRateLimitTestService(t *testing.T, settingsJSON string) *service.SettingService {
	t.Helper()
	repo := &panelRateLimitStubRepo{}
	if settingsJSON != "" {
		repo.values = map[string]string{"panel_rate_limit_settings": settingsJSON}
	}
	return service.NewSettingService(repo, &config.Config{})
}

type panelTestIdentity struct {
	userID int64
	role   string
}

func newPanelTestRouter(limiter gin.HandlerFunc, identity *panelTestIdentity) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if identity != nil {
		router.Use(func(c *gin.Context) {
			c.Set(string(ContextKeyUser), AuthSubject{UserID: identity.userID})
			c.Set(string(ContextKeyUserRole), identity.role)
			c.Next()
		})
	}
	router.Use(limiter)
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return router
}

func performPanelRequest(router *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPanelRateLimiterGlobalPerUser(t *testing.T) {
	allower := &fakePanelAllower{}
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":2,"heavy_rpm":1,"exempt_admin":true,"public_ip_rpm":0}`),
	}

	userA := newPanelTestRouter(p.Global(), &panelTestIdentity{userID: 1, role: service.RoleUser})
	userB := newPanelTestRouter(p.Global(), &panelTestIdentity{userID: 2, role: service.RoleUser})

	require.Equal(t, http.StatusOK, performPanelRequest(userA, "127.0.0.1:1000").Code)
	require.Equal(t, http.StatusOK, performPanelRequest(userA, "127.0.0.1:1000").Code)
	// 用户 A 超限
	third := performPanelRequest(userA, "127.0.0.1:1000")
	require.Equal(t, http.StatusTooManyRequests, third.Code)
	require.NotEmpty(t, third.Header().Get("Retry-After"))
	require.Contains(t, third.Body.String(), "RATE_LIMITED")
	// 用户 B 不受影响（同一来源 IP 也互不干扰）
	require.Equal(t, http.StatusOK, performPanelRequest(userB, "127.0.0.1:1000").Code)

	allower.mu.Lock()
	defer allower.mu.Unlock()
	require.Contains(t, allower.counts, "panel:global:user:1")
	require.Contains(t, allower.counts, "panel:global:user:2")
}

func TestPanelRateLimiterHeavyUsesHeavyRPM(t *testing.T) {
	allower := &fakePanelAllower{}
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"exempt_admin":true,"public_ip_rpm":0}`),
	}

	router := newPanelTestRouter(p.Heavy(), &panelTestIdentity{userID: 7, role: service.RoleUser})
	require.Equal(t, http.StatusOK, performPanelRequest(router, "127.0.0.1:1000").Code)
	require.Equal(t, http.StatusTooManyRequests, performPanelRequest(router, "127.0.0.1:1000").Code)

	allower.mu.Lock()
	defer allower.mu.Unlock()
	require.Contains(t, allower.counts, "panel:heavy:user:7")
}

func TestPanelRateLimiterAdminExemption(t *testing.T) {
	// 豁免开启：管理员不计数
	p := &PanelRateLimiter{
		limiter:        &fakePanelAllower{},
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":1,"heavy_rpm":1,"exempt_admin":true,"public_ip_rpm":0}`),
	}
	admin := newPanelTestRouter(p.Global(), &panelTestIdentity{userID: 9, role: service.RoleAdmin})
	for i := 0; i < 5; i++ {
		require.Equal(t, http.StatusOK, performPanelRequest(admin, "127.0.0.1:1000").Code)
	}

	// 豁免关闭：管理员一样受限
	p2 := &PanelRateLimiter{
		limiter:        &fakePanelAllower{},
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":1,"heavy_rpm":1,"exempt_admin":false,"public_ip_rpm":0}`),
	}
	admin2 := newPanelTestRouter(p2.Global(), &panelTestIdentity{userID: 9, role: service.RoleAdmin})
	require.Equal(t, http.StatusOK, performPanelRequest(admin2, "127.0.0.1:1000").Code)
	require.Equal(t, http.StatusTooManyRequests, performPanelRequest(admin2, "127.0.0.1:1000").Code)
}

func TestPanelRateLimiterDisabledOrMissingSubject(t *testing.T) {
	// 总开关关闭
	p := &PanelRateLimiter{
		limiter:        &fakePanelAllower{},
		settingService: newPanelRateLimitTestService(t, `{"enabled":false,"user_rpm":1,"heavy_rpm":1,"exempt_admin":true,"public_ip_rpm":1}`),
	}
	router := newPanelTestRouter(p.Global(), &panelTestIdentity{userID: 3, role: service.RoleUser})
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusOK, performPanelRequest(router, "127.0.0.1:1000").Code)
	}

	// 无认证主体：放行（防御分支）
	p2 := &PanelRateLimiter{
		limiter:        &fakePanelAllower{},
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":1,"heavy_rpm":1,"exempt_admin":true,"public_ip_rpm":0}`),
	}
	anonymous := newPanelTestRouter(p2.Global(), nil)
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusOK, performPanelRequest(anonymous, "127.0.0.1:1000").Code)
	}

	// nil 限流器（测试环境注入 nil）：直接放行
	var nilLimiter *PanelRateLimiter
	nilRouter := newPanelTestRouter(nilLimiter.Global(), &panelTestIdentity{userID: 3, role: service.RoleUser})
	require.Equal(t, http.StatusOK, performPanelRequest(nilRouter, "127.0.0.1:1000").Code)
}

func TestPanelRateLimiterFailOpenOnRedisError(t *testing.T) {
	p := &PanelRateLimiter{
		limiter:        &fakePanelAllower{err: errors.New("redis down")},
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":1,"heavy_rpm":1,"exempt_admin":true,"public_ip_rpm":1}`),
	}
	router := newPanelTestRouter(p.Global(), &panelTestIdentity{userID: 5, role: service.RoleUser})
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusOK, performPanelRequest(router, "127.0.0.1:1000").Code)
	}

	publicRouter := newPanelTestRouter(p.PublicIP(), nil)
	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusOK, performPanelRequest(publicRouter, "203.0.113.9:1000").Code)
	}
}

func TestPanelRateLimiterPublicIP(t *testing.T) {
	allower := &fakePanelAllower{}
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":0,"heavy_rpm":0,"exempt_admin":true,"public_ip_rpm":1}`),
	}
	router := newPanelTestRouter(p.PublicIP(), nil)

	// 公网 IP：第二次被限
	require.Equal(t, http.StatusOK, performPanelRequest(router, "203.0.113.9:1000").Code)
	require.Equal(t, http.StatusTooManyRequests, performPanelRequest(router, "203.0.113.9:1000").Code)
	// 其他公网 IP 独立计数
	require.Equal(t, http.StatusOK, performPanelRequest(router, "198.51.100.7:1000").Code)

	allower.mu.Lock()
	require.Contains(t, allower.counts, "panel:public:ip:203.0.113.9")
	require.Contains(t, allower.counts, "panel:public:ip:198.51.100.7")
	allower.mu.Unlock()
}

// 私网/回环地址（未配 trusted_proxies 时的真实解析结果）必须**参与计数**，
// 否则公开接口限流在默认部署下静默失效（F-02 的同类问题）。
// 但坍缩桶必须放宽，避免整条反代链路的用户合并进同一个严苛桶而大面积 429。
func TestPanelRateLimiterPublicIPCountsPrivateAddressesWithWiderBucket(t *testing.T) {
	allower := &fakePanelAllower{}
	// public_ip_rpm=1：若私网地址仍走严苛桶，第二次请求就会被拦（本次修复前的
	// 「跳过计数」行为则是永远放行）。放宽倍率后必须能放行更多次。
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":0,"heavy_rpm":0,"exempt_admin":true,"public_ip_rpm":1}`),
	}
	router := newPanelTestRouter(p.PublicIP(), nil)

	const privateAddr = "10.0.0.8:1000"
	// 放宽后的桶容量 = public_ip_rpm * collapsedPrivateIPBucketMultiplier。
	allowed := 0
	for i := 0; i < collapsedPrivateIPBucketMultiplier+2; i++ {
		if performPanelRequest(router, privateAddr).Code == http.StatusOK {
			allowed++
			continue
		}
		break
	}
	require.Equal(t, collapsedPrivateIPBucketMultiplier, allowed,
		"私网地址必须计数，且桶容量应为 public_ip_rpm * 放宽倍率")
	require.Equal(t, http.StatusTooManyRequests, performPanelRequest(router, privateAddr).Code,
		"坍缩桶耗尽后仍要拦截，不能静默失效")

	allower.mu.Lock()
	defer allower.mu.Unlock()
	// 坍缩桶用独立前缀，键里仍带该私网地址本身：运维既能从 Redis 看出来源，
	// 也能把坍缩流量与公网精确计数分开观察。
	require.Contains(t, allower.counts, "panel:public:ip-collapsed:10.0.0.8")
	for key := range allower.counts {
		require.NotEqual(t, "panel:public:ip:10.0.0.8", key,
			"私网地址不得混入公网精确计数桶")
	}
}

// 放宽只作用于坍缩桶：真实公网客户端必须仍是原阈值，绝不允许借放宽拿到 10 倍额度。
func TestPanelRateLimiterPublicIPPublicAddressKeepsOriginalLimit(t *testing.T) {
	allower := &fakePanelAllower{}
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":0,"heavy_rpm":0,"exempt_admin":true,"public_ip_rpm":3}`),
	}
	router := newPanelTestRouter(p.PublicIP(), nil)

	const publicAddr = "203.0.113.9:1000"
	for i := 1; i <= 3; i++ {
		require.Equal(t, http.StatusOK, performPanelRequest(router, publicAddr).Code, "第 %d 次应放行", i)
	}
	require.Equal(t, http.StatusTooManyRequests, performPanelRequest(router, publicAddr).Code,
		"公网地址必须按 public_ip_rpm 原阈值限流，不得放宽")

	allower.mu.Lock()
	defer allower.mu.Unlock()
	require.Contains(t, allower.counts, "panel:public:ip:203.0.113.9")
	for key := range allower.counts {
		require.NotContains(t, key, "ip-collapsed", "公网地址不得进入坍缩桶")
	}
}

// 不同私网地址各自独立计数（反代链路里可能同时出现 docker bridge 与 loopback）。
func TestPanelRateLimiterPublicIPPrivateAddressesIndependent(t *testing.T) {
	allower := &fakePanelAllower{}
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, `{"enabled":true,"user_rpm":0,"heavy_rpm":0,"exempt_admin":true,"public_ip_rpm":2}`),
	}
	router := newPanelTestRouter(p.PublicIP(), nil)

	// 每个私网地址都按放宽后的桶各自计数，互不消耗额度。
	for _, addr := range []string{"127.0.0.1:1000", "10.0.0.8:1000", "172.17.0.1:1000", "192.168.1.30:1000"} {
		require.Equal(t, http.StatusOK, performPanelRequest(router, addr).Code, addr)
	}
}

// ──────────────────────────────────────────────────────────
// RedeemGuard（/api/v1/redeem 专用限流）
// ──────────────────────────────────────────────────────────

func newRedeemGuardTestRouter(p *PanelRateLimiter, identity *panelTestIdentity) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if identity != nil {
		router.Use(func(c *gin.Context) {
			c.Set(string(ContextKeyUser), AuthSubject{UserID: identity.userID})
			c.Set(string(ContextKeyUserRole), identity.role)
			c.Next()
		})
	}
	router.Use(p.RedeemGuard())
	// 模拟 handler：按请求头决定本次「兑换」成功(200)还是失败(404)。
	router.POST("/redeem", func(c *gin.Context) {
		if c.GetHeader("X-Test-Outcome") == "ok" {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "reason": "REDEEM_CODE_NOT_FOUND"})
	})
	return router
}

func postRedeemGuard(router *gin.Engine, outcome string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/redeem", nil)
	if outcome != "" {
		req.Header.Set("X-Test-Outcome", outcome)
	}
	req.RemoteAddr = "203.0.113.90:1234"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// 请求速率层：每个登录用户 5/min，第 6 次 429，且必须带 Retry-After。
func TestRedeemGuardRateLimitsPerUser(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	allower := &fakePanelAllower{}
	p := &PanelRateLimiter{
		limiter:        allower,
		settingService: newPanelRateLimitTestService(t, ""),
		redis:          rdb,
	}

	userA := newRedeemGuardTestRouter(p, &panelTestIdentity{userID: 11, role: service.RoleUser})
	for i := 1; i <= 5; i++ {
		require.Equal(t, http.StatusNotFound, postRedeemGuard(userA, "").Code, "第 %d 次应进入 handler", i)
	}
	blocked := postRedeemGuard(userA, "")
	require.Equal(t, http.StatusTooManyRequests, blocked.Code)
	require.NotEmpty(t, blocked.Header().Get("Retry-After"), "429 必须带 Retry-After，客户端不能盲重试")
	require.Contains(t, blocked.Body.String(), "RATE_LIMITED")

	// 攻击者只能烧自己的额度：换一个 userID 立刻可用。
	userB := newRedeemGuardTestRouter(p, &panelTestIdentity{userID: 12, role: service.RoleUser})
	require.Equal(t, http.StatusNotFound, postRedeemGuard(userB, "").Code,
		"其他用户必须不受影响（键是登录用户的 userID，攻击者无法替别人计数）")

	allower.mu.Lock()
	defer allower.mu.Unlock()
	require.Contains(t, allower.counts, "panel:redeem:user:11")
	require.Contains(t, allower.counts, "panel:redeem:user:12")
}

// 请求速率层 fail-close：Redis 故障时拒绝，而不是放行（否则 oracle 重开）。
func TestRedeemGuardFailsClosedWhenRedisUnavailable(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })

	p := &PanelRateLimiter{
		limiter:        middleware.NewRateLimiter(rdb),
		settingService: newPanelRateLimitTestService(t, ""),
		redis:          rdb,
	}
	router := newRedeemGuardTestRouter(p, &panelTestIdentity{userID: 31, role: service.RoleUser})

	rec := postRedeemGuard(router, "")
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "Redis 不可用时必须 fail-close")
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}

// alwaysAllowPanelAllower 让请求速率层恒定放行，用于单独观察失败计数层。
type alwaysAllowPanelAllower struct{ calls int64 }

func (a *alwaysAllowPanelAllower) Allow(context.Context, string, int, time.Duration) (middleware.AllowResult, error) {
	a.calls++
	return middleware.AllowResult{Allowed: true, Count: 1}, nil
}

// 失败次数层：10 次失败后收紧到 429；成功兑换不消耗失败额度、也不清零既有计数。
func TestRedeemGuardTracksFailuresPerUser(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	// 请求速率层用「恒定放行」替身隔离，避免 5/min 的速率层掩盖失败计数层的行为。
	p := &PanelRateLimiter{
		limiter:        &alwaysAllowPanelAllower{},
		settingService: newPanelRateLimitTestService(t, ""),
		redis:          rdb,
	}
	router := newRedeemGuardTestRouter(p, &panelTestIdentity{userID: 41, role: service.RoleUser})

	for i := 1; i <= redeemFailedAttemptLimit; i++ {
		require.Equal(t, http.StatusNotFound, postRedeemGuard(router, "").Code, "第 %d 次失败仍应进入 handler", i)
	}
	count, err := rdb.Get(context.Background(), "panel:redeem:fail:41").Int()
	require.NoError(t, err)
	require.Equal(t, redeemFailedAttemptLimit, count, "只有失败才累加")

	// 达到上限后，即使这次「成功」也会被失败计数层拦住（固定窗口未过期）。
	blocked := postRedeemGuard(router, "ok")
	require.Equal(t, http.StatusTooManyRequests, blocked.Code, "失败次数达上限后必须收紧")
	require.NotEmpty(t, blocked.Header().Get("Retry-After"))

	// 窗口过期后自动恢复，不需要任何跨窗口状态。
	mr.FastForward(redeemFailedAttemptWindow + time.Second)
	require.Equal(t, http.StatusOK, postRedeemGuard(router, "ok").Code, "窗口过期后应恢复可用")
}

// 成功兑换不得消耗失败额度。
func TestRedeemGuardSuccessfulRedeemDoesNotCountAsFailure(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	p := &PanelRateLimiter{
		limiter:        &fakePanelAllower{},
		settingService: newPanelRateLimitTestService(t, ""),
		redis:          rdb,
	}
	router := newRedeemGuardTestRouter(p, &panelTestIdentity{userID: 51, role: service.RoleUser})

	for i := 0; i < 3; i++ {
		require.Equal(t, http.StatusOK, postRedeemGuard(router, "ok").Code)
	}
	exists, err := rdb.Exists(context.Background(), "panel:redeem:fail:51").Result()
	require.NoError(t, err)
	require.Zero(t, exists, "成功兑换不得写入失败计数")
}

// 无认证主体时不在限流层拦截（交给下游返回 401），避免把 401 变成 429。
func TestRedeemGuardPassesThroughWithoutSubject(t *testing.T) {
	p := &PanelRateLimiter{
		limiter:        &fakePanelAllower{},
		settingService: newPanelRateLimitTestService(t, ""),
	}
	router := newRedeemGuardTestRouter(p, nil)
	require.Equal(t, http.StatusNotFound, postRedeemGuard(router, "").Code)
}

func TestIsPubliclyRoutableClientIP(t *testing.T) {
	require.True(t, isPubliclyRoutableClientIP("203.0.113.9"))
	require.True(t, isPubliclyRoutableClientIP("2001:db8::1"))
	require.False(t, isPubliclyRoutableClientIP("127.0.0.1"))
	require.False(t, isPubliclyRoutableClientIP("::1"))
	require.False(t, isPubliclyRoutableClientIP("10.1.2.3"))
	require.False(t, isPubliclyRoutableClientIP("172.16.0.1"))
	require.False(t, isPubliclyRoutableClientIP("192.168.0.1"))
	require.False(t, isPubliclyRoutableClientIP("169.254.1.1"))
	require.False(t, isPubliclyRoutableClientIP("fe80::1"))
	require.False(t, isPubliclyRoutableClientIP("fc00::1"))
	require.False(t, isPubliclyRoutableClientIP("0.0.0.0"))
	require.False(t, isPubliclyRoutableClientIP(""))
	require.False(t, isPubliclyRoutableClientIP("not-an-ip"))
}
