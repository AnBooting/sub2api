package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ingressRejectRecorderStub struct {
	mu       sync.Mutex
	calls    int
	clientIP string
}

func (r *ingressRejectRecorderStub) RecordIngressReject(_, _, _, clientIP string, _, _ int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.clientIP = clientIP
}

func TestNormalizeIngressRejectIP(t *testing.T) {
	require.Equal(t, "2001:db8:abcd:1234::", normalizeIngressRejectIP("2001:db8:abcd:1234:ffff::1"))
	require.Equal(t, "192.0.2.4", normalizeIngressRejectIP("::ffff:192.0.2.4"))
	require.Equal(t, "0.0.0.0", normalizeIngressRejectIP("not-an-ip"))
}

func TestLoggerRecordsIngressRejectOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &ingressRejectRecorderStub{}
	SetIngressRejectRecorder(recorder)
	t.Cleanup(func() { SetIngressRejectRecorder(nil) })
	router := gin.New()
	router.Use(Logger())
	router.GET("/v1/messages", func(c *gin.Context) {
		MarkIngressRejected(c, IngressRejectInvalidAPIKey)
		c.Status(http.StatusUnauthorized)
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	request.RemoteAddr = "[2001:db8:abcd:1234:ffff::1]:1234"
	router.ServeHTTP(httptest.NewRecorder(), request)
	recorder.mu.Lock()
	require.Equal(t, 1, recorder.calls)
	require.Equal(t, "2001:db8:abcd:1234::", recorder.clientIP)
	recorder.mu.Unlock()
}

func TestInvalidAuthClientKeyDefersToLimiterEmptyKeyPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	// 注入不可解析的客户端 IP，模拟 Gin 在边缘配置下给出空值/非 IP 的情况。
	req = req.WithContext(service.WithSessionBinding(req.Context(), &service.SessionBinding{IP: "not-an-ip"}))
	c.Request = req
	// 不可识别客户端不得折叠成共享哨兵，否则所有此类客户端共用一个限流桶互相连带封禁。
	require.Empty(t, invalidAuthClientKey(c))
}
