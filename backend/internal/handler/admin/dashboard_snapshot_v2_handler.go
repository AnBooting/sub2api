package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

var dashboardSnapshotV2Cache = newSnapshotCache(30 * time.Second)

type dashboardSnapshotV2Stats struct {
	usagestats.DashboardStats
	Uptime int64 `json:"uptime"`
}

// Explicitly preserve uptime: promoting DashboardStats.MarshalJSON from the
// embedded field would otherwise silently omit fields on this response wrapper.
func (s dashboardSnapshotV2Stats) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(s.DashboardStats)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["uptime"], err = json.Marshal(s.Uptime)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

type dashboardSnapshotV2Response struct {
	GeneratedAt string `json:"generated_at"`

	StartDate           string   `json:"start_date"`
	EndDate             string   `json:"end_date"`
	Granularity         string   `json:"granularity"`
	UnavailableSections []string `json:"unavailable_sections,omitempty"`

	Stats      *dashboardSnapshotV2Stats        `json:"stats,omitempty"`
	Trend      []usagestats.TrendDataPoint      `json:"trend,omitempty"`
	Models     []usagestats.ModelStat           `json:"models,omitempty"`
	Groups     []usagestats.GroupStat           `json:"groups,omitempty"`
	UsersTrend []usagestats.UserUsageTrendPoint `json:"users_trend,omitempty"`
}

type dashboardSnapshotV2Filters struct {
	UserID                int64
	APIKeyID              int64
	AccountID             int64
	GroupID               int64
	Model                 string
	RequestType           *int16
	Stream                *bool
	NativeCompactionV2    *bool
	BillingType           *int8
	UpstreamModelMismatch *bool
}

type dashboardSnapshotV2CacheKey struct {
	StartTime             string `json:"start_time"`
	EndTime               string `json:"end_time"`
	Granularity           string `json:"granularity"`
	UserID                int64  `json:"user_id"`
	APIKeyID              int64  `json:"api_key_id"`
	AccountID             int64  `json:"account_id"`
	GroupID               int64  `json:"group_id"`
	Model                 string `json:"model"`
	RequestType           *int16 `json:"request_type"`
	Stream                *bool  `json:"stream"`
	NativeCompactionV2    *bool  `json:"native_compaction_v2"`
	BillingType           *int8  `json:"billing_type"`
	UpstreamModelMismatch *bool  `json:"upstream_model_mismatch"`
	IncludeStats          bool   `json:"include_stats"`
	IncludeTrend          bool   `json:"include_trend"`
	IncludeModels         bool   `json:"include_models"`
	IncludeGroups         bool   `json:"include_groups"`
	IncludeUsersTrend     bool   `json:"include_users_trend"`
	UsersTrendLimit       int    `json:"users_trend_limit"`
}

