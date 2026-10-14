package service

func imagePriceConfigFromAPIKey(apiKey *APIKey) *ImagePriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return &ImagePriceConfig{
		Price1K: apiKey.Group.ImagePrice1K,
		Price2K: apiKey.Group.ImagePrice2K,
		Price4K: apiKey.Group.ImagePrice4K,
	}
}

func apiKeyHasConfiguredImagePrice(apiKey *APIKey, imageSize string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetImagePrice(imageSize) != nil
}

func videoPriceConfigFromAPIKey(apiKey *APIKey) *VideoPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return &VideoPriceConfig{
		Price480P:   apiKey.Group.VideoPrice480P,
		Price720P:   apiKey.Group.VideoPrice720P,
		Price1080P:  apiKey.Group.VideoPrice1080P,
		ModelPrices: apiKey.Group.VideoModelPrices,
	}
}

func apiKeyHasConfiguredVideoPrice(apiKey *APIKey, model, resolution string) bool {
	return apiKey != nil && apiKey.Group != nil && apiKey.Group.GetVideoPriceForModel(model, resolution) != nil
}

// GroupHasSeedanceVideoPrice reports whether the group carries an operator-set
// Seedance price. PRD-06/K-4 sells Seedance per request or per second; there is no
// bundled default and no price source is guessed, so callers must fail closed when
// this returns false instead of charging another model's tariff.
func GroupHasSeedanceVideoPrice(group *Group) bool {
	if group == nil {
		return false
	}
	// A per-model slot counts if ANY tier is priced; checking a single default tier
	// would miss a group that only configured e.g. 720p.
	prices := NormalizeVideoModelPrices(group.VideoModelPrices)
	for _, tier := range []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P} {
		if LookupVideoModelPrice(prices, VideoPriceFamilySeedance, tier) != nil {
			return true
		}
	}
	return group.VideoPrice480P != nil || group.VideoPrice720P != nil || group.VideoPrice1080P != nil
}

func webSearchPricePerCallFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.WebSearchPricePerCall
}

func groupSearchPricePer1kFromAPIKey(apiKey *APIKey) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	return apiKey.Group.GetSearchPricePer1k()
}

func groupAudioPriceConfigFromAPIKey(apiKey *APIKey) *audioPriceConfig {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	g := apiKey.Group
	return &audioPriceConfig{
		RealtimePerMin: g.AudioRealtimePricePerMin,
		TTSPerMChars:   g.AudioTTSPricePerMillionChars,
		STTPerHour:     g.AudioSTTPricePerHour,
	}
}
