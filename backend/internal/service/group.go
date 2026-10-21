package service

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

type OpenAIMessagesDispatchModelConfig = domain.OpenAIMessagesDispatchModelConfig
type GroupCodexModelsManifestConfig = domain.GroupCodexModelsManifestConfig
type ReasoningEffortMapping = domain.ReasoningEffortMapping

type Group struct {
	ID             int64
	Name           string
	Description    string
	Platform       string
	RateMultiplier float64
	// 高峰时段倍率：peak_rate_enabled 为 true 且当前时刻处于 [PeakStart, PeakEnd) 时，
	// token 计费倍率额外乘以 PeakRateMultiplier。详见 PeakMultiplierAt。
	PeakRateEnabled    bool
	PeakStart          string
	PeakEnd            string
	PeakRateMultiplier float64
	IsExclusive        bool
	Status             string
	Hydrated           bool // indicates the group was loaded from a trusted repository source
	// DuplicateOperationID is internal persistence metadata used only to recover
	// an already committed one-click copy. It must never be mapped to API DTOs.
	DuplicateOperationID string

	SubscriptionType    string
	DailyLimitUSD       *float64
	WeeklyLimitUSD      *float64
	MonthlyLimitUSD     *float64
	DefaultValidityDays int

	// 图片生成计费配置（antigravity 和 gemini 平台使用）
	AllowImageGeneration         bool
	AllowBatchImageGeneration    bool
	ImageRateIndependent         bool
	ImageRateMultiplier          float64
	ImagePrice1K                 *float64
	ImagePrice2K                 *float64
	ImagePrice4K                 *float64
	BatchImageDiscountMultiplier float64
	BatchImageHoldMultiplier     float64
	VideoRateIndependent         bool
	VideoRateMultiplier          float64
	VideoPrice480P               *float64
	VideoPrice720P               *float64
	VideoPrice1080P              *float64
	// VideoModelPrices is optional per-model-family per-second pricing
	// (groups.video_model_prices JSONB). Shape: family → resolution → USD/s.
	// When set for a model, overrides VideoPrice* for that model only.
	VideoModelPrices map[string]map[string]float64
	// VideoPricePlans declares, per model family, the metering mode and unit price
	// (groups.video_price_plans JSONB, PRD-06/K-4). Unlike VideoModelPrices this is
	// not implicitly per-second: Ark Seedance bills per token, xAI Grok per second
	// and MiniMax per request. An absent plan means "not configured" and callers
	// must fail closed rather than guess a tariff.
	VideoPricePlans map[string]domain.VideoPricePlan
	// Codex alpha/search 网页搜索单次价格（USD/次，仅 openai 平台使用）；
	// nil 表示使用默认价 defaultWebSearchPricePerCall（官方 $10/1000 次）。
	WebSearchPricePerCall *float64

	// 搜索工具显式定价（per 1k calls）。
	SearchPricePer1k *float64
	// Grok Voice 显式定价（分组级，不按文本 RateMultiplier）。
	AudioRealtimePricePerMin     *float64
	AudioTTSPricePerMillionChars *float64
	AudioSTTPricePerHour         *float64

	// ModelPricing overrides channel and built-in prices for matching models.
	// Token intervals are selected only when LongContextPricingEnabled is true.
	LongContextPricingEnabled bool
	ModelPricing              []ChannelModelPricing

	// Claude Code 客户端限制
	ClaudeCodeOnly  bool
	FallbackGroupID *int64
	// 无效请求兜底分组（仅 anthropic 平台使用）
	FallbackGroupIDOnInvalidRequest *int64

	// 模型路由配置
	// key: 模型匹配模式（支持 * 通配符，如 "claude-opus-*"）
	// value: 优先账号 ID 列表
	ModelRouting        map[string][]int64
	ModelRoutingEnabled bool

	// MCP XML 协议注入开关（仅 antigravity 平台使用）
	MCPXMLInject bool

	// 支持的模型系列（仅 antigravity 平台使用）
	// 可选值: claude, gemini_text, gemini_image
	SupportedModelScopes []string

	// 分组排序
	SortOrder int

	// OpenAI Messages 调度配置（仅 openai 平台使用）
	AllowMessagesDispatch       bool
	AllowLive                   bool
	ForceOpenAIFast             bool // 强制 OpenAI 网关请求使用 service_tier=priority
	FreeOpenAIFast              bool // OpenAI Fast 请求按 Standard 价格向用户计费
	RequireOAuthOnly            bool // 仅允许非 apikey 类型账号关联（OpenAI/Antigravity/Anthropic/Gemini）
	RequirePrivacySet           bool // 调度时仅允许 privacy 已成功设置的账号（OpenAI/Antigravity/Anthropic/Gemini）
	DefaultMappedModel          string
	MessagesDispatchModelConfig OpenAIMessagesDispatchModelConfig
	ModelAllowlist              GroupModelAllowlist
	// CodexModelsManifestConfig 开启后，普通模型列表与 Codex manifest 优先使用
	// 固定账号列表拉取并合并，不经过调度器（仅 openai 平台）。
	CodexModelsManifestConfig GroupCodexModelsManifestConfig

	// RPMLimit 分组级每分钟请求数上限（0 = 不限制）。
	// 一旦设置即接管该分组用户的限流（覆盖用户级 rpm_limit），可被 user-group rpm_override 进一步覆盖。
	RPMLimit int

	// MaxReasoningEffort limits the effective Anthropic/OpenAI reasoning effort.
	// Empty means unlimited; supported values are minimal/low/medium/high/xhigh/max.
	MaxReasoningEffort string
	// MaxReasoningEffortOverLimit is the access control when an explicit effort
	// exceeds the ceiling: downgrade (default) or deny.
	MaxReasoningEffortOverLimit string
	// ReasoningEffortMappings rewrites explicit request values before applying the ceiling.
	ReasoningEffortMappings []ReasoningEffortMapping

	// 分组利润控制（五个 token 计费平台可启用）。
	// 调度准入条件：账号倍率 U 满足 U <= D*(1-margin-buffer)，
	// D 为请求用户当刻有效下游倍率（用户覆盖 ?? 分组默认，再乘高峰因子）。
	// 只过滤候选账号，不改变既有排序/评分/粘性/熔断。
	ProfitControlEnabled bool
	ProfitMinMargin      float64 // 最低毛利率，小数存储（0.30=30%）
	ProfitSafetyBuffer   float64 // 安全缓冲，小数，与 margin 相加后从 D 中扣除

	// SchedulingStrategy 分组级调度策略覆盖（C-4）：inherit（默认，随全局）
	// 或 stable/balanced/latency。只决定调度策略名，不改并发硬上限与 C-5 分类。
	SchedulingStrategy string

	// AllowHighAvailability marks this group as usable as a user-authorized
	// high-availability fallback (PRD-04/TS-04). The admin decides which groups may
	// serve that role; the user still has to opt in per API key (EnabledByUserAt).
	// Supported models reuse ModelAllowlist and the multiplier reuses
	// RateMultiplier, so no additional admin surface is needed.
	AllowHighAvailability bool

	CreatedAt time.Time
	UpdatedAt time.Time

	AccountGroups           []AccountGroup
	AccountCount            int64
	ActiveAccountCount      int64
	RateLimitedAccountCount int64
}

