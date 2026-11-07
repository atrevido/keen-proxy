// Localized display formatting for tables and charts.
import { t } from './i18n'
import type { BuildIdentity } from './types'

// Keep the build label consistent in the footer, Settings, and Diagnostics.
export function formatBuild(build: BuildIdentity): string {
  const revision = build.revision && (build.dirty ? `${build.revision}+dirty` : build.revision)
  return [build.variant, build.arch.toUpperCase(), revision].filter(Boolean).join(' · ')
}

// Format a byte count using binary units from the active locale.
export function bytes(n: number): string {
  const units = t('units.bytes').split(',')
  if (!n) return `0 ${units[0]}`
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)} ${units[i]}`
}

// Format bits per second using decimal network-rate units.
export function bits(bps: number): string {
  if (bps < 1000) return t('units.bps', { n: Math.round(bps) })
  if (bps < 1e6) return t('units.kbps', { n: (bps / 1e3).toFixed(bps < 1e4 ? 1 : 0) })
  return t('units.mbps', { n: (bps / 1e6).toFixed(bps < 1e7 ? 1 : 0) })
}

// Format bits per second as a unitless Mbps chart value.
export function mbit(bps: number): string {
  const v = bps / 1e6
  return v >= 10 ? v.toFixed(0) : v.toFixed(1)
}

// Format an epoch-millisecond timestamp in local time.
export function hhmm(ms: number): string {
  const d = new Date(ms)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// Describe elapsed time since an epoch-millisecond timestamp.
export function ago(ms: number): string {
  const s = Math.max(0, (Date.now() - ms) / 1000)
  if (s < 60) return t('time.justNow')
  if (s < 3600) return t('time.minAgo', { n: Math.floor(s / 60) })
  if (s < 86400) return t('time.hAgo', { n: Math.floor(s / 3600) })
  return t('time.dAgo', { n: Math.floor(s / 86400) })
}

// Format uptime since an ISO timestamp in whole days, hours, and minutes.
export function duration(fromIso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(fromIso).getTime()) / 1000))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  return d > 0 ? t('time.dh', { d, h }) : h > 0 ? t('time.hm', { h, m }) : t('time.m', { m })
}
