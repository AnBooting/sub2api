package usagestats

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardPartialJSONPreservesUnavailableAcrossCacheRoundtrip(t *testing.T) {
	var stats DashboardStats
	require.NoError(t, json.Unmarshal([]byte(`{"total_users":3,"total_actual_cost":999,"today_actual_cost":5,"usage_availability":{"total":false,"today":true,"hourly":false,"performance":false}}`), &stats))
	for i := 0; i < 2; i++ {
		data, err := json.Marshal(&stats)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal(data, &got))
		require.Nil(t, got["total_actual_cost"])
		require.Nil(t, got["rpm"])
		require.Equal(t, float64(0), got["active_users"], "active users follow today's availability, not hourly")
		require.Equal(t, float64(5), got["today_actual_cost"])
		require.Equal(t, float64(3), got["total_users"])
		require.NoError(t, json.Unmarshal(data, &stats))
	}
}
