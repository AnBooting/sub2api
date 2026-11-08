//go:build unit

package server

import (
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	ratelimitmw "github.com/Wei-Shaw/sub2api/internal/middleware"
	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	servermw "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewPR18ComposeHostProxyKeepsClientBuckets(t *testing.T) {
	for _, trust := range []bool{true, false} {
		t.Run(fmt.Sprint(trust), func(t *testing.T) {
			defer ippkg.SetTrustedProxyPolicy(nil, false)
			mr := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer rdb.Close()
			cfg := &config.Config{}
			cfg.Server.Mode = "release"
			cfg.Server.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
			cfg.Server.TrustedProxiesConfigured = true
			cfg.SetTrustForwardedIPForAPIKeyACL(trust)
			router := gin.New()
			configureClientIPTrust(router, cfg)
			router.Use(servermw.SessionBindingContext(cfg))
			router.GET("/login", ratelimitmw.NewRateLimiter(rdb).Limit("auth-login", 20, time.Minute), func(c *gin.Context) { c.String(http.StatusOK, ippkg.GetClientIP(c)) })
			for i := 1; i <= 21; i++ {
				req := httptest.NewRequest(http.MethodGet, "/login", nil)
				req.RemoteAddr = "172.18.0.1:5000"
				req.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", i))
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if i == 1 || i == 21 {
					t.Logf("trust=%t client=%d status=%d body=%s", trust, i, rec.Code, rec.Body.String())
				}
				if rec.Code != http.StatusOK {
					t.Errorf("distinct client %d shares proxy rate bucket", i)
				}
			}
		})
	}
}
