//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestMessagesLockedPricingExcludesGeminiPlatformBranch 锁定媒体边界：Gemini 平台分组
// 走 Messages 的独立转发分支，其 ForwardResult 会带 ImageCount（resolveGeminiImageCount），
// 而本切片锁价消费者只覆盖纯 token 形态。因此该分支必须留在旧行为，不能被接进锁价。
//
// 反例：若把该分支接进锁价，图片结果会在消费侧触发媒体守卫（用户已收到图片却零计费），
// 或者更糟地按 token 价错价。
func TestMessagesLockedPricingExcludesGeminiPlatformBranch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(7700)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	h := &GatewayHandler{cfg: cfg}
	key := &service.APIKey{ID: 7720, GroupID: &groupID, Group: group}

	// 直接验证分支判定：分组平台为 Gemini 时必须排除锁价。
	require.True(t, messagePlatformResolvesToGemini(nil, key), "Gemini 分组必须排除在锁价之外")

	// force platform（/antigravity 等路由）解析为 Gemini 时同样排除。
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	c.Request = req
	c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformGemini)
	require.True(t, messagePlatformResolvesToGemini(c, key))

	// composite 解析出的目标平台为 Gemini 时同样排除。
	recorder2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(recorder2)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))
	req2 = req2.WithContext(service.WithResolvedTargetPlatform(context.WithValue(req2.Context(), ctxkey.Group, group), service.PlatformGemini))
	c2.Request = req2
	anthropicKey := &service.APIKey{ID: 7721, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformAnthropic}}
	require.True(t, messagePlatformResolvesToGemini(c2, anthropicKey))

	// 非 Gemini 的普通 Anthropic 分组不排除。
	anthropicGroup := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
	require.False(t, messagePlatformResolvesToGemini(nil, &service.APIKey{ID: 7722, GroupID: &groupID, Group: anthropicGroup}))
	_ = h
}
