/**
 * IP geolocation lookup.
 *
 * Disabled by default: the frontend must not send user IPs to a third party.
 * To enable, set VITE_IP_GEO_PROXY_URL to a same-deployment backend proxy
 * endpoint that accepts ?ip=... (comma-separated for batch requests) and
 * returns the geojs-compatible shape below. Without this variable no network
 * request is made.
 */

import { reactive } from 'vue'

export type IpGeoStatus = 'idle' | 'loading' | 'success' | 'error' | 'private'

export interface IpGeoDetail {
  countryCode?: string
  region?: string
  city?: string
  organization?: string
  timezone?: string
  accuracy?: number
  latitude?: string
  longitude?: string
}

export interface IpGeoEntry {
  status: IpGeoStatus
  label?: string
  detail?: IpGeoDetail
  fetchedAt?: number
}

const IDLE_ENTRY: IpGeoEntry = { status: 'idle' }
const CACHE_STORAGE_KEY = 'sub2api:ip-geo-cache:v1'
const CACHE_TTL_MS = 24 * 60 * 60 * 1000
const BATCH_CHUNK_SIZE = 50
const GEO_PROXY_ENV_KEY = 'VITE_IP_GEO_PROXY_URL'

const configuredGeoProxyBase = (
  (import.meta.env as Record<string, string | undefined>)[GEO_PROXY_ENV_KEY] ?? ''
).trim().replace(/\/+$/, '')

interface StoredEntry {
  label: string
  detail?: IpGeoDetail
  fetchedAt: number
}

const cache = reactive(new Map<string, IpGeoEntry>())

function isFreshSuccess(entry: IpGeoEntry | undefined): entry is IpGeoEntry & { status: 'success'; fetchedAt: number } {
  return entry?.status === 'success' && typeof entry.fetchedAt === 'number' && Date.now() - entry.fetchedAt <= CACHE_TTL_MS
}

function getFreshEntry(ip: string): IpGeoEntry | undefined {
  const entry = cache.get(ip)
  if (entry?.status === 'success' && !isFreshSuccess(entry)) {
    cache.delete(ip)
    persistToStorage()
    return undefined
  }
  return entry
}

function buildProxyQuery(proxyBase: string, ips: string[]): string {
  const query = 'ip=' + ips.map(encodeURIComponent).join(',')
  return proxyBase.includes('?') ? proxyBase + '&' + query : proxyBase + '?' + query
}

export function isPrivateIp(ip: string): boolean {
  const v4 = ip.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  if (v4) {
    const a = Number(v4[1])
    const b = Number(v4[2])
    if (a === 10) return true
    if (a === 127) return true
    if (a === 169 && b === 254) return true
    if (a === 172 && b >= 16 && b <= 31) return true
    if (a === 192 && b === 168) return true
    return false
  }
  const lower = ip.toLowerCase()
  if (lower === '::1') return true
  const firstSegment = lower.split(':', 1)[0]
  if (/^fe[89ab][0-9a-f]$/.test(firstSegment)) return true
  if (/^f[cd][0-9a-f]{2}$/.test(firstSegment)) return true
  return false
}

function loadFromStorage(): void {
  try {
    const raw = localStorage.getItem(CACHE_STORAGE_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw) as Record<string, StoredEntry>
    const now = Date.now()
    for (const [ip, stored] of Object.entries(parsed)) {
      if (!stored || typeof stored.fetchedAt !== 'number') continue
      if (now - stored.fetchedAt > CACHE_TTL_MS) continue
      cache.set(ip, { status: 'success', label: stored.label, detail: stored.detail, fetchedAt: stored.fetchedAt })
    }
  } catch {
    // 忽略损坏的本地缓存
  }
}

