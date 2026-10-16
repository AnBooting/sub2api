package domain

// 调度策略（TS-02 / PRD-02 C-4）。
//
// 单一事实源放在 domain，供 ent schema 默认值、service 校验与迁移注释共享；
// service 层再以 SchedulingStrategy 类型别名暴露给业务代码。
const (
	// SchedulingStrategyStable 稳定优先。
	SchedulingStrategyStable = "stable"
	// SchedulingStrategyBalanced 平衡（全局默认）。
	SchedulingStrategyBalanced = "balanced"
	// SchedulingStrategyLatency 响应速度优先。
	SchedulingStrategyLatency = "latency"
	// SchedulingStrategyInherit 分组级哨兵：不覆盖，随全局策略。
	SchedulingStrategyInherit = "inherit"
)

// SchedulingStrategyValues 是合法的全局策略取值。
var SchedulingStrategyValues = []string{
	SchedulingStrategyStable,
	SchedulingStrategyBalanced,
	SchedulingStrategyLatency,
}

// IsValidSchedulingStrategy 报告 value 是否为合法的全局策略。
func IsValidSchedulingStrategy(value string) bool {
	for _, candidate := range SchedulingStrategyValues {
		if candidate == value {
			return true
		}
	}
	return false
}

// IsValidGroupSchedulingStrategy 报告 value 是否为合法的分组级取值（含 inherit）。
func IsValidGroupSchedulingStrategy(value string) bool {
	return value == SchedulingStrategyInherit || IsValidSchedulingStrategy(value)
}
