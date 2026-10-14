//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// Cross-family freeze regressions use real PrepareRequestBilling and scheduler
// snapshots; expected values come from the actual forwarding-family resolver.

// crossFamilySnapshotCacheStub implements SchedulerCache with direct per-bucket
// account snapshots. This is the same test fixture already used by inflight
// reservation tests; it is not a fake accountMappingSnapshot callable.
type crossFamilySnapshotCacheStub struct {
	inflightSnapshotCacheStub
}

func crossFamilyGatewaySnapshot(svc *GatewayService, groupIDs map[int64]string, accounts []Account) {
	snap := &crossFamilySnapshotCacheStub{inflightSnapshotCacheStub{byBucket: map[string][]Account{}}}
	for gid, platform := range groupIDs {
		snap.set(gid, platform, accounts)
	}
	attachInflightSnapshot(svc, &snap.inflightSnapshotCacheStub)
}

func crossFamilyOpenAISnapshot(svc *OpenAIGatewayService, groupID *int64, platform string, accounts []Account) {
	snap := &crossFamilySnapshotCacheStub{inflightSnapshotCacheStub{byBucket: map[string][]Account{}}}
	snap.set(derefGroupID(groupID), platform, accounts)
	repo := &inflightCountingAccountRepo{}
	svc.accountRepo = repo
	svc.schedulerSnapshot = NewSchedulerSnapshotService(snap, nil, repo, nil, svc.cfg)
}

func crossFamilyOpenAIService(t *testing.T) *OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1000, MaxInputTokens: 200000, MaxOutputTokens: 128000}
	billing := NewBillingService(cfg, nil)
	return &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
}

// TestCrossFamilySharedGatewayPrepareOpenAIOAuthProbe freezes the OpenAI OAuth
// target through the shared GatewayService path. Expected is the model that
// OpenAIGatewayService.Forward actually resolves for this account. Known
// previous behavior: shared resolver used mappedOrRequested without
// resolveOpenAIAccountUpstreamModelForRequest, so a mapping like
// gpt-5.5 -> gpt-5.4-high freezes "gpt-5.4-high" while Forward sends/normalizes
// the *Codex* model chain and reports "gpt-5.4".
func TestCrossFamilySharedGatewayPrepareOpenAIOAuthProbe(t *testing.T) {
	var (
		groupA  = int64(100)
		groupB  = int64(200)
		account = int64(11)
	)
	model := "gpt-5.5"
	billing := NewBillingService(&config.Config{}, nil)
	cs := newTestChannelServiceWithCache(t, newEmptyChannelCache())
	svc := &GatewayService{
		cfg:            &config.Config{},
		billingService: billing,
		resolver:       NewModelPricingResolver(cs, billing),
		channelService: cs,
	}
	crossFamilyGatewaySnapshot(svc, map[int64]string{groupB: PlatformOpenAI}, []Account{{
		ID: account, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{model: "gpt-5.4-high"}},
		Extra:       map[string]any{"openai_passthrough": false, "openai_long_context_billing_enabled": true},
	}})
	key := &APIKey{
		UserID:  1,
		User:    &User{ID: 1},
		GroupID: &groupA,
		Groups: []APIKeyGroupLink{
			{GroupID: groupA, Role: APIKeyGroupRoleNormal, Position: 0, Group: &Group{ID: groupA, Platform: PlatformAnthropic, RateMultiplier: 1}},
			{GroupID: groupB, Role: APIKeyGroupRoleNormal, Position: 1, Group: &Group{ID: groupB, Platform: PlatformOpenAI, RateMultiplier: 1}},
		},
	}
	ctx, bundle, err := svc.PrepareRequestBilling(WithGatewayRequestThinkingPreference(context.Background(), true, "medium"), key, InflightEstimateRequest{Model: model, Kind: InflightEstimateToken})
	require.NoError(t, err, "prepare must succeed")
	_ = ctx
	frozen, err := bundle.UpstreamModelFor(groupB, account)
	require.NoError(t, err, "frozen mapping must contain the OpenAI target")
	expected := resolveOpenAIAccountUpstreamModelForRequest(&Account{
		ID: account, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{model: "gpt-5.4-high"}},
		Extra:       map[string]any{"openai_passthrough": false, "openai_long_context_billing_enabled": true},
	}, model, false)
	t.Logf("frozen=%q expected=%q", frozen, expected)
	require.Equal(t, expected, frozen,
		"shared freeze resolver must match actual OpenAI Forward resolver for cross-family target; got frozen=%q expected=%q", frozen, expected)
}

