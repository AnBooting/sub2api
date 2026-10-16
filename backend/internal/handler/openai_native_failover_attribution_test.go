//go:build unit

package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// rev31-d correctly noted that asserting classifyGroupFailover alone does not bind the
// entries to it: reverting an entry to the old inline none/group/account logic left
// the suite green, and an HA-served request would be recorded as failover=group.
//
// This guard scans the OpenAI-native entries for that inline shape, so re-introducing
// it fails loudly. It is a source assertion rather than a behavioural one because the
// entries need a full Gin + billing stack to execute, and the defect is precisely
// "the entry does not call the shared classifier".
func TestOpenAINativeEntriesDelegateFailoverAttribution(t *testing.T) {
	inline := regexp.MustCompile(`(?s)kind := service\.RoutingFailoverNone\s*\n\s*if \*requested != `)
	for _, file := range []string{
		"openai_chat_completions.go",
		"openai_gateway_handler.go",
	} {
		body, err := os.ReadFile(file)
		require.NoError(t, err)
		src := string(body)

		require.False(t, inline.MatchString(src),
			"%s still classifies failover inline; an HA-served request would be recorded as failover=group", file)

		if strings.Contains(src, "RoutingUsageMetadata{") {
			require.Contains(t, src, "classifyGroupFailover",
				"%s builds RoutingUsageMetadata but never calls the shared classifier", file)
		}
	}
}
