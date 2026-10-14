package service

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	APIKeyGroupRoleNormal = "normal"
	APIKeyGroupRoleHA     = "ha"
)

var ErrGroupChainConflict = infraerrors.Conflict("group_chain_conflict", "group chain changed; refresh before saving")

// APIKeyGroupLink carries both configuration and the server-loaded candidate group.
// Authorization timestamps are read-only: mutations always reuse the database value.
type APIKeyGroupLink struct {
	GroupID         int64      `json:"group_id"`
	Role            string     `json:"role"`
	Position        int        `json:"position"`
	EnabledByUserAt *time.Time `json:"enabled_by_user_at,omitempty"`
	Group           *Group     `json:"-"`
}

// APIKeyGroupChainPresence separates omission from null without changing legacy pointer fields.
func APIKeyGroupChainPresence(data []byte) (groupIDProvided, groupsNull bool, err error) {
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(data, &fields); err != nil {
		return
	}
	_, groupIDProvided = fields["group_id"]
	if raw, ok := fields["groups"]; ok {
		groupsNull = bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	}
	return
}

func ValidateAPIKeyGroupChainInput(groupID *int64, groupIDProvided bool, groups *[]APIKeyGroupLink, groupsNull, admin, create bool, expected *int64) error {
	if groupsNull {
		return infraerrors.BadRequest("INVALID_GROUPS", "groups must be an array, not null")
	}
	if groups != nil && (groupIDProvided || groupID != nil) {
		return infraerrors.BadRequest("GROUP_FIELDS_CONFLICT", "group_id and groups cannot both be provided")
	}
	if groupID != nil && (*groupID < 0 || (!admin && *groupID == 0)) {
		return infraerrors.BadRequest("INVALID_GROUP_ID", "group_id must be positive")
	}
	if groups != nil {
		if !create && (expected == nil || *expected < 0) {
			return ErrGroupChainConflict
		}
		_, err := NormalizeAPIKeyGroupLinks(*groups, admin)
		return err
	}
	return nil
}

// NormalizeAPIKeyGroupLinks validates role-local continuous positions and global group uniqueness.
func NormalizeAPIKeyGroupLinks(links []APIKeyGroupLink, normalOnly bool) ([]APIKeyGroupLink, error) {
	out := append([]APIKeyGroupLink{}, links...)
	seen := make(map[int64]bool, len(out))
	for _, link := range out {
		if link.GroupID <= 0 || link.Position < 0 || (link.Role != APIKeyGroupRoleNormal && link.Role != APIKeyGroupRoleHA) || (normalOnly && link.Role != APIKeyGroupRoleNormal) {
			return nil, infraerrors.BadRequest("INVALID_GROUP_CHAIN", "invalid group id, role or position")
		}
		if seen[link.GroupID] {
			return nil, infraerrors.BadRequest("DUPLICATE_GROUP", "groups must not contain duplicate group ids")
		}
		seen[link.GroupID] = true
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role == out[j].Role {
			return out[i].Position < out[j].Position
		}
		return out[i].Role == APIKeyGroupRoleNormal
	})
	positions := map[string]int{}
	for _, link := range out {
		if link.Position != positions[link.Role] {
			return nil, infraerrors.BadRequest("INVALID_GROUP_POSITION", "group positions must be contiguous from zero within each role")
		}
		positions[link.Role]++
	}
	return out, nil
}

// ResolveAPIKeyGroupChain is used only with the Key row locked by the repository.
// This task cannot create new HA authorizations. Later HA enablement must be explicit.
func ResolveAPIKeyGroupChain(current, desired []APIKeyGroupLink, normalOnly bool) ([]APIKeyGroupLink, error) {
	out, err := NormalizeAPIKeyGroupLinks(desired, normalOnly)
	if err != nil {
		return nil, err
	}
	oldHA := map[int64]APIKeyGroupLink{}
	for _, link := range current {
		if link.Role == APIKeyGroupRoleHA {
			oldHA[link.GroupID] = link
		}
	}
	if normalOnly {
		for _, link := range current {
			if link.Role == APIKeyGroupRoleHA {
				out = append(out, link)
			}
		}
	}
	for i := range out {
		out[i].Group = nil
		if out[i].Role == APIKeyGroupRoleNormal {
			out[i].EnabledByUserAt = nil
			continue
		}
		old, ok := oldHA[out[i].GroupID]
		if !ok || old.EnabledByUserAt == nil {
			return nil, infraerrors.Forbidden("HA_AUTHORIZATION_REQUIRED", "new HA authorization is not enabled")
		}
		at := *old.EnabledByUserAt
		out[i].EnabledByUserAt = &at
	}
	return NormalizeAPIKeyGroupLinks(out, false)
}

