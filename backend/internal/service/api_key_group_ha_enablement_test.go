//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// haChainRepoStub 实现 APIKeyGroupChainTxRepository + APIKeyRepository，
// 用于在不触库的前提下验证用户主动开启/关闭的校验顺序与链变更。
type haChainRepoStub struct {
	APIKeyRepository
	key         *APIKey
	updateCalls int
	lastFields  APIKeyUpdateFields
	conflict    bool
}

func (s *haChainRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	clone := *s.key
	clone.Groups = CloneAPIKeyGroupLinks(s.key.Groups)
	return &clone, nil
}

func (s *haChainRepoStub) Update(_ context.Context, key *APIKey, fields APIKeyUpdateFields) error {
	if fields.Groups {
		if fields.ExpectedGroupChainRevision == nil {
			return ErrGroupChainConflict
		}
		if s.conflict || *fields.ExpectedGroupChainRevision != s.key.GroupChainRevision {
			return ErrGroupChainConflict
		}
	}
	s.updateCalls++
	s.lastFields = fields
	// 模拟真实仓储：以数据库当前 revision 为基线，链真正变化才递增版本。
	key.GroupChainRevision = s.key.GroupChainRevision
	if !APIKeyGroupChainsEqual(s.key.Groups, key.Groups) {
		key.GroupChainRevision++
	}
	key.GroupID = FirstNormalAPIKeyGroupID(key.Groups)
	clone := *key
	clone.Groups = CloneAPIKeyGroupLinks(key.Groups)
	s.key = &clone
	return nil
}

func (s *haChainRepoStub) WithGroupChainTransaction(ctx context.Context, _ int64, expected *int64, fn func(context.Context, *APIKey) error) error {
	if expected != nil && (s.conflict || *expected != s.key.GroupChainRevision) {
		return ErrGroupChainConflict
	}
	clone := *s.key
	clone.Groups = CloneAPIKeyGroupLinks(s.key.Groups)
	return fn(ctx, &clone)
}

func (s *haChainRepoStub) ListKeysByUserID(context.Context, int64) ([]string, error) { return nil, nil }
func (s *haChainRepoStub) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	return nil, nil
}

func newHAEnablementService(t *testing.T, key *APIKey, group *Group) (*APIKeyService, *haChainRepoStub) {
	t.Helper()
	repo := &haChainRepoStub{key: key}
	svc := NewAPIKeyService(repo, nil, &haGroupRepoStub{group: group}, nil, nil, nil, nil)
	return svc, repo
}

type haGroupRepoStub struct {
	GroupRepository
	group  *Group
	groups []Group
}

func (s *haGroupRepoStub) GetByID(context.Context, int64) (*Group, error) {
	clone := *s.group
	return &clone, nil
}

func haTestGroup(id int64, allowHA bool) *Group {
	return &Group{ID: id, Name: "ha", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 3, AllowHighAvailability: allowHA}
}

func haTestKey(userID int64, groups ...APIKeyGroupLink) *APIKey {
	return &APIKey{ID: 1, UserID: userID, Key: "sk-ha", Name: "ha", Status: StatusActive, Groups: groups}
}

// ① 未标记 allow_high_availability 的组被拒。
func TestSetAPIKeyHAGroupRejectsUnmarkedGroup(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	svc, repo := newHAEnablementService(t, key, haTestGroup(9, false))
	_, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.Error(t, err)
	require.Zero(t, repo.updateCalls, "unmarked group must not be written")
	require.False(t, haLinkPresent(repo.key.Groups, 9), "unmarked group must not be linked")
}

// haLinkPresent 报告链上是否存在指定分组。
func haLinkPresent(links []APIKeyGroupLink, groupID int64) bool {
	for _, link := range links {
		if link.GroupID == groupID {
			return true
		}
	}
	return false
}

