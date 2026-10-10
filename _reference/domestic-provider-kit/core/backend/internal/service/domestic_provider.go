package service

import (
	"fmt"
	"strings"
)

const (
	DomesticProviderDeepSeek = "deepseek"
	DomesticProviderQwen     = "qwen"
	DomesticProviderDoubao   = "doubao"
	DomesticProviderKimi     = "kimi"
	DomesticProviderZhipu    = "zhipu"
	DomesticProviderMiniMax  = "minimax"
	DomesticProviderBaichuan = "baichuan"
	DomesticProviderYi       = "yi"
	DomesticProviderStepFun  = "stepfun"
	DomesticProviderCustom   = "custom"
)

var supportedDomesticProviders = map[string]struct{}{
	DomesticProviderDeepSeek: {},
	DomesticProviderQwen:     {},
	DomesticProviderDoubao:   {},
	DomesticProviderKimi:     {},
	DomesticProviderZhipu:    {},
	DomesticProviderMiniMax:  {},
	DomesticProviderBaichuan: {},
	DomesticProviderYi:       {},
	DomesticProviderStepFun:  {},
	DomesticProviderCustom:   {},
}

func isDomesticProviderPlatform(platform string) bool {
	return platform == PlatformOpenAI || platform == PlatformAnthropic
}

// NormalizeDomesticProvider validates the display classification for both
// OpenAI-compatible and Anthropic-compatible domestic accounts.
func NormalizeDomesticProvider(platform, provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "", nil
	}
	if !isDomesticProviderPlatform(platform) {
		return "", fmt.Errorf("domestic_provider requires openai or anthropic platform")
	}
	if _, ok := supportedDomesticProviders[provider]; !ok {
		return "", fmt.Errorf("unsupported domestic_provider %q", provider)
	}
	return provider, nil
}

func isDomesticProviderCompatible(target, source string) bool {
	return strings.EqualFold(strings.TrimSpace(target), strings.TrimSpace(source))
}
