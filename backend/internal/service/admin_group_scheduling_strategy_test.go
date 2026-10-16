//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func newAdminGroupSchedulingStrategyTestService(t *testing.T) (*adminServiceImpl, *groupRepoStubForAdmin) {
	t.Helper()
	repo := &groupRepoStubForAdmin{}
	return &adminServiceImpl{cfg: &config.Config{}, groupRepo: repo}, repo
}

// 通用分组字段的调度策略（C-4）：大小写/首尾空白归一（与全局 settings 口径一致）。
func TestAdminService_CreateGroup_NormalizesSchedulingStrategy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "lowercase stable", input: "stable", want: "stable"},
		{name: "uppercase", input: "STABLE", want: "stable"},
		{name: "mixed case with spaces", input: " Stable ", want: "stable"},
		{name: "mixed case latency", input: "LaTeNcY", want: "latency"},
		{name: "inherit uppercase", input: " InHeRiT ", want: "inherit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newAdminGroupSchedulingStrategyTestService(t)

			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name:               "strategy-group",
				RateMultiplier:     1,
				Platform:           PlatformOpenAI,
				SchedulingStrategy: tc.input,
			})

			require.NoError(t, err)
			require.Equal(t, tc.want, group.SchedulingStrategy)
			require.Equal(t, tc.want, repo.created.SchedulingStrategy)
		})
	}
}

// 未提供该字段（含值类型无法区分的空串/空白）时落库默认 inherit（随全局）。
func TestAdminService_CreateGroup_DefaultsSchedulingStrategyToInherit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{name: "omitted"},
		{name: "empty string", input: ""},
		{name: "blank string", input: "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newAdminGroupSchedulingStrategyTestService(t)

			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name:               "no-strategy-group",
				RateMultiplier:     1,
				Platform:           PlatformOpenAI,
				SchedulingStrategy: tc.input,
			})

			require.NoError(t, err)
			require.Equal(t, SchedulingStrategyInherit, group.SchedulingStrategy)
			require.Equal(t, SchedulingStrategyInherit, repo.created.SchedulingStrategy)
		})
	}
}

// 非法值拒绝且不落库（与全局路径一致：显式非法值不静默回落）。
//
// 注意 create 路径的调度策略是值类型字段，"", "   " 无法与「未提供」区分，
// 因此按未配置处理落 inherit（与 CreateGroupRequest 其它值类型字段同一语义）；
// 「显式空串必须 400」只适用于更新路径 —— 那里的 *string 指针能区分两者。
func TestAdminService_CreateGroup_RejectsInvalidSchedulingStrategy(t *testing.T) {
	for _, input := range []string{"turbo", "global", "balanced/stable"} {
		t.Run("input="+input, func(t *testing.T) {
			svc, repo := newAdminGroupSchedulingStrategyTestService(t)

			_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name:               "bad-strategy-group",
				RateMultiplier:     1,
				Platform:           PlatformOpenAI,
				SchedulingStrategy: input,
			})

			require.Error(t, err)
			require.Equal(t, 400, infraerrors.Code(err))
			require.Equal(t, "INVALID_SCHEDULING_STRATEGY", infraerrors.Reason(err))
			require.Nil(t, repo.created)
		})
	}
}

// 更新路径：nil 不改动；显式值归一；显式空串与非法值 400（不是当成 inherit）。
func TestAdminService_UpdateGroup_SchedulingStrategy(t *testing.T) {
	t.Run("nil keeps stored value", func(t *testing.T) {
		existing := &Group{ID: 1, Name: "g", Platform: PlatformOpenAI, Status: StatusActive, SchedulingStrategy: "latency"}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := &adminServiceImpl{cfg: &config.Config{}, groupRepo: repo}

		_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{Platform: PlatformOpenAI})

		require.NoError(t, err)
		require.Equal(t, "latency", repo.updated.SchedulingStrategy)
	})

	t.Run("explicit value normalises case and space", func(t *testing.T) {
		existing := &Group{ID: 1, Name: "g", Platform: PlatformOpenAI, Status: StatusActive, SchedulingStrategy: SchedulingStrategyInherit}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := &adminServiceImpl{cfg: &config.Config{}, groupRepo: repo}
		value := " Stable "

		_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
			Platform:           PlatformOpenAI,
			SchedulingStrategy: &value,
		})

		require.NoError(t, err)
		require.Equal(t, "stable", repo.updated.SchedulingStrategy)
	})

	t.Run("explicit empty string is rejected, not treated as inherit", func(t *testing.T) {
		existing := &Group{ID: 1, Name: "g", Platform: PlatformOpenAI, Status: StatusActive, SchedulingStrategy: "latency"}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := &adminServiceImpl{cfg: &config.Config{}, groupRepo: repo}
		value := ""

		_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{
			Platform:           PlatformOpenAI,
			SchedulingStrategy: &value,
		})

		require.Error(t, err)
		require.Equal(t, 400, infraerrors.Code(err))
		require.Equal(t, "INVALID_SCHEDULING_STRATEGY", infraerrors.Reason(err))
		require.Nil(t, repo.updated, "invalid value must not be persisted")
	})

	t.Run("legacy dirty value converges to inherit", func(t *testing.T) {
		existing := &Group{ID: 1, Name: "g", Platform: PlatformOpenAI, Status: StatusActive, SchedulingStrategy: "legacy-junk"}
		repo := &groupRepoStubForAdmin{getByID: existing}
		svc := &adminServiceImpl{cfg: &config.Config{}, groupRepo: repo}

		_, err := svc.UpdateGroup(context.Background(), existing.ID, &UpdateGroupInput{Platform: PlatformOpenAI})

		require.NoError(t, err)
		require.Equal(t, SchedulingStrategyInherit, repo.updated.SchedulingStrategy)
	})
}