// ② 非本人 Key 被拒，且不写库。
func TestSetAPIKeyHAGroupRejectsForeignKey(t *testing.T) {
	key := haTestKey(8, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	svc, repo := newHAEnablementService(t, key, haTestGroup(9, true))
	_, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.Zero(t, repo.updateCalls)
}

// ③ 请求体带倍率被忽略：倍率始终取服务端分组配置。
func TestSetAPIKeyHAGroupIgnoresClientSuppliedRateMultiplier(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	group := haTestGroup(9, true)
	group.RateMultiplier = 2.5
	svc, repo := newHAEnablementService(t, key, group)

	result, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.NoError(t, err)
	require.Equal(t, 2.5, result.RateMultiplier, "multiplier must come from the server-side group config")

	// 再次开启时客户端传入的倍率也不得影响结果（幂等，不递增版本）。
	revision := repo.key.GroupChainRevision
	again, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.NoError(t, err)
	require.Equal(t, 2.5, again.RateMultiplier)
	require.Equal(t, revision, repo.key.GroupChainRevision, "repeated enable must be idempotent")
}

// ④ 开启后链接 role=ha 且 enabled_by_user_at 非空；⑤ 关闭后置空；⑥ revision 递增。
func TestSetAPIKeyHAGroupEnableDisableLifecycle(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	svc, repo := newHAEnablementService(t, key, haTestGroup(9, true))

	enabled, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.NoError(t, err)
	require.True(t, enabled.Enabled)
	require.Equal(t, int64(1), enabled.GroupChainRevision, "chain change must increment revision")

	var ha *APIKeyGroupLink
	for i := range repo.key.Groups {
		if repo.key.Groups[i].GroupID == 9 {
			ha = &repo.key.Groups[i]
		}
	}
	require.NotNil(t, ha, "enabling must add an ha link")
	require.Equal(t, APIKeyGroupRoleHA, ha.Role)
	require.NotNil(t, ha.EnabledByUserAt, "enabled_by_user_at must be written on enable")

	disabled, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, false)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	require.Equal(t, int64(2), disabled.GroupChainRevision, "disabling must also increment revision")
	for i := range repo.key.Groups {
		if repo.key.Groups[i].GroupID == 9 {
			require.Equal(t, APIKeyGroupRoleHA, repo.key.Groups[i].Role)
			require.Nil(t, repo.key.Groups[i].EnabledByUserAt, "disabling must clear enabled_by_user_at")
		}
	}
}

// 关闭一个未开启的 HA 组是幂等的，不递增版本。
func TestSetAPIKeyHAGroupDisableWithoutEnableIsIdempotent(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	svc, repo := newHAEnablementService(t, key, haTestGroup(9, true))
	result, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, false)
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.Zero(t, repo.key.GroupChainRevision)
	require.False(t, haLinkPresent(repo.key.Groups, 9), "disabling an unenabled group must not add a link")
}

// 组模型白名单与 Key 适用范围不匹配时拒绝。
func TestSetAPIKeyHAGroupRejectsModelScopeMismatch(t *testing.T) {
	normalGroup := &Group{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}}
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0, Group: normalGroup})
	haGroup := haTestGroup(9, true)
	haGroup.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"claude-sonnet-4.5"}}
	svc, repo := newHAEnablementService(t, key, haGroup)

	_, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.Error(t, err)
	require.Zero(t, repo.updateCalls, "scope mismatch must not be written")

	// 白名单覆盖 Key 适用范围时允许开启。
	haGroup.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4", "gpt-*"}}
	svc, repo = newHAEnablementService(t, key, haGroup)
	_, err = svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), repo.key.GroupChainRevision)
}

// 并发冲突（revision 失配）时不写库，返回冲突错误。
func TestSetAPIKeyHAGroupConflictsAreNotWritten(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	key.GroupChainRevision = 4
	svc, repo := newHAEnablementService(t, key, haTestGroup(9, true))
	repo.conflict = true

	_, err := svc.SetAPIKeyHAGroupEnabled(context.Background(), 7, 1, 9, true)
	require.Error(t, err)
	require.Zero(t, repo.updateCalls)
}

// 用户接口写入的授权时间戳由服务端生成，不接受客户端提交值。
func TestApplyUserHAEnablementAlwaysStampsServerTime(t *testing.T) {
	forged := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	current := []APIKeyGroupLink{{GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &forged}}
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	// 已开启的组再次开启：保持原时间戳（幂等）。
	same, err := ApplyUserHAEnablement(current, 9, true, now)
	require.NoError(t, err)
	require.True(t, same[0].EnabledByUserAt.Equal(forged))

	// 关闭后重新开启：使用服务端 now，而不是客户端传入值。
	closed, err := ApplyUserHAEnablement(current, 9, false, now)
	require.NoError(t, err)
	require.Nil(t, closed[0].EnabledByUserAt)
	reopened, err := ApplyUserHAEnablement(closed, 9, true, now)
	require.NoError(t, err)
	require.True(t, reopened[0].EnabledByUserAt.Equal(now))
}

