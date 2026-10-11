package service

import (
	"context"
	"fmt"
	"strings"
)

type requestAccountMapping struct {
	Model                     string
	LongContextBillingEnabled bool
}

// Only account ids and resolved model strings cross this boundary. Account
// credentials remain in the existing scheduler snapshot, never in billing.
func requestAccountMappingsFromSnapshot(list inflightSnapshotLister, platformFor func(context.Context, *APIKey, string) (string, bool, bool), actualModelResolver ...func(*Account, string) string) func(context.Context, *APIKey, string) (map[int64]requestAccountMapping, error) {
	return func(ctx context.Context, key *APIKey, model string) (map[int64]requestAccountMapping, error) {
		platform, forced, ok := platformFor(ctx, key, model)
		if !ok || platform == "" {
			return nil, fmt.Errorf("%w: account mapping platform unavailable", ErrModelPricingUnavailable)
		}
		accounts, err := list(ctx, key.GroupID, platform, forced)
		if err != nil {
			return nil, fmt.Errorf("freeze account model mappings: %w", err)
		}
		results := make(map[int64]requestAccountMapping, len(accounts))
		for i := range accounts {
			mapped := model
			if len(actualModelResolver) > 0 {
				mapped = actualModelResolver[0](&accounts[i], model)
			} else if resolved, matched := accounts[i].ResolveMappedModel(model); matched {
				mapped = resolved
			}
			mapped = strings.TrimSpace(mapped)
			if mapped == "" {
				return nil, fmt.Errorf("%w: empty account mapping", ErrModelPricingUnavailable)
			}
			results[accounts[i].ID] = requestAccountMapping{Model: mapped, LongContextBillingEnabled: accounts[i].IsOpenAILongContextBillingEnabled()}
		}
		return results, nil
	}
}
