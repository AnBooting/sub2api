//go:build unit

package ip

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetTrustedClientIPUsesGinClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))

	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetTrustedClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

// setTestTrustedProxies 把「允许写入转发头的直连对端」设为给定网段，
// 测试结束后恢复默认策略（未配置 => 只信任同机反代）。
func setTestTrustedProxies(t *testing.T, cidrs ...string) {
	t.Helper()
	require.NoError(t, SetTrustedProxyPolicy(cidrs, true))
	t.Cleanup(func() {
		require.NoError(t, SetTrustedProxyPolicy(nil, false))
	})
}

func TestDefaultTrustedProxyPolicyOnlyTrustsLoopback(t *testing.T) {
	require.NoError(t, SetTrustedProxyPolicy(nil, false))

	require.True(t, IsTrustedDirectPeer("127.0.0.1"))
	require.True(t, IsTrustedDirectPeer("127.9.9.9"))
	require.True(t, IsTrustedDirectPeer("::1"))
	// 私网/公网都不是信任信号：只有显式声明的代理才可信。
	require.False(t, IsTrustedDirectPeer("10.0.0.5"))
	require.False(t, IsTrustedDirectPeer("192.168.32.1"))
	require.False(t, IsTrustedDirectPeer("203.0.113.7"))
	require.False(t, IsTrustedDirectPeer(""))
	require.False(t, IsTrustedDirectPeer("not-an-ip"))
}

func TestSetTrustedProxyPolicy(t *testing.T) {
	require.NoError(t, SetTrustedProxyPolicy([]string{"10.0.0.0/8", " 198.51.100.9 "}, true))
	t.Cleanup(func() { require.NoError(t, SetTrustedProxyPolicy(nil, false)) })

	require.True(t, IsTrustedDirectPeer("10.1.2.3"))
	require.True(t, IsTrustedDirectPeer("198.51.100.9"))
	require.False(t, IsTrustedDirectPeer("127.0.0.1"), "显式配置会取代 loopback 默认值")

	// 显式空列表：谁都不信任
	require.NoError(t, SetTrustedProxyPolicy([]string{}, true))
	require.False(t, IsTrustedDirectPeer("127.0.0.1"))
	require.False(t, IsTrustedDirectPeer("10.1.2.3"))

	// 非法配置不清空既有策略
	require.NoError(t, SetTrustedProxyPolicy([]string{"10.0.0.0/8"}, true))
	require.Error(t, SetTrustedProxyPolicy([]string{"not-an-ip"}, true))
	require.True(t, IsTrustedDirectPeer("10.1.2.3"))
}

func TestGetClientIPPreservesLegacyDockerForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 该用例覆盖「受信反代之后」的链选择：Docker 网关地址必须由运维显式声明为受信代理。
	setTestTrustedProxies(t, "192.168.32.1/32")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "192.168.32.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.2, 203.0.113.42")
	req.Header.Set("X-Real-IP", "192.168.32.1")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "203.0.113.42", w.Body.String())
}

