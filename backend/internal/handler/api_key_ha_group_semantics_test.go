//go:build unit

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newHASetHandlerRouterBoth 注册 PUT 与 DELETE 两个方法，覆盖 task-10 要求的
// 7 种请求组合。复用同包 api_key_ha_group_handler_test.go 中的仓储桩。
func newHASetHandlerRouterBoth(t *testing.T, key *service.APIKey, group *service.Group) (*gin.Engine, *haHTTPKeyRepo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &haHTTPKeyRepo{key: key}
	svc := service.NewAPIKeyService(repo, nil, &haHTTPGroupRepo{group: group}, nil, nil, nil, nil)
	handler := NewAPIKeyHandler(svc)
	router := gin.New()
	router.Use(withUserSubject(7))
	router.PUT("/api/v1/keys/:id/ha-groups/:groupID", handler.SetAPIKeyHAGroup)
	router.DELETE("/api/v1/keys/:id/ha-groups/:groupID", handler.SetAPIKeyHAGroup)
	return router, repo
}

// callHASetMethod 发送指定方法与 body；contentType 为空表示不设置该头。
func callHASetMethod(router *gin.Engine, method, body, contentType string) *httptest.ResponseRecorder {
	var reader *bytes.Buffer
	if body == "" {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = bytes.NewBufferString(body)
	}
	request := httptest.NewRequest(method, "/api/v1/keys/1/ha-groups/9", reader)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	return result
}

func haResponseEnabled(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()
	var envelope struct {
		Data struct {
			Enabled bool `json:"enabled"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	return envelope.Data.Enabled
}

func newHAHandlerTestKey() *service.APIKey {
	return &service.APIKey{ID: 1, UserID: 7, Key: "sk-ha", Name: "ha", Status: service.StatusActive,
		Groups: []service.APIKeyGroupLink{{GroupID: 1, Role: service.APIKeyGroupRoleNormal, Position: 0}}}
}

func newHAHandlerTestGroup() *service.Group {
	return &service.Group{ID: 9, Name: "ha", Platform: service.PlatformOpenAI, Status: service.StatusActive, RateMultiplier: 2.5, AllowHighAvailability: true}
}

func haHTTPLinkEnabled(t *testing.T, repo *haHTTPKeyRepo) (bool, bool) {
	t.Helper()
	for _, link := range repo.key.Groups {
		if link.GroupID == 9 {
			return true, link.EnabledByUserAt != nil
		}
	}
	return false, false
}

// ① DELETE 无 body：必须关闭，且不得 400。
func TestHASetHTTPDeleteWithoutBodyDisables(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	require.Equal(t, http.StatusOK, callHASetMethod(router, http.MethodPut, `{"enabled":true}`, "application/json").Code)

	rec := callHASetMethod(router, http.MethodDelete, "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, haResponseEnabled(t, rec))
	present, enabled := haHTTPLinkEnabled(t, repo)
	require.True(t, present, "DELETE keeps the admin-configured ha link")
	require.False(t, enabled, "DELETE must clear enabled_by_user_at")
}

// ② DELETE 空 body + JSON 头：真实客户端默认带该头，必须同样成功关闭。
func TestHASetHTTPDeleteEmptyBodyWithJSONHeaderDisables(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	require.Equal(t, http.StatusOK, callHASetMethod(router, http.MethodPut, `{"enabled":true}`, "application/json").Code)

	rec := callHASetMethod(router, http.MethodDelete, "", "application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, haResponseEnabled(t, rec))
	present, enabled := haHTTPLinkEnabled(t, repo)
	require.True(t, present)
	require.False(t, enabled)
}

// ③ DELETE 携带 {"enabled":true}：仍必须关闭，body 不得反转语义。
func TestHASetHTTPDeleteWithEnabledTrueStillDisables(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	require.Equal(t, http.StatusOK, callHASetMethod(router, http.MethodPut, `{"enabled":true}`, "application/json").Code)

	rec := callHASetMethod(router, http.MethodDelete, `{"enabled":true}`, "application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, haResponseEnabled(t, rec), "DELETE must ignore body.enabled")
	present, enabled := haHTTPLinkEnabled(t, repo)
	require.True(t, present)
	require.False(t, enabled)
}

// ④ PUT {}：必须 400，不得静默开启。
func TestHASetHTTPPutEmptyObjectIsRejected(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	rec := callHASetMethod(router, http.MethodPut, `{}`, "application/json")
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	present, _ := haHTTPLinkEnabled(t, repo)
	require.False(t, present, "rejected PUT must not write")
	require.Zero(t, repo.key.GroupChainRevision)
}

// ⑤ PUT {"enabled":false}：显式关闭。
func TestHASetHTTPPutExplicitFalseDisables(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	rec := callHASetMethod(router, http.MethodPut, `{"enabled":false}`, "application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, haResponseEnabled(t, rec))
	present, _ := haHTTPLinkEnabled(t, repo)
	require.False(t, present)
}

// ⑥ PUT {"enabled":true}：显式开启，倍率取服务端配置，未知字段被忽略。
func TestHASetHTTPPutExplicitTrueEnables(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	rec := callHASetMethod(router, http.MethodPut, `{"enabled":true,"unknown_field":123}`, "application/json")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, haResponseEnabled(t, rec))
	present, enabled := haHTTPLinkEnabled(t, repo)
	require.True(t, present)
	require.True(t, enabled)
	require.Equal(t, int64(1), repo.key.GroupChainRevision)
}

// ⑦ 重复开启幂等：时间戳与 revision 都不变。
func TestHASetHTTPRepeatedEnableIsIdempotent(t *testing.T) {
	router, repo := newHASetHandlerRouterBoth(t, newHAHandlerTestKey(), newHAHandlerTestGroup())
	first := callHASetMethod(router, http.MethodPut, `{"enabled":true}`, "application/json")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())

	var stamped *service.APIKeyGroupLink
	for i := range repo.key.Groups {
		if repo.key.Groups[i].GroupID == 9 {
			stamped = &repo.key.Groups[i]
		}
	}
	require.NotNil(t, stamped)
	require.NotNil(t, stamped.EnabledByUserAt)
	firstAt := *stamped.EnabledByUserAt
	firstRev := repo.key.GroupChainRevision

	second := callHASetMethod(router, http.MethodPut, `{"enabled":true}`, "application/json")
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	for i := range repo.key.Groups {
		if repo.key.Groups[i].GroupID == 9 {
			require.True(t, repo.key.Groups[i].EnabledByUserAt.Equal(firstAt), "repeated enable must keep the original timestamp")
		}
	}
	require.Equal(t, firstRev, repo.key.GroupChainRevision, "repeated enable must not increment revision")
}
