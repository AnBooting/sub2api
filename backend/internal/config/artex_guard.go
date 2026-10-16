package config

import "fmt"

const (
	ArtexGuardOff     = "off"
	ArtexGuardMonitor = "monitor"
	ArtexGuardEnforce = "enforce"
)

// ArtexGuardConfig applies only the version-bound ARTEX core signatures.
// Empty zero-value configs retain the loading default; explicit invalid modes
// are errors, not silent off/monitor fallbacks. Changes take effect on restart.
type ArtexGuardConfig struct {
	Mode string `mapstructure:"mode"`
}

func (c ArtexGuardConfig) EffectiveMode() string {
	if c.Mode == "" {
		return ArtexGuardEnforce
	}
	return c.Mode
}
func (c ArtexGuardConfig) Validate() error {
	switch c.Mode {
	case "", ArtexGuardOff, ArtexGuardMonitor, ArtexGuardEnforce:
		return nil
	default:
		return fmt.Errorf("gateway.artex_guard.mode must be off, monitor or enforce")
	}
}
func ResolveArtexGuardMode(c *Config) string {
	if c == nil {
		return ArtexGuardEnforce
	}
	return c.Gateway.ArtexGuard.EffectiveMode()
}
