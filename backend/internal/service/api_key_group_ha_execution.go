package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// AuthorizeHAGroupExecution reloads execution identity for a user-authorized
// high-availability fallback group. frozenHAGroupIDs must come from the ORIGINAL
// request billing bundle and is an allow-set only; it never expands that bundle.
//
// PRD-04 requires BOTH conditions before a fallback group may serve:
//   - the administrator configured the group on this Key with role=ha, and
//   - the user actively enabled it (enabled_by_user_at is set).
//
// A missing user authorization, a normal-role link, or a group outside the frozen
// HA allow-set is rejected here. The returned group is current authorization
// state, NOT a pricing snapshot: callers keep their frozen billing context and
// separately check subscription windows, financial limits and destination RPM.
func (s *APIKeyService) AuthorizeHAGroupExecution(ctx context.Context, keyID, userID, groupID int64, frozenHAGroupIDs []int64) (*APIKey, *UserSubscription, error) {
	allowed := false
	for _, id := range frozenHAGroupIDs {
		if id == groupID {
			allowed = true
			break
		}
	}
	if keyID <= 0 || userID <= 0 || groupID <= 0 || !allowed {
		return nil, nil, ErrGroupNotAllowed
	}
	if s == nil || s.apiKeyRepo == nil || s.userRepo == nil || s.groupRepo == nil {
		return nil, nil, ErrBillingServiceUnavailable
	}
	key, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, nil, fmt.Errorf("reload execution key: %w", err)
	}
	if key == nil || key.ID != keyID {
		return nil, nil, ErrAPIKeyNotFound
	}
	if key.UserID != userID {
		return nil, nil, ErrInsufficientPerms
	}
	if key.Status == StatusAPIKeyExpired || key.IsExpired() {
		return nil, nil, ErrAPIKeyExpired
	}
	if key.Status == StatusAPIKeyQuotaExhausted || key.IsQuotaExhausted() {
		return nil, nil, ErrAPIKeyQuotaExhausted
	}
	if !key.IsActive() {
		return nil, nil, infraerrors.Unauthorized("API_KEY_INACTIVE", "api key is not active")
	}
	// A legacy single-group Key can never carry an HA authorization.
	authorized := false
	for _, link := range key.Groups {
		if link.GroupID != groupID {
			continue
		}
		if link.Role != APIKeyGroupRoleHA {
			return nil, nil, ErrGroupNotAllowed
		}
		if link.EnabledByUserAt == nil {
			return nil, nil, ErrGroupNotAllowed
		}
		authorized = true
	}
	if !authorized {
		return nil, nil, ErrGroupNotAllowed
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("reload execution user: %w", err)
	}
	if user == nil || user.ID != userID {
		return nil, nil, ErrUserNotFound
	}
	if !user.IsActive() || user.DeletedAt != nil {
		return nil, nil, ErrUserNotActive
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, nil, fmt.Errorf("reload execution group: %w", err)
	}
	if group == nil || group.ID != groupID {
		return nil, nil, ErrGroupNotFound
	}
	if !group.IsActive() {
		return nil, nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
	}
	// PRD-04 §2.3/§2.6: 管理员取消该组的兜底配置后，存量用户授权也必须停止生效。
	// 与其他授权拒绝一致地返回 ErrGroupNotAllowed，fail closed 且不新增错误面。
	if !group.AllowHighAvailability {
		return nil, nil, ErrGroupNotAllowed
	}
	var subscription *UserSubscription
	if group.IsSubscriptionType() {
		if s.userSubRepo == nil {
			return nil, nil, ErrBillingServiceUnavailable
		}
		sub, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, userID, groupID)
		if err != nil {
			return nil, nil, fmt.Errorf("reload execution subscription: %w", err)
		}
		if sub == nil {
			return nil, nil, ErrSubscriptionNotFound
		}
		if sub.UserID != userID || sub.GroupID != groupID || sub.DeletedAt != nil || !sub.IsActive() || sub.StartsAt.After(time.Now()) {
			return nil, nil, ErrSubscriptionInvalid
		}
		copy := *sub
		copy.DailyWindowStart = cloneGroupValuePointer(sub.DailyWindowStart)
		copy.WeeklyWindowStart = cloneGroupValuePointer(sub.WeeklyWindowStart)
		copy.MonthlyWindowStart = cloneGroupValuePointer(sub.MonthlyWindowStart)
		copy.AssignedBy = cloneGroupValuePointer(sub.AssignedBy)
		copy.User, copy.Group, copy.AssignedByUser = nil, nil, nil
		subscription = &copy
	} else if !user.CanBindGroup(groupID, group.IsExclusive) {
		// K-2: a public HA group needs no prior access (the user authorization is
		// the grant), but an exclusive group still requires its explicit grant.
		return nil, nil, ErrGroupNotAllowed
	}
	executionUser := *user
	executionUser.UserGroupRPMOverride = nil
	executionUser.AllowedGroups = append([]int64(nil), user.AllowedGroups...)
	executionUser.LastLoginAt = cloneGroupValuePointer(user.LastLoginAt)
	executionUser.LastActiveAt = cloneGroupValuePointer(user.LastActiveAt)
	executionUser.LastUsedAt = cloneGroupValuePointer(user.LastUsedAt)
	executionUser.DeletedAt = cloneGroupValuePointer(user.DeletedAt)
	executionUser.TotpSecretEncrypted = cloneGroupValuePointer(user.TotpSecretEncrypted)
	executionUser.TotpEnabledAt = cloneGroupValuePointer(user.TotpEnabledAt)
	executionUser.BalanceNotifyThreshold = cloneGroupValuePointer(user.BalanceNotifyThreshold)
	executionUser.BalanceNotifyExtraEmails = append([]NotifyEmailEntry(nil), user.BalanceNotifyExtraEmails...)
	executionUser.GroupRates = nil
	executionUser.APIKeys, executionUser.Subscriptions = nil, nil
	execution := *key
	execution.User = &executionUser
	execution.Group = cloneAPIKeyGroup(group)
	execution.Group.AccountGroups = nil
	execution.GroupID = cloneGroupValuePointer(&groupID)
	execution.Groups = CloneAPIKeyGroupLinks(key.Groups)
	for i := range execution.Groups {
		if execution.Groups[i].Group != nil {
			execution.Groups[i].Group.AccountGroups = nil
		}
	}
	execution.CompiledIPWhitelist, execution.CompiledIPBlacklist = nil, nil
	s.compileAPIKeyIPRules(&execution)
	execution.IPWhitelist = append([]string(nil), key.IPWhitelist...)
	execution.IPBlacklist = append([]string(nil), key.IPBlacklist...)
	execution.ExpiresAt = cloneGroupValuePointer(key.ExpiresAt)
	execution.LastUsedAt = cloneGroupValuePointer(key.LastUsedAt)
	execution.LastUsedIP = cloneGroupValuePointer(key.LastUsedIP)
	execution.Window5hStart = cloneGroupValuePointer(key.Window5hStart)
	execution.Window1dStart = cloneGroupValuePointer(key.Window1dStart)
	execution.Window7dStart = cloneGroupValuePointer(key.Window7dStart)
	return &execution, subscription, nil
}