// IsGroupBindableInSimpleMode is the shared policy for groups that may be
// surfaced and bound to accounts while running in simple mode.
func IsGroupBindableInSimpleMode(group *Group) bool {
	return group != nil && group.Platform != PlatformComposite
}

func (g *Group) IsActive() bool {
	return g.Status == StatusActive
}

func (g *Group) IsSubscriptionType() bool {
	return g.SubscriptionType == SubscriptionTypeSubscription
}

func (g *Group) HasDailyLimit() bool {
	return g.DailyLimitUSD != nil && *g.DailyLimitUSD > 0
}

func (g *Group) HasWeeklyLimit() bool {
	return g.WeeklyLimitUSD != nil && *g.WeeklyLimitUSD > 0
}

func (g *Group) HasMonthlyLimit() bool {
	return g.MonthlyLimitUSD != nil && *g.MonthlyLimitUSD > 0
}

// GetImagePrice 根据 image_size 返回对应的图片生成价格
// 如果分组未配置价格，返回 nil（调用方应使用默认值）
func (g *Group) GetImagePrice(imageSize string) *float64 {
	switch imageSize {
	case "1K":
		return g.ImagePrice1K
	case "2K":
		return g.ImagePrice2K
	case "4K":
		return g.ImagePrice4K
	default:
		// 未知尺寸默认按 2K 计费
		return g.ImagePrice2K
	}
}

