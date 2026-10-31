import { describe, expect, it } from 'vitest'
import { isUsageStatsIncomplete, USAGE_STATS_INCOMPLETE_CODE } from '../usageStatsAvailability'

describe('isUsageStatsIncomplete (N-02 口径)', () => {
  it('pins the code the client interceptor emits', () => {
    expect(USAGE_STATS_INCOMPLETE_CODE).toBe('USAGE_STATS_INCOMPLETE')
  })

  it('recognises the code produced by the axios interceptor', () => {
    expect(
      isUsageStatsIncomplete({
        status: 503,
        code: 'USAGE_STATS_INCOMPLETE',
        reason: 'usage_stats_incomplete',
      }),
    ).toBe(true)
  })

  it('recognises a raw reason for call sites that bypass the interceptor', () => {
    expect(isUsageStatsIncomplete({ status: 503, reason: 'usage_stats_incomplete' })).toBe(true)
  })

  it('does not swallow real failures', () => {
    expect(isUsageStatsIncomplete(null)).toBe(false)
    expect(isUsageStatsIncomplete(undefined)).toBe(false)
    expect(isUsageStatsIncomplete({ status: 500, code: 500 })).toBe(false)
    expect(isUsageStatsIncomplete({ status: 503, code: 'ERR_NETWORK' })).toBe(false)
    expect(isUsageStatsIncomplete({ status: 503, reason: 'upstream_unavailable' })).toBe(false)
    expect(isUsageStatsIncomplete('usage_stats_incomplete')).toBe(false)
  })

  it('never treats a generic 503 as "stats incomplete"', () => {
    // 关键：503 也可能是真实上游故障，只有 reason/code 才定性。
    expect(isUsageStatsIncomplete({ status: 503 })).toBe(false)
  })
})