// APIKeyHAGroupEnablementResult 是用户主动开启/关闭 HA 兜底组后的状态。
//
// RateMultiplier 恒为服务端分组配置值（groups.rate_multiplier）：请求体里的任何倍率
// 字段都不参与计算，用户不能为自己协商兜底倍率。
type APIKeyHAGroupEnablementResult struct {
	APIKey             *APIKey
	Group              *Group
	Enabled            bool
	RateMultiplier     float64
	GroupChainRevision int64
}

// haGroupScopeCompatible 判断 HA 组能否承载 Key 当前普通链的模型适用范围。
//
// 复用既有分组模型白名单判定（GroupModelAllowlist.Allows）：HA 组未启用白名单即不受限，
// 直接兼容；普通组未启用白名单时其范围无法静态收窄，跳过该组（真正的逐请求能力检查
// 由执行期 AuthorizeHAGroupExecution 与网关白名单完成）。通配条目无法枚举，跳过。
func haGroupScopeCompatible(haGroup *Group, key *APIKey) bool {
	if haGroup == nil || !haGroup.ModelAllowlistEnabled() {
		return true
	}
	if key == nil {
		return true
	}
	for _, link := range key.Groups {
		if link.Role != APIKeyGroupRoleNormal || link.Group == nil || !link.Group.ModelAllowlistEnabled() {
			continue
		}
		for _, model := range link.Group.ModelAllowlist.Models {
			if strings.Contains(model, "*") {
				continue
			}
			if !haGroup.ModelAllowlist.Allows(model) {
				return false
			}
		}
	}
	return true
}

