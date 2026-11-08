//go:build unit

package service

import (
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

// Run the existing lifecycle test unchanged, observing its requested settlement
// delays without actually sleeping for seven minutes. The original test must
// replace the new settlement retry clock just as it replaces the observer clock.
func TestReviewExistingLifecycleCaseMustNotUseProductionSettlementBackoff(t *testing.T) {
    original := liveSettlementRetryInterval
    var requested []time.Duration
    liveSettlementRetryInterval = func(attempt int) time.Duration {
        requested = append(requested, original(attempt))
        return 0
    }
    t.Cleanup(func() { liveSettlementRetryInterval = original })
    TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize(t)
    var total time.Duration
    for _, wait := range requested {
        total += wait
    }
    t.Logf("existing lifecycle test requests production delays %v (total %s)", requested, total)
    require.Empty(t, requested, "existing unit test must replace the production settlement backoff; otherwise it spends 455 seconds inside one case")
}
