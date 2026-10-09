package service

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type controlsPricingClient struct {
	calls   atomic.Int32
	started chan context.Context
	release chan struct{}
}

func (c *controlsPricingClient) FetchPricingJSON(ctx context.Context, _ string) ([]byte, error) {
	c.calls.Add(1)
	if c.started != nil {
		c.started <- ctx
		<-c.release
	}
	return []byte(hotReloadCatalogJSON), nil
}
func (c *controlsPricingClient) FetchHashText(context.Context, string) (string, error) {
	c.calls.Add(1)
	return "changed", nil
}

func TestPricingPullDisabledOfflineStartupAndManual(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "cold", true: "cached"}[cached], func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Pricing.DataDir = t.TempDir()
			cfg.Pricing.RemoteURL = "https://example.com/prices.json"
			cfg.Pricing.HashURL = "https://example.com/hash"
			cfg.Pricing.FallbackFile = filepath.Join(cfg.Pricing.DataDir, "fallback.json")
			require.NoError(t, os.WriteFile(cfg.Pricing.FallbackFile, []byte(hotReloadCatalogJSON), 0600))
			client := &controlsPricingClient{}
			svc := NewPricingService(cfg, client)
			require.NoError(t, svc.UpdateControls(context.Background(), PricingControls{RemoteEnabled: false}))
			if cached {
				require.NoError(t, os.WriteFile(svc.getPricingFilePath(), []byte(hotReloadCatalogJSON), 0600))
			}
			require.NoError(t, svc.checkAndUpdatePricing())
			require.NoError(t, svc.syncWithRemote())
			require.ErrorIs(t, svc.ForceUpdate(), ErrPricingPullDisabled)
			_, err := svc.fetchRemoteHash()
			require.ErrorIs(t, err, ErrPricingPullDisabled)
			require.Zero(t, client.calls.Load())
			require.NotNil(t, svc.GetModelPricing("remote-model"))
		})
	}
}

func TestPricingDisableCancelsAndRejectsStaleDownload(t *testing.T) {
	cfg := &config.Config{}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Pricing.RemoteURL = "https://example.com/prices.json"
	client := &controlsPricingClient{started: make(chan context.Context, 1), release: make(chan struct{})}
	svc := NewPricingService(cfg, client)
	done := make(chan error, 1)
	go func() { done <- svc.downloadPricingData() }()
	ctx := <-client.started
	require.NoError(t, svc.UpdateControls(context.Background(), PricingControls{RemoteEnabled: false}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	close(client.release)
	require.ErrorIs(t, <-done, ErrPricingPullDisabled)
	_, err := os.Stat(svc.getPricingFilePath())
	require.True(t, os.IsNotExist(err))
	require.Empty(t, svc.pricingData)
}