// GetVideoPrice 根据 resolution 返回对应的视频生成价格。
// 如果分组未配置价格，返回 nil（调用方应使用默认值）。
func (g *Group) GetVideoPrice(resolution string) *float64 {
	switch NormalizeVideoBillingResolutionOrDefault(resolution) {
	case VideoBillingResolution480P:
		return g.VideoPrice480P
	case VideoBillingResolution720P:
		return g.VideoPrice720P
	case VideoBillingResolution1080P:
		return g.VideoPrice1080P
	default:
		return g.VideoPrice480P
	}
}

// GetVideoPriceForModel prefers VideoModelPrices for the model family, then flat columns.
func (g *Group) GetVideoPriceForModel(model, resolution string) *float64 {
	if g == nil {
		return nil
	}
	if price := LookupVideoModelPrice(g.VideoModelPrices, model, resolution); price != nil {
		return price
	}
	return g.GetVideoPrice(resolution)
}

// VideoPriceConfig builds billing config including optional per-model map.
func (g *Group) VideoPriceConfig() *VideoPriceConfig {
	if g == nil {
		return nil
	}
	return &VideoPriceConfig{
		Price480P:   g.VideoPrice480P,
		Price720P:   g.VideoPrice720P,
		Price1080P:  g.VideoPrice1080P,
		ModelPrices: NormalizeVideoModelPrices(g.VideoModelPrices),
	}
}

// IsGroupContextValid reports whether a group from context has the fields required for routing decisions.
func IsGroupContextValid(group *Group) bool {
	if group == nil {
		return false
	}
	if group.ID <= 0 {
		return false
	}
	if !group.Hydrated {
		return false
	}
	if group.Platform == "" || group.Status == "" {
		return false
	}
	return true
}

// GetRoutingAccountIDs 根据请求模型获取路由账号 ID 列表
// 返回匹配的优先账号 ID 列表，如果没有匹配规则则返回 nil
func (g *Group) GetRoutingAccountIDs(requestedModel string) []int64 {
	if !g.ModelRoutingEnabled || len(g.ModelRouting) == 0 || requestedModel == "" {
		return nil
	}

	// 1. 精确匹配优先
	if accountIDs, ok := g.ModelRouting[requestedModel]; ok && len(accountIDs) > 0 {
		return accountIDs
	}

	// 2. 通配符匹配（前缀匹配）
	for pattern, accountIDs := range g.ModelRouting {
		if matchModelPattern(pattern, requestedModel) && len(accountIDs) > 0 {
			return accountIDs
		}
	}

	return nil
}

// matchModelPattern 检查模型是否匹配模式
// 支持 * 通配符，如 "claude-opus-*" 匹配 "claude-opus-4-20250514"
func matchModelPattern(pattern, model string) bool {
	if pattern == model {
		return true
	}

	// 处理 * 通配符（仅支持末尾通配符）
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(model, prefix)
	}

	return false
}

