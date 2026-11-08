package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// userUsageStatsUnavailableRepo 让用户端图表查询返回 ErrUsageStatsIncomplete。
type userUsageStatsUnavailableRepo struct {
	service.UsageLogRepository
}

func (r *userUsageStatsUnavailableRepo) GetUsageTrendWithFilters(
	context.Context, time.Time, time.Time, string, int64, int64, int64, int64, string, *int16, *bool, *int8,
) ([]usagestats.TrendDataPoint, error) {
	return nil, service.ErrUsageStatsIncomplete
}

func (r *userUsageStatsUnavailableRepo) GetModelStatsWithFilters(
	context.Context, time.Time, time.Time, int64, int64, int64, int64, *int16, *bool, *int8,
) ([]usagestats.ModelStat, error) {
	return nil, service.ErrUsageStatsIncomplete
}

// N-02（用户端）：统计未就绪时必须与管理端同口径。
//
// 现状是 500（trend/models 均为 response.ErrorFrom → 500 internal error），
// 线上则表现为"静默空数组"，用户会误以为当天没有用量。两条都不可接受：
// 契约统一为 HTTP 503 + reason=usage_stats_incomplete。
func TestUserUsageStatsUnavailableMatchesAdminContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userUsageStatsUnavailableRepo{}
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 3})
		c.Next()
	})
	router.GET("/usage/dashboard/trend", handler.DashboardTrend)
	router.GET("/usage/dashboard/models", handler.DashboardModels)
	router.GET("/usage/dashboard/snapshot-v2", handler.DashboardSnapshotV2)

	for _, path := range []string{
		"/usage/dashboard/trend",
		"/usage/dashboard/models",
		"/usage/dashboard/trend?period=today",
		"/usage/dashboard/snapshot-v2",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			var body struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Reason  string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())

			require.NotEqual(t, http.StatusInternalServerError, rec.Code,
				"统计未就绪不得映射成 500（当前响应：%s）", rec.Body.String())
			require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
			require.Equal(t, "usage_stats_incomplete", body.Reason,
				"用户端必须与管理端同一标记字段，否则前端无法统一降级")
		})
	}
}