func (h *DashboardHandler) GetSnapshotV2(c *gin.Context) {
	startTime, endTime := parseTimeRange(c)
	granularity := strings.TrimSpace(c.DefaultQuery("granularity", "day"))
	if granularity != "hour" {
		granularity = "day"
	}

	includeStats := parseBoolQueryWithDefault(c.Query("include_stats"), true)
	includeTrend := parseBoolQueryWithDefault(c.Query("include_trend"), true)
	includeModels := parseBoolQueryWithDefault(c.Query("include_model_stats"), true)
	includeGroups := parseBoolQueryWithDefault(c.Query("include_group_stats"), false)
	includeUsersTrend := parseBoolQueryWithDefault(c.Query("include_users_trend"), false)
	usersTrendLimit := 12
	if raw := strings.TrimSpace(c.Query("users_trend_limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 50 {
			usersTrendLimit = parsed
		}
	}

	filters, err := parseDashboardSnapshotV2Filters(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	keyRaw, _ := json.Marshal(dashboardSnapshotV2CacheKey{
		StartTime:             startTime.UTC().Format(time.RFC3339),
		EndTime:               endTime.UTC().Format(time.RFC3339),
		Granularity:           granularity,
		UserID:                filters.UserID,
		APIKeyID:              filters.APIKeyID,
		AccountID:             filters.AccountID,
		GroupID:               filters.GroupID,
		Model:                 filters.Model,
		RequestType:           filters.RequestType,
		Stream:                filters.Stream,
		NativeCompactionV2:    filters.NativeCompactionV2,
		BillingType:           filters.BillingType,
		UpstreamModelMismatch: filters.UpstreamModelMismatch,
		IncludeStats:          includeStats,
		IncludeTrend:          includeTrend,
		IncludeModels:         includeModels,
		IncludeGroups:         includeGroups,
		IncludeUsersTrend:     includeUsersTrend,
		UsersTrendLimit:       usersTrendLimit,
	})
	cacheKey := string(keyRaw)

	cached, hit, err := dashboardSnapshotV2Cache.GetOrLoad(cacheKey, func() (any, error) {
		return h.buildSnapshotV2Response(
			c.Request.Context(),
			startTime,
			endTime,
			granularity,
			filters,
			includeStats,
			includeTrend,
			includeModels,
			includeGroups,
			includeUsersTrend,
			usersTrendLimit,
		)
	})
	if err != nil {
		// N-02：snapshot-v2 内部已把 ErrUsageStatsIncomplete 转成 degraded 区块，
		// 这里兜住其余仍以该 sentinel 冒出的路径（例如仅请求 stats 区块时），保持同一口径。
		if respondUsageStatsIncomplete(c, err) {
			return
		}
		response.Error(c, 500, err.Error())
		return
	}
	if cached.ETag != "" {
		c.Header("ETag", cached.ETag)
		c.Header("Vary", "If-None-Match")
		if ifNoneMatchMatched(c.GetHeader("If-None-Match"), cached.ETag) {
			c.Status(http.StatusNotModified)
			return
		}
	}
	c.Header("X-Snapshot-Cache", cacheStatusValue(hit))
	response.Success(c, cached.Payload)
}

func (h *DashboardHandler) buildSnapshotV2Response(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	filters *dashboardSnapshotV2Filters,
	includeStats, includeTrend, includeModels, includeGroups, includeUsersTrend bool,
	usersTrendLimit int,
) (*dashboardSnapshotV2Response, error) {
	var result *dashboardSnapshotV2Response
	err := h.dashboardService.WithUsageStatsSnapshot(ctx, func(snapshot *service.DashboardService) error {
		scoped := *h
		scoped.dashboardService = snapshot
		var err error
		result, err = scoped.buildSnapshotV2InUsageSnapshot(ctx, startTime, endTime, granularity, filters, includeStats, includeTrend, includeModels, includeGroups, includeUsersTrend, usersTrendLimit)
		return err
	})
	return result, err
}

func (h *DashboardHandler) buildSnapshotV2InUsageSnapshot(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	filters *dashboardSnapshotV2Filters,
	includeStats, includeTrend, includeModels, includeGroups, includeUsersTrend bool,
	usersTrendLimit int,
) (*dashboardSnapshotV2Response, error) {
	resp := &dashboardSnapshotV2Response{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		StartDate:   startTime.Format("2006-01-02"),
		EndDate:     endTime.Add(-24 * time.Hour).Format("2006-01-02"),
		Granularity: granularity,
	}

	if includeStats {
		stats, err := h.dashboardService.GetDashboardStats(ctx)
		if err != nil {
			return nil, errors.New("failed to get dashboard statistics")
		}
		resp.Stats = &dashboardSnapshotV2Stats{
			DashboardStats: *stats,
			Uptime:         int64(time.Since(h.startTime).Seconds()),
		}
	}

	// Do not let an unavailable chart discard independently available cards.
	// Raw chart sections share this range, including filtered model/user series.
	if includeTrend || includeModels || includeGroups || includeUsersTrend {
		if err := h.dashboardService.CheckUsageStatsAvailable(ctx, startTime, endTime); err != nil {
			if !errors.Is(err, service.ErrUsageStatsIncomplete) {
				return nil, err
			}
			for _, section := range []struct {
				name    string
				include bool
			}{
				{"trend", includeTrend}, {"models", includeModels}, {"groups", includeGroups}, {"users_trend", includeUsersTrend},
			} {
				if section.include {
					resp.UnavailableSections = append(resp.UnavailableSections, section.name)
				}
			}
			return resp, nil
		}
	}

	usageFilters := usagestats.UsageLogFilters{
		UserID: filters.UserID, APIKeyID: filters.APIKeyID, AccountID: filters.AccountID, GroupID: filters.GroupID,
		Model: filters.Model, RequestType: filters.RequestType, Stream: filters.Stream,
		NativeCompactionV2: filters.NativeCompactionV2, BillingType: filters.BillingType,
		UpstreamModelMismatch: filters.UpstreamModelMismatch,
	}
	// Cache the complete response in GetSnapshotV2. Section caches may describe
	// another committed state, so bypass them while constructing this snapshot.
	if includeTrend {
		trend, err := h.dashboardService.GetUsageTrendWithUsageFilters(ctx, startTime, endTime, granularity, usageFilters)
		if err != nil {
			return nil, err
		}
		resp.Trend = trend
	}
	if includeModels {
		models, err := h.dashboardService.GetModelStatsWithUsageFiltersBySource(ctx, startTime, endTime, usageFilters, usagestats.ModelSourceRequested)
		if err != nil {
			return nil, err
		}
		resp.Models = models
	}
	if includeGroups {
		groups, err := h.dashboardService.GetGroupStatsWithUsageFilters(ctx, startTime, endTime, usageFilters)
		if err != nil {
			return nil, err
		}
		resp.Groups = groups
	}
	if includeUsersTrend {
		usersTrend, err := h.dashboardService.GetUserUsageTrend(ctx, startTime, endTime, granularity, usersTrendLimit, "tokens")
		if err != nil {
			return nil, err
		}
		resp.UsersTrend = usersTrend
	}

	return resp, nil
}

func parseDashboardSnapshotV2Filters(c *gin.Context) (*dashboardSnapshotV2Filters, error) {
	filters := &dashboardSnapshotV2Filters{
		Model: strings.TrimSpace(c.Query("model")),
	}

	if userIDStr := strings.TrimSpace(c.Query("user_id")); userIDStr != "" {
		id, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			return nil, err
		}
		filters.UserID = id
	}
	if apiKeyIDStr := strings.TrimSpace(c.Query("api_key_id")); apiKeyIDStr != "" {
		id, err := strconv.ParseInt(apiKeyIDStr, 10, 64)
		if err != nil {
			return nil, err
		}
		filters.APIKeyID = id
	}
	if accountIDStr := strings.TrimSpace(c.Query("account_id")); accountIDStr != "" {
		id, err := strconv.ParseInt(accountIDStr, 10, 64)
		if err != nil {
			return nil, err
		}
		filters.AccountID = id
	}
	if groupIDStr := strings.TrimSpace(c.Query("group_id")); groupIDStr != "" {
		id, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			return nil, err
		}
		filters.GroupID = id
	}

	if requestTypeStr := strings.TrimSpace(c.Query("request_type")); requestTypeStr != "" {
		parsed, err := service.ParseUsageRequestType(requestTypeStr)
		if err != nil {
			return nil, err
		}
		value := int16(parsed)
		filters.RequestType = &value
	} else if streamStr := strings.TrimSpace(c.Query("stream")); streamStr != "" {
		streamVal, err := strconv.ParseBool(streamStr)
		if err != nil {
			return nil, err
		}
		filters.Stream = &streamVal
	}

	if nativeCompactionV2Str := strings.TrimSpace(c.Query("native_compaction_v2")); nativeCompactionV2Str != "" {
		value, err := strconv.ParseBool(nativeCompactionV2Str)
		if err != nil {
			return nil, err
		}
		filters.NativeCompactionV2 = &value
	}

	if billingTypeStr := strings.TrimSpace(c.Query("billing_type")); billingTypeStr != "" {
		v, err := strconv.ParseInt(billingTypeStr, 10, 8)
		if err != nil {
			return nil, err
		}
		bt := int8(v)
		filters.BillingType = &bt
	}

	if mismatchStr := strings.TrimSpace(c.Query("upstream_model_mismatch")); mismatchStr != "" {
		value, err := strconv.ParseBool(mismatchStr)
		if err != nil {
			return nil, err
		}
		filters.UpstreamModelMismatch = &value
	}

	return filters, nil
}
