package service

import (
	"context"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// AuthorizeNormalGroupExecution reloads execution identity directly from repositories.
// frozenNormalGroupIDs must come from the ORIGINAL request billing bundle; it is
// an allow-set, never a source to expand or reorder that bundle. HA is not supported.
// The returned group is current authorization state, NOT a replacement pricing
// snapshot. Callers must retain their frozen billing context and separately check
// model policy, subscription windows/financial limits and destination-group RPM.
// Reads are fresh but not an atomic reservation against concurrent revocation.
func (s *APIKeyService) AuthorizeNormalGroupExecution(ctx context.Context, keyID, userID, groupID int64, frozenNormalGroupIDs []int64) (*APIKey, *UserSubscription, error) {
	allowed := false
	for _, id := range frozenNormalGroupIDs {
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
	bound := len(key.Groups) == 0 && key.GroupID != nil && *key.GroupID == groupID
	for _, link := range key.Groups {
		if link.GroupID == groupID {
			if link.Role != APIKeyGroupRoleNormal {
				return nil, nil, ErrGroupNotAllowed
			}
			bound = true
		}
	}
	if !bound {
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
		return nil, nil, ErrGroupNotAllowed
	}
	// Do not mutate repository/auth-cache objects or carry the first group's RPM
	// override into the destination group. Loaded relations are not execution inputs.
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
