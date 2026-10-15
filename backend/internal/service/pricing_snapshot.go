package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// PricingSnapshotVersionV2 是 C-1 锁价快照的版本号。
const PricingSnapshotVersionV2 = 2

// pricingSnapshotItemJSON / pricingSnapshotPeriodJSON / pricingSnapshotMediaJSON 是
// C-1 契约的落库形态。字段名与 project-docs/prd/11-共享契约.md 及前端
// PricingSnapshotV2 类型逐字对齐，改动任一名称都会让前端读不到快照。
type pricingSnapshotItemJSON struct {
	Kind            string  `json:"kind"`
	BaseCreditPrice float64 `json:"base_credit_price"`
	Usage           float64 `json:"usage"`
	NormalizedUsage float64 `json:"normalized_usage"`
	Subtotal        float64 `json:"subtotal"`
}

type pricingSnapshotPeriodJSON struct {
	Label    string `json:"label,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Window   string `json:"window,omitempty"`
}

type pricingSnapshotMediaJSON struct {
	Mode            string  `json:"mode,omitempty"`
	BillableCount   int     `json:"billable_count"`
	BillableSeconds float64 `json:"billable_seconds"`
	SecondsSource   string  `json:"seconds_source,omitempty"`
}

// costPricingSnapshot 生成 usage_logs.pricing_snapshot。
//
// 当 cost 携带分项事实时输出 C-1 v2 形态（含 v/quota_unit/items/group_multiplier/
// total_credit），否则保持历史 v1 形态。v1 与 v2 并存是刻意的：历史行不回填，读取方
// 按版本分支（PRD 11-共享契约 C-1）。这保证任何新增字段都不会让旧行变成不可读。
func costPricingSnapshot(cost *CostBreakdown) json.RawMessage {
	if cost == nil {
		return nil
	}
	currency := cost.PricingCurrency
	if currency == "" {
		currency = "USD"
	}
	source := cost.PricingSource
	if source == "" {
		source = "default"
	}

	// v2：有分项事实（或显式声明了 v2 的倍率/媒体事实）时输出完整锁价快照。
	if len(cost.PricingItems) > 0 || cost.Media != nil {
		values := map[string]any{
			"v":                PricingSnapshotVersionV2,
			"quota_unit":       "credits",
			"currency":         currency,
			"source":           source,
			"unit":             cost.Unit,
			"total_cost":       cost.TotalCost,
			"actual_cost":      cost.ActualCost,
			"total_credit":     cost.ActualCost,
			"group_multiplier": cost.EffectiveMultiplier(),
		}
		if cost.Unit == "" {
			values["unit"] = PricingSnapshotUnitPer1MTokens
		}
		if !cost.RequestStartedAt.IsZero() {
			values["request_started_at"] = cost.RequestStartedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if cost.PriceVersion != "" {
			values["price_version"] = cost.PriceVersion
		}
		if cost.Period != nil {
			values["period"] = pricingSnapshotPeriodJSON{
				Label:    cost.Period.Label,
				Timezone: cost.Period.Timezone,
				Window:   cost.Period.Window,
			}
		}
		if cost.ServedGroupID != nil {
			values["served_group_id"] = *cost.ServedGroupID
		}
		items := make([]pricingSnapshotItemJSON, 0, len(cost.PricingItems))
		for _, item := range cost.PricingItems {
			items = append(items, pricingSnapshotItemJSON{
				Kind:            item.Kind,
				BaseCreditPrice: item.BaseCreditPrice,
				Usage:           item.Usage,
				NormalizedUsage: item.NormalizedUsage,
				Subtotal:        item.Subtotal,
			})
		}
		values["items"] = items
		if cost.Media != nil {
			values["media"] = pricingSnapshotMediaJSON{
				Mode:            cost.Media.Mode,
				BillableCount:   cost.Media.BillableCount,
				BillableSeconds: cost.Media.BillableSeconds,
				SecondsSource:   cost.Media.SecondsSource,
			}
		}
		if p := cost.PricingSnapshot; p != nil {
			values["input_per_token"] = p.InputPricePerToken
			values["output_per_token"] = p.OutputPricePerToken
			values["cache_read_per_token"] = p.CacheReadPricePerToken
			values["cache_write_per_token"] = p.CacheCreationPricePerToken
			values["cache_write_1h_per_token"] = p.CacheCreation1hPrice
			if p.NativeCard != nil {
				values["fetched_at"] = p.NativeCard.FetchedAt
				values["provider"] = p.NativeCard.Provider
			}
		}
		if body, err := json.Marshal(values); err == nil {
			return body
		}
		// 序列化失败时退回 v1，绝不写入半成品快照。
	}

	values := map[string]any{"currency": currency, "source": source, "quota_unit": "credits", "total_cost": cost.TotalCost, "actual_cost": cost.ActualCost}
	if p := cost.PricingSnapshot; p != nil {
		values["input_per_token"] = p.InputPricePerToken
		values["output_per_token"] = p.OutputPricePerToken
		values["cache_read_per_token"] = p.CacheReadPricePerToken
		values["cache_write_per_token"] = p.CacheCreationPricePerToken
		values["cache_write_1h_per_token"] = p.CacheCreation1hPrice
		if p.NativeCard != nil {
			values["fetched_at"] = p.NativeCard.FetchedAt
			values["provider"] = p.NativeCard.Provider
		}
	}
	body, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return body
}

// nativeCNYPriceVersion 构造 C-2 的 price_version：来源 + 拉取时间 + 内容哈希前缀。
// 形如 "cny:2026-10-10T00:00:00Z#a1b2"。版本标识只用于让账单可追溯「当时用的是哪一版
// 价表」，因此用拉取时间与内容哈希即可，无需引入新的持久化字段。
func nativeCNYPriceVersion(pricing *ModelPricing) string {
	if pricing == nil || pricing.NativePricingAt.IsZero() {
		return ""
	}
	source := pricing.PricingSource
	if source == "" {
		source = "native_cny"
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%.12g|%.12g",
		source, pricing.NativePricingAt.UTC().Format(time.RFC3339),
		pricing.InputPricePerToken, pricing.OutputPricePerToken)))
	return fmt.Sprintf("%s:%s#%s", source, pricing.NativePricingAt.UTC().Format(time.RFC3339), hex.EncodeToString(digest[:2]))
}

// mediaBillingModeForSnapshot 把结算模式映射为 C-1 media.mode。
// 契约只定义 per_request / per_second 两个值；token 计费的视频不写 media。
func mediaBillingModeForSnapshot(cost *CostBreakdown) string {
	if cost == nil {
		return ""
	}
	switch cost.BillingMode {
	case string(BillingModePerRequest):
		return "per_request"
	case string(BillingModeVideo):
		return "per_second"
	default:
		return ""
	}
}

// videoSecondsSource 标注 C-1 media.seconds_source：时长来自上游状态查询还是请求本身。
//
// 判定依据是上游状态响应里是否真的带了 duration/frames（Seedance status body 的采集
// 路径会置位 VideoDurationFromStatus）。未采集到时如实标注 "request"，不猜测、不默认成
// upstream——seconds_source 的全部意义就是让账单能区分这两种来源。
func videoSecondsSource(result *OpenAIForwardResult) string {
	if result == nil || result.VideoDurationSeconds <= 0 {
		return ""
	}
	if result.VideoDurationFromStatus {
		return "upstream"
	}
	return "request"
}

