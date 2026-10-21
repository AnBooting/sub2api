package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Q6: 代理导入遇到同名时不得静默覆盖 name→ID 反查表，必须拒绝该条并记录错误。
// 这里覆盖三条契约：
//  1. 与 DB 既有代理同名但 proxy_key 不同 -> 拒绝该条，不创建新代理；
//  2. 同一批次内两条不同 proxy_key 使用同一 name -> 第二条被拒绝；
//  3. 同名且 proxy_key 相同（同一条代理）-> 仍按复用处理，不误判为冲突。

func importPayload(t *testing.T, proxies []map[string]any, accounts []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"type":     dataType,
			"version":  dataVersion,
			"proxies":  proxies,
			"accounts": accounts,
		},
		"skip_default_group_bind": true,
	})
	require.NoError(t, err)
	return body
}

func postImport(t *testing.T, router *gin.Engine, body []byte) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	return resp.Data
}

func errorMessages(data map[string]any) []string {
	raw, _ := data["errors"].([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			if msg, ok := entry["message"].(string); ok {
				out = append(out, msg)
			}
		}
	}
	return out
}

func TestImportDataRejectsProxyNameConflictWithExistingProxy(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.proxies = []service.Proxy{{
		ID:       1,
		Name:     "shared-name",
		Protocol: "socks5",
		Host:     "1.2.3.4",
		Port:     1080,
		Username: "u",
		Password: "p",
		Status:   service.StatusActive,
	}}

	body := importPayload(t, []map[string]any{{
		"proxy_key": "http|9.9.9.9|8080|other|secret",
		"name":      "shared-name",
		"protocol":  "http",
		"host":      "9.9.9.9",
		"port":      8080,
		"username":  "other",
		"password":  "secret",
		"status":    "active",
	}}, []map[string]any{})

	data := postImport(t, router, body)

	require.EqualValues(t, 0, data["proxy_created"], "同名冲突不得创建新代理")
	require.EqualValues(t, 0, data["proxy_reused"], "同名冲突不得被当作复用")
	require.EqualValues(t, 1, data["proxy_failed"], "同名冲突必须计入失败")
	require.Len(t, adminSvc.createdProxies, 0, "同名冲突不得调用 CreateProxy")
	require.Contains(t, strings.Join(errorMessages(data), "\n"), "duplicate proxy name \"shared-name\"")
}

func TestImportDataRejectsDuplicateProxyNameWithinBatch(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	body := importPayload(t, []map[string]any{
		{
			"proxy_key": "http|1.1.1.1|8080||",
			"name":      "dup",
			"protocol":  "http",
			"host":      "1.1.1.1",
			"port":      8080,
		},
		{
			"proxy_key": "http|2.2.2.2|8080||",
			"name":      "dup",
			"protocol":  "http",
			"host":      "2.2.2.2",
			"port":      8080,
		},
	}, []map[string]any{})

	data := postImport(t, router, body)

	require.EqualValues(t, 1, data["proxy_created"], "第一条应正常创建")
	require.EqualValues(t, 1, data["proxy_failed"], "批内第二条同名必须被拒绝")
	require.Len(t, adminSvc.createdProxies, 1, "只允许创建第一条")
	require.Contains(t, strings.Join(errorMessages(data), "\n"), "duplicate proxy name \"dup\"")
}

func TestImportDataReusesSameProxyWithSameName(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.proxies = []service.Proxy{{
		ID:       1,
		Name:     "proxy",
		Protocol: "socks5",
		Host:     "1.2.3.4",
		Port:     1080,
		Username: "u",
		Password: "p",
		Status:   service.StatusActive,
	}}

	body := importPayload(t, []map[string]any{{
		"proxy_key": "socks5|1.2.3.4|1080|u|p",
		"name":      "proxy",
		"protocol":  "socks5",
		"host":      "1.2.3.4",
		"port":      1080,
		"username":  "u",
		"password":  "p",
		"status":    "active",
	}}, []map[string]any{})

	data := postImport(t, router, body)

	require.EqualValues(t, 1, data["proxy_reused"], "同 key 同名仍是复用")
	require.EqualValues(t, 0, data["proxy_failed"])
	require.Empty(t, errorMessages(data))
	require.Len(t, adminSvc.createdProxies, 0)
}