// TestCrossFamilyOpenAIRequestPrepareAntigravityProbe freezes the Antigravity
// OAuth target through the OpenAI service's PrepareRequestBilling. Expected is
// the actual shared Antigravity forward resolver with the request thinking
// preference set to medium (the two cases approved by Lead). It is intentionally
// not skipped for "no AG resolver" theory; it uses the actual billing lambda at
// billing_inflight_reservation.go:820-825 and demonstrates a concrete mismatch
// if one exists.
func TestCrossFamilyOpenAIRequestPrepareAntigravityProbe(t *testing.T) {
	var (
		groupA  = int64(300)
		groupB  = int64(400)
		account = int64(12)
	)
	model := "gemini-3.8-flash"
	cs := newTestChannelServiceWithCache(t, newEmptyChannelCache())
	svc := crossFamilyOpenAIService(t)
	svc.channelService = cs
	crossFamilyOpenAISnapshot(svc, &groupB, PlatformAntigravity, []Account{{
		ID: account, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{model: "gemini-3.8-flash"}},
	}})
	key := &APIKey{
		UserID:  1,
		User:    &User{ID: 1},
		GroupID: &groupA,
		Groups: []APIKeyGroupLink{
			{GroupID: groupA, Role: APIKeyGroupRoleNormal, Position: 0, Group: &Group{ID: groupA, Platform: PlatformOpenAI, RateMultiplier: 1}},
			{GroupID: groupB, Role: APIKeyGroupRoleNormal, Position: 1, Group: &Group{ID: groupB, Platform: PlatformAntigravity, RateMultiplier: 1}},
		},
	}
	ctx, bundle, err := svc.PrepareRequestBilling(WithGatewayRequestThinkingPreference(context.Background(), true, "medium"), key, InflightEstimateRequest{Model: model, Kind: InflightEstimateToken})
	require.NoError(t, err, "prepare must succeed")
	_ = ctx
	frozen, err := bundle.UpstreamModelFor(groupB, account)
	expectedCtx := WithGatewayRequestThinkingPreference(context.Background(), true, "medium")
	expected := resolveAntigravityAccountUpstreamModel(expectedCtx,
		&Account{ID: account, Platform: PlatformAntigravity, Type: AccountTypeOAuth,
			Credentials: map[string]any{"model_mapping": map[string]any{model: "gemini-3.8-flash"}}}, model)
	t.Logf("AG snapshot frozen=%q error=%v expected=%q", frozen, err, expected)
	require.NoError(t, err, "frozen mapping must contain the AG target")
	t.Logf("frozen=%q expected=%q", frozen, expected)
	require.Equal(t, expected, frozen,
		"OpenAI Prepare freezing AG target must use same AG thinking resolver as actual AG Forward; got frozen=%q expected=%q", frozen, expected)
}

