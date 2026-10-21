/**
 * 价格面板共用的规则数据加载：渠道列表 + 分组列表 → 审计数据（作用域 + 候选模型）。
 *
 * 沿用 ChannelsView 的既有做法（adminAPI.channels.list(1, 1000) 一次取全量渠道），
 * 不新增任何后端接口。
 */
import { computed, ref } from 'vue'
import { adminAPI } from '@/api/admin'
import type { Channel } from '@/api/admin/channels'
import type { AdminGroup } from '@/types'
import { collectPricingAuditData } from './pricingAudit'
import type { PricingAuditData } from './pricingAudit'

export function usePricingRuleData() {
  const channels = ref<Channel[]>([])
  const groups = ref<AdminGroup[]>([])
  const loading = ref(false)
  const loadFailed = ref(false)

  async function load() {
    loading.value = true
    loadFailed.value = false
    try {
      const [channelRes, groupList] = await Promise.all([
        adminAPI.channels.list(1, 1000),
        adminAPI.groups.getAll()
      ])
      channels.value = channelRes?.items || []
      groups.value = groupList || []
    } catch {
      channels.value = []
      groups.value = []
      loadFailed.value = true
    } finally {
      loading.value = false
    }
  }

  const data = computed<PricingAuditData>(() =>
    collectPricingAuditData(channels.value, groups.value)
  )

  return { channels, groups, loading, loadFailed, data, load }
}