// 普通组不能被当作 HA 组开启（同一分组不得同时是 normal 与 ha）。
func TestApplyUserHAEnablementRejectsNormalGroup(t *testing.T) {
	current := []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}}
	_, err := ApplyUserHAEnablement(current, 1, true, time.Now())
	require.Error(t, err)
}

// 管理员解析：HA 链恒取数据库既有值，且不能凭空开启。
//
// 契约（11-共享契约.md:75、13-接口文档.md:33）：管理员的 groups 只允许 normal，
// 数据库保留既有 HA。这里同时覆盖「管理员绕过校验提交 HA」的情形——HA 条目被忽略，
// 而不是让其它既有授权被丢弃。
func TestResolveAdminAPIKeyGroupChainKeepsDatabaseAuthorization(t *testing.T) {
	at := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	forged := at.Add(time.Hour)
	current := []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &at}}

	// 提交里带 ha 且伪造时间戳：HA 以数据库为准，时间戳沿用，伪造值无效。
	got, err := ResolveAdminAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &forged}})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.True(t, got[1].EnabledByUserAt.Equal(at))

	// 只改普通组：保留既有 ha。
	got, err = ResolveAdminAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 2, Role: APIKeyGroupRoleNormal, Position: 0}})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, APIKeyGroupRoleHA, got[1].Role)
	require.True(t, got[1].EnabledByUserAt.Equal(at))

	// 管理员提交里新增了一条用户未开启的 ha：**忽略**该条目，且既有 ha 不受影响。
	// 管理员不能代替用户开启兜底（HA 开启是用户动作），也不能因此丢掉其它授权。
	got, err = ResolveAdminAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &forged}, {GroupID: 10, Role: APIKeyGroupRoleHA, Position: 1}})
	require.NoError(t, err)
	require.Len(t, got, 2, "the admin-declared HA must be ignored, not created")
	require.True(t, got[1].EnabledByUserAt.Equal(at))
	for _, link := range got {
		require.NotEqual(t, int64(10), link.GroupID, "admin must not create an HA authorization")
	}
}

// 用户授权解析：不允许凭空新增 HA 开启；关闭既有授权是允许的。
func TestResolveUserAuthorizedAPIKeyGroupChain(t *testing.T) {
	at := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	current := []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &at}}

	_, err := ResolveUserAuthorizedAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 10, Role: APIKeyGroupRoleHA, Position: 0}})
	require.Error(t, err, "user path must not create a brand new HA authorization without a server timestamp")

	got, err := ResolveUserAuthorizedAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0}})
	require.NoError(t, err)
	require.Nil(t, got[1].EnabledByUserAt, "closing an existing authorization clears the timestamp")

	kept, err := ResolveUserAuthorizedAPIKeyGroupChain(current, []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &at}})
	require.NoError(t, err)
	require.True(t, kept[1].EnabledByUserAt.Equal(at))
}

// 仓储层：AllowHADeclaration 走管理员解析，AuthoritativeHAGroups 走用户解析，
// 默认（普通路径）仍不允许伪造 HA。
//
// 三条路径都不得由非用户操作创建 HA 授权：
//   - 默认路径直接拒绝；
//   - 管理员路径忽略提交里的 HA（HA 开启是用户动作，管理员只编辑 normal 链）；
//   - 用户权威路径要求服务端生成的时间戳。
func TestAPIKeyUpdateFieldsHADeclarationRouting(t *testing.T) {
	current := []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}}
	desired := []APIKeyGroupLink{{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0}, {GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0}}

	// 默认路径：不得新增 HA。
	if _, err := ResolveAPIKeyGroupChain(current, desired, false); err == nil {
		t.Fatal("default path must not allow new HA authorization")
	}
	// 管理员路径：只保留 normal，提交里的 HA 被忽略（既不能开启，也不能借此丢授权）。
	admin, err := ResolveAdminAPIKeyGroupChain(current, desired)
	require.NoError(t, err)
	require.Len(t, admin, 1, "the admin path must not create an HA authorization")
	require.Equal(t, APIKeyGroupRoleNormal, admin[0].Role)
	// 用户权威集合：不带服务端时间戳时拒绝。
	_, err = ResolveUserAuthorizedAPIKeyGroupChain(current, desired)
	require.Error(t, err)
}

