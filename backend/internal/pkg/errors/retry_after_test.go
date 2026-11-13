//go:build unit

package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithRetryAfterCarriesPositiveSeconds(t *testing.T) {
	base := TooManyRequests("TEST_RATE_LIMITED", "slow down")
	err := base.WithRetryAfter(600)
	seconds, ok := RetryAfter(err)
	require.True(t, ok)
	require.Equal(t, 600, seconds)

	wrapped := fmt.Errorf("wrapped: %w", err)
	seconds, ok = RetryAfter(wrapped)
	require.True(t, ok)
	require.Equal(t, 600, seconds)

	// WithRetryAfter 走克隆，不得污染作为包级单例的原始错误值。
	require.Nil(t, base.Metadata)
}

func TestRetryAfterRejectsInvalidValues(t *testing.T) {
	base := TooManyRequests("TEST_RATE_LIMITED", "slow down")
	_, ok := RetryAfter(base)
	require.False(t, ok)
	_, ok = RetryAfter(base.WithRetryAfter(0))
	require.False(t, ok)
	_, ok = RetryAfter(base.WithRetryAfter(-5))
	require.False(t, ok)
	_, ok = RetryAfter(base.WithMetadata(map[string]string{MetadataKeyRetryAfter: "abc"}))
	require.False(t, ok)
	_, ok = RetryAfter(nil)
	require.False(t, ok)
	_, ok = RetryAfter(errors.New("plain"))
	require.False(t, ok)
}
