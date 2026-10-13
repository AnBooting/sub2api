package service

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"golang.org/x/sync/singleflight"
)

// TS-02（PRD-02）调度策略配置骨架（C-4，project-docs/prd/11-共享契约.md:81-88）。
//
// 本文件只做「存储 / 继承解析 / 缓存 / 常量映射」，**不接线到调度执行路径**：
// 粘性、等待、错误容忍与回切行为仍由现有 Scheduler 决定，本切片不读取本表。
// 硬边界：任何策略都不得放宽并发硬上限与 C-5 分类（contract:88），本文件也不得
// 触碰 maxAccountSwitches、并发上限或故障分类。
type SchedulingStrategy string

const (
	// SchedulingStrategyStable 稳定优先：最长亲和保留、最保守的错误容忍与回切。
	SchedulingStrategyStable SchedulingStrategy = SchedulingStrategy(domain.SchedulingStrategyStable)
	// SchedulingStrategyBalanced 平衡（全局默认）：在响应速度与账号稳定之间折中。
	SchedulingStrategyBalanced SchedulingStrategy = SchedulingStrategy(domain.SchedulingStrategyBalanced)
	// SchedulingStrategyLatency 响应速度优先：最短等待、最激进的换号。
	SchedulingStrategyLatency SchedulingStrategy = SchedulingStrategy(domain.SchedulingStrategyLatency)
)

// SchedulingStrategyInherit 是分组级哨兵值：该组不覆盖，随全局策略生效。
// 只允许出现在分组字段，不允许作为全局默认。
const SchedulingStrategyInherit = domain.SchedulingStrategyInherit

// DefaultSchedulingStrategy 是 C-4/C-8 的全局默认策略（balanced）。
const DefaultSchedulingStrategy = SchedulingStrategyBalanced

// SchedulingStrategyProfile 是「策略 → 底层参数」的映射表条目。表以代码常量落地并
// 由评审固化（contract:88），不通过管理 API 暴露，站长只选择三个策略名。
//
// 字段语义（全部为后续接线切片提供只读常量，本切片不生效）：
//   - StickyReuseTTL：会话已建立的亲和账号冷却后仍优先复用的时窗。
//   - StickyWait：亲和账号满载/限流时，等待其释放容量的上限；到期即按现有
//     调度逻辑换号。等待期间仍受并发硬上限约束，不构成额外并发额度。
//   - ErrorToleranceAttempts：同一账号允许的连续可重试错误次数，达到即切换；
//     它只影响「何时换号」，不改变 C-5 分类，也不豁免 C-5 的 fail_client/
//     fail_policy/fail_uncertain 不重试约束。
//   - SwitchBackAfterRecovery：原优先账号恢复健康后，新请求是否回切原账号。
type SchedulingStrategyProfile struct {
	Strategy SchedulingStrategy

	StickyReuseTTL time.Duration
	StickyWait     time.Duration

	ErrorToleranceAttempts int

	SwitchBackAfterRecovery bool
}

// 映射表常量：stable / balanced / latency 的三档取值。
//
// 刻意用命名常量而非散落字面量，便于评审核对且避免后续接线时复制出第二套口径。
const (
	schedulingStableStickyReuseTTL = 30 * time.Minute
	schedulingStableStickyWait     = 30 * time.Second
	schedulingStableErrorTolerance = 1

	schedulingBalancedStickyReuseTTL = 10 * time.Minute
	schedulingBalancedStickyWait     = 10 * time.Second
	schedulingBalancedErrorTolerance = 2

	schedulingLatencyStickyReuseTTL = 2 * time.Minute
	schedulingLatencyStickyWait     = 2 * time.Second
	schedulingLatencyErrorTolerance = 3
)

