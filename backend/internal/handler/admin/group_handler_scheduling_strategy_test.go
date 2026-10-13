//go:build unit

package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// 分组调度策略请求字段（C-4）：缺省/显式值语义由 binding tag 与 service 归一化决定。
func TestGroupSchedulingStrategyRequestBinding(t *testing.T) {
	t.Run("create defaults to empty string", func(t *testing.T) {
		var req CreateGroupRequest
		require.NoError(t, json.Unmarshal([]byte("{}"), &req))
		require.Empty(t, req.SchedulingStrategy)
	})

	for _, value := range []string{"inherit", "stable", "balanced", "latency"} {
		t.Run("create accepts "+value, func(t *testing.T) {
			var req CreateGroupRequest
			payload := []byte("{\"scheduling_strategy\":\"" + value + "\"}")
			require.NoError(t, json.Unmarshal(payload, &req))
			require.Equal(t, value, req.SchedulingStrategy)
		})
	}

	// binding 口径必须与全局 settings 路径一致：大小写/首尾空白由 service 归一，
	// 不能被 gin 的 oneof（大小写敏感）挡在 400。
	t.Run("create binding accepts mixed case with surrounding spaces", func(t *testing.T) {
		var req CreateGroupRequest
		require.NoError(t, bindGroupPlatformJSON(t, &req, `{"name":"g","platform":"openai","scheduling_strategy":" Stable "}`),
			"分组入口必须与全局路径同样归一大小写/空白，而不是 binding 400")
		require.Equal(t, " Stable ", req.SchedulingStrategy)
	})

	t.Run("update omitted means unchanged", func(t *testing.T) {
		var req UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte("{}"), &req))
		require.Nil(t, req.SchedulingStrategy)
	})

	for _, value := range []string{"inherit", "stable", "balanced", "latency"} {
		t.Run("update accepts "+value, func(t *testing.T) {
			var req UpdateGroupRequest
			payload := []byte("{\"scheduling_strategy\":\"" + value + "\"}")
			require.NoError(t, json.Unmarshal(payload, &req))
			require.NotNil(t, req.SchedulingStrategy)
			require.Equal(t, value, *req.SchedulingStrategy)
		})
	}

	t.Run("update binding accepts mixed case with surrounding spaces", func(t *testing.T) {
		var req UpdateGroupRequest
		require.NoError(t, bindGroupPlatformJSON(t, &req, `{"scheduling_strategy":" StAbLe "}`),
			"更新入口必须与全局路径同样归一大小写/空白，而不是 binding 400")
		require.NotNil(t, req.SchedulingStrategy)
		require.Equal(t, " StAbLe ", *req.SchedulingStrategy)
	})
}
