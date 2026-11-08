package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type channelMonitorV2SnapshotRepoStub struct {
	service.ChannelMonitorV2Repository
	admin bool
}

func (s *channelMonitorV2SnapshotRepoStub) GetConfig(context.Context) (*service.ChannelMonitorV2Config, error) {
	return &service.ChannelMonitorV2Config{Enabled: true}, nil
}

func (s *channelMonitorV2SnapshotRepoStub) GetSnapshot(_ context.Context, _ service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2Snapshot, error) {
	s.admin = admin
	return &service.ChannelMonitorV2Snapshot{
		Config: service.ChannelMonitorV2Config{
			Version: 3, Enabled: true, RefreshIntervalSeconds: 300,
			Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"gpt-5"}}},
			GroupIDs:  []int64{7}, IgnoredErrorCategories: []string{"timeout"},
		},
		Metrics: service.ChannelMonitorV2Metric{RequestCount: 100, ErrorRate: 0.1, RPM: 5},
		Trend:   []service.ChannelMonitorV2TrendPoint{{Metrics: service.ChannelMonitorV2Metric{RequestCount: 40, ErrorRate: 0.2, RPM: 2}}},
	}, nil
}

func TestChannelMonitorV2SnapshotConfigResponseContract(t *testing.T) {
	for _, admin := range []bool{false, true} {
		name := "user"
		if admin {
			name = "admin"
		}
		t.Run(name, func(t *testing.T) {
			repo := &channelMonitorV2SnapshotRepoStub{}
			h := &ChannelMonitorV2Handler{
				service:       service.NewChannelMonitorV2Service(repo),
				apiKeyService: &channelMonitorV2GroupAuthorizerStub{groups: []service.Group{{ID: 7}}},
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/snapshot", nil)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
			if admin {
				h.AdminSnapshot(c)
			} else {
				h.Snapshot(c)
			}
			require.Equal(t, http.StatusOK, w.Code)
			require.Equal(t, admin, repo.admin)
			var body struct {
				Data struct {
					Config  map[string]json.RawMessage           `json:"config"`
					Metrics service.ChannelMonitorV2Metric       `json:"metrics"`
					Trend   []service.ChannelMonitorV2TrendPoint `json:"trend"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			if admin {
				for _, key := range []string{"version", "enabled", "platforms", "group_ids", "health_thresholds", "ignored_error_categories"} {
					require.Contains(t, body.Data.Config, key)
				}
				require.JSONEq(t, `[7]`, string(body.Data.Config["group_ids"]))
				require.JSONEq(t, `["timeout"]`, string(body.Data.Config["ignored_error_categories"]))
				require.Equal(t, int64(100), body.Data.Metrics.RequestCount)
			} else {
				require.Len(t, body.Data.Config, 1)
				require.JSONEq(t, `300`, string(body.Data.Config["refresh_interval_seconds"]))
				require.Zero(t, body.Data.Metrics.RequestCount)
				require.Zero(t, body.Data.Metrics.RPM)
				require.InDelta(t, 0.1, body.Data.Metrics.ErrorRate, 0.0001)
				require.Zero(t, body.Data.Trend[0].Metrics.RequestCount)
			}
		})
	}
}

type channelMonitorV2GroupAuthorizerStub struct {
	groups []service.Group
	err    error
	calls  []int64
}

func (s *channelMonitorV2GroupAuthorizerStub) GetAvailableGroups(_ context.Context, userID int64) ([]service.Group, error) {
	s.calls = append(s.calls, userID)
	return s.groups, s.err
}

func TestChannelMonitorV2QueryListSupportsRepeatedAndCommaValues(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/?platform=openai,grok&platform=anthropic", nil)
	require.Equal(t, []string{"openai", "grok", "anthropic"}, queryList(c, "platform"))
}

func TestChannelMonitorV2GroupByQueryDefaultsAndRejectsInvalid(t *testing.T) {
	groupBy, err := service.ParseChannelMonitorV2GroupBy("")
	require.NoError(t, err)
	require.Equal(t, service.ChannelMonitorV2GroupByPlatformGroup, groupBy)
	_, err = service.ParseChannelMonitorV2GroupBy("invalid")
	require.Error(t, err)
}

func TestChannelMonitorV2MatrixHandlerRejectsInvalidGroupBy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/matrix?group_by=invalid", nil)
	h := NewChannelMonitorV2Handler(service.NewChannelMonitorV2Service(nil), nil)
	h.Matrix(c)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestChannelMonitorV2ScopeFilterUsesAvailableGroupsForOrdinaryUser(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/snapshot", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	authorizer := &channelMonitorV2GroupAuthorizerStub{groups: []service.Group{{ID: 3}, {ID: 7}}}
	h := &ChannelMonitorV2Handler{apiKeyService: authorizer}
	filter := service.ChannelMonitorV2Filter{GroupIDs: []int64{7, 9}}

	require.True(t, h.scopeFilter(c, &filter, false))
	require.True(t, filter.RestrictGroups)
	require.Equal(t, []int64{3, 7}, filter.AllowedGroupIDs)
	require.Equal(t, []int64{42}, authorizer.calls)
}

func TestChannelMonitorV2ScopeFilterPreservesEmptyOrdinaryUserScope(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/channel-monitor-v2/snapshot", nil)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	h := &ChannelMonitorV2Handler{apiKeyService: &channelMonitorV2GroupAuthorizerStub{}}
	filter := service.ChannelMonitorV2Filter{}

	require.True(t, h.scopeFilter(c, &filter, false))
	require.True(t, filter.RestrictGroups)
	require.Empty(t, filter.AllowedGroupIDs)
}

func TestChannelMonitorV2ScopeFilterLeavesAdminUnrestricted(t *testing.T) {
	authorizer := &channelMonitorV2GroupAuthorizerStub{}
	h := &ChannelMonitorV2Handler{apiKeyService: authorizer}
	filter := service.ChannelMonitorV2Filter{GroupIDs: []int64{9}}

	require.True(t, h.scopeFilter(nil, &filter, true))
	require.False(t, filter.RestrictGroups)
	require.Nil(t, filter.AllowedGroupIDs)
	require.Empty(t, authorizer.calls)
}
