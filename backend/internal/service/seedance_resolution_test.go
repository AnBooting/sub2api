//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// PRD-06/K-4 B-2: the query response carries the billed resolution officially, and
// billing is tiered by it. A missing tier must surface as "" so callers fail closed
// instead of silently pricing a 1080p job at the 480p rate.
func TestSeedanceResolutionFromStatusBody(t *testing.T) {
	require.Equal(t, "1080p", SeedanceResolutionFromStatusBody([]byte(`{"resolution":"1080p"}`)))
	require.Equal(t, "720p", SeedanceResolutionFromStatusBody([]byte(`{"resolution":"720P"}`)))
	require.Equal(t, "480p", SeedanceResolutionFromStatusBody([]byte(`{"resolution":"480"}`)))
	require.Equal(t, "720p", SeedanceResolutionFromStatusBody([]byte(`{"content":{"resolution":"720p"}}`)))
	require.Equal(t, "720p", SeedanceResolutionFromStatusBody([]byte(`{"output":{"resolution":"720p"}}`)))

	// Absent / unusable must stay empty: never coerce to the cheapest tier.
	require.Equal(t, "", SeedanceResolutionFromStatusBody(nil))
	require.Equal(t, "", SeedanceResolutionFromStatusBody([]byte(`{}`)))
	// 4k is a real Ark Seedance tier (PRD 22:54 lists a 4k rate), so it must be
	// recognised rather than treated as unknown.
	require.Equal(t, "4k", SeedanceResolutionFromStatusBody([]byte(`{"resolution":"4k"}`)))
	require.Equal(t, "4k", SeedanceResolutionFromStatusBody([]byte(`{"resolution":"2160p"}`)))
	// A non-string or genuinely unknown tier stays empty.
	require.Equal(t, "", SeedanceResolutionFromStatusBody([]byte(`{"resolution":720}`)))
	require.Equal(t, "", SeedanceResolutionFromStatusBody([]byte(`{"resolution":"1080i"}`)))
	require.Equal(t, "", SeedanceResolutionFromStatusBody([]byte("not json")))
}

// The create response does not echo the resolution, so the request body is the
// create-time source that must be snapshotted for later billing.
func TestSeedanceResolutionFromRequest(t *testing.T) {
	require.Equal(t, "1080p", SeedanceResolutionFromRequest([]byte(`{"model":"doubao-seedance-2-0-260128","resolution":"1080p"}`)))
	require.Equal(t, "720p", SeedanceResolutionFromRequest([]byte(`{"resolution":"720p"}`)))
	require.Equal(t, "", SeedanceResolutionFromRequest([]byte(`{"model":"doubao-seedance-2-0-260128"}`)))
	require.Equal(t, "", SeedanceResolutionFromRequest(nil))
}

// PRD-06/K-4 B-3: the create request is the only create-time duration source, and
// the -1 sentinel means "let the model decide" — it must not become a billed value.
func TestSeedanceDurationSecondsFromRequest(t *testing.T) {
	require.Equal(t, 5, SeedanceDurationSecondsFromRequest([]byte(`{"duration":5}`)))
	require.Equal(t, 5, SeedanceDurationSecondsFromRequest([]byte(`{"frames":120}`)))
	require.Equal(t, 4, SeedanceDurationSecondsFromRequest([]byte(`{"frames":119}`)))

	// duration wins over frames, matching the documented precedence.
	require.Equal(t, 9, SeedanceDurationSecondsFromRequest([]byte(`{"duration":9,"frames":240}`)))

	// -1 sentinel and unusable input mean "unknown", never a guessed duration.
	require.Equal(t, 0, SeedanceDurationSecondsFromRequest([]byte(`{"duration":-1}`)))
	require.Equal(t, 0, SeedanceDurationSecondsFromRequest([]byte(`{}`)))
	require.Equal(t, 0, SeedanceDurationSecondsFromRequest(nil))
	require.Equal(t, 0, SeedanceDurationSecondsFromRequest([]byte("not json")))
}