// ResolveAdminAPIKeyGroupChain 是管理员配置路径：允许声明/保留 role=ha 链接，
// 但授权时间戳只能沿用数据库既有值——管理员不能代替用户主动开启。
//
// 语义（与用户路径 ResolveAPIKeyGroupChain 区分开）：
//   - desired 中的 normal 链接整体替换当前 normal 链接；
//   - desired 显式声明了 ha 链接时，ha 部分以 desired 为准（时间戳沿用 DB，缺失即未开启）；
//   - desired 未声明任何 ha 链接时，保留当前的全部 ha 链接（兼容“只改普通组”的旧调用）。
func ResolveAdminAPIKeyGroupChain(current, desired []APIKeyGroupLink) ([]APIKeyGroupLink, error) {
	out, err := NormalizeAPIKeyGroupLinks(desired, false)
	if err != nil {
		return nil, err
	}
	oldHA := map[int64]APIKeyGroupLink{}
	for _, link := range current {
		if link.Role == APIKeyGroupRoleHA {
			oldHA[link.GroupID] = link
		}
	}
	declaresHA := false
	merged := make([]APIKeyGroupLink, 0, len(out)+len(oldHA))
	for _, link := range out {
		link.Group = nil
		if link.Role == APIKeyGroupRoleNormal {
			link.EnabledByUserAt = nil
			merged = append(merged, link)
			continue
		}
		declaresHA = true
		// 授权时间戳只从数据库沿用；客户端提交的值一律忽略，避免伪造开启状态。
		if old, ok := oldHA[link.GroupID]; ok && old.EnabledByUserAt != nil {
			at := *old.EnabledByUserAt
			link.EnabledByUserAt = &at
		} else {
			link.EnabledByUserAt = nil
		}
		merged = append(merged, link)
	}
	if !declaresHA {
		for _, link := range current {
			if link.Role != APIKeyGroupRoleHA {
				continue
			}
			link.Group = nil
			if link.EnabledByUserAt != nil {
				at := *link.EnabledByUserAt
				link.EnabledByUserAt = &at
			}
			merged = append(merged, link)
		}
	}
	return NormalizeAPIKeyGroupLinks(merged, false)
}

// ResolveUserAuthorizedAPIKeyGroupChain 是用户主动开启路径：desired 的 HA 集合即最终状态。
//
// 与管理员路径的区别在于时间戳语义：这里不继承数据库值，因为“关闭”正是用空时间戳
// 表达的。安全边界仍由本函数守住——新增 role=ha 链接必须带服务端生成的时间戳；
// 空时间戳只允许用于关闭一个数据库里已存在的 HA 授权，从而无法凭空伪造开启状态。
func ResolveUserAuthorizedAPIKeyGroupChain(current, desired []APIKeyGroupLink) ([]APIKeyGroupLink, error) {
	out, err := NormalizeAPIKeyGroupLinks(desired, false)
	if err != nil {
		return nil, err
	}
	oldHA := make(map[int64]APIKeyGroupLink, len(current))
	for _, link := range current {
		if link.Role == APIKeyGroupRoleHA {
			oldHA[link.GroupID] = link
		}
	}
	for i := range out {
		out[i].Group = nil
		if out[i].Role == APIKeyGroupRoleNormal {
			out[i].EnabledByUserAt = nil
			continue
		}
		if out[i].EnabledByUserAt != nil {
			// 服务端生成的开启时间戳：原样保留（调用方已在锁内基于当前链构造）。
			continue
		}
		if _, ok := oldHA[out[i].GroupID]; !ok {
			return nil, infraerrors.Forbidden("HA_AUTHORIZATION_REQUIRED", "new HA authorization is not enabled")
		}
		// 关闭既有授权：显式置 NULL。
		out[i].EnabledByUserAt = nil
	}
	return NormalizeAPIKeyGroupLinks(out, false)
}

// APIKeyHAUserEnablement 描述用户在自己 Key 上对某个 HA 兜底组的主动开启/关闭意图。
// 它只能由用户接口产生；倍率不在此结构内，始终读服务端分组配置。
type APIKeyHAUserEnablement struct {
	GroupID int64
	Enable  bool
}

