package service

import (
	"context"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// GetGroupModelMappingCandidates returns concrete client model names from the
// group's enabled upstream accounts. It never adds a global pricing catalog or
// exposes account credentials, and does not contact upstream providers.
func (s *adminServiceImpl) GetGroupModelMappingCandidates(ctx context.Context, id int64, platform string) ([]string, error) {
	if id <= 0 {
		return nil, infraerrors.BadRequest("INVALID_GROUP_ID", "an existing group is required")
	}
	group, err := s.groupRepo.GetByIDLite(ctx, id)
	if err != nil {
		return nil, err
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = group.Platform
	}
	if !isConcreteRequestPlatform(platform) || (group.Platform != PlatformComposite && group.Platform != platform) {
		return nil, infraerrors.BadRequest("INVALID_PLATFORM", "select a concrete platform supported by this group")
	}
	models := make([]string, 0)
	if s.accountRepo == nil {
		return models, nil
	}
	accounts, err := s.accountRepo.ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]string)
	for i := range accounts {
		account := &accounts[i]
		if account.Platform != platform || account.Status != StatusActive || !account.Schedulable {
			continue
		}
		for model := range account.GetModelMapping() {
			model = strings.TrimSpace(model)
			if model == "" || strings.Contains(model, "*") {
				continue
			}
			key := strings.ToLower(model)
			if previous, exists := seen[key]; !exists || model < previous {
				seen[key] = model
			}
		}
	}
	for _, model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models, nil
}