function persistToStorage(): void {
  try {
    const toStore: Record<string, StoredEntry> = {}
    for (const [ip, entry] of cache.entries()) {
      if (entry.status === 'success' && entry.label && entry.fetchedAt) {
        toStore[ip] = { label: entry.label, detail: entry.detail, fetchedAt: entry.fetchedAt }
      }
    }
    localStorage.setItem(CACHE_STORAGE_KEY, JSON.stringify(toStore))
  } catch {
    // 存储写入失败（如隐私模式禁用 localStorage）不影响功能
  }
}

loadFromStorage()

export function getEntry(ip: string): IpGeoEntry {
  return getFreshEntry(ip) ?? IDLE_ENTRY
}

export function formatGeoLabel(detail: IpGeoDetail): string {
  const parts = [detail.countryCode, detail.region, detail.city].filter(
    (part): part is string => Boolean(part && part.trim())
  )
  return parts.join(' · ')
}

interface RawGeoResponse {
  ip: string
  country_code?: string
  region?: string
  city?: string
  organization?: string
  timezone?: string
  accuracy?: number
  latitude?: string
  longitude?: string
}

function toDetail(raw: RawGeoResponse): IpGeoDetail {
  return {
    countryCode: raw.country_code,
    region: raw.region,
    city: raw.city,
    organization: raw.organization,
    timezone: raw.timezone,
    accuracy: raw.accuracy,
    latitude: raw.latitude,
    longitude: raw.longitude,
  }
}

function applyResult(ip: string, raw: RawGeoResponse | undefined): void {
  if (!raw || !raw.country_code) {
    cache.set(ip, { status: 'error' })
    return
  }
  const detail = toDetail(raw)
  cache.set(ip, {
    status: 'success',
    label: formatGeoLabel(detail),
    detail,
    fetchedAt: Date.now(),
  })
}

export async function fetchOne(ip: string, force = false): Promise<void> {
  if (isPrivateIp(ip)) {
    cache.set(ip, { status: 'private' })
    return
  }
  const existing = getFreshEntry(ip)
  if (!force && (isFreshSuccess(existing) || existing?.status === 'loading')) {
    return
  }
  cache.set(ip, { status: 'loading' })
  if (!configuredGeoProxyBase) {
    cache.set(ip, { status: 'error' })
    return
  }
  try {
    const response = await fetch(buildProxyQuery(configuredGeoProxyBase, [ip]))
    if (!response.ok) {
      cache.set(ip, { status: 'error' })
      return
    }
    const raw = (await response.json()) as RawGeoResponse
    applyResult(ip, raw)
    persistToStorage()
  } catch {
    cache.set(ip, { status: 'error' })
  }
}

export async function fetchBatch(ips: string[]): Promise<boolean> {
  const unique = Array.from(new Set(ips))
  const targets: string[] = []
  for (const ip of unique) {
    if (isPrivateIp(ip)) {
      cache.set(ip, { status: 'private' })
      continue
    }
    const existing = cache.get(ip)
    if (isFreshSuccess(existing) || existing?.status === 'loading') continue
    targets.push(ip)
  }
  if (targets.length === 0) return true

  if (!configuredGeoProxyBase) {
    targets.forEach((ip) => cache.set(ip, { status: 'error' }))
    return false
  }

  targets.forEach((ip) => cache.set(ip, { status: 'loading' }))

  let allChunksOk = true
  for (let i = 0; i < targets.length; i += BATCH_CHUNK_SIZE) {
    const chunk = targets.slice(i, i + BATCH_CHUNK_SIZE)
    try {
      const response = await fetch(buildProxyQuery(configuredGeoProxyBase, chunk))
      if (!response.ok) {
        chunk.forEach((ip) => cache.set(ip, { status: 'error' }))
        allChunksOk = false
        continue
      }
      const results = (await response.json()) as RawGeoResponse[]
      const byIp = new Map(results.map((r) => [r.ip, r]))
      chunk.forEach((ip) => applyResult(ip, byIp.get(ip)))
      persistToStorage()
    } catch {
      chunk.forEach((ip) => cache.set(ip, { status: 'error' }))
      allChunksOk = false
    }
  }
  return allChunksOk
}
