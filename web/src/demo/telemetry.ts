import type { Config, JournalEntry, JournalKind, JournalPage, StatPoint, StatsSnapshot } from '../types'

export const DEMO_TICK_MS = 3000
export const DEMO_JOURNAL_LIMIT = 120
const HISTORY = 60
const SEED_ROUTES = 12
const hosts = ['video.example.com', 'work.example.com', 'updates.example.com']
const fields = {
  routes: ['outbound', 'client', 'network'],
  audit: ['actor', 'result', 'target'],
  system: ['level', 'source'],
} as const

type Query = Record<string, string | number | undefined>

function tags(config: Config) {
  return [...(config.connections ?? []).map(c => c.id), 'direct']
}

// Cumulative traffic counters use bytes, while sampled rates use bits per second.
function volume(frame: number, scale: number) {
  return scale * (3_000_000 + frame * 120_000 + Math.round(Math.sin(frame / 4 + scale) * 180_000)) * 3
}

function uploadVolume(downloadBytes: number) {
  return Math.floor(downloadBytes / 10)
}

function sample(frame: number, scale: number, epoch: number): StatPoint {
  const total = volume(frame, scale)
  const previous = volume(frame - 1, scale)
  const seconds = DEMO_TICK_MS / 1000
  const down = (total - previous) * 8 / seconds
  const up = (uploadVolume(total) - uploadVolume(previous)) * 8 / seconds
  return { t: epoch + frame * DEMO_TICK_MS, unique: Math.round(7 + scale + 3 * Math.sin(frame / 3 + scale)), down, up }
}

// Build a moving time window without replaying missed ticks or changing old points.
function snapshot(config: Config, frame: number, epoch: number, series: boolean): StatsSnapshot {
  const outbounds = tags(config).map(tag => {
    const scale = 1 + [...tag].reduce((sum, char) => sum + char.charCodeAt(0), 0) % 4
    const current = sample(frame, scale, epoch)
    const total = volume(frame, scale)
    return {
      tag, active: current.unique + scale, unique: current.unique, down: current.down, up: current.up,
      down_total: total, up_total: uploadVolume(total), peak_down: 1_320_000 * scale, peak_unique: 10 + scale,
      series: series ? Array.from({ length: HISTORY }, (_, i) => sample(frame - HISTORY + 1 + i, scale, epoch)) : null,
      top: hosts.map((host, index) => {
        const share = [0.6, 0.25, 0.15][index]!
        return { host, down: Math.floor(total * share), up: Math.floor(total * share / 10), requests: Math.floor((frame + 1) * scale * share), last_seen: current.t }
      }),
    }
  })
  const cpu = Math.round(16 + 7 * Math.sin(frame / 4))
  const memory = Math.round(72 + 4 * Math.sin(frame / 12))
  const process = {
    cpu: Math.round((2.4 + Math.sin(frame / 4)) * 10) / 10,
    rss_bytes: Math.round(38 + 2 * Math.sin(frame / 12)) * 1024 ** 2,
  }
  return { system: { cpu, mem_pct: Math.round(memory / 256 * 100), mem_mb: 256, process }, outbounds }
}

function routeEntry(frame: number, epoch: number, outbounds: string[]): JournalEntry {
  return {
    id: frame + 1, time: new Date(epoch + frame * DEMO_TICK_MS).toISOString(),
    host: hosts[Math.floor(frame / outbounds.length) % hosts.length], ip: '192.0.2.10',
    client: '192.168.7.10', outbound: outbounds[frame % outbounds.length], rule: 'Simulated traffic',
    network: 'tcp', destination_port: 443,
  }
}

function matches(entry: JournalEntry, keys: readonly (keyof JournalEntry)[], query: Query, search: string) {
  return keys.every(key => !query[key] || String(entry[key]) === String(query[key])) &&
    Object.values(entry).some(value => typeof value === 'string' && value.toLowerCase().includes(search))
}

function journalPage(records: JournalEntry[], kind: JournalKind, query: Query): JournalPage {
  const keys = fields[kind]
  const before = Number(query.before) || Infinity
  const limit = Math.min(DEMO_JOURNAL_LIMIT, Math.max(1, Math.floor(Number(query.limit) || 50)))
  const search = String(query.q ?? '').trim().toLowerCase()
  const found = records.filter(entry => entry.id < before && matches(entry, keys, query, search)).reverse()
  const facets = Object.fromEntries(keys.map(key => [key, [...new Set(records.map(entry => String(entry[key] ?? '')).filter(Boolean))].sort()]))
  return { entries: found.slice(0, limit).map(entry => ({ ...entry })), has_more: found.length > limit, facets }
}

// Share one session clock and a bounded journal between snapshots and subscriptions.
export function createTelemetry(now: () => number) {
  const started = now()
  const epoch = started - (HISTORY - 1) * DEMO_TICK_MS
  let frame = HISTORY - 1 - SEED_ROUTES
  const time = new Date(started).toISOString()
  const records: Record<JournalKind, JournalEntry[]> = {
    routes: [],
    audit: [{ id: 1, time, actor: 'system', action: 'Demo session started', target: 'demo', result: 'info' }],
    system: [{ id: 1, time, level: 'INFO', source: 'demo', message: 'Browser demo: no router or network operations' }],
  }

  function sync(config: Config) {
    const next = HISTORY - 1 + Math.floor((now() - started) / DEMO_TICK_MS)
    if (next <= frame) return
    const outbounds = tags(config)
    for (let i = Math.max(frame + 1, next - DEMO_JOURNAL_LIMIT + 1); i <= next; i++) {
      records.routes.push(routeEntry(i, epoch, outbounds))
    }
    frame = next
    records.routes = records.routes.slice(-DEMO_JOURNAL_LIMIT)
  }

  function applied() {
    const audit = records.audit
    audit.push({ id: audit.at(-1)!.id + 1, time: new Date(now()).toISOString(), actor: 'user', action: 'config.apply', target: 'demo', result: 'ok', details: 'Synthetic session only' })
    records.audit = audit.slice(-DEMO_JOURNAL_LIMIT)
  }

  return {
    started: time, sync, applied,
    stats: (config: Config, series: boolean) => snapshot(config, frame, epoch, series),
    journal: (kind: JournalKind, query: Query) => journalPage(records[kind], kind, query),
  }
}