// schedulingStrategyProfiles 是策略到参数的唯一映射表。刻意不导出：表内容属于评审
// 固化的实现常量，外部只能经 SchedulingStrategyProfileFor 取值，避免被运行时改写。
// 查表未命中的策略必须按默认策略处理，不得产生零值参数。
var schedulingStrategyProfiles = map[SchedulingStrategy]SchedulingStrategyProfile{
	SchedulingStrategyStable: {
		Strategy:                SchedulingStrategyStable,
		StickyReuseTTL:          schedulingStableStickyReuseTTL,
		StickyWait:              schedulingStableStickyWait,
		ErrorToleranceAttempts:  schedulingStableErrorTolerance,
		SwitchBackAfterRecovery: false,
	},
	SchedulingStrategyBalanced: {
		Strategy:                SchedulingStrategyBalanced,
		StickyReuseTTL:          schedulingBalancedStickyReuseTTL,
		StickyWait:              schedulingBalancedStickyWait,
		ErrorToleranceAttempts:  schedulingBalancedErrorTolerance,
		SwitchBackAfterRecovery: true,
	},
	SchedulingStrategyLatency: {
		Strategy:                SchedulingStrategyLatency,
		StickyReuseTTL:          schedulingLatencyStickyReuseTTL,
		StickyWait:              schedulingLatencyStickyWait,
		ErrorToleranceAttempts:  schedulingLatencyErrorTolerance,
		SwitchBackAfterRecovery: true,
	},
}

// ParseSchedulingStrategy 严格解析全局策略值：仅接受 stable/balanced/latency
// （大小写不敏感、忽略首尾空白）。空值与非法值返回 false，调用方决定拒绝或回落，
// 绝不在这里静默改写用户输入。管理 API 用本函数把非法值直接 400。
func ParseSchedulingStrategy(raw string) (SchedulingStrategy, bool) {
	switch SchedulingStrategy(strings.ToLower(strings.TrimSpace(raw))) {
	case SchedulingStrategyStable:
		return SchedulingStrategyStable, true
	case SchedulingStrategyBalanced:
		return SchedulingStrategyBalanced, true
	case SchedulingStrategyLatency:
		return SchedulingStrategyLatency, true
	default:
		return "", false
	}
}

// ParseGroupSchedulingStrategy 严格解析分组级值：inherit 加三个策略值。
// 空值按未配置处理，同样返回 false：分组字段有 DB 默认 inherit，空串只可能是旧行或
// 调用方未提供，不在这里自行做覆盖决定。
func ParseGroupSchedulingStrategy(raw string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == SchedulingStrategyInherit {
		return SchedulingStrategyInherit, true
	}
	strategy, ok := ParseSchedulingStrategy(trimmed)
	if !ok {
		return "", false
	}
	return string(strategy), true
}

// NormalizeSchedulingStrategy 把任意存量值收敛到一个可用策略：合法值原样返回，
// 空值/非法值回落默认 balanced。读取热路径用它保证永远拿到三档之一。
func NormalizeSchedulingStrategy(raw string) SchedulingStrategy {
	if strategy, ok := ParseSchedulingStrategy(raw); ok {
		return strategy
	}
	return DefaultSchedulingStrategy
}

// ResolveSchedulingStrategy 是有效策略解析的**单一函数**（C-4 contract:88）：
//
//	有效策略 = 分组值 != inherit ? 分组值 : 全局值
//
// 边界（本函数是唯一事实源，调用方不得自行复制这段逻辑）：
//   - 分组值合法且非 inherit：直接覆盖全局值，全局值非法也不影响。
//   - 分组值为 inherit / 空 / 非法存量值：视为随全局，返回归一化后的全局值。
//     非法存量分组值不静默当成某个具体策略（否则会把脏数据变成运维没选过的策略）。
//   - 全局值非法（含空）：回落默认 balanced。
//   - 客户端历史行为不受影响：本函数只决定策略名，不改并发上限与 C-5 分类。
func ResolveSchedulingStrategy(groupValue, globalValue string) SchedulingStrategy {
	if groupStrategy, ok := ParseSchedulingStrategy(groupValue); ok {
		return groupStrategy
	}
	return NormalizeSchedulingStrategy(globalValue)
}

