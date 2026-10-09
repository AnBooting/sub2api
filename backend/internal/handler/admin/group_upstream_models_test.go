package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupUpstreamStub struct {
	service.AdminService
	groups map[int64][]service.Account
}

func (s *groupUpstreamStub) GetGroup(context.Context, int64) (*service.Group, error) {
	return &service.Group{Platform: service.PlatformOpenAI}, nil
}

func (s *groupUpstreamStub) ListAccountsForSchedulerScoreFilter(_ context.Context, _, _, _, _ string, id int64, _ string) ([]service.Account, error) {
	return s.groups[id], nil
}

type upstreamFetcherStub struct {
	mu    sync.Mutex
	calls map[int64]int
	live  map[int64][]string
}

func (s *upstreamFetcherStub) FetchUpstreamSupportedModels(_ context.Context, a *service.Account) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[a.ID]++
	models, ok := s.live[a.ID]
	if !ok {
		return nil, errors.New("upstream unavailable")
	}
	return models, nil
}

func TestSyncGroupUpstreamModelsAcrossGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := func(id int64) service.Account {
		return service.Account{ID: id, Platform: service.PlatformOpenAI, Status: service.StatusActive, Schedulable: true}
	}
	a, b, disabled := account(1), account(2), account(3)
	a.Credentials = map[string]any{"model_mapping": map[string]any{"alias": "upstream-real", "stale": "missing"}}
	disabled.Schedulable = false
	svc := &groupUpstreamStub{groups: map[int64][]service.Account{1: {a, disabled}, 2: {a, b, account(4)}}}
	fetcher := &upstreamFetcherStub{calls: map[int64]int{}, live: map[int64][]string{1: {"upstream-real", "blocked"}, 2: {"new-live", "alias"}}}
	h := NewGroupHandler(svc, nil, nil)
	h.upstreamModelFetcher = fetcher
	router := gin.New()
	router.POST("/sync", h.SyncUpstreamModels)
	req := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(`{"group_ids":[1,2,1],"platform":"openai"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"models":["alias","new-live"],"failed_accounts":1,"total_accounts":3}}`, rec.Body.String())
	require.Equal(t, map[int64]int{1: 1, 2: 1, 4: 1}, fetcher.calls)
}

func TestSyncGroupUpstreamModelsAllFailuresDoNotAddConfiguredModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &groupUpstreamStub{groups: map[int64][]service.Account{1: {{ID: 1, Platform: service.PlatformOpenAI, Status: service.StatusActive, Schedulable: true}}}}
	h := NewGroupHandler(svc, nil, nil)
	h.upstreamModelFetcher = &upstreamFetcherStub{calls: map[int64]int{}, live: map[int64][]string{}}
	router := gin.New()
	router.POST("/sync", h.SyncUpstreamModels)
	for _, body := range []string{`{"group_ids":[],"platform":"openai"}`, `{"group_ids":[0],"platform":"openai"}`, `{"group_ids":[1],"platform":"composite"}`, `{"group_ids":[1],"platform":"anthropic"}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(`{"group_ids":[1],"platform":"openai"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadGateway, rec.Code)
}