// ApplyUserHAEnablement 在给定链上应用一次用户主动开启/关闭。
//
// 开启：写 enabled_by_user_at=now；重复开启保持原时间戳（幂等，不产生无意义版本递增）。
// 关闭：置 enabled_by_user_at=NULL（链接保留，管理员配置不受影响）。
// 同一个分组不允许同时是 normal 与 ha。
func ApplyUserHAEnablement(current []APIKeyGroupLink, groupID int64, enable bool, now time.Time) ([]APIKeyGroupLink, error) {
	if groupID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_GROUP_ID", "group_id must be positive")
	}
	out := CloneAPIKeyGroupLinks(current)
	target := -1
	for i := range out {
		if out[i].GroupID != groupID {
			continue
		}
		if out[i].Role == APIKeyGroupRoleNormal {
			return nil, infraerrors.BadRequest("GROUP_ALREADY_NORMAL", "group is already bound as a normal group")
		}
		if out[i].Role == APIKeyGroupRoleHA {
			target = i
		}
	}
	if !enable {
		if target < 0 {
			// 关闭一个本就未开启的组：幂等无变化。
			return NormalizeAPIKeyGroupLinks(out, false)
		}
		out[target].EnabledByUserAt = nil
		return NormalizeAPIKeyGroupLinks(out, false)
	}
	if target >= 0 {
		if out[target].EnabledByUserAt != nil {
			return NormalizeAPIKeyGroupLinks(out, false)
		}
		at := now.UTC()
		out[target].EnabledByUserAt = &at
		return NormalizeAPIKeyGroupLinks(out, false)
	}
	position := 0
	for _, link := range out {
		if link.Role == APIKeyGroupRoleHA && link.Position >= position {
			position = link.Position + 1
		}
	}
	at := now.UTC()
	out = append(out, APIKeyGroupLink{GroupID: groupID, Role: APIKeyGroupRoleHA, Position: position, EnabledByUserAt: &at})
	return NormalizeAPIKeyGroupLinks(out, false)
}

func APIKeyGroupChainsEqual(a, b []APIKeyGroupLink) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GroupID != b[i].GroupID || a[i].Role != b[i].Role || a[i].Position != b[i].Position {
			return false
		}
		if (a[i].EnabledByUserAt == nil) != (b[i].EnabledByUserAt == nil) {
			return false
		}
		if a[i].EnabledByUserAt != nil && !a[i].EnabledByUserAt.Equal(*b[i].EnabledByUserAt) {
			return false
		}
	}
	return true
}

// Cache-bound pricing clones also own optional scalar prices and tier bounds.
// ChannelModelPricing.Clone on this baseline only clones container fields.
func cloneAPIKeyGroupPricing(source ChannelModelPricing) ChannelModelPricing {
	out := source.Clone()
	out.InputPrice = cloneGroupValuePointer(source.InputPrice)
	out.OutputPrice = cloneGroupValuePointer(source.OutputPrice)
	out.CacheWritePrice = cloneGroupValuePointer(source.CacheWritePrice)
	out.CacheWrite1hPrice = cloneGroupValuePointer(source.CacheWrite1hPrice)
	out.CacheReadPrice = cloneGroupValuePointer(source.CacheReadPrice)
	out.FastMultiplier = cloneGroupValuePointer(source.FastMultiplier)
	out.FlexMultiplier = cloneGroupValuePointer(source.FlexMultiplier)
	out.ImageInputPrice = cloneGroupValuePointer(source.ImageInputPrice)
	out.ImageOutputPrice = cloneGroupValuePointer(source.ImageOutputPrice)
	out.PerRequestPrice = cloneGroupValuePointer(source.PerRequestPrice)
	for i := range out.Intervals {
		out.Intervals[i].MaxTokens = cloneGroupValuePointer(source.Intervals[i].MaxTokens)
		out.Intervals[i].InputPrice = cloneGroupValuePointer(source.Intervals[i].InputPrice)
		out.Intervals[i].OutputPrice = cloneGroupValuePointer(source.Intervals[i].OutputPrice)
		out.Intervals[i].CacheWritePrice = cloneGroupValuePointer(source.Intervals[i].CacheWritePrice)
		out.Intervals[i].CacheWrite1hPrice = cloneGroupValuePointer(source.Intervals[i].CacheWrite1hPrice)
		out.Intervals[i].CacheReadPrice = cloneGroupValuePointer(source.Intervals[i].CacheReadPrice)
		out.Intervals[i].InputMultiplier = cloneGroupValuePointer(source.Intervals[i].InputMultiplier)
		out.Intervals[i].OutputMultiplier = cloneGroupValuePointer(source.Intervals[i].OutputMultiplier)
		out.Intervals[i].CacheWriteMultiplier = cloneGroupValuePointer(source.Intervals[i].CacheWriteMultiplier)
		out.Intervals[i].CacheReadMultiplier = cloneGroupValuePointer(source.Intervals[i].CacheReadMultiplier)
		out.Intervals[i].PerRequestPrice = cloneGroupValuePointer(source.Intervals[i].PerRequestPrice)
	}
	return out
}

