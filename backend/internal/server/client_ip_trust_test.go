//go:build unit

package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	ratelimitmw "github.com/Wei-Shaw/sub2api/internal/middleware"
	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	servermw "github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// resetTrustedProxyPolicy 把 pkg/ip 的受信对端策略恢复为默认值（只信任同机反代），
// 避免测试之间通过进程级原子快照互相污染。
func resetTrustedProxyPolicy(t *testing.T) {
	t.Helper()
	require.NoError(t, ippkg.SetTrustedProxyPolicy(nil, false))
	t.Cleanup(func() {
		require.NoError(t, ippkg.SetTrustedProxyPolicy(nil, false))
	})
}

// 锁住启动期接线本身：configureClientIPTrust 必须把 server.trusted_proxies
// 同时灌进 gin 的可信链与 pkg/ip 的受信对端策略。删掉接线会让本测试变红。
func TestConfigureClientIPTrustWiresTrustedProxyPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetTrustedProxyPolicy(t)

	cfg := &config.Config{}
	cfg.Server.TrustedProxies = []string{"172.18.0.0/16"}
	cfg.Server.TrustedProxiesConfigured = true
	cfg.SetTrustForwardedIPForAPIKeyACL(true)

	r := gin.New()
	configureClientIPTrust(r, cfg)

	// pkg/ip 侧：代理网段受信，其它公网对端不受信。
	require.True(t, ippkg.IsTrustedDirectPeer("172.18.0.1"))
	require.False(t, ippkg.IsTrustedDirectPeer("203.0.113.5"))

	// 端到端：受信代理写入的 X-Real-IP 被采信；公网直连客户端伪造的同一头被忽略。
	r.GET("/t", func(c *gin.Context) {
		c.String(http.StatusOK, c.Request.Header.Get("X-Real-IP")+"|"+ipForTest(c, cfg))
	})

	trustedReq := httptest.NewRequest(http.MethodGet, "/t", nil)
	trustedReq.RemoteAddr = "172.18.0.1:40000"
	trustedReq.Header.Set("X-Real-IP", "198.51.100.9")
	trustedRec := httptest.NewRecorder()
	r.ServeHTTP(trustedRec, trustedReq)
	require.Equal(t, "198.51.100.9|198.51.100.9", trustedRec.Body.String())

	untrustedReq := httptest.NewRequest(http.MethodGet, "/t", nil)
	untrustedReq.RemoteAddr = "203.0.113.5:40000"
	untrustedReq.Header.Set("X-Real-IP", "198.51.100.9")
	untrustedRec := httptest.NewRecorder()
	r.ServeHTTP(untrustedRec, untrustedReq)
	require.Equal(t, "198.51.100.9|203.0.113.5", untrustedRec.Body.String())
}

// ipForTest 读取与限流/审计一致的客户端 IP（不修改请求头）。
func ipForTest(c *gin.Context, cfg *config.Config) string {
	return ippkg.GetSecurityClientIP(c, cfg.TrustForwardedIPForAPIKeyACL())
}

func TestShouldWarnClientIPCollapse(t *testing.T) {
	require.True(t, shouldWarnClientIPCollapse("release", false, false),
		"release + 未配可信代理 + 不采信转发头 => 必须告警")
	require.False(t, shouldWarnClientIPCollapse("release", true, false),
		"兼容开关开启时仍采信转发头，不会坍缩，无需告警")
	require.False(t, shouldWarnClientIPCollapse("release", false, true),
		"已配置可信代理时不会坍缩")
	require.False(t, shouldWarnClientIPCollapse("debug", false, false),
		"非 release 模式不告警，避免本地开发噪音")
}

func TestComposeGatewayKeepsClientBucketsAndRejectsOtherPeers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			resetTrustedProxyPolicy(t)
			cfg := &config.Config{}
			cfg.Server.Mode = "release"
			cfg.Server.TrustedProxies = []string{"172.30.18.1/32"}
			cfg.Server.TrustedProxiesConfigured = true
			cfg.SetTrustForwardedIPForAPIKeyACL(legacy)
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { require.NoError(t, rdb.Close()) })
			router := gin.New()
			configureClientIPTrust(router, cfg)
			router.Use(servermw.SessionBindingContext(cfg))
			router.GET("/login", ratelimitmw.NewRateLimiter(rdb).Limit("auth-login", 20, time.Minute), func(c *gin.Context) {
				c.String(http.StatusOK, ippkg.GetClientIP(c))
			})

			for i := 1; i <= 21; i++ {
				req := httptest.NewRequest(http.MethodGet, "/login", nil)
				req.RemoteAddr = "172.30.18.1:5000"
				req.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", i))
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, fmt.Sprintf("203.0.113.%d", i), rec.Body.String(),
					"trusted Compose gateway must preserve distinct real client IPs")
			}

			for _, peer := range []string{"172.30.18.2", "172.30.18.3", "203.0.113.254"} {
				req := httptest.NewRequest(http.MethodGet, "/login", nil)
				req.RemoteAddr = peer + ":5000"
				req.Header.Set("X-Real-IP", "198.51.100.99")
				req.Header.Set("X-Forwarded-For", "198.51.100.99")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, peer, rec.Body.String(),
					"only the gateway /32 may supply forwarding headers")
			}
		})
	}
}
