package service

import (
	"context"
	"fmt"
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
