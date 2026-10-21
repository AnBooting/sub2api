/**
 * 默认价卡逐模型探测的执行器。
 *
 * 约束（来自面板需求）：
 *  - 限并发：同时在途请求数不超过 MAX_CONCURRENT_PROBES
 *  - 可中断：在途请求通过 AbortController 取消；停止后不再发起新请求
 *  - 有进度：已完成数 / 总数
 *  - 失败不阻断：单个模型失败只记录该模型，其余继续
 *
 * 组件卸载时调用 stop() 即可 abort 全部在途请求。
 */
import { reactive } from 'vue'
import { adminAPI } from '@/api/admin'
import type { ModelDefaultPricing } from '@/api/admin/channels'

/** 同时在途的探测请求上限。 */
export const MAX_CONCURRENT_PROBES = 20

export type ProbeStatus = 'idle' | 'running' | 'done' | 'stopped'

export interface ProbeState {
  status: ProbeStatus
  total: number
  completed: number
  results: Record<string, ModelDefaultPricing>
  failures: Record<string, string>
}

export interface PriceCardProbe {
  state: ProbeState
  start(models: string[]): Promise<void>
  stop(): void
  reset(): void
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message
  if (typeof error === 'string' && error) return error
  return 'probe_failed'
}

export function createPriceCardProbe(): PriceCardProbe {
  const state = reactive<ProbeState>({
    status: 'idle',
    total: 0,
    completed: 0,
    results: {},
    failures: {}
  }) as ProbeState

  let controller: AbortController | null = null
  let stopped = false

  function stop() {
    stopped = true
    controller?.abort()
    controller = null
    if (state.status === 'running') state.status = 'stopped'
  }

  function reset() {
    stop()
    state.status = 'idle'
    state.total = 0
    state.completed = 0
    state.results = {}
    state.failures = {}
  }

  async function start(models: string[]): Promise<void> {
    stop()
    stopped = false
    const ctrl = new AbortController()
    controller = ctrl

    state.status = 'running'
    state.total = models.length
    state.completed = 0
    state.results = {}
    state.failures = {}

    let cursor = 0

    const worker = async () => {
      while (cursor < models.length) {
        // 已取消：不再发起新请求
        if (stopped || ctrl.signal.aborted) return
        const model = models[cursor++]
        try {
          const price = await adminAPI.channels.getModelDefaultPricing(model, { signal: ctrl.signal })
          if (stopped || ctrl.signal.aborted) return
          state.results[model] = price
        } catch (error) {
          if (stopped || ctrl.signal.aborted) return
          state.failures[model] = errorMessage(error)
        }
        state.completed++
      }
    }

    const workerCount = Math.min(MAX_CONCURRENT_PROBES, models.length)
    await Promise.all(Array.from({ length: workerCount }, () => worker()))

    state.status = stopped || ctrl.signal.aborted ? 'stopped' : 'done'
    if (controller === ctrl) controller = null
  }

  return { state, start, stop, reset }
}
