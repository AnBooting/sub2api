//go:build unit

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The plan keeps normal-chain semantics and gates HA behind a retryable failure.
func TestNormalGroupPlanGatesHABehindRetryableFailure(t *testing.T) {
	p := newNormalGroupPlan([]int64{10, 20}, []int64{30})
	require.True(t, p.active())
	require.Equal(t, 0, p.index)

	// Normal -> normal advancement does not need a retryable class.
	id, isHA, ok := p.peek(false)
	require.True(t, ok)
	require.False(t, isHA)
	require.Equal(t, int64(20), id)
	require.Equal(t, 0, p.index, "peek must not consume the candidate")
	p.commit()
	require.Equal(t, 1, p.index)

	// Normal -> HA requires a retryable class and stays unconsumed when refused.
	_, _, ok = p.peek(false)
	require.False(t, ok, "non-retryable failure must not enter the HA phase")
	require.Equal(t, 1, p.index)
	id, isHA, ok = p.peek(true)
	require.True(t, ok)
	require.True(t, isHA)
	require.Equal(t, int64(30), id)
	p.commit()
	require.Equal(t, 2, p.index)

	// Exhausted plan reports no further candidate.
	_, _, ok = p.peek(true)
	require.False(t, ok)
}

// PRD-04 AC-B4: HA must not pre-empt a normal group that can still serve.
func TestNormalGroupPlanPrefersNormalChain(t *testing.T) {
	p := newNormalGroupPlan([]int64{10, 20, 40}, []int64{30})
	for _, want := range []int64{20, 40} {
		id, isHA, ok := p.peek(true)
		require.True(t, ok)
		require.False(t, isHA, "HA must only be reached after every normal candidate")
		require.Equal(t, want, id)
		p.commit()
	}
	id, isHA, ok := p.peek(true)
	require.True(t, ok)
	require.True(t, isHA)
	require.Equal(t, int64(30), id)
}

// Active-chain boundaries. PRD-04 orders HA strictly after the normal chain, so a
// plan needs a real chain to be active: a lone normal candidate or a lone HA
// candidate is not one.
func TestNormalGroupPlanActiveBoundaries(t *testing.T) {
	require.False(t, newNormalGroupPlan(nil, []int64{30}).active())
	require.False(t, newNormalGroupPlan([]int64{10}, nil).active())
	require.True(t, newNormalGroupPlan([]int64{10}, []int64{30}).active())
	require.True(t, newNormalGroupPlan([]int64{10, 20}, nil).active())
}

// Honest boundary: a Key configured with ONLY an HA link has no normal chain to
// exhaust. The plan therefore never activates and the request fails closed rather
// than serving HA as if it were an ordinary group. This is deliberate; making a
// lone HA link serve is a product decision not specified by PRD-04.
func TestNormalGroupPlanHAOnlyFailsClosed(t *testing.T) {
	p := newNormalGroupPlan(nil, []int64{30})
	require.False(t, p.active())
	_, _, ok := p.peek(true)
	require.False(t, ok, "an HA-only plan must not become a servable chain")

	// Two or more HA links and no normal link is still HA-only. It must stay inactive
	// so callers that start from the plan's first candidate never index an empty
	// normal chain.
	multi := newNormalGroupPlan(nil, []int64{30, 31})
	require.False(t, multi.active(), "an HA-only plan stays inactive regardless of link count")
	_, _, ok = multi.peek(true)
	require.False(t, ok)

	// first() is the safe starting accessor: it never indexes an empty normal chain.
	id, isHA, ok := newNormalGroupPlan(nil, []int64{30, 31}).first()
	require.True(t, ok)
	require.Equal(t, int64(30), id)
	require.True(t, isHA)

	id, isHA, ok = newNormalGroupPlan([]int64{10}, []int64{30}).first()
	require.True(t, ok)
	require.Equal(t, int64(10), id)
	require.False(t, isHA, "the normal chain always starts first")
}