// SchedulingStrategyProfileFor 返回策略对应的底层参数常量。未知策略按默认策略查表，
// 保证调用方永远拿到一套完整参数。
func SchedulingStrategyProfileFor(strategy SchedulingStrategy) SchedulingStrategyProfile {
	if profile, ok := schedulingStrategyProfiles[strategy]; ok {
		return profile
	}
	return schedulingStrategyProfiles[DefaultSchedulingStrategy]
}

type cachedSchedulingStrategy struct {
	strategy  SchedulingStrategy
	expiresAt int64 // unix nano
}

var schedulingStrategyCache atomic.Value // *cachedSchedulingStrategy
var schedulingStrategySF singleflight.Group

const schedulingStrategyCacheTTL = 60 * time.Second
const schedulingStrategyCacheErrorTTL = 5 * time.Second
const schedulingStrategyCacheDBTimeout = 5 * time.Second

// SchedulingStrategy 返回全局调度策略（C-4/C-8），带进程内缓存与 singleflight 合流，
// 与 gateway 重试预算访问器（GatewayRequestAttemptBudgetLimits）同一模式。
//
// fail-safe：s/settingRepo 为 nil、键缺省、读取失败、存量值非法，一律回落默认
// balanced；绝不把空/脏值透传给调度路径。管理 API 在写入侧拒绝非法值，这里是热路径兜底。
func (s *SettingService) SchedulingStrategy(ctx context.Context) SchedulingStrategy {
	if s == nil || s.settingRepo == nil {
		return DefaultSchedulingStrategy
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cached, ok := schedulingStrategyCache.Load().(*cachedSchedulingStrategy); ok {
		if cached != nil && cached.strategy != "" && time.Now().UnixNano() < cached.expiresAt {
			return cached.strategy
		}
	}

	result, err, _ := schedulingStrategySF.Do(SettingKeySchedulingStrategy, func() (any, error) {
		if cached, ok := schedulingStrategyCache.Load().(*cachedSchedulingStrategy); ok {
			if cached != nil && cached.strategy != "" && time.Now().UnixNano() < cached.expiresAt {
				return cached, nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), schedulingStrategyCacheDBTimeout)
		defer cancel()

		values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeySchedulingStrategy})
		if err != nil {
			slog.Warn("failed to get scheduling strategy, falling back to default", "error", err)
			entry := &cachedSchedulingStrategy{
				strategy:  DefaultSchedulingStrategy,
				expiresAt: time.Now().Add(schedulingStrategyCacheErrorTTL).UnixNano(),
			}
			schedulingStrategyCache.Store(entry)
			return entry, nil
		}

		raw := strings.TrimSpace(values[SettingKeySchedulingStrategy])
		strategy, ok := ParseSchedulingStrategy(raw)
		if !ok {
			if raw != "" {
				slog.Warn("invalid scheduling_strategy, falling back to default",
					"value", raw, "default", DefaultSchedulingStrategy)
			}
			strategy = DefaultSchedulingStrategy
		}

		entry := &cachedSchedulingStrategy{
			strategy:  strategy,
			expiresAt: time.Now().Add(schedulingStrategyCacheTTL).UnixNano(),
		}
		schedulingStrategyCache.Store(entry)
		return entry, nil
	})
	if err != nil {
		return DefaultSchedulingStrategy
	}
	if entry, ok := result.(*cachedSchedulingStrategy); ok && entry != nil && entry.strategy != "" {
		return entry.strategy
	}
	return DefaultSchedulingStrategy
}

// EffectiveSchedulingStrategy 解析某个分组在该全局策略下的有效策略。
// 分组值直接来自 groups.scheduling_strategy（或认证缓存中的同名字段）。
func (s *SettingService) EffectiveSchedulingStrategy(ctx context.Context, groupValue string) SchedulingStrategy {
	return ResolveSchedulingStrategy(groupValue, string(s.SchedulingStrategy(ctx)))
}

// DefaultSchedulingStrategySetting 返回写入 settings 表的默认值字符串。
func DefaultSchedulingStrategySetting() string {
	return string(DefaultSchedulingStrategy)
}