func TestCheckIPRestrictionWithCompiledRules(t *testing.T) {
	whitelist := CompileIPRules([]string{"10.0.0.0/8", "192.168.1.2"})
	blacklist := CompileIPRules([]string{"10.1.1.1"})

	allowed, reason := CheckIPRestrictionWithCompiledRules("10.2.3.4", whitelist, blacklist)
	require.True(t, allowed)
	require.Equal(t, "", reason)

	allowed, reason = CheckIPRestrictionWithCompiledRules("10.1.1.1", whitelist, blacklist)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestCheckIPRestrictionWithCompiledRules_InvalidWhitelistStillDenies(t *testing.T) {
	// 与旧实现保持一致：白名单有配置但全无效时，最终应拒绝访问。
	invalidWhitelist := CompileIPRules([]string{"not-a-valid-pattern"})
	allowed, reason := CheckIPRestrictionWithCompiledRules("8.8.8.8", invalidWhitelist, nil)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestGetSecurityClientIPSwitchEnabledUsesLegacyHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setTestTrustedProxies(t, "9.9.9.9/32")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPCustomHeaderPrecedenceAndFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		trustForward   bool
		headers        []string
		requestHeaders map[string]string
		want           string
	}{
		{
			name:         "configured order precedes built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":      "198.51.100.10",
				"X-CDN-Second":     "203.0.113.20",
				"CF-Connecting-IP": "8.8.8.8",
			},
			want: "198.51.100.10",
		},
		{
			name:         "comma candidates skip invalid and private values",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":  "not-an-ip, 10.0.0.8",
				"X-CDN-Second": "also-bad, 203.0.113.9",
			},
			want: "203.0.113.9",
		},
		{
			name:         "legacy public header wins over custom private fallback",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "1.2.3.4",
			},
			want: "1.2.3.4",
		},
		{
			name:         "custom private fallback retains configured precedence",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "192.168.1.4",
			},
			want: "10.0.0.8",
		},
		{
			// CF-Connecting-IP 默认不再读取：客户端自带的该头在基线反代之后会原样
			// 穿过，采信它等于把身份交给攻击者，故回落到直连对端。
			name:         "invalid custom value and CF-Connecting-IP defaults to peer",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":         "1.2.3.4:443",
				"CF-Connecting-IP": "4.4.4.4",
			},
			want: "9.9.9.9",
		},
		{
			// 运维在 forwarded_client_ip_headers 里显式声明后，CF-Connecting-IP
			// 才被当作代理写入的可信头（Cloudflare 直连场景）。
			name:         "invalid custom value continues to explicitly trusted CF-Connecting-IP",
			trustForward: true,
			headers:      []string{"X-CDN-IP", "CF-Connecting-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":         "1.2.3.4:443",
				"CF-Connecting-IP": "4.4.4.4",
			},
			want: "4.4.4.4",
		},
		{
			// 代理基线覆盖的 X-Real-IP 仍然参与内置优先级。
			name:         "invalid custom value continues to built-in X-Real-IP",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "1.2.3.4:443",
				"X-Real-IP": "4.4.4.4",
			},
			want: "4.4.4.4",
		},
		{
			name:         "invalid legacy values continue to a valid forwarded address",
			trustForward: true,
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "unknown",
				"X-Real-IP":        "proxy.internal",
				"X-Forwarded-For":  "also-invalid, 203.0.113.50",
			},
			want: "203.0.113.50",
		},
		{
			name:         "all invalid legacy values fall back to the connection address",
			trustForward: true,
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "unknown",
				"X-Real-IP":        "proxy.internal",
				"X-Forwarded-For":  "also-invalid",
			},
			want: "9.9.9.9",
		},
		{
			name:         "disabled mode ignores custom and legacy headers",
			trustForward: false,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "1.2.3.4",
				"X-Real-IP": "4.4.4.4",
			},
			want: "9.9.9.9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setTestTrustedProxies(t, "9.9.9.9/32")
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettings(c, test.trustForward, test.headers)
				c.String(200, GetSecurityClientIP(c, !test.trustForward))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "9.9.9.9:12345"
			for name, value := range test.requestHeaders {
				req.Header.Set(name, value)
			}
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

func TestGetSecurityClientIPSwitchDisabledUsesConfiguredTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"9.9.9.9"}))
	r.GET("/t", func(c *gin.Context) { c.String(200, GetSecurityClientIP(c, false)) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetClientIPSwitchDisabledUsesTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetLegacyForwardedIPTrust(c, false)
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotCopiesCustomHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setTestTrustedProxies(t, "9.9.9.9/32")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		headers := []string{"X-Original-IP"}
		SetForwardedIPSettings(c, true, headers)
		headers[0] = "X-Mutated-IP"
		c.String(200, GetSecurityClientIP(c, false))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Original-IP", "1.2.3.4")
	req.Header.Set("X-Mutated-IP", "4.4.4.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotOverridesLiveFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		requestTrust  bool
		fallbackTrust bool
		want          string
	}{
		{name: "captured secure mode wins", requestTrust: false, fallbackTrust: true, want: "9.9.9.9"},
		{name: "captured compatibility mode wins", requestTrust: true, fallbackTrust: false, want: "1.2.3.4"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setTestTrustedProxies(t, "9.9.9.9/32")
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetLegacyForwardedIPTrust(c, test.requestTrust)
				c.String(200, GetSecurityClientIP(c, test.fallbackTrust))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "9.9.9.9:12345"
			req.Header.Set("X-Real-IP", "1.2.3.4")
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

// ---- F-01：转发头信任方向必须由「谁写的头」决定 ----
// 直连对端不是受信代理时，所有原始转发头都可能由客户端伪造，必须完全忽略。

func TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, []string{"X-CDN-IP"})
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	// 直连对端是公网地址，且未被声明为受信代理
	req.RemoteAddr = "203.0.113.7:41234"
	req.Header.Set("CF-Connecting-IP", "198.51.100.1")
	req.Header.Set("X-Real-IP", "198.51.100.2")
	req.Header.Set("X-Forwarded-For", "198.51.100.3")
	req.Header.Set("X-CDN-IP", "198.51.100.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "203.0.113.7", w.Body.String())
}

// 私网对端同样不是信任信号：只有显式声明的代理网段才可信。
func TestGetSecurityClientIPIgnoresForwardedHeadersFromUntrustedPrivatePeer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "192.168.32.1:41234"
	req.Header.Set("X-Forwarded-For", "10.0.0.2, 203.0.113.42")
	req.Header.Set("X-Real-IP", "203.0.113.43")
	r.ServeHTTP(w, req)

	require.Equal(t, "192.168.32.1", w.Body.String())
}

// 线上复现的绕过手法：每请求轮换 X-Real-IP，必须无法改变解析结果。
func TestGetSecurityClientIPRotatingForwardedHeadersCannotChangeIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, true))
	})

	seen := make(map[string]struct{}, 25)
	for i := 0; i < 25; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/t", nil)
		req.RemoteAddr = "203.0.113.50:5000"
		req.Header.Set("X-Real-IP", "198.51.100."+strconv.Itoa(10+i))
		r.ServeHTTP(w, req)
		seen[w.Body.String()] = struct{}{}
	}

	require.Len(t, seen, 1, "轮换 X-Real-IP 不得产生多个身份: %v", seen)
	_, ok := seen["203.0.113.50"]
	require.True(t, ok)
}

