package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestUserFacingGroupNameFollowsRequestLanguage 锁住分组 i18n 的第一条语义：
// 用户侧看到的分组名与描述随请求语言变化，译文表不外泄，且 service 层对象不被改写
// （计费侧读到的仍是原名）。
func TestUserFacingGroupNameFollowsRequestLanguage(t *testing.T) {
	group := &service.Group{
		ID: 7, Name: "标准", Description: "号池", Platform: "anthropic",
		I18n: service.GroupI18n{"en": {Name: "Standard", Description: "Pro + Max pool"}},
	}

	for _, tc := range []struct {
		lang            string
		wantName        string
		wantDescription string
	}{
		{"en", "Standard", "Pro + Max pool"},
		{"en-US,en;q=0.9", "Standard", "Pro + Max pool"},
		{"zh-CN,zh;q=0.9", "标准", "号池"},
		{"", "标准", "号池"},
	} {
		t.Run("lang="+tc.lang, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/groups/available", nil)
			if tc.lang != "" {
				c.Request.Header.Set("Accept-Language", tc.lang)
			}

			out := dto.GroupFromService(group).Localized(c.GetHeader("Accept-Language"))
			require.Equal(t, tc.wantName, out.Name)
			require.Equal(t, tc.wantDescription, out.Description)

			raw, err := json.Marshal(out)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "i18n", "译文表不得外泄给用户")

			// API Key 携带的分组走同一条本地化路径。
			keyOut := dto.APIKeyFromService(&service.APIKey{ID: 3, Group: group}).Localized(c.GetHeader("Accept-Language"))
			require.NotNil(t, keyOut.Group)
			require.Equal(t, tc.wantName, keyOut.Group.Name)
		})
	}

	// service 层对象不被本地化改写：计费侧读到的仍是原名。
	require.Equal(t, "标准", group.Name)
	require.Equal(t, "号池", group.Description)
}

// TestBillingAndAdminPathsNeverLocalizeGroupNames 锁住分组 i18n 的第二条语义：
// 本地化只允许发生在用户侧出口。service（计费/鉴权）与 admin（管理端）两个方向都
// 不得调用 DTO 本地化，否则计费口径或管理端回写会随请求语言变化。
func TestBillingAndAdminPathsNeverLocalizeGroupNames(t *testing.T) {
	for _, dir := range []string{"../service", "admin"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "扫描目录不能为空: %s", dir)

		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			for i, line := range strings.Split(string(source), "\n") {
				require.NotContainsf(t, line, ".Localized(",
					"%s:%d localizes a group outside the user-facing output", file, i+1)
			}
		}
	}
}