// parseMinutes 把 "HH:MM" 解析为当日分钟数（0..1439），格式非法返回 (0,false)。
// 手工解析而非 time.Parse：本函数位于每请求的计费热路径（PeakMultiplierAt），
// 避免对静态配置字符串重复走 layout 解析与 time.Time 分配。
// 接受集与 time.Parse("15:04", s) 完全一致（存量数据按旧解析写入，不得收窄）：
// 小时 1–2 位数字（0..23，允许不补零如 "1:30"），分钟固定 2 位数字（00..59）。
func parseMinutes(hhmm string) (int, bool) {
	colon := strings.IndexByte(hhmm, ':')
	if (colon != 1 && colon != 2) || len(hhmm)-colon-1 != 2 {
		return 0, false
	}
	h := 0
	for i := 0; i < colon; i++ {
		d := hhmm[i] - '0'
		if d > 9 {
			return 0, false
		}
		h = h*10 + int(d)
	}
	m1, m2 := hhmm[colon+1]-'0', hhmm[colon+2]-'0'
	if m1 > 9 || m2 > 9 {
		return 0, false
	}
	m := int(m1)*10 + int(m2)
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// peakWindow 是自定义峰谷窗口在某个时刻的判定结果。
type peakWindow struct {
	// Configured 表示分组配置了合法窗口（与计费口径同一门槛）。
	Configured bool
	// StartMinutes / EndMinutes 是窗口的当日分钟数（左闭右开）。
	StartMinutes int
	EndMinutes   int
	// Active 表示 now 落在窗口内。
	Active bool
}

// customPeakWindowAt 判定分组自定义峰谷窗口在 now 时刻的状态。
//
// PeakMultiplierAt（计费热路径）与 DeepSeekPricingStateAt（只读展示）共用它：
// 窗口判定只有这一处口径，避免出现「展示说在峰段、计费按谷价」这种分叉。
func (g *Group) customPeakWindowAt(now time.Time) peakWindow {
	if g == nil || !g.IsSubscriptionType() || !g.PeakRateEnabled || g.PeakStart == "" || g.PeakEnd == "" {
		return peakWindow{}
	}
	start, ok1 := parseMinutes(g.PeakStart)
	end, ok2 := parseMinutes(g.PeakEnd)
	if !ok1 || !ok2 || start >= end {
		return peakWindow{}
	}
	t := now.In(timezone.Location())
	cur := t.Hour()*60 + t.Minute()
	return peakWindow{
		Configured:   true,
		StartMinutes: start,
		EndMinutes:   end,
		Active:       cur >= start && cur < end,
	}
}

// PeakMultiplierAt 返回指定时刻 now 的高峰因子。
//   - 未启用 / 未配置 / 配置非法（start>=end 或格式错误） / 非高峰时段 → 返回 1.0（安全降级）
//   - 区间为左闭右开 [PeakStart, PeakEnd)，仅支持当日区间，不支持跨天（如 22:00-次日02:00）
//   - 时刻基于全局系统时区（timezone.Location）判定
//
// 该方法是纯函数，不读取任何外部状态，便于单测。
func (g *Group) PeakMultiplierAt(now time.Time) float64 {
	if !g.customPeakWindowAt(now).Active {
		return 1.0
	}
	return g.PeakRateMultiplier
}

// ---------------------------------------------------------------------------
// DeepSeek 峰谷状态（只读暴露）
// ---------------------------------------------------------------------------

// DeepSeek 峰谷状态的取值。
const (
	// DeepSeekPricingStatePeak 当前处于峰价窗口。
	DeepSeekPricingStatePeak = "peak"
	// DeepSeekPricingStateValley 当前处于谷价。
	DeepSeekPricingStateValley = "valley"
	// DeepSeekPricingStateNone 该分组不适用峰谷计价。
	DeepSeekPricingStateNone = "none"
)

// DeepSeekPricingTimezone 是官方峰谷判定的时区（与 deepseek_calendar.go 一致）。
const DeepSeekPricingTimezone = "Asia/Shanghai"

// deepseekOfficialPeakWindow 是官方峰价窗口说明（工作日，北京时间）。
const deepseekOfficialPeakWindow = "09:00-12:00 / 14:00-18:00"

// deepseekOfficialPeakWindowLabel 是官方峰价窗口的可读说明。
const deepseekOfficialPeakWindowLabel = "峰价时段 " + deepseekOfficialPeakWindow

// DeepSeekPricingState 描述分组当前的峰谷计价状态。
//
// 只读暴露：不参与计费，也不写任何状态；计费仍由 PeakMultiplierAt 与
// billing_service 的 DeepSeek 价卡口径决定。
type DeepSeekPricingState struct {
	// State 取值：peak / valley / none。
	State string
	// Label 是人类可读说明（峰/谷 + 生效窗口）。
	Label string
	// Timezone 固定为 Asia/Shanghai。
	Timezone string
	// Multiplier 是当前生效的峰谷倍率；不适用时为 1。
	Multiplier float64
	// NextSwitchAt 是下一次峰/谷状态切换的时刻（RFC3339 语义）。
	// 它与 State 来自同一次判定（同一份日历、同一套窗口规则），
	// 因此「到点切换」与「计费档位」必然自洽。
	// state=none（不适用）时为 nil。
	NextSwitchAt *time.Time
}

// DeepSeekPricingStateAt 返回分组当前的峰谷状态。
//
// 判定完全复用既有实现，不另写一套口径：
//   - 分组自定义了峰谷窗口（peak_rate_enabled）：用 PeakMultiplierAt 的同一判定，
//     Label 反映**自定义**窗口；
//   - 否则，若是 DeepSeek 平台分组（模型走 DeepSeek 官方价卡）：
//     用 deepseekPeakMultiplierWithCalendar（工作日 09:00-12:00 / 14:00-18:00，
//     法定假日日历优先）；
//   - 其余分组不适用：state=none、multiplier=1。
func (g *Group) DeepSeekPricingStateAt(now time.Time) *DeepSeekPricingState {
	state := &DeepSeekPricingState{
		State:      DeepSeekPricingStateNone,
		Label:      "本分组不适用 DeepSeek 官方峰谷",
		Timezone:   DeepSeekPricingTimezone,
		Multiplier: 1,
	}
	if g == nil {
		return state
	}

	if window := g.customPeakWindowAt(now); window.Configured {
		if window.Active {
			state.State = DeepSeekPricingStatePeak
			state.Multiplier = g.PeakRateMultiplier
			state.Label = fmt.Sprintf("峰价时段 %s-%s（自定义）", g.PeakStart, g.PeakEnd)
		} else {
			state.State = DeepSeekPricingStateValley
			state.Label = fmt.Sprintf("谷价（自定义峰价时段 %s-%s 之外）", g.PeakStart, g.PeakEnd)
		}
		if next, ok := g.customPeakWindowNextSwitchAt(now); ok {
			state.NextSwitchAt = &next
		}
		return state
	}

	if !g.appliesDeepseekOfficialPeak() {
		return state
	}
	calendar := deepseekPricingCalendar()
	state.State, state.Label, state.Multiplier = deepseekOfficialStateAt(now, calendar)
	if next, ok := deepseekOfficialNextSwitchAt(now, calendar); ok {
		state.NextSwitchAt = &next
	}
	return state
}

// appliesDeepseekOfficialPeak 判断该分组的模型是否走 DeepSeek 官方价卡。
//
// 官方峰谷价只作用在 DeepSeek 官方模型的默认价卡上（见 billing_service 的
// applyModelSpecificPricingPolicyEx），因此只有 DeepSeek 平台分组才适用；
// 其它平台分组不暴露峰谷状态（state=none）。
func (g *Group) appliesDeepseekOfficialPeak() bool {
	return g != nil && g.Platform == PlatformDeepseek
}

// deepseekPricingCalendarProvider 返回当前生效的 DeepSeek 假日日历。
//
// 为什么用进程级 provider：分组 DTO 的映射发生在 handler 层（dto/mappers.go），
// 那条路径拿不到 PricingService 实例；而计费路径用的是同一个 PricingService 的日历，
// 两边必须看到同一份日历，否则「当前峰/谷」会和实际计费对不上。
// 未注册时回落到内置日历（defaultDeepseekCalendar）。
var deepseekPricingCalendarProvider atomic.Pointer[func() *deepseekHolidayCalendar]

// setDeepseekPricingCalendarProvider 注册当前生效的 DeepSeek 假日日历来源（装配期调用一次）。
func setDeepseekPricingCalendarProvider(provider func() *deepseekHolidayCalendar) {
	if provider == nil {
		deepseekPricingCalendarProvider.Store(nil)
		return
	}
	deepseekPricingCalendarProvider.Store(&provider)
}

// deepseekPricingCalendar 返回当前生效的 DeepSeek 假日日历（未注册时用内置日历）。
func deepseekPricingCalendar() *deepseekHolidayCalendar {
	if provider := deepseekPricingCalendarProvider.Load(); provider != nil {
		if calendar := (*provider)(); calendar != nil {
			return calendar
		}
	}
	return defaultDeepseekCalendar
}

// ValidatePeakRateConfig 是高峰倍率配置的唯一校验来源，供 handler 与 service 层共用。
// enabled=true 时仅允许订阅类型分组；并要求 start/end 合法且 end>start（不支持跨天），multiplier>=0。
// multiplier=0 是允许的，表示高峰 token 请求按 0 倍计费，可用于折扣/免费策略。
// enabled=false 时放行（不关心类型）。subscriptionType 为空按 standard 处理。
func ValidatePeakRateConfig(subscriptionType string, enabled bool, start, end string, multiplier float64) error {
	if !enabled {
		return nil
	}
	if subscriptionType != SubscriptionTypeSubscription {
		return errors.New("高峰时段倍率仅支持订阅类型分组")
	}
	if start == "" || end == "" {
		return errors.New("peak_rate_enabled 为 true 时 peak_start 与 peak_end 必填")
	}
	st, okStart := parseMinutes(start)
	if !okStart {
		return fmt.Errorf("peak_start 格式应为 HH:MM，got %q", start)
	}
	en, okEnd := parseMinutes(end)
	if !okEnd {
		return fmt.Errorf("peak_end 格式应为 HH:MM，got %q", end)
	}
	if st >= en {
		return errors.New("peak_end 必须大于 peak_start（不支持跨天区间，如 22:00-02:00）")
	}
	if multiplier < 0 {
		return errors.New("peak_rate_multiplier 不能为负")
	}
	return nil
}

// NormalizePeakRateConfig 归一化最终落库的高峰配置，CreateGroup 与 UpdateGroup 两条写路径共用（唯一收口）：
//   - 非订阅类型分组不携带任何高峰配置，一律清空（enabled=false、窗口置空、倍率归 1.0）；
//   - 订阅分组关闭高峰时保留已配置的合法窗口（便于临时停用后再启用），
//     但清掉无法解析的脏字符串与负倍率，避免脏数据入库。
//
// 与 ValidatePeakRateConfig 的分工：enabled=true 时校验已保证各字段合法，本函数为无操作；
// enabled=false 时校验放行，由本函数兜底清洗。调用顺序为先归一化、后校验，
// 使"订阅转标准"这类更新能静默清空高峰配置而不是被校验拒绝。
func NormalizePeakRateConfig(subscriptionType string, enabled bool, start, end string, multiplier float64) (bool, string, string, float64) {
	if subscriptionType != SubscriptionTypeSubscription {
		return false, "", "", 1.0
	}
	if !enabled {
		if _, ok := parseMinutes(start); !ok {
			start = ""
		}
		if _, ok := parseMinutes(end); !ok {
			end = ""
		}
		if multiplier < 0 {
			multiplier = 1.0
		}
	}
	return enabled, start, end, multiplier
}

// computePeakAwareMultipliers 把"基础 token 倍率 base"（已含系统/分组/用户级倍率，但不含高峰）
// 拆分为最终 token 倍率与图片按次倍率：图片按次倍率基于 base 现算、不受高峰影响；token 倍率在 base 上叠加高峰因子。
// gateway_service.recordUsageCore 与 openai_gateway_service.RecordUsage 共用此函数，
// 锁死"高峰因子只乘入 token 倍率、图片按次倍率不受影响"这一叠加顺序——任何调换都会被 group_peak_rate_test 覆盖。
func computePeakAwareMultipliers(apiKey *APIKey, base float64, now time.Time) (text, image float64) {
	image = resolveImageRateMultiplier(apiKey, base)
	peak := 1.0
	if apiKey != nil && apiKey.Group != nil {
		peak = apiKey.Group.PeakMultiplierAt(now)
	}
	text = base * peak
	return
}

// validProfitControlRatio 判定 margin/buffer 是否为可落库的合法小数：[0,1) 且非 NaN/Inf。
func validProfitControlRatio(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v < 1
}

// NormalizeGroupPlatform 把创建分组时省略的 platform 归一化为默认平台。
// handler 的入参预校验必须与 CreateGroup 落库时用同一个归一化结果，否则
// 「省略 platform + 启用利润控制」会被 handler 以「平台不支持」400 掉，
// 而该分组本会被建成受支持的 anthropic 分组。
func NormalizeGroupPlatform(platform string) string {
	if platform == "" {
		return PlatformAnthropic
	}
	return platform
}

// ValidateProfitControlConfig 是分组利润控制配置的唯一校验来源，handler 与 service 层共用。
// enabled=true 时仅允许五个可计费平台分组；margin/buffer 各自 ∈ [0,1)，且 margin+buffer < 1
// （相加 >=1 时阈值 <=0，所有可核价账号都会被排除，视为配置错误而不是静默全黑）。
// enabled=false 时放行（不关心平台），由 Normalize 兜底清洗数值。
func ValidateProfitControlConfig(platform string, enabled bool, minMargin, safetyBuffer float64) error {
	if !enabled {
		return nil
	}
	if !profitControlPlatformSupported(platform) {
		return errors.New("利润控制仅支持 openai、anthropic、gemini、grok、antigravity 平台分组")
	}
	if !validProfitControlRatio(minMargin) {
		return fmt.Errorf("profit_min_margin 应为 [0,1) 的小数，got %v", minMargin)
	}
	if !validProfitControlRatio(safetyBuffer) {
		return fmt.Errorf("profit_safety_buffer 应为 [0,1) 的小数，got %v", safetyBuffer)
	}
	if minMargin+safetyBuffer >= 1 {
		return errors.New("profit_min_margin 与 profit_safety_buffer 之和必须小于 1，否则将排除全部账号")
	}
	return nil
}

// NormalizeProfitControlConfig 归一化最终落库的利润控制配置，CreateGroup 与 UpdateGroup 共用（唯一收口）：
//   - 非五个平台分组不携带利润控制，一律重置为默认（关、0、0）；
//   - 支持平台关闭开关时保留合法数值（便于再次启用），清洗 NaN/Inf/越界脏值。
//
// 与 ValidateProfitControlConfig 的分工同高峰倍率：先归一化、后校验，
// 使"openai 转其他平台"这类更新能静默清空利润配置而不是被校验拒绝。
func NormalizeProfitControlConfig(platform string, enabled bool, minMargin, safetyBuffer float64) (bool, float64, float64) {
	if !profitControlPlatformSupported(platform) {
		return false, 0, 0
	}
	if !enabled {
		if !validProfitControlRatio(minMargin) {
			minMargin = 0
		}
		if !validProfitControlRatio(safetyBuffer) {
			safetyBuffer = 0
		}
	}
	return enabled, minMargin, safetyBuffer
}

func profitControlPlatformSupported(platform string) bool {
	switch platform {
	case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok, PlatformAntigravity:
		return true
	default:
		return false
	}
}

// GetSearchPricePer1k returns explicit search/tool price per 1k calls if configured.
func (g *Group) GetSearchPricePer1k() *float64 {
	if g == nil {
		return nil
	}
	return g.SearchPricePer1k
}
