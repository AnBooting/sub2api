package dto

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGroupChainDTORevisionZeroAndOwnedAuthorization(t *testing.T) {
	empty := APIKeyFromService(&service.APIKey{})
	raw, err := json.Marshal(empty)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"group_chain_revision":0`)
	require.Contains(t, string(raw), `"groups":[]`)
	at := time.Now().UTC()
	key := &service.APIKey{Groups: []service.APIKeyGroupLink{{GroupID: 9, Role: "ha", EnabledByUserAt: &at}}}
	mapped := APIKeyFromService(key)
	*mapped.Groups[0].EnabledByUserAt = at.Add(time.Hour)
	require.True(t, key.Groups[0].EnabledByUserAt.Equal(at))
}

func TestUsageLogDTOC6HistoricalNullAndExplicitZero(t *testing.T) {
	raw, err := json.Marshal(UsageLogFromService(&service.UsageLog{}))
	require.NoError(t, err)
	for _, field := range []string{"requested_group_id", "served_group_id", "failover", "attempt_count"} {
		require.Contains(t, string(raw), `"`+field+`":null`)
	}
	requested, served := int64(10), int64(20)
	none := "none"
	attempts := 0
	log := &service.UsageLog{GroupID: &served, RequestedGroupID: &requested, ServedGroupID: &served, Failover: &none, AttemptCount: &attempts}
	mapped := UsageLogFromService(log)
	require.Equal(t, log.GroupID, mapped.GroupID)
	require.Equal(t, log.RequestedGroupID, mapped.RequestedGroupID)
	require.Equal(t, log.ServedGroupID, mapped.ServedGroupID)
	require.Equal(t, "none", *mapped.Failover)
	require.Zero(t, *mapped.AttemptCount)
	raw, err = json.Marshal(mapped)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"attempt_count":0`)
	require.Contains(t, string(raw), `"failover":"none"`)
	admin := UsageLogFromServiceAdmin(log)
	require.Equal(t, log.AttemptCount, admin.AttemptCount)
	require.NotContains(t, string(raw), "credentials")
	require.NotContains(t, string(raw), "account_rate_multiplier")
}
