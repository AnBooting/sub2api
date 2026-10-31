package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// usageStatsIncompleteRepo 让**所有**图表/汇总查询返回 ErrUsageStatsIncomplete。
//
// 线上只有"不走预聚合"的请求才会命中该 sentinel（shouldUsePreaggregatedTrend 要求
// 全部过滤为空），因此这条缺陷无法靠线上参数组合稳定复现，必须用 stub 锁死。
type usageStatsIncompleteRepo struct {
	service.UsageLogRepository
}

var errUsageStatsIncompleteStub = service.ErrUsageStatsIncomplete

func (r *usageStatsIncompleteRepo) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetUsageTrendWithUsageFilters(
	context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters,
) ([]usagestats.TrendDataPoint, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetUsageTrendWithFilters(
	context.Context, time.Time, time.Time, string, int64, int64, int64, int64, string, *int16, *bool, *int8,
) ([]usagestats.TrendDataPoint, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetModelStatsWithUsageFiltersBySource(
	context.Context, time.Time, time.Time, usagestats.UsageLogFilters, string,
) ([]usagestats.ModelStat, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetModelStatsWithFilters(
	context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8,
) ([]usagestats.ModelStat, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetGroupStatsWithFilters(
	context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8,
) ([]usagestats.GroupStat, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetAPIKeyUsageTrend(
	context.Context, time.Time, time.Time, string, int,
) ([]usagestats.APIKeyUsageTrendPoint, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetUserUsageTrend(
	context.Context, time.Time, time.Time, string, int, string,
) ([]usagestats.UserUsageTrendPoint, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetUserSpendingRanking(
	context.Context, time.Time, time.Time, int,
) (*usagestats.UserSpendingRankingResponse, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetBatchUserUsageStats(
	context.Context, []int64, time.Time, time.Time,
) (map[int64]*usagestats.BatchUserUsageStats, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetBatchAPIKeyUsageStats(
	context.Context, []int64, time.Time, time.Time,
) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetUserBreakdownStats(
	context.Context, time.Time, time.Time, usagestats.UserBreakdownDimension, int,
) ([]usagestats.UserBreakdownItem, error) {
	return nil, errUsageStatsIncompleteStub
}

func (r *usageStatsIncompleteRepo) GetAllGroupUsageSummary(
	context.Context, time.Time,
) ([]usagestats.GroupUsageSummary, error) {
	return nil, errUsageStatsIncompleteStub
}

// 契约常量只有一个事实源：service 包。handler 与 handler/admin 都引用它，
// 任何一端自写字面量都会让本条断言变红，从而杜绝"两端文案漂移"。
func TestUsageStatsIncompleteContractIsSingleSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	require.Equal(t, "usage_stats_incomplete", service.UsageStatsIncompleteReason,
		"reason 是前端硬编码的降级标记，改动等于破坏契约")

	dashboardSvc := service.NewDashboardService(&usageStatsIncompleteRepo{}, nil, nil, nil)
	router := gin.New()
	router.GET("/trend", NewDashboardHandler(dashboardSvc, nil).GetUsageTrend)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/trend", nil))

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	// 断言的是"客户端实际收到的"，不是常量本身。
	require.Equal(t, service.UsageStatsIncompleteReason, body.Reason)
	require.Equal(t, service.UsageStatsIncompleteMessage, body.Message)
	require.Equal(t, http.StatusServiceUnavailable, body.Code)
}

// 所有管理端图表/汇总入口必须同一口径：不许 500 "Failed to ..."，
// 也不许把该 sentinel 静默吞成空数据。
//
// **范围说明（刻意排除 4 个入口）**：GetUserSpendingRanking、GetBatchUsersUsage、
// GetBatchAPIKeysUsage、GetUserBreakdown 不纳入本契约测试，原因是它们的仓储函数
// （GetUserSpendingRanking / GetBatchUserUsageStats / GetBatchAPIKeyUsageStats /
// GetUserBreakdownStats）结构上不经过 usage_stats_completeness 守卫，err 永远不可能
// 是 service.ErrUsageStatsIncomplete；对应的 handler 映射也已刻意不接
// respondUsageStatsIncomplete（否则是误导性的死代码）。
// 若未来在 repo 层为它们补上 completeness 守卫，请把 handler 映射与这 4 条测试
// 用例一并恢复。
func TestAllAdminUsageStatsEndpointsShareContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(resetDashboardReadCachesForTest)

	dashboardSvc := service.NewDashboardService(&usageStatsIncompleteRepo{}, nil, nil, nil)
	dh := NewDashboardHandler(dashboardSvc, nil)
	dh.startTime = time.Now().Add(-time.Minute)
	gh := &GroupHandler{dashboardService: dashboardSvc}

	router := gin.New()
	router.GET("/dashboard/stats", dh.GetStats)
	router.GET("/dashboard/trend", dh.GetUsageTrend)
	router.GET("/dashboard/models", dh.GetModelStats)
	router.GET("/dashboard/groups", dh.GetGroupStats)
	router.GET("/dashboard/api-keys-trend", dh.GetAPIKeyUsageTrend)
	router.GET("/dashboard/users-trend", dh.GetUserUsageTrend)
	router.GET("/groups/usage-summary", gh.GetUsageSummary)

	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/dashboard/stats", ""},
		{http.MethodGet, "/dashboard/trend", ""},
		{http.MethodGet, "/dashboard/trend?granularity=hour", ""},
		{http.MethodGet, "/dashboard/trend?start_date=2026-10-01&end_date=2026-10-07", ""},
		{http.MethodGet, "/dashboard/models", ""},
		{http.MethodGet, "/dashboard/groups", ""},
		{http.MethodGet, "/dashboard/api-keys-trend", ""},
		{http.MethodGet, "/dashboard/users-trend", ""},
		{http.MethodGet, "/groups/usage-summary", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.NotEqual(t, http.StatusInternalServerError, rec.Code,
				"统计未就绪不得映射成 500：%s", rec.Body.String())
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())

			var body struct {
				Reason string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, service.UsageStatsIncompleteReason, body.Reason, rec.Body.String())
		})
	}
}

// snapshot-v2 的既有 degraded 契约（200 + unavailable_sections）必须保持：
// 它已经正确表达"区块不可用"，不应被改成 503。
func TestSnapshotV2KeepsDegradedContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	repo := &snapshotDegradedRepo{usageStatsIncompleteRepo: &usageStatsIncompleteRepo{}}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	dh := NewDashboardHandler(dashboardSvc, nil)

	router := gin.New()
	router.GET("/dashboard/snapshot", dh.GetSnapshotV2)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/snapshot?include_trend=true", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "unavailable_sections", rec.Body.String())
}

// snapshotDegradedRepo 只让 CheckUsageStatsAvailable 判定不可用，
// 走 snapshot-v2 自己的 degraded 分支。
type snapshotDegradedRepo struct {
	*usageStatsIncompleteRepo
}

func (r *snapshotDegradedRepo) CheckUsageStatsAvailable(context.Context, time.Time, time.Time) error {
	return service.ErrUsageStatsIncomplete
}

// GetDashboardStats 正常返回：degraded 只应影响图表区块，不应连带丢掉独立可用的卡片。
func (r *snapshotDegradedRepo) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return &usagestats.DashboardStats{
		TotalUsers: 8,
		UsageAvailability: &usagestats.DashboardUsageAvailability{
			Today: true, Hourly: true, Performance: true,
		},
	}, nil
}

// 真实基础设施故障必须仍然是 500：degraded 映射只吃 ErrUsageStatsIncomplete。
func TestAdminUsageStatsRealFailureStays500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	dashboardSvc := service.NewDashboardService(&usageStatsBrokenRepo{}, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/dashboard/trend", handler.GetUsageTrend)
	router.GET("/groups/usage-summary", (&GroupHandler{dashboardService: dashboardSvc}).GetUsageSummary)

	for _, path := range []string{"/dashboard/trend", "/groups/usage-summary"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusInternalServerError, rec.Code,
			path+" 真实故障必须保持 500："+rec.Body.String())
	}
}

type usageStatsBrokenRepo struct {
	service.UsageLogRepository
}

func (r *usageStatsBrokenRepo) GetUsageTrendWithUsageFilters(
	context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters,
) ([]usagestats.TrendDataPoint, error) {
	return nil, errRealFailure
}

func (r *usageStatsBrokenRepo) GetAllGroupUsageSummary(
	context.Context, time.Time,
) ([]usagestats.GroupUsageSummary, error) {
	return nil, errRealFailure
}

// errRealFailure 模拟基础设施故障（数据库不可用），必须保持 500。
var errRealFailure = errors.New("database unavailable")
