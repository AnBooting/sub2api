package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSTurnPricingFreezesCalendarPerTurn(t *testing.T) {
	oldRules, newRules := &service.RequestPricingSnapshot{}, &service.RequestPricingSnapshot{}
	p := openAIWSTurnPricing{rules: oldRules}
	require.Same(t, oldRules, p.currentSnapshot(), "first frame uses admission rules")
	p.freeze(time.Now(), oldRules)
	parent := service.WithRequestPricingSnapshot(context.Background(), p.currentSnapshot())
	var recorded *service.RequestPricingSnapshot
	queued, _ := wrapUsageRecordTaskContext(parent, func(ctx context.Context) { recorded = service.RequestPricingSnapshotFromContext(ctx) })
	p.freeze(time.Now(), newRules)
	queued(context.Background())
	require.Same(t, oldRules, recorded, "queued old-turn billing must not see next-turn rules")
	require.Same(t, newRules, p.currentSnapshot())
}

func TestOpenAIWSTurnPricingCurrentOr(t *testing.T) {
	fallback := time.Date(2024, time.January, 2, 2, 0, 0, 0, time.UTC)

	t.Run("frozen time takes precedence", func(t *testing.T) {
		frozen := fallback.Add(time.Minute)
		var p openAIWSTurnPricing
		p.freeze(frozen)
		require.Equal(t, frozen, p.currentOr(fallback))
	})

	t.Run("zero value falls back to turn start", func(t *testing.T) {
		var p openAIWSTurnPricing
		require.Equal(t, fallback, p.currentOr(fallback))
	})
}

// TestOpenAIWSTurnPricingFreezePerTurn 钉死每个 turn 的 BeforeTurn 都会覆盖
// 上一个 turn 的定价时刻：长连接跨峰谷时后续 turn 不得沿用旧时刻。
func TestOpenAIWSTurnPricingFreezePerTurn(t *testing.T) {
	var p openAIWSTurnPricing
	turn1 := time.Now().Add(-time.Hour)
	turn2 := time.Now()

	p.freeze(turn1)
	require.Equal(t, turn1, p.currentOr(time.Time{}))

	p.freeze(turn2)
	require.Equal(t, turn2, p.currentOr(time.Time{}), "后续 turn 必须使用自己的定价时刻")
}
