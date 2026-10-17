//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIRequestAccountSnapshotUsesActualProtocolModels(t *testing.T) {
	for _, test := range []struct {
		name, kind, mapped, want string
		pass                     bool
	}{
		{name: "passthrough_identity", kind: AccountTypeAPIKey, mapped: "gpt-5.4", want: "gpt-5.5", pass: true},
		{name: "ordinary_apikey_mapping", kind: AccountTypeAPIKey, mapped: "gpt-5.4", want: "gpt-5.4"},
		{name: "ordinary_oauth_normalization", kind: AccountTypeOAuth, mapped: "gpt-5.4-high", want: "gpt-5.4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := Account{ID: 11, Platform: PlatformOpenAI, Type: test.kind, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.5": test.mapped}}, Extra: map[string]any{"openai_passthrough": test.pass, "openai_long_context_billing_enabled": true}}
			list := func(context.Context, *int64, string, bool) ([]Account, error) { return []Account{account}, nil }
			platform := func(context.Context, *APIKey, string) (string, bool, bool) { return PlatformOpenAI, false, true }
			actual := requestAccountMappingsFromSnapshot(list, platform, func(_ context.Context, account *Account, model string) string {
				return resolveOpenAIAccountUpstreamModelForRequest(account, model, false)
			})
			result, err := actual(context.Background(), &APIKey{}, "gpt-5.5")
			require.NoError(t, err)
			require.Equal(t, test.want, result[11].Model)
			require.True(t, result[11].LongContextBillingEnabled)
			generic, err := requestAccountMappingsFromSnapshot(list, platform)(context.Background(), &APIKey{}, "gpt-5.5")
			require.NoError(t, err)
			require.Equal(t, test.mapped, generic[11].Model, "generic Gateway mapping semantics remain unchanged")
		})
	}
}

func TestGatewayRequestPricingTransferKeepsBundleAndWorkerDeadline(t *testing.T) {
	billing := newTestBillingService()
	one, zero := 1e-6, 0.0
	group := &Group{ID: 1, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"client-model"}, InputPrice: &one, OutputPrice: &one, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
	deps := inflightEstimateDeps{billing: billing, accountMappingSnapshot: func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
		return map[int64]requestAccountMapping{11: {Model: "client-model"}}, nil
	}}
	parent, cancel := context.WithCancel(context.Background())
	ctx, request, err := deps.prepareRequestBilling(parent, &APIKey{GroupID: requestPricingTestInt64(1), Group: group}, InflightEstimateRequest{Model: "client-model"})
	require.NoError(t, err)
	unbound := TransferGatewayRequestPricingContext(context.Background(), context.WithoutCancel(ctx))
	_, _, err = frozenGatewayRequestAccountModel(unbound, 11)
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "transfer cannot fabricate an execution account")
	_, err = WithGatewayRequestBillingAccount(ctx, 1, 0)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	_, err = WithGatewayRequestBillingAccount(ctx, 1, 99)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	ctx, err = WithGatewayRequestBillingAccount(ctx, 1, 11)
	require.NoError(t, err)
	cancel()
	worker, stop := context.WithTimeout(context.Background(), time.Minute)
	defer stop()
	detached := TransferGatewayRequestPricingContext(worker, context.WithoutCancel(ctx))
	require.NoError(t, detached.Err())
	require.Same(t, request, GatewayRequestBillingFromContext(detached))
	before, _ := worker.Deadline()
	after, _ := detached.Deadline()
	require.Equal(t, before, after)
	facts, ok := gatewayRequestBillingAccountFromContext(detached)
	require.True(t, ok)
	require.Equal(t, int64(11), facts.accountID)
	model, locked, err := frozenGatewayRequestAccountModel(detached, 11)
	require.NoError(t, err)
	require.True(t, locked)
	require.Equal(t, "client-model", model)
	_, _, err = frozenGatewayRequestAccountModel(detached, 12)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	openAI := &OpenAIGatewayService{}
	priced, at := openAI.WithOpenAIRequestPricingContext(detached, requestPricingTestInt64(1))
	require.Equal(t, request.StartedAt(), at)
	require.Same(t, request, GatewayRequestBillingFromContext(priced))
}
