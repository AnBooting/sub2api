//go:build unit

package handler

import (
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
)

type partialMediaHTTP struct {
	service.HTTPUpstream
	calls  int
	mutate func()
	oauth  bool
}

func (u *partialMediaHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	if u.mutate != nil {
		u.mutate()
	}
	payload := `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"YQ=="}},{"inlineData":{"mimeType":"image/png","data":"Yg=="}}]}}]}`
	if u.oauth {
		payload = `{"response":` + payload + "}"
	}
	payload = "data: " + payload + "\n\n"
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &nativePartialReader{Reader: strings.NewReader(payload + payload), err: io.ErrUnexpectedEOF}}, nil
}
func TestGeminiPartialMediaAggregateBillsObservedImages(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprint(native, stream), func(t *testing.T) {
				f, _ := geminiImageFixture(t)
				u := &partialMediaHTTP{oauth: !stream}
				if !stream {
					f.account.Type = service.AccountTypeOAuth
					f.account.Credentials = map[string]any{"access_token": "fixture", "project_id": "fixture-project"}
				}
				u.mutate = func() { *f.group.ModelPricing[0].PerRequestPrice = 99; f.group.ImageRateMultiplier = 99 }
				f.handler.geminiCompatService = service.NewGeminiMessagesCompatService(&ccGeminiAccountRepo{}, nil, nil, nil, &service.GeminiTokenProvider{}, nil, u, nil, f.handler.cfg)
				rec := serveGeminiImage(t, f, native, stream, "2K")
				if stream {
					require.Equal(t, 200, rec.Code)
				} else {
					require.Equal(t, 502, rec.Code)
				}
				require.Equal(t, 1, u.calls)
				require.Len(t, f.ledger.commands, 1)
				require.InDelta(t, 1.5, f.ledger.commands[0].BalanceCost, 1e-12)
				require.Len(t, f.logs.rows, 1)
				require.Equal(t, 2, f.logs.rows[0].ImageCount)
				assertResponsesRouting(t, f.logs.rows[0], f.group.ID, f.group.ID, service.RoutingFailoverNone, 1)
			})
		}
	}
}
