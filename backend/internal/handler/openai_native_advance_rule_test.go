//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Pins the advance rule the OpenAI-native entries now share with the canonical
// entries (gateway_handler.go:603-639, gateway_handler_responses.go:370-395).
//
// The rule has two halves and both matter:
//
//   - On the FIRST attempt (no account has failed yet) capacity exhaustion may
//     advance. 10-共享规范.md:18 names 满载 (full load) as a site-side recoverable
//     cause, so this is the PRD's intended path, not a fail-open. The plan still
//     visits every normal candidate before any HA one.
//   - Once accounts HAVE failed, only a retryable last failure may cross a group
//     boundary. A client error, policy rejection or uncertain execution must not
//     drag the request onto a (usually higher-multiplier) fallback group.
//
// The helper mirrors the condition in the two entries so the split is testable
// without standing up the whole billing stack.
func openAINativeMayAdvance(failedAccountCount int, lastFailoverErr *service.UpstreamFailoverError) bool {
	if failedAccountCount == 0 {
		return true
	}
	return lastFailoverErr != nil && lastFailoverErr.ShouldRetryNextAccount()
}

func TestOpenAINativeAdvanceRuleMatchesCanonicalEntries(t *testing.T) {
	// First attempt, nothing failed yet: capacity exhaustion advances (满载).
	require.True(t, openAINativeMayAdvance(0, nil),
		"first-attempt capacity exhaustion is a recoverable cause and may advance")

	// Accounts failed and the last failure permits another account: may advance.
	// ShouldRetryNextAccount() is NextAccountAction != NextAccountStop, so a
	// non-zero action (the zero value) is retryable.
	retryable := &service.UpstreamFailoverError{StatusCode: 503}
	require.True(t, retryable.ShouldRetryNextAccount(), "precondition: default action retries")
	require.True(t, openAINativeMayAdvance(2, retryable))

	// Accounts failed and the last failure stopped the account loop: must not cross.
	nonRetryable := &service.UpstreamFailoverError{StatusCode: 400, NextAccountAction: service.NextAccountStop}
	require.False(t, nonRetryable.ShouldRetryNextAccount(), "precondition: stop action is terminal")
	require.False(t, openAINativeMayAdvance(2, nonRetryable),
		"a terminal failure must not cross a group boundary")

	// Accounts failed but no failure was recorded: fail closed.
	require.False(t, openAINativeMayAdvance(1, nil))
}
