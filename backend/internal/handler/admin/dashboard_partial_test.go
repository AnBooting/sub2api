package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type partialDashboardRepo struct {
	dashboardUsageRepoCapture
	availabilityErr error
	statsErr        error
}

func (r *partialDashboardRepo) CheckUsageStatsAvailable(context.Context, time.Time, time.Time) error {
	return r.availabilityErr
}

func (r *partialDashboardRepo) GetDashboardStats(context.Context) (*usagestats.DashboardStats, error) {
	return &usagestats.DashboardStats{
		TotalUsers: 8, TotalActualCost: 999, TodayActualCost: 5,
		UsageAvailability: &usagestats.DashboardUsageAvailability{Today: true, Hourly: true, Performance: true},
	}, r.statsErr
}

func TestDashboardPartialHTTPPreservesCardsAndUptime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/stats", "/snapshot?include_users_trend=true&include_group_stats=true"} {
		t.Run(path, func(t *testing.T) {
			resetDashboardReadCachesForTest()
			t.Cleanup(resetDashboardReadCachesForTest)
			repo := &partialDashboardRepo{availabilityErr: service.ErrUsageStatsIncomplete}
			handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
			handler.startTime = time.Now().Add(-time.Minute)
			router := gin.New()
			router.GET("/stats", handler.GetStats)
			router.GET("/snapshot", handler.GetSnapshotV2)
			for i := 0; i < 2; i++ { // Check both fresh and cached snapshot JSON.
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
				require.Equal(t, 200, rec.Code, rec.Body.String())
				var body struct {
					Data map[string]any `json:"data"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				stats := body.Data
				if path != "/stats" {
					require.ElementsMatch(t, []any{"trend", "models", "groups", "users_trend"}, body.Data["unavailable_sections"])
					for _, section := range []string{"trend", "models", "groups", "users_trend"} {
						require.NotContains(t, body.Data, section)
					}
					stats = body.Data["stats"].(map[string]any)
				}
				require.Nil(t, stats["total_actual_cost"])
				require.Equal(t, float64(5), stats["today_actual_cost"])
				require.Equal(t, float64(8), stats["total_users"])
				require.GreaterOrEqual(t, stats["uptime"], float64(59))
				require.Equal(t, false, stats["usage_availability"].(map[string]any)["total"])
			}
		})
	}
}

func TestDashboardPartialDoesNotHideDatabaseErrors(t *testing.T) {
	repo := &partialDashboardRepo{availabilityErr: errors.New("database unavailable")}
	handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	_, err := handler.buildSnapshotV2Response(context.Background(), time.Now().Add(-time.Hour), time.Now(), "hour", &dashboardSnapshotV2Filters{}, true, true, false, false, false, 12)
	require.ErrorContains(t, err, "database unavailable")
}

type snapshotChartRepo struct {
	partialDashboardRepo
	cost float64
}

func (r *snapshotChartRepo) GetUsageTrendWithUsageFilters(context.Context, time.Time, time.Time, string, usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	return []usagestats.TrendDataPoint{{ActualCost: r.cost}}, nil
}

func TestDashboardPartialSnapshotDoesNotMixSectionCaches(t *testing.T) {
	resetDashboardReadCachesForTest()
	t.Cleanup(resetDashboardReadCachesForTest)
	repo := &snapshotChartRepo{cost: 999}
	handler := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	start := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	ctx := context.Background()
	_, _, err := handler.getUsageTrendCached(ctx, start, end, "day", 0, 0, 0, 0, "", nil, nil, nil, nil, nil)
	require.NoError(t, err)
	repo.cost = 5
	resp, err := handler.buildSnapshotV2Response(ctx, start, end, "day", &dashboardSnapshotV2Filters{}, false, true, false, false, false, 12)
	require.NoError(t, err)
	require.Len(t, resp.Trend, 1)
	require.Equal(t, float64(5), resp.Trend[0].ActualCost, "the entire response cache is allowed; unrelated section caches must not override snapshot reads")
}