func TestCrossFamilyPrepareTargetFamilies(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformOpenAI, PlatformGrok, "kimi"} {
		t.Run(platform, func(t *testing.T) {
			id := int64(501)
			account := Account{ID: 52, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target-model"}}}
			if platform == PlatformAnthropic {
				account.Extra = map[string]any{"anthropic_passthrough": true}
			}
			svc := crossFamilyOpenAIService(t)
			crossFamilyOpenAISnapshot(svc, &id, platform, []Account{account})
			group := &Group{ID: id, Platform: platform, RateMultiplier: 1}
			key := &APIKey{GroupID: &id, Group: group, User: &User{ID: 1}}
			ctx := WithGatewayRequestThinkingPreference(context.Background(), true, "medium")
			_, bundle, err := svc.PrepareRequestBilling(ctx, key, InflightEstimateRequest{Model: "alias"})
			require.NoError(t, err)
			frozen, err := bundle.UpstreamModelFor(id, account.ID)
			require.NoError(t, err)
			expected := resolveGatewayRequestAccountUpstreamModel(ctx, &account, "alias")
			require.Equal(t, expected, frozen)
			if platform == PlatformAnthropic || platform == PlatformGemini {
				require.Equal(t, "target-model", frozen)
			}
			if platform == PlatformGrok || platform == "kimi" {
				require.Equal(t, resolveOpenAIAccountUpstreamModelForRequest(&account, "alias", false), frozen)
			}
		})
	}
}
func TestCrossFamilyPrepareAntigravityMedium(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprint(shared), func(t *testing.T) {
			id := int64(601)
			account := Account{ID: 62, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-3.8-flash-medium": "gemini-3.8-flash-medium", "gemini-3.8-flash-high": "gemini-3.8-flash-high"}}}
			group := &Group{ID: id, Platform: PlatformAntigravity, RateMultiplier: 1}
			key := &APIKey{GroupID: &id, Group: group, User: &User{ID: 1}}
			ctx := WithGatewayRequestThinkingPreference(context.Background(), true, "medium")
			req := InflightEstimateRequest{Model: "gemini-3.8-flash"}
			svc := crossFamilyOpenAIService(t)
			var bundle *RequestBillingContext
			var err error
			if shared {
				gateway := &GatewayService{cfg: svc.cfg, billingService: svc.billingService, resolver: svc.resolver}
				crossFamilyGatewaySnapshot(gateway, map[int64]string{id: PlatformAntigravity}, []Account{account})
				_, bundle, err = gateway.PrepareRequestBilling(ctx, key, req)
			} else {
				crossFamilyOpenAISnapshot(svc, &id, PlatformAntigravity, []Account{account})
				_, bundle, err = svc.PrepareRequestBilling(ctx, key, req)
			}
			require.NoError(t, err)
			frozen, err := bundle.UpstreamModelFor(id, account.ID)
			require.NoError(t, err)
			require.Equal(t, "gemini-3.8-flash-medium", frozen)
		})
	}
}
func TestCrossFamilyPreparePreservesCompactAndWS(t *testing.T) {
	for _, mode := range []string{"http", "compact", "ws"} {
		t.Run(mode, func(t *testing.T) {
			id := int64(701)
			account := Account{ID: 72, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "gpt-5.4-high"}, "compact_model_mapping": map[string]any{"alias": "gpt-5.4-mini"}}}
			svc := crossFamilyOpenAIService(t)
			crossFamilyOpenAISnapshot(svc, &id, PlatformOpenAI, []Account{account})
			group := &Group{ID: id, Platform: PlatformOpenAI, RateMultiplier: 1}
			key := &APIKey{GroupID: &id, Group: group, User: &User{ID: 1}}
			req := InflightEstimateRequest{Model: "alias", RequireCompact: mode == "compact", RequireWSIngressModel: mode == "ws"}
			_, bundle, err := svc.PrepareRequestBilling(context.Background(), key, req)
			require.NoError(t, err)
			frozen, err := bundle.UpstreamModelFor(id, account.ID)
			require.NoError(t, err)
			expected := resolveOpenAIAccountUpstreamModelForRequest(&account, "alias", mode == "compact")
			if mode == "ws" {
				expected = resolveOpenAIAccountWSIngressUpstreamModel(&account, "alias", svc.cfg.Gateway.OpenAIWS.IngressModeDefault)
			}
			require.Equal(t, expected, frozen)
		})
	}
}

func TestCrossFamilyPreparePlatformBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, platform string
		compact, ws    bool
		wantAccount    bool
	}{
		{"unknown-http", "not-a-platform", false, false, false},
		{"ag-compact-stays-openai", PlatformAntigravity, true, false, false},
		{"ag-ws-stays-openai", PlatformAntigravity, false, true, false},
		{"composite-legacy-openai", PlatformComposite, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := int64(801)
			account := Account{ID: 82, Platform: tc.platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target-model"}}}
			bucket := tc.platform
			if tc.platform == PlatformComposite {
				bucket = PlatformOpenAI
				account.Platform = PlatformOpenAI
			}
			svc := crossFamilyOpenAIService(t)
			crossFamilyOpenAISnapshot(svc, &id, bucket, []Account{account})
			group := &Group{ID: id, Platform: tc.platform, RateMultiplier: 1}
			key := &APIKey{GroupID: &id, Group: group, User: &User{ID: 1}}
			req := InflightEstimateRequest{Model: "alias", RequireCompact: tc.compact, RequireWSIngressModel: tc.ws}
			if tc.platform == "not-a-platform" {
				accounts, snapshotErr := svc.inflightEstimateDeps(req).accountMappingSnapshot(context.Background(), key, "alias")
				require.Empty(t, accounts)
				require.ErrorIs(t, snapshotErr, ErrModelPricingUnavailable)
				return
			}
			_, bundle, err := svc.PrepareRequestBilling(context.Background(), key, req)
			require.NoError(t, err)
			_, err = bundle.UpstreamModelFor(id, account.ID)
			if tc.wantAccount {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
