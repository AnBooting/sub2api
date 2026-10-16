//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Regression for the OpenAI-native HA gap (PRD-04 AC-B2).
//
// HAGroupIDs() had only four consumers: the messages, chat-completions (shared),
// responses (shared) and Gemini entries. The two OpenAI-native entries
// (openai_chat_completions.go and openai_gateway_handler.go) read only
// NormalGroupIDs(), so once the ordinary chain was exhausted the request ended with
// "no available accounts" even though the user had authorized a fallback. AC-B2 says
// the fallback must be reachable when the user enabled it, so those entries now drive
// the same normalGroupPlan.
//
// These tests pin the plan-level contract the entries rely on: HA is reachable only
// after the normal chain is exhausted AND the failure class is retryable, and an
// HA-only chain never serves as if it were an ordinary group.
func TestOpenAINativeEntryPlanReachesAuthorizedHA(t *testing.T) {
	normal := []int64{10, 11}
	ha := []int64{30, 31}

	plan := newNormalGroupPlan(normal, ha)
	require.True(t, plan.active(), "a chain with a normal candidate and an HA candidate is active")

	// The entry has already admitted the plan's first candidate (index 0), so peek
	// yields the NEXT candidate: the remaining normal one, then the HA phase.
	id, isHA, ok := plan.peek(true)
	require.True(t, ok)
	require.Equal(t, int64(11), id, "the remaining normal candidate comes next")
	require.False(t, isHA)
	plan.commit()

	// A retryable failure opens the fallback.
	id, isHA, ok = plan.peek(true)
	require.True(t, ok, "the authorized HA candidate must be reachable after the normal chain is exhausted")
	require.True(t, isHA)
	require.Equal(t, int64(30), id)
	plan.commit()

	id, isHA, ok = plan.peek(true)
	require.True(t, ok)
	require.True(t, isHA)
	require.Equal(t, int64(31), id)
	plan.commit()

	_, _, ok = plan.peek(true)
	require.False(t, ok, "the chain is exhausted")

	// A non-retryable failure must NOT open the fallback: with only one normal
	// candidate left, peek must stop at the normal chain rather than cross into HA.
	nonRetryable := newNormalGroupPlan(normal, ha)
	id, isHA, ok = nonRetryable.peek(false)
	require.True(t, ok)
	require.Equal(t, int64(11), id)
	require.False(t, isHA)
	nonRetryable.commit()
	_, _, ok = nonRetryable.peek(false)
	require.False(t, ok, "a client error must never open the HA fallback")
}

// An HA-only authorized chain must not be served as an ordinary group, and the
// entries must not index normalIDs[0] blindly.
func TestOpenAINativeEntryPlanHandlesHAOnlyChain(t *testing.T) {
	plan := newNormalGroupPlan(nil, []int64{30})
	require.False(t, plan.active(), "an HA-only chain has no normal chain to exhaust")
	_, _, ok := plan.peek(true)
	require.False(t, ok, "an HA-only chain must not yield a candidate through the failover path")

	// first() is the admission helper the entries use; it must resolve the HA-only
	// chain rather than panicking on normalIDs[0].
	id, isHA, hasStart := plan.first()
	require.True(t, hasStart)
	require.True(t, isHA)
	require.Equal(t, int64(30), id)
}

// The classifier the entries use must label an HA-served request as HA, which is what
// makes the record distinguishable (PRD-04 BE-05).
func TestOpenAINativeEntryAttributesHAServedGroup(t *testing.T) {
	require.Equal(t, service.RoutingFailoverHA,
		classifyGroupFailover(10, 30, []int64{30, 31}, false))
	require.Equal(t, service.RoutingFailoverHA,
		classifyGroupFailover(10, 31, []int64{30, 31}, true))
	require.Equal(t, service.RoutingFailoverGroup,
		classifyGroupFailover(10, 20, []int64{30}, false))
}
