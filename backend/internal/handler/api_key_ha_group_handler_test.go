//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// haHTTPKeyRepo 是用户 HA 开启接口的 Key 仓储桩：实现链事务接口，模拟真实仓储的
// revision 递增与链替换语义。
type haHTTPKeyRepo struct {
	service.APIKeyRepository
	key *service.APIKey
}

func (r *haHTTPKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	clone := *r.key
	clone.Groups = service.CloneAPIKeyGroupLinks(r.key.Groups)
	return &clone, nil
}

func (r *haHTTPKeyRepo) ListKeysByUserID(context.Context, int64) ([]string, error)  { return nil, nil }
func (r *haHTTPKeyRepo) ListKeysByGroupID(context.Context, int64) ([]string, error) { return nil, nil }

func (r *haHTTPKeyRepo) Update(_ context.Context, key *service.APIKey, fields service.APIKeyUpdateFields) error {
	if fields.Groups && fields.ExpectedGroupChainRevision == nil {
		return service.ErrGroupChainConflict
	}
	if fields.Groups && *fields.ExpectedGroupChainRevision != r.key.GroupChainRevision {
		return service.ErrGroupChainConflict
	}
	key.GroupChainRevision = r.key.GroupChainRevision
	if !service.APIKeyGroupChainsEqual(r.key.Groups, key.Groups) {
		key.GroupChainRevision++
	}
	key.GroupID = service.FirstNormalAPIKeyGroupID(key.Groups)
	clone := *key
	clone.Groups = service.CloneAPIKeyGroupLinks(key.Groups)
	r.key = &clone
	return nil
}

func (r *haHTTPKeyRepo) WithGroupChainTransaction(ctx context.Context, _ int64, expected *int64, fn func(context.Context, *service.APIKey) error) error {
	if expected != nil && *expected != r.key.GroupChainRevision {
		return service.ErrGroupChainConflict
	}
	clone := *r.key
	clone.Groups = service.CloneAPIKeyGroupLinks(r.key.Groups)
	return fn(ctx, &clone)
}

type haHTTPGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r *haHTTPGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	clone := *r.group
	return &clone, nil
}

func newHASetHandlerFixture(t *testing.T, key *service.APIKey, group *service.Group) (*gin.Engine, *haHTTPKeyRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &haHTTPKeyRepo{key: key}
	svc := service.NewAPIKeyService(repo, nil, &haHTTPGroupRepo{group: group}, nil, nil, nil, nil)
	router := gin.New()
	router.Use(withUserSubject(7))
	router.PUT("/api/v1/keys/:id/ha-groups/:groupID", NewAPIKeyHandler(svc).SetAPIKeyHAGroup)
	return router, repo
}

func callHASet(router *gin.Engine, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/keys/1/ha-groups/9", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	return result
}

func TestSetAPIKeyHAGroupHTTPEnableDisableAndMultiplierIgnored(t *testing.T) {
	key := &service.APIKey{ID: 1, UserID: 7, Key: "sk-ha", Name: "ha", Status: service.StatusActive,
		Groups: []service.APIKeyGroupLink{{GroupID: 1, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
	group := &service.Group{ID: 9, Name: "ha", Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 2.5, AllowHighAvailability: true}
	router, repo := newHASetHandlerFixture(t, key, group)

	// 请求体带倍率：必须被忽略，返回服务端配置倍率。
	rec := callHASet(router, `{"enabled":true,"rate_multiplier":0.0001}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var envelope struct {
		Data struct {
			Enabled            bool    `json:"enabled"`
			RateMultiplier     float64 `json:"rate_multiplier"`
			GroupChainRevision int64   `json:"group_chain_revision"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	payload := envelope.Data
	require.True(t, payload.Enabled)
	require.Equal(t, 2.5, payload.RateMultiplier, "client-supplied multiplier must be ignored")
	require.Equal(t, int64(1), payload.GroupChainRevision)

	var ha *service.APIKeyGroupLink
	for i := range repo.key.Groups {
		if repo.key.Groups[i].GroupID == 9 {
			ha = &repo.key.Groups[i]
		}
	}
	require.NotNil(t, ha)
	require.Equal(t, service.APIKeyGroupRoleHA, ha.Role)
	require.NotNil(t, ha.EnabledByUserAt)

	// 关闭：置空授权时间戳，链接保留。
	rec = callHASet(router, `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for i := range repo.key.Groups {
		if repo.key.Groups[i].GroupID == 9 {
			require.Equal(t, service.APIKeyGroupRoleHA, repo.key.Groups[i].Role)
			require.Nil(t, repo.key.Groups[i].EnabledByUserAt)
		}
	}
}

func TestSetAPIKeyHAGroupHTTPRejectsUnmarkedAndForeignKey(t *testing.T) {
	t.Run("unmarked group", func(t *testing.T) {
		key := &service.APIKey{ID: 1, UserID: 7, Key: "sk-ha", Status: service.StatusActive,
			Groups: []service.APIKeyGroupLink{{GroupID: 1, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
		router, repo := newHASetHandlerFixture(t, key, &service.Group{ID: 9, Status: service.StatusActive, AllowHighAvailability: false})
		rec := callHASet(router, `{"enabled":true}`)
		require.NotEqual(t, http.StatusOK, rec.Code)
		require.False(t, haHTTPLinkPresent(repo.key.Groups, 9))
	})

	t.Run("foreign key", func(t *testing.T) {
		key := &service.APIKey{ID: 1, UserID: 8, Key: "sk-other", Status: service.StatusActive,
			Groups: []service.APIKeyGroupLink{{GroupID: 1, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
		router, repo := newHASetHandlerFixture(t, key, &service.Group{ID: 9, Status: service.StatusActive, AllowHighAvailability: true})
		rec := callHASet(router, `{"enabled":true}`)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		require.False(t, haHTTPLinkPresent(repo.key.Groups, 9))
	})
}

func haHTTPLinkPresent(links []service.APIKeyGroupLink, groupID int64) bool {
	for _, link := range links {
		if link.GroupID == groupID {
			return true
		}
	}
	return false
}
