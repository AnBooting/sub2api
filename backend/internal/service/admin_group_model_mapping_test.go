//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type mappingCandidateAccounts struct {
	AccountRepository
	accounts []Account
	err      error
}

func (r *mappingCandidateAccounts) ListByGroup(context.Context, int64) ([]Account, error) {
	return r.accounts, r.err
}

type mappingCandidateGroups struct {
	GroupRepository
	group *Group
}

func (r *mappingCandidateGroups) GetByIDLite(context.Context, int64) (*Group, error) {
	return r.group, nil
}

func TestGroupModelMappingCandidates(t *testing.T) {
	account := func(platform, status string, enabled bool, models map[string]any) Account {
		return Account{Platform: platform, Status: status, Schedulable: enabled,
			Credentials: map[string]any{"model_mapping": models}}
	}
	repo := &mappingCandidateAccounts{accounts: []Account{
		account(PlatformOpenAI, StatusActive, true, map[string]any{"alias": "private-upstream-id", "gpt-test": "gpt-test", "gpt-*": "*", "": "empty"}),
		account(PlatformOpenAI, StatusActive, true, map[string]any{"GPT-test": "different-target", " new-model ": "new-model"}),
		account(PlatformGemini, StatusActive, true, map[string]any{"wrong-platform": "x"}),
		account(PlatformOpenAI, StatusDisabled, true, map[string]any{"disabled": "x"}),
		account(PlatformOpenAI, StatusActive, false, map[string]any{"unschedulable": "x"}),
	}}
	svc := &adminServiceImpl{groupRepo: &mappingCandidateGroups{group: &Group{Platform: PlatformOpenAI}}, accountRepo: repo}
	models, err := svc.GetGroupModelMappingCandidates(context.Background(), 3, "")
	require.NoError(t, err)
	require.Equal(t, []string{"GPT-test", "alias", "new-model"}, models)
	// A composite group must still isolate the selected concrete request platform.
	svc.groupRepo = &mappingCandidateGroups{group: &Group{Platform: PlatformComposite}}
	models, err = svc.GetGroupModelMappingCandidates(context.Background(), 3, PlatformGemini)
	require.NoError(t, err)
	require.Equal(t, []string{"wrong-platform"}, models)
}

func TestGroupModelMappingCandidatesValidationAndErrors(t *testing.T) {
	repo := &mappingCandidateAccounts{}
	svc := &adminServiceImpl{groupRepo: &mappingCandidateGroups{group: &Group{Platform: PlatformOpenAI}}, accountRepo: repo}
	for _, input := range []struct {
		id       int64
		platform string
	}{{0, PlatformOpenAI}, {3, PlatformGemini}, {3, PlatformComposite}, {3, "unknown"}} {
		_, err := svc.GetGroupModelMappingCandidates(context.Background(), input.id, input.platform)
		require.Error(t, err)
	}
	models, err := svc.GetGroupModelMappingCandidates(context.Background(), 3, PlatformOpenAI)
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Empty(t, models) // No advertised global catalog when accounts have no mappings.
	repo.err = errors.New("database unavailable")
	_, err = svc.GetGroupModelMappingCandidates(context.Background(), 3, PlatformOpenAI)
	require.ErrorIs(t, err, repo.err)
}