// 同机反代（loopback 对端）是唯一无需额外配置即可信任的形态。
func TestGetSecurityClientIPHonorsForwardedHeadersFromLoopbackPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "127.0.0.1:51000"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

// 受信代理写入的头仍然优先：确认修复没有把合法反代部署一并打死。
func TestGetSecurityClientIPUsesHeadersFromConfiguredTrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setTestTrustedProxies(t, "10.20.0.0/16")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "10.20.30.40:51000"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

// 反例矩阵：不受信对端写入的各种转发头变体都不得改变解析结果，
// 受信对端写入时则必须采信代理值。
func TestGetSecurityClientIPForwardedHeaderCounterExamples(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		trustedPeers []string
		remoteAddr   string
		headers      [][2]string
		custom       []string
		want         string
	}{
		{
			name:       "untrusted peer with IPv4-mapped IPv6 header value",
			remoteAddr: "203.0.113.9:1234",
			headers:    [][2]string{{"X-Real-IP", "::ffff:198.51.100.7"}},
			want:       "203.0.113.9",
		},
		{
			name:       "untrusted peer with port in header value",
			remoteAddr: "203.0.113.9:1234",
			headers:    [][2]string{{"X-Real-IP", "198.51.100.7:1234"}},
			want:       "203.0.113.9",
		},
		{
			name:       "untrusted peer with comma chain",
			remoteAddr: "203.0.113.9:1234",
			headers:    [][2]string{{"X-Forwarded-For", "1.2.3.4, 5.6.7.8"}},
			want:       "203.0.113.9",
		},
		{
			name:       "untrusted peer with duplicated X-Real-IP headers",
			remoteAddr: "203.0.113.9:1234",
			headers:    [][2]string{{"X-Real-IP", "1.2.3.4"}, {"X-Real-IP", "5.6.7.8"}},
			want:       "203.0.113.9",
		},
		{
			name:       "untrusted peer with lower-case header name",
			remoteAddr: "203.0.113.9:1234",
			headers:    [][2]string{{"x-real-ip", "1.2.3.4"}},
			want:       "203.0.113.9",
		},
		{
			name:       "untrusted peer with admin-configured custom header",
			remoteAddr: "203.0.113.9:1234",
			headers:    [][2]string{{"X-CDN-Client-IP", "1.2.3.4"}, {"CF-Connecting-IP", "5.6.7.8"}},
			custom:     []string{"X-CDN-Client-IP"},
			want:       "203.0.113.9",
		},
		{
			name:         "trusted peer header value wins over peer address",
			trustedPeers: []string{"10.20.0.0/16"},
			remoteAddr:   "10.20.30.40:1234",
			headers:      [][2]string{{"X-Real-IP", "1.2.3.4"}},
			want:         "1.2.3.4",
		},
		{
			name:         "trusted peer custom header wins over built-ins",
			trustedPeers: []string{"10.20.0.0/16"},
			remoteAddr:   "10.20.30.40:1234",
			headers:      [][2]string{{"X-CDN-Client-IP", "198.51.100.10"}, {"X-Real-IP", "1.2.3.4"}},
			custom:       []string{"X-CDN-Client-IP"},
			want:         "198.51.100.10",
		},
		{
			name:         "trusted peer with port value is normalized",
			trustedPeers: []string{"10.20.0.0/16"},
			remoteAddr:   "10.20.30.40:1234",
			headers:      [][2]string{{"X-Real-IP", "198.51.100.7:8443"}},
			want:         "198.51.100.7",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.trustedPeers) > 0 {
				setTestTrustedProxies(t, test.trustedPeers...)
			}

			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettings(c, true, test.custom)
				c.String(200, GetSecurityClientIP(c, true))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = test.remoteAddr
			for _, header := range test.headers {
				req.Header.Add(header[0], header[1])
			}
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

// 受信对端网段本身也必须是可用的 CIDR 判定，不能靠字符串前缀匹配。
func TestIsTrustedDirectPeerUsesNetworkSemantics(t *testing.T) {
	setTestTrustedProxies(t, "10.20.0.0/16", "2001:db8:1234::/48")

	require.True(t, IsTrustedDirectPeer("10.20.255.254"))
	require.True(t, IsTrustedDirectPeer("2001:db8:1234::1"))
	require.False(t, IsTrustedDirectPeer("10.21.0.1"))
	// 边界：/16 的相邻网段不因字符串前缀相似而被误判
	require.False(t, IsTrustedDirectPeer("10.200.0.1"))
	require.False(t, IsTrustedDirectPeer("2001:db8:1235::1"))
}

// 异常 RemoteAddr（空/非法）时不得返回空身份：否则审计日志、会话绑定、
// 限流键会把这些请求全部聚到空串上。
func TestGetClientIPNeverReturnsEmptyForUnparseableRemoteAddr(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, remoteAddr := range []string{"", "garbage", "not:a:host:port", "  "} {
		t.Run("remote_addr="+remoteAddr, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettings(c, true, nil)
				got := GetClientIP(c)
				require.NotEmpty(t, got, "GetClientIP 不得返回空身份")
				c.String(200, got)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = remoteAddr
			req.Header.Set("X-Real-IP", "198.51.100.7")
			r.ServeHTTP(w, req)

			require.Equal(t, 200, w.Code)
			require.NotEmpty(t, w.Body.String(), "响应体不得为空身份")
			// 无法判定对端 => 不采信伪造头
			require.NotEqual(t, "198.51.100.7", w.Body.String())
		})
	}
}

