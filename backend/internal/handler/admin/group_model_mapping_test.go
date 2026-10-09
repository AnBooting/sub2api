package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupMappingCandidatesStub struct {
	service.AdminService
	groupID  int64
	platform string
}

func (s *groupMappingCandidatesStub) GetGroupModelMappingCandidates(_ context.Context, id int64, platform string) ([]string, error) {
	s.groupID, s.platform = id, platform
	return []string{"gpt-custom"}, nil
}

func TestGroupModelMappingCandidatesHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &groupMappingCandidatesStub{}
	h := NewGroupHandler(svc, nil, nil)
	router := gin.New()
	router.GET("/groups/:id/model-mapping-candidates", h.GetGroupModelMappingCandidates)
	for _, id := range []string{"0", "-1", "invalid"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/groups/"+id+"/model-mapping-candidates", nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	require.Zero(t, svc.groupID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/groups/3/model-mapping-candidates?platform=openai", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(3), svc.groupID)
	require.Equal(t, "openai", svc.platform)
	var result struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result.Data, 1)
	require.JSONEq(t, `["gpt-custom"]`, string(result.Data["models"]))
}