// ListActive 供候选列表使用：返回所有活跃分组，由服务层按兜底标记过滤。
func (s *haGroupRepoStub) ListActive(context.Context) ([]Group, error) {
	if s.groups != nil {
		return append([]Group(nil), s.groups...), nil
	}
	if s.group == nil {
		return nil, nil
	}
	clone := *s.group
	return []Group{clone}, nil
}

// PRD-04/TS-04：候选列表只按「管理员标记兜底」过滤，不套用普通分组绑定权限。
func TestListAPIKeyHAGroupCandidatesFiltersByAdminFlagOnly(t *testing.T) {
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0})
	svc := &APIKeyService{
		apiKeyRepo: &haChainRepoStub{key: key},
		groupRepo: &haGroupRepoStub{groups: []Group{
			{ID: 1, Name: "normal", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1},
			{ID: 9, Name: "fallback", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 2.5, AllowHighAvailability: true},
			{ID: 10, Name: "not-fallback", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 1},
		}},
	}

	candidates, err := svc.ListAPIKeyHAGroupCandidates(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "only the admin-marked group is a candidate")
	require.Equal(t, int64(9), candidates[0].GroupID)
	require.Equal(t, 2.5, candidates[0].RateMultiplier, "multiplier is the server-side group config")
	require.False(t, candidates[0].Enabled, "no consent recorded yet")
}

// 已开启的候选必须回报 enabled，并带上授权时刻。
func TestListAPIKeyHAGroupCandidatesReportsExistingConsent(t *testing.T) {
	enabledAt := time.Now().Add(-time.Hour)
	key := haTestKey(7,
		APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0},
		APIKeyGroupLink{GroupID: 9, Role: APIKeyGroupRoleHA, Position: 0, EnabledByUserAt: &enabledAt},
	)
	svc := &APIKeyService{
		apiKeyRepo: &haChainRepoStub{key: key},
		groupRepo:  &haGroupRepoStub{groups: []Group{{ID: 9, Name: "fallback", Status: StatusActive, RateMultiplier: 3, AllowHighAvailability: true}}},
	}

	candidates, err := svc.ListAPIKeyHAGroupCandidates(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.True(t, candidates[0].Enabled)
	require.NotNil(t, candidates[0].EnabledByUserAt)
}

// 非本人 Key 一律拒绝，不泄漏 Key 是否存在。
func TestListAPIKeyHAGroupCandidatesRejectsForeignKey(t *testing.T) {
	key := haTestKey(8)
	svc := &APIKeyService{
		apiKeyRepo: &haChainRepoStub{key: key},
		groupRepo:  &haGroupRepoStub{groups: []Group{{ID: 9, AllowHighAvailability: true}}},
	}

	_, err := svc.ListAPIKeyHAGroupCandidates(context.Background(), 7, 1)
	require.ErrorIs(t, err, ErrInsufficientPerms)
}

// 模型范围不匹配的候选仍然列出（让用户看到原因），但标记为不可开启。
func TestListAPIKeyHAGroupCandidatesFlagsScopeMismatch(t *testing.T) {
	normal := Group{
		ID: 1, Name: "normal", Status: StatusActive,
		ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-4o"}},
	}
	fallback := Group{
		ID: 9, Name: "fallback", Status: StatusActive, RateMultiplier: 2,
		AllowHighAvailability: true,
		ModelAllowlist:        GroupModelAllowlist{Enabled: true, Models: []string{"claude-3"}},
	}
	key := haTestKey(7, APIKeyGroupLink{GroupID: 1, Role: APIKeyGroupRoleNormal, Position: 0, Group: &normal})
	svc := &APIKeyService{
		apiKeyRepo: &haChainRepoStub{key: key},
		groupRepo:  &haGroupRepoStub{groups: []Group{fallback}},
	}

	candidates, err := svc.ListAPIKeyHAGroupCandidates(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "an incompatible candidate is still listed")
	require.False(t, candidates[0].ScopeCompatible)
}