// SetAPIKeyHAGroupEnabled 让用户在自己某个 API Key 上主动开启/关闭某个 HA 兜底组。
//
// 校验顺序即安全边界（任一步失败都不落库）：
//  1. Key 归属当前登录用户，否则 403，不泄漏 Key 是否存在；
//  2. 目标组 allow_high_availability=true 且 active，否则拒绝；
//  3. 组模型白名单与 Key 当前适用范围匹配（复用 allowlist 判定）；
//  4. 倍率一律读服务端分组配置，请求体携带的倍率被忽略；
//  5. 开启写 enabled_by_user_at=now()，关闭置 NULL。
//
// 写入走既有 Key 行锁 + group_chain_revision CAS 事务，变更后失效该 Key 的鉴权缓存。
func (s *APIKeyService) SetAPIKeyHAGroupEnabled(ctx context.Context, userID, keyID, groupID int64, enable bool) (*APIKeyHAGroupEnablementResult, error) {
	if userID <= 0 || keyID <= 0 || groupID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_HA_GROUP_REQUEST", "user, key and group ids must be positive")
	}
	if s == nil || s.apiKeyRepo == nil || s.groupRepo == nil {
		return nil, ErrBillingServiceUnavailable
	}
	repo, ok := s.apiKeyRepo.(APIKeyGroupChainTxRepository)
	if !ok {
		return nil, infraerrors.InternalServer("GROUP_CHAIN_TRANSACTION_UNAVAILABLE", "group chain transaction is not configured")
	}

	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get ha group: %w", err)
	}
	if group == nil || group.ID != groupID {
		return nil, ErrGroupNotFound
	}
	// 只有管理员标记为可作兜底的分组才能被用户开启。
	if !group.AllowHighAvailability {
		return nil, infraerrors.Forbidden("HA_GROUP_NOT_CONFIGURED", "group is not configured as a high-availability fallback")
	}
	if !group.IsActive() {
		return nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
	}

	result := &APIKeyHAGroupEnablementResult{Group: group, Enabled: enable, RateMultiplier: group.RateMultiplier}
	err = repo.WithGroupChainTransaction(ctx, keyID, nil, func(txCtx context.Context, key *APIKey) error {
		// ① 归属校验：非本人 Key 一律 403，且不区分「不存在」与「不属于我」。
		if key == nil || key.ID != keyID {
			return ErrAPIKeyNotFound
		}
		if key.UserID != userID {
			return ErrInsufficientPerms
		}
		// ③ 组模型白名单与 Key 适用范围匹配。
		if !haGroupScopeCompatible(group, key) {
			return infraerrors.Forbidden("HA_GROUP_MODEL_SCOPE_MISMATCH", "group cannot serve the model scope of this api key")
		}
		// ④ FE-02：开启前确认该组在执行期真的能服务，否则界面会显示一个执行必拒的
		// 「已开启」。执行期对非订阅专属组要求 CanBindGroup，这里复用同一判定源。
		// 订阅型组的授权由订阅关系承担，写入时无法静态确认，仍由执行期校验。
		// 放在归属校验之后，避免用「用户是否存在」这类信息替代 403。
		// 只有**专属**非订阅组才需要用户授权：公共组 CanBindGroup 恒为 true（K-3 保证
		// 兜底不依赖普通分组访问权限），订阅组的授权由订阅关系承担。因此仅在真正需要
		// 判定访问时读用户，避免给公共组路径引入无谓的依赖与失败面。
		if enable && !group.IsSubscriptionType() && group.IsExclusive {
			if s.userRepo == nil {
				return ErrBillingServiceUnavailable
			}
			enablementUser, err := s.userRepo.GetByID(txCtx, userID)
			if err != nil {
				return fmt.Errorf("get ha enablement user: %w", err)
			}
			if enablementUser == nil || enablementUser.ID != userID {
				return ErrUserNotFound
			}
			if !haGroupAccessSatisfied(enablementUser, group) {
				return infraerrors.Forbidden("HA_GROUP_ACCESS_REQUIRED",
					"this fallback group requires an explicit grant before it can serve")
			}
		}
		resolved, err := ApplyUserHAEnablement(key.Groups, groupID, enable, time.Now())
		if err != nil {
			return err
		}
		key.Groups = resolved
		key.GroupID = FirstNormalAPIKeyGroupID(resolved)
		key.Group = nil
		revision := key.GroupChainRevision
		if err := s.apiKeyRepo.Update(txCtx, key, APIKeyUpdateFields{
			Groups:                     true,
			AuthoritativeHAGroups:      true,
			ExpectedGroupChainRevision: &revision,
		}); err != nil {
			return err
		}
		result.APIKey = key
		result.GroupChainRevision = key.GroupChainRevision
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 分组快照新增/清除 enabled_by_user_at 后必须失效鉴权缓存，否则旧快照继续生效。
	if result.APIKey != nil {
		s.InvalidateAuthCacheByKey(ctx, result.APIKey.Key)
	}
	return result, nil
}

// HAGroupCandidate is one admin-designated high-availability fallback group that a
// user may opt into for a given API key (PRD-04/TS-04).
//
// It is a display-only projection: Enabled reflects the user's CURRENT consent for
// this key (enabled_by_user_at is set), and RateMultiplier is the server-side group
// multiplier that billing will actually apply. The client never supplies a rate.
type HAGroupCandidate struct {
	GroupID         int64      `json:"group_id"`
	Name            string     `json:"name"`
	Platform        string     `json:"platform"`
	RateMultiplier  float64    `json:"rate_multiplier"`
	Enabled         bool       `json:"enabled"`
	EnabledByUserAt *time.Time `json:"enabled_by_user_at,omitempty"`
	// ScopeCompatible reports whether this candidate can serve the model scope of
	// the key. An incompatible candidate is still listed (so the user sees why it is
	// unavailable) but cannot be enabled; the write path re-checks it.
	ScopeCompatible bool `json:"scope_compatible"`
	// AccessGranted reports whether the user currently holds the access this group
	// needs to actually serve (an exclusive group needs an explicit grant; a public
	// group needs none). The write path enforces the same condition, so a candidate
	// shown as enableable can really serve — PRD-04 FE-02 forbids showing a
	// misleading "enabled" state.
	AccessGranted bool `json:"access_granted"`
}

// ListAPIKeyHAGroupCandidates returns the HA fallback groups the user may opt this
// key into, together with their current consent state.
//
// Listing deliberately does NOT reuse GetAvailableGroups: PRD-04/K-3 states the HA
// fallback is an extra stability capability that does NOT depend on ordinary group
// access permission. Filtering by canUserBindGroup would hide an admin-designated
// fallback from a user who cannot bind it as a normal group — exactly the case this
// feature exists for. Only the admin flag and group activity gate listing;
// ownership is checked on the key.
func (s *APIKeyService) ListAPIKeyHAGroupCandidates(ctx context.Context, userID, keyID int64) ([]HAGroupCandidate, error) {
	if userID <= 0 || keyID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_HA_GROUP_REQUEST", "user and key ids must be positive")
	}
	if s == nil || s.apiKeyRepo == nil || s.groupRepo == nil {
		return nil, ErrBillingServiceUnavailable
	}
	key, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("load key for ha candidates: %w", err)
	}
	if key == nil || key.ID != keyID {
		return nil, ErrAPIKeyNotFound
	}
	// Ownership is the same boundary as the write path: never reveal another
	// user's key, and never distinguish "missing" from "not yours".
	if key.UserID != userID {
		return nil, ErrInsufficientPerms
	}

	consent := make(map[int64]*time.Time, len(key.Groups))
	for _, link := range key.Groups {
		if link.Role != APIKeyGroupRoleHA {
			continue
		}
		consent[link.GroupID] = link.EnabledByUserAt
	}

	// FE-02：候选列表必须如实反映「能否真的服务」，否则界面会显示一个执行必拒的
	// 可开启项。访问前置条件与写入路径、执行路径共用同一判定源。
	// 仅在存在专属候选时才需要读用户：公共组与订阅组都不依赖 AllowedGroups。
	var listUser *User
	if s.userRepo != nil {
		listUser, err = s.userRepo.GetByID(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("load user for ha candidates: %w", err)
		}
	}

	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active groups for ha candidates: %w", err)
	}
	out := make([]HAGroupCandidate, 0)
	for i := range groups {
		group := groups[i]
		if !group.AllowHighAvailability {
			continue
		}
		out = append(out, HAGroupCandidate{
			GroupID:         group.ID,
			Name:            group.Name,
			Platform:        group.Platform,
			RateMultiplier:  group.RateMultiplier,
			EnabledByUserAt: consent[group.ID],
			Enabled:         consent[group.ID] != nil,
			ScopeCompatible: haGroupScopeCompatible(&group, key),
			AccessGranted:   haGroupAccessSatisfied(listUser, &group),
		})
	}
	// Enabled first, then by id: the user's current choices stay at the top and the
	// order is stable across calls.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return out[i].GroupID < out[j].GroupID
	})
	return out, nil
}


// haGroupAccessSatisfied 报告用户当前是否满足「让该 HA 组实际服务」的访问前置条件。
//
// 为什么开启期必须复用执行期的判定（PRD-04 FE-02「无可用候选时不显示误导性的已启用
// 状态」）：执行期 AuthorizeHAGroupExecution 对**非订阅专属组**要求 CanBindGroup 通过，
// 而开启期原本不检查。于是用户可以开启一个执行时必被拒的兜底组，界面显示「已开启」，
// 真到需要兜底时却拿不到服务——这正是 FE-02 禁止的误导状态。
//
// 注意这不违反 K-3/BE-02「兜底不依赖普通分组访问权限」：公共组（非专属）
// CanBindGroup 恒为 true，仍然无需任何预授权；只有**专属组**才要求显式授权，
// 与执行期完全一致。订阅型组的授权由订阅关系承担，这里不重复判定。
func haGroupAccessSatisfied(user *User, group *Group) bool {
	if group == nil {
		return false
	}
	if group.IsSubscriptionType() {
		return true
	}
	if user == nil {
		return false
	}
	return user.CanBindGroup(group.ID, group.IsExclusive)
}