// ---- 受信代理之后的信任边界（reviewer 反例）----
// 受信只说明代理可信，不能说明每个头都由代理写入：基线反代只覆盖
// X-Real-IP / X-Forwarded-For，客户端自带的 CF-Connecting-IP 会原样穿过。

func TestTrustedPeerCannotRotateIdentityViaClientSuppliedCFConnectingIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setTestTrustedProxies(t, "127.0.0.0/8")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, true))
	})

	seen := make(map[string]struct{}, 5)
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/t", nil)
		// 对端受信（同机 Caddy），但 Caddy 基线不覆盖 CF-Connecting-IP
		req.RemoteAddr = "127.0.0.1:5000"
		req.Header.Set("CF-Connecting-IP", "198.51.100."+strconv.Itoa(10+i))
		r.ServeHTTP(w, req)
		seen[w.Body.String()] = struct{}{}
	}

	require.Len(t, seen, 1, "客户端自带的 CF-Connecting-IP 不得产生多个身份: %v", seen)
	_, ok := seen["127.0.0.1"]
	require.True(t, ok, "没有代理写入的可信头时应取直连对端")
}

func TestTrustedPeerPrefersProxyAppendedForwardedForTail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setTestTrustedProxies(t, "127.0.0.0/8")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	// 攻击者自带链首 1.1.1.1，Caddy 追加真实对端 203.0.113.7
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.7")
	r.ServeHTTP(w, req)

	require.Equal(t, "203.0.113.7", w.Body.String(), "XFF 必须从右往左取代理追加的真值")
}

// 显式声明 CF-Connecting-IP 为受信头时才读它（forwarded_client_ip_headers 的语义）。
func TestCFConnectingIPIsReadOnlyWhenExplicitlyConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setTestTrustedProxies(t, "127.0.0.0/8")

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, []string{"CF-Connecting-IP"})
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	req.Header.Set("CF-Connecting-IP", "198.51.100.77")
	r.ServeHTTP(w, req)

	require.Equal(t, "198.51.100.77", w.Body.String(),
		"运维显式声明后必须生效（Cloudflare 等直连场景）")
}

// secure 路径（开关关闭）同样不得返回空身份。
func TestGetSecurityClientIPNeverReturnsEmptyOnSecurePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, remoteAddr := range []string{"", "garbage", "not:a:host:port"} {
		t.Run("remote_addr="+remoteAddr, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				require.NotEmpty(t, GetTrustedClientIP(c))
				require.NotEmpty(t, GetSecurityClientIP(c, false))
				c.String(200, GetSecurityClientIP(c, false))
			})
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = remoteAddr
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, "unknown", w.Body.String())
		})
	}
}
