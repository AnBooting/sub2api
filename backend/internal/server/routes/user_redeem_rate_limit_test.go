package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// redeemRouteNotFoundRepo 只实现「兑换码不存在」这一条路径所需的方法。
// 其余方法一旦被调用即说明测试走错了分支，直接 panic 暴露问题。
type redeemRouteNotFoundRepo struct{}

func (redeemRouteNotFoundRepo) Create(context.Context, *service.RedeemCode) error {
	panic("unexpected Create")
}

func (redeemRouteNotFoundRepo) CreateBatch(context.Context, []service.RedeemCode) error {
	panic("unexpected CreateBatch")
}

func (redeemRouteNotFoundRepo) GetByID(context.Context, int64) (*service.RedeemCode, error) {
	panic("unexpected GetByID")
}

func (redeemRouteNotFoundRepo) GetByCode(context.Context, string) (*service.RedeemCode, error) {
	return nil, service.ErrRedeemCodeNotFound
}

func (redeemRouteNotFoundRepo) Update(context.Context, *service.RedeemCode) error {
	panic("unexpected Update")
}

func (redeemRouteNotFoundRepo) BatchUpdate(context.Context, []int64, service.RedeemCodeBatchUpdateFields) (int64, error) {
	panic("unexpected BatchUpdate")
}

func (redeemRouteNotFoundRepo) Delete(context.Context, int64) error { panic("unexpected Delete") }

func (redeemRouteNotFoundRepo) Use(context.Context, int64, int64) error { panic("unexpected Use") }

func (redeemRouteNotFoundRepo) List(context.Context, pagination.PaginationParams) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected List")
}

func (redeemRouteNotFoundRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters")
}

func (redeemRouteNotFoundRepo) ListByUser(context.Context, int64, int) ([]service.RedeemCode, error) {
	panic("unexpected ListByUser")
}

func (redeemRouteNotFoundRepo) ListByUserPaginated(context.Context, int64, pagination.PaginationParams, string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserPaginated")
}

func (redeemRouteNotFoundRepo) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	panic("unexpected SumPositiveBalanceByUser")
}

// redeemRouteSettingRepoStub 只提供面板限流设置，用于让 Global()/Heavy() 失效以免干扰本用例。
type redeemRouteSettingRepoStub struct {
	values map[string]string
}

func (s *redeemRouteSettingRepoStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (s *redeemRouteSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (s *redeemRouteSettingRepoStub) Set(context.Context, string, string) error { return nil }

func (s *redeemRouteSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *redeemRouteSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *redeemRouteSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}

func (s *redeemRouteSettingRepoStub) Delete(context.Context, string) error { return nil }

// newRedeemRouteTestRouter 用真实的路由注册函数装配 /api/v1/redeem，
// 只有 handler 背后的 RedeemService 用「永远不存在」的仓储替身。
// JWT 中间件通过请求头模拟不同登录用户，便于断言用户维度互不影响。
func newRedeemRouteTestRouter(t *testing.T, rdb *redis.Client) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	redeemService := service.NewRedeemService(redeemRouteNotFoundRepo{}, nil, nil, nil, nil, nil, nil, nil)
	settingService := service.NewSettingService(&redeemRouteSettingRepoStub{
		// 面板全局限流关闭，确保本用例只测 /redeem 自己的限流层。
		values: map[string]string{
			service.SettingKeyPanelRateLimitSettings: `{"enabled":false,"user_rpm":0,"heavy_rpm":0,"exempt_admin":true,"public_ip_rpm":0}`,
		},
	}, &config.Config{})

	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterUserRoutes(
		v1,
		&handler.Handlers{Redeem: handler.NewRedeemHandler(redeemService)},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			raw := c.GetHeader("X-Test-User")
			userID, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: userID})
			c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleUser)
			c.Next()
		}),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }),
		settingService,
		servermiddleware.NewPanelRateLimiter(rdb, settingService),
	)
	return router
}

func postRedeem(router *gin.Engine, userID int64, code string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/redeem", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", strconv.FormatInt(userID, 10))
	req.RemoteAddr = "203.0.113.77:1234"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestRedeemRouteRateLimitPerUser(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	router := newRedeemRouteTestRouter(t, rdb)

	// 前 5 次进入 handler，返回兑换码不存在的既有 404（错误体不改变）。
	for i := 1; i <= 5; i++ {
		w := postRedeem(router, 11, "MISSING")
		require.Equal(t, http.StatusNotFound, w.Code, "第 %d 次请求应进入 handler: %s", i, w.Body.String())
		require.Contains(t, w.Body.String(), "REDEEM_CODE_NOT_FOUND")
	}

	// 第 6 次在路由层被限流，且不再触达 handler。
	w := postRedeem(router, 11, "MISSING")
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	require.NotEmpty(t, w.Header().Get("Retry-After"))

	// 另一个用户有独立额度：攻击者烧自己的额度不会影响别人。
	w = postRedeem(router, 12, "MISSING")
	require.Equal(t, http.StatusNotFound, w.Code, "其他用户必须不受影响: %s", w.Body.String())
}

func TestRedeemRouteRateLimitedBeforeHandlerRuns(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	router := newRedeemRouteTestRouter(t, rdb)

	for i := 0; i < 5; i++ {
		require.Equal(t, http.StatusNotFound, postRedeem(router, 21, "MISSING").Code)
	}
	require.Equal(t, http.StatusTooManyRequests, postRedeem(router, 21, "MISSING").Code)

	// 限流发生在 handler 之前：Redis 里不应出现第 6 次兑换尝试造成的失败计数增长。
	// （失败计数上限是 10，若第 6 次也进入 handler 计数会变成 6。）
	count, err := rdb.Get(context.Background(), "panel:redeem:fail:21").Int()
	require.NoError(t, err)
	require.Equal(t, 5, count, "被限流的请求不得计入失败次数")
}
