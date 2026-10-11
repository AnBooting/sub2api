package service

import (
	"context"
	"fmt"
	"strings"
)

// Only account ids and resolved model strings cross this boundary. Account
// credentials remain in the existing scheduler snapshot, never in billing.
func requestAccountMappingsFromSnapshot(list inflightSnapshotLister, platformFor func(context.Context, *APIKey, string) (string, bool, bool)) func(context.Context, *APIKey, string) (map[int64]string, error) {
	return func(ctx context.Context, key *APIKey, model string) (map[int64]string, error) {
		platform, forced, ok := platformFor(ctx, key, model)
		if !ok || platform == "" {
			return nil, fmt.Errorf("%w: account mapping platform unavailable", ErrModelPricingUnavailable)
		}
		accounts, err := list(ctx, key.GroupID, platform, forced)
		if err != nil {
			return nil, fmt.Errorf("freeze account model mappings: %w", err)
		}
		results := make(map[int64]string, len(accounts))
		for i := range accounts {
			mapped, matched := accounts[i].ResolveMappedModel(model)
			if !matched {
				mapped = model
			}
			mapped = strings.TrimSpace(mapped)
			if mapped == "" {
				return nil, fmt.Errorf("%w: empty account mapping", ErrModelPricingUnavailable)
			}
			results[accounts[i].ID] = mapped
		}
		return results, nil
	}
}
