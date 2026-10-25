package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestArtexGuardModeLoading(t *testing.T) {
	for _, mode := range []string{"", ArtexGuardOff, ArtexGuardMonitor, ArtexGuardEnforce, "OFF", " monitor ", "audit", "disabled"} {
		t.Run("mode_"+mode, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			if mode != "" {
				t.Setenv("GATEWAY_ARTEX_GUARD_MODE", mode)
			} else {
				t.Setenv("GATEWAY_ARTEX_GUARD_MODE", "")
			}
			cfg, err := Load()
			switch mode {
			case "", ArtexGuardOff, ArtexGuardMonitor, ArtexGuardEnforce:
				require.NoError(t, err)
				expected := mode
				if expected == "" {
					expected = ArtexGuardEnforce
				}
				require.Equal(t, expected, ResolveArtexGuardMode(cfg))
			default:
				require.ErrorContains(t, err, "gateway.artex_guard.mode")
			}
		})
	}
	require.Equal(t, ArtexGuardEnforce, ResolveArtexGuardMode(nil))
	require.Equal(t, ArtexGuardEnforce, ResolveArtexGuardMode(&Config{}))
	require.NoError(t, (ArtexGuardConfig{}).Validate())
	require.Error(t, (ArtexGuardConfig{Mode: "invalid"}).Validate())
	require.Equal(t, ArtexGuardEnforce, (ArtexGuardConfig{Mode: "OFF"}).EffectiveMode())
	require.Equal(t, ArtexGuardEnforce, (ArtexGuardConfig{Mode: "audit"}).EffectiveMode())
}
