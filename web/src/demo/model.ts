import initial from './config.json' with { type: 'json' }
import type { Catalog, Config, DiagnosticReport, DomainStatus, JournalKind, JournalPage, StatsSnapshot, Status } from '../types'

// Only synthetic examples are bundled; the demo never imports runtime configuration.
export const catalog: Catalog = {
  presets: [
    { id: 'sample-work', name: 'Example work sites', category: 'Other', domains: ['work.example.com', 'meet.example.com'], prefixes: [], description: 'Synthetic domains for exploring the preset preview.' },
  ],
  dns: [{ id: 'example', name: 'Example resolver', filtering: 'none', doh: 'https://dns.example.com/dns-query', dot: 'tls://dns.example.com', ip: '192.0.2.53', source: 'Synthetic demo data' }],
  filters: [{ id: 'sample-ads', name: 'Example advertising list', category: 'Other', url: 'https://lists.example.com/ads.txt', enabled: true, auto_update: false, source: 'Synthetic demo data', license: 'GPL-3.0-only' }],
}

// JSON copies accept Vue proxies and isolate saved state from editable drafts.
function copy<T>(value: T): T { return JSON.parse(JSON.stringify(value)) }

// This small validation subset protects the demo state; the real daemon is authoritative.
function validate(config: Config) {
  const ids = (config.connections ?? []).map(c => c.id)
  if (new Set(ids).size !== ids.length || ids.some(id => !id)) throw new Error('Connection IDs must be non-empty and unique')
  const targets = new Set([...ids, 'direct', 'block'])
  for (const group of config.groups ?? []) {
    if (!targets.has(group.target)) throw new Error('Unknown group target')
  }
}

// Owns one browser session's synthetic applied state and previous configuration.
export function createDemo() {
  let saved = copy(initial as Config)
  let previous = copy(saved)
  const started = new Date().toISOString()

  function save(config: Config) {
    validate(config)
    previous = copy(saved)
    saved = copy(config)
    return copy(saved)
  }

  return { config: () => copy(saved), previous: () => copy(previous), save, validate, preview, diagnostics: (connection: string) => diagnostics(saved, connection), domains: (names?: string[]) => domains(saved, names), status: () => status(saved, started), stats: () => stats(saved), journal }
}

// Preview updates a copy and leaves the session unchanged.
function preview(config: Config, id: string, target: string, index?: number) {
  const preset = catalog.presets.find(p => p.id === id)
  if (!preset) throw new Error('Unknown demo preset')
  const draft = copy(config)
  draft.groups ??= []
  const old = index === undefined ? [] : draft.groups[index]?.entries ?? []
  const group = { name: preset.name, target, entries: [...preset.domains], preset: { id, version: 'demo', base: [...preset.domains], auto_update: false } }
  if (index === undefined) draft.groups.push(group)
  else draft.groups[index] = group
  validate(draft)
  return { config: draft, diff: { added: group.entries.filter(e => !old.includes(e)), removed: old.filter(e => !group.entries.includes(e)) } }
}

// Return simulated checks only for applied profiles.
function diagnostics(saved: Config, connection: string): DiagnosticReport {
  if (!saved.connections?.some(c => c.id === connection)) throw new Error('Unknown connection')
  return { connection, demo: true, results: (['http', 'https', 'transfer'] as const).map((kind, i) => ({ kind, ok: true, checked: new Date().toISOString(), latency_ms: 42 + i * 17, bytes: kind === 'transfer' ? 65536 : undefined })) }
}

// Reserved example IPs represent every synthetic DNS answer.
function domains(saved: Config, names = (saved.groups ?? []).flatMap(g => g.entries)): DomainStatus {
  const now = new Date().toISOString()
  return { demo: true, limited: false, conflicts: {}, written_at: now, application: 'synthetic', records: [...new Set(names)].map(name => ({ name, source: 'demo', checked_at: now, attempted_at: now, addresses: name.endsWith('.invalid') ? [] : [{ ip: '192.0.2.10', expires_at: new Date(Date.now() + 300000).toISOString() }], error: name.endsWith('.invalid') ? 'Synthetic NXDOMAIN' : undefined })) }
}

// Derive summary counts and health from the synthetic applied configuration.
function status(saved: Config, started: string): Status {
  return {
    started, mode: saved.intercept.mode, sources: 2, connections: saved.connections?.length ?? 0,
    groups: saved.groups?.length ?? 0, entries: (saved.groups ?? []).reduce((n, g) => n + g.entries.length, 0),
    clients: saved.clients?.length ?? 0, online_devices: 4, dns: '127.0.0.1:5353', firewall: true, restored: 0,
    trusttunnel: [], geoip: { loaded: true, type: 'Synthetic demo', built: started }, conn_countries: { fi: 'FI', nl: 'NL', de: 'DE' },
    failover: { fi: { active: 'fi', chain: ['fi', 'nl', 'de'] } },
    health: Object.fromEntries((saved.connections ?? []).map(c => [c.id, { ok: true, checked: new Date().toISOString(), since: started, latency_ms: 42 }])),
  }
}

// Generate illustrative traffic history without a timer or network operation.
function stats(saved: Config): StatsSnapshot {
  const now = Date.now()
  return { system: { cpu: 14, mem_pct: 28, mem_mb: 72 }, outbounds: [...(saved.connections ?? []).map(c => c.id), 'direct'].map((tag, i) => ({
    tag, active: 3 + i, unique: 5 + i, down: 280000 * (i + 1), up: 32000 * (i + 1), down_total: 182000000 * (i + 1), up_total: 21000000 * (i + 1), peak_down: 1800000, peak_unique: 20,
    series: Array.from({ length: 60 }, (_, n) => ({ t: now - (59 - n) * 3000, unique: 5 + i, down: (Math.sin(n / 4 + i) + 2) * 200000, up: 32000 })),
    top: [{ host: 'video.example.com', down: 12000000, up: 800000, requests: 42, last_seen: now }],
  })) }
}

// Provide one representative record for each journal category.
function journal(kind: JournalKind): JournalPage {
  const time = new Date().toISOString()
  const records = {
    routes: [{ id: 1, time, host: 'video.example.com', ip: '192.0.2.10', client: '192.168.7.10', outbound: 'nl', rule: 'Video and social media', network: 'Main LAN', destination_port: 443, count: 3 }],
    audit: [{ id: 2, time, actor: 'demo', action: 'config.apply', result: 'ok', details: 'Synthetic session only' }],
    system: [{ id: 3, time, level: 'info', source: 'demo', message: 'Browser demo: no router or network operations' }],
  }
  return { entries: records[kind], has_more: false, facets: {} }
}
