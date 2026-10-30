package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// usageByIDRepo 让 GetByID 按 id 返回预置记录；缺失 id 返回 ErrUsageLogNotFound
// （与真实仓库的语义一致，见 repository/usage_log_repo_query.go）。
type usageByIDRepo struct {
	service.UsageLogRepository
	records map[int64]*service.UsageLog
}

func (r *usageByIDRepo) GetByID(_ context.Context, id int64) (*service.UsageLog, error) {
	if log, ok := r.records[id]; ok {
		return log, nil
	}
	return nil, service.ErrUsageLogNotFound
}

func newUsageByIDRouter(records map[int64]*service.UsageLog, viewerID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(&usageByIDRepo{records: records}, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: viewerID})
		c.Next()
	})
	router.GET("/usage/:id", handler.GetByID)
	return router
}

type usageIDErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// F-07：/api/v1/usage/:id 不得用 403/404 的差异泄露"这条记录是否存在"。
//
// 线上实测：别人的记录 → 403 "Not authorized to access this record"；
// 不存在的 id → 404 USAGE_LOG_NOT_FOUND。攻击者据此可枚举全站 usage_log 的 id。
//
// 契约：两种情形**同码同体**（都返回 404 + USAGE_LOG_NOT_FOUND），
// 外部无法区分"不存在"与"不属于你"。
func TestGetUsageByID_NotFoundAndNotOwnedAreIndistinguishable(t *testing.T) {
	records := map[int64]*service.UsageLog{
		11: {ID: 11, UserID: 999, Model: "other-users-record"}, // 存在，但属于别人
	}
	router := newUsageByIDRouter(records, 3) // 当前用户 uid=3

	fetch := func(path string) (*httptest.ResponseRecorder, usageIDErrorBody) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var body usageIDErrorBody
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
		return rec, body
	}

	foreignRec, foreignBody := fetch("/usage/11")
	missingRec, missingBody := fetch("/usage/999999")

	// 单独先断言"不是 403"，让失败信息直接指向缺陷。
	require.NotEqual(t, http.StatusForbidden, foreignRec.Code,
		"越权访问不得返回 403，否则与 404 形成存在性 oracle：%s", foreignRec.Body.String())

	require.Equal(t, http.StatusNotFound, foreignRec.Code, "越权访问应伪装成未找到：%s", foreignRec.Body.String())
	require.Equal(t, missingRec.Code, foreignRec.Code, "两种情形的状态码必须一致")
	require.Equal(t, missingBody.Message, foreignBody.Message, "两种情形必须同码同体（message）")
	require.Equal(t, missingBody.Reason, foreignBody.Reason, "两种情形必须同码同体（reason）")
	require.Equal(t, "USAGE_LOG_NOT_FOUND", foreignBody.Reason)
	require.Equal(t, "usage log not found", foreignBody.Message)
}

// 自己的记录必须照常返回 200：同码化只吞掉"别人的"和"不存在的"。
func TestGetUsageByID_OwnerStillReceivesRecord(t *testing.T) {
	records := map[int64]*service.UsageLog{
		12: {ID: 12, UserID: 3, Model: "my-model"},
	}
	router := newUsageByIDRouter(records, 3)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/12", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Code int `json:"code"`
		Data struct {
			ID    int64  `json:"id"`
			Model string `json:"model"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), rec.Body.String())
	require.Equal(t, 0, body.Code)
	require.Equal(t, int64(12), body.Data.ID)
	require.Equal(t, "my-model", body.Data.Model)
}

// 参数非法仍然是 400（这不是存在性信号，不需要伪装）。
func TestGetUsageByID_InvalidIDStaysBadRequest(t *testing.T) {
	router := newUsageByIDRouter(map[int64]*service.UsageLog{}, 3)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/usage/abc", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

var _ = infraerrors.Reason