func cloneAPIKeyGroup(source *Group) *Group {
	if source == nil {
		return nil
	}
	out := *source
	out.DailyLimitUSD = cloneGroupValuePointer(source.DailyLimitUSD)
	out.WeeklyLimitUSD = cloneGroupValuePointer(source.WeeklyLimitUSD)
	out.MonthlyLimitUSD = cloneGroupValuePointer(source.MonthlyLimitUSD)
	out.ImagePrice1K = cloneGroupValuePointer(source.ImagePrice1K)
	out.ImagePrice2K = cloneGroupValuePointer(source.ImagePrice2K)
	out.ImagePrice4K = cloneGroupValuePointer(source.ImagePrice4K)
	out.VideoPrice480P = cloneGroupValuePointer(source.VideoPrice480P)
	out.VideoPrice720P = cloneGroupValuePointer(source.VideoPrice720P)
	out.VideoPrice1080P = cloneGroupValuePointer(source.VideoPrice1080P)
	out.WebSearchPricePerCall = cloneGroupValuePointer(source.WebSearchPricePerCall)
	out.SearchPricePer1k = cloneGroupValuePointer(source.SearchPricePer1k)
	out.AudioRealtimePricePerMin = cloneGroupValuePointer(source.AudioRealtimePricePerMin)
	out.AudioTTSPricePerMillionChars = cloneGroupValuePointer(source.AudioTTSPricePerMillionChars)
	out.AudioSTTPricePerHour = cloneGroupValuePointer(source.AudioSTTPricePerHour)
	out.FallbackGroupID = cloneGroupValuePointer(source.FallbackGroupID)
	out.FallbackGroupIDOnInvalidRequest = cloneGroupValuePointer(source.FallbackGroupIDOnInvalidRequest)
	out.ModelRouting = cloneGroupModelRouting(source.ModelRouting)
	out.VideoModelPrices = cloneGroupVideoModelPrices(source.VideoModelPrices)
	out.MessagesDispatchModelConfig = cloneGroupMessagesDispatchModelConfig(source.MessagesDispatchModelConfig)
	out.SupportedModelScopes = append([]string(nil), source.SupportedModelScopes...)
	out.ModelAllowlist.Models = append([]string(nil), source.ModelAllowlist.Models...)
	out.CodexModelsManifestConfig.AccountIDs = append([]int64(nil), source.CodexModelsManifestConfig.AccountIDs...)
	out.ReasoningEffortMappings = append([]ReasoningEffortMapping(nil), source.ReasoningEffortMappings...)
	if source.ModelPricing != nil {
		out.ModelPricing = make([]ChannelModelPricing, len(source.ModelPricing))
		for i := range source.ModelPricing {
			out.ModelPricing[i] = cloneAPIKeyGroupPricing(source.ModelPricing[i])
		}
	}
	out.AccountGroups = append([]AccountGroup(nil), source.AccountGroups...)
	return &out
}

// CloneAPIKeyGroupLinks owns every returned mutable group and authorization timestamp.
func CloneAPIKeyGroupLinks(source []APIKeyGroupLink) []APIKeyGroupLink {
	out := make([]APIKeyGroupLink, len(source))
	for i, link := range source {
		out[i] = link
		out[i].EnabledByUserAt = cloneGroupValuePointer(link.EnabledByUserAt)
		out[i].Group = cloneAPIKeyGroup(link.Group)
	}
	return out
}

func NormalAPIKeyGroupLinks(groupID *int64) []APIKeyGroupLink {
	if groupID == nil || *groupID <= 0 {
		return []APIKeyGroupLink{}
	}
	return []APIKeyGroupLink{{GroupID: *groupID, Role: APIKeyGroupRoleNormal, Position: 0}}
}

func FirstNormalAPIKeyGroupID(links []APIKeyGroupLink) *int64 {
	for _, link := range links {
		if link.Role == APIKeyGroupRoleNormal && link.Position == 0 {
			id := link.GroupID
			return &id
		}
	}
	return nil
}
