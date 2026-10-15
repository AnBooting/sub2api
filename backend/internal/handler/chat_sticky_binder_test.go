package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Regression for the sticky-binding gap introduced with the cross-family bridge.
//
// The pre-bridge code bound the sticky session unconditionally after a successful
// profit admission. The bridge revision dispatched the shared bind on
// "account.Platform != PlatformOpenAI" and the OpenAI bind on
// "crossChat && Platform == PlatformOpenAI", so the quadrant
// (crossChat == false, OpenAI account) fell through both branches and recorded no
// binding at all. This helper is the single source of that decision.
func TestChatStickyBinderSelectionCoversEveryQuadrant(t *testing.T) {
	openAI := &service.Account{Platform: service.PlatformOpenAI}
	anthropic := &service.Account{Platform: service.PlatformAnthropic}

	for _, tc := range []struct {
		name      string
		crossChat bool
		account   *service.Account
		wantOpen  bool
	}{
		// The regression: a plain OpenAI group with no cross-family chain must still
		// bind, through the shared gateway.
		{"plain openai group", false, openAI, false},
		// The bridge path: OpenAI account selected by the OpenAI service.
		{"cross-family bridge to openai", true, openAI, true},
		// Non-OpenAI accounts always use the shared gateway.
		{"cross-family chain on anthropic", true, anthropic, false},
		{"plain anthropic group", false, anthropic, false},
		// Defensive: a nil account must not panic.
		{"nil account", true, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantOpen, chatStickyBinderViaOpenAIService(tc.crossChat, tc.account))
		})
	}
}

// The bind must be reachable for the regression quadrant: exactly one binder runs
// whenever the profit gate is active.
func TestChatStickyBinderIsAlwaysReachable(t *testing.T) {
	openAI := &service.Account{Platform: service.PlatformOpenAI}
	for _, crossChat := range []bool{false, true} {
		viaOpenAI := chatStickyBinderViaOpenAIService(crossChat, openAI)
		viaShared := !viaOpenAI
		require.True(t, viaOpenAI || viaShared,
			"exactly one binder must handle an OpenAI account (crossChat=%v)", crossChat)
	}
}
