export type DomesticProviderId =
  | 'deepseek'
  | 'qwen'
  | 'doubao'
  | 'kimi'
  | 'zhipu'
  | 'minimax'
  | 'baichuan'
  | 'yi'
  | 'stepfun'
  | 'custom'

export interface DomesticProvider {
  id: DomesticProviderId
  label: string
  company: string
  baseUrl: string
  iconModel: string
}

/**
 * Single source of truth shared by account and group forms. A domestic
 * provider is display/rating metadata only; requests still use platform=openai.
 */
export const DOMESTIC_PROVIDERS: readonly DomesticProvider[] = [
  { id: 'deepseek', label: 'DeepSeek', company: '深度求索', baseUrl: 'https://api.deepseek.com', iconModel: 'deepseek' },
  { id: 'qwen', label: '通义千问', company: '阿里云百炼国际', baseUrl: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', iconModel: 'qwen' },
  { id: 'doubao', label: '豆包', company: 'BytePlus ModelArk', baseUrl: 'https://ark.ap-southeast.bytepluses.com/api/v3', iconModel: 'doubao' },
  { id: 'kimi', label: 'Kimi', company: 'Moonshot AI', baseUrl: 'https://api.moonshot.ai/v1', iconModel: 'kimi' },
  { id: 'zhipu', label: '智谱 GLM', company: 'Z.AI', baseUrl: 'https://api.z.ai/api/paas/v4/', iconModel: 'zhipu' },
  { id: 'minimax', label: 'MiniMax', company: 'MiniMax International', baseUrl: 'https://api.minimax.io/v1', iconModel: 'minimax' },
  // No verified international USD API scope is available for these providers.
  // Keep the endpoint empty so administrators must explicitly configure the
  // matching endpoint instead of mixing a mainland endpoint with USD prices.
  { id: 'baichuan', label: '百川', company: '百川智能 · 待定价', baseUrl: '', iconModel: 'baichuan' },
  { id: 'yi', label: '零一万物', company: '零一万物 · 待定价', baseUrl: '', iconModel: 'yi-large' },
  { id: 'stepfun', label: '阶跃星辰', company: 'StepFun International', baseUrl: 'https://api.stepfun.ai/v1', iconModel: 'stepfun' },
  { id: 'custom', label: '其他 OpenAI 兼容', company: '其他国内厂商', baseUrl: '', iconModel: '' }
] as const

export function getDomesticProvider(provider?: string | null): DomesticProvider | null {
  const normalized = (provider || '').trim().toLowerCase()
  return DOMESTIC_PROVIDERS.find((item) => item.id === normalized) || null
}

export function normalizeDomesticProviderId(
  provider?: string | null,
  fallback: DomesticProviderId = 'custom'
): DomesticProviderId {
  return getDomesticProvider(provider)?.id || fallback
}
