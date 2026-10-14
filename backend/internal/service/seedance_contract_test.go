//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Ark official contract: the task query response carries top-level duration in
// integer seconds, or top-level frames (never both). duration = frames / 24,
// floored. The create response has no duration at all.
func TestSeedanceDurationSecondsFromStatusBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want int
	}{
		{"top level duration wins", `{"status":"succeeded","duration":8}`, 8},
		{"duration 12", `{"status":"succeeded","duration":12}`, 12},
		{"frames converted by 24 floored", `{"status":"succeeded","frames":120}`, 5},
		{"frames floors partial second", `{"status":"succeeded","frames":119}`, 4},
		{"frames less than one second floors to zero", `{"status":"succeeded","frames":23}`, 0},
		{"create sentinel duration=-1 is not a duration", `{"status":"succeeded","duration":-1}`, 0},
		{"duration takes precedence over frames", `{"status":"succeeded","duration":7,"frames":240}`, 7},
		{"no duration and no frames stays unknown", `{"status":"succeeded"}`, 0},
		{"empty body", ``, 0},
		{"invalid json", `{`, 0},
		{"non numeric duration", `{"status":"succeeded","duration":"8"}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, SeedanceDurationSecondsFromStatusBody([]byte(tc.body)))
		})
	}
}

// Real field path: content.video_url marks completion, usage.completion_tokens is
// the token reconciliation figure, and top-level duration is the billed duration.
func TestSeedanceStatusReadsRealCompletionFields(t *testing.T) {
	body := `{"id":"task-1","status":"succeeded","model":"ep-seedance","content":{"video_url":"https://cdn.example/video.mp4"},"usage":{"completion_tokens":12345},"duration":8}`
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/task-1", nil)

	result, err := svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointStatus, "seedance:task-1", nil)
	require.NoError(t, err)
	require.Equal(t, 12345, result.Usage.OutputTokens, "usage.completion_tokens must reach billing")
	require.Equal(t, 8, result.VideoDurationSeconds, "top-level duration must reach billing")
	require.Equal(t, "seedance:task-1", result.ResponseID)
	require.Equal(t, "ep-seedance", result.UpstreamModel)
}

// frames-only responses (the other documented shape) convert to seconds.
func TestSeedanceStatusDerivesDurationFromFrames(t *testing.T) {
	body := `{"id":"task-1","status":"succeeded","model":"ep-seedance","content":{"video_url":"https://cdn.example/video.mp4"},"usage":{"completion_tokens":600},"frames":120}`
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/task-1", nil)

	result, err := svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointStatus, "seedance:task-1", nil)
	require.NoError(t, err)
	require.Equal(t, 5, result.VideoDurationSeconds)
	require.Equal(t, 600, result.Usage.OutputTokens)
}

// When the query exposes neither duration nor frames (e.g. the create request
// used duration=-1) the result must stay 0 so the caller keeps its
// request-parameter fallback instead of billing a fabricated duration.
func TestSeedanceStatusWithoutDurationLeavesZeroForFallback(t *testing.T) {
	body := `{"id":"task-1","status":"succeeded","model":"ep-seedance","content":{"video_url":"https://cdn.example/video.mp4"},"usage":{"completion_tokens":321},"duration":-1}`
	upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/task-1", nil)

	result, err := svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointStatus, "seedance:task-1", nil)
	require.NoError(t, err)
	require.Zero(t, result.VideoDurationSeconds, "duration=-1 must not be billed as a duration")
	require.Equal(t, 321, result.Usage.OutputTokens)
	require.Zero(t, result.VideoCount, "Seedance must stay on token billing, not Grok seconds")
}

// Non-success statuses must not surface billable tokens or duration.
func TestSeedanceStatusNonSuccessCarriesNoBillableUnits(t *testing.T) {
	for _, status := range []string{"queued", "running", "failed", "cancelled", "expired"} {
		body := `{"id":"task-1","status":"` + status + `","model":"ep-seedance","content":{"video_url":"https://cdn.example/video.mp4"},"usage":{"completion_tokens":12345},"duration":8}`
		upstream := &grokMediaContentUpstreamStub{response: grokMediaContentStatusResponse(body)}
		svc := &OpenAIGatewayService{httpUpstream: upstream}
		c, _ := grokMediaContentTestContext(http.MethodGet, "/api/v3/contents/generations/tasks/task-1", nil)

		result, err := svc.ForwardSeedance(context.Background(), c, seedanceTestAccount(), SeedanceEndpointStatus, "seedance:task-1", nil)
		require.NoError(t, err)
		require.Zero(t, result.Usage.OutputTokens, "status=%s must not bill tokens", status)
		require.Zero(t, result.VideoDurationSeconds, "status=%s must not bill duration", status)
	}
}
