import { catalog, createDemo } from './model.ts'
import { DEMO_JOURNAL_LIMIT, DEMO_TICK_MS } from './telemetry.ts'
import type { Config, JournalKind, LinkPreview } from '../types'

const demo = createDemo()

// The demo accepts only metadata previews; it never connects to the supplied endpoint.
function parseLink(link: string): LinkPreview {
  const url = new URL(link)
  return { type: url.protocol === 'tt:' ? 'trusttunnel' : 'link', name: decodeURIComponent(url.hash.slice(1)) || 'Demo connection', protocol: url.protocol.slice(0, -1), server: url.hostname, port: Number(url.port) || 443, tls: url.searchParams.get('security') !== 'none' }
}

function download(data: unknown, type: string) {
  return URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type }))
}

// Poll the local journal and deliver only records added after subscription.
function journalStream(kind: JournalKind) {
  let cursor = demo.journal(kind, { limit: 1 }).entries[0]?.id ?? 0
  const stream = {
    onmessage: null as ((event: MessageEvent) => void) | null,
    close() { clearInterval(timer); stream.onmessage = null },
  }
  const timer = setInterval(() => {
    const { entries } = demo.journal(kind, { limit: DEMO_JOURNAL_LIMIT })
    for (const entry of entries.reverse()) {
      if (entry.id <= cursor) continue
      cursor = entry.id
      stream.onmessage?.(new MessageEvent('message', { data: JSON.stringify(entry) }))
    }
  }, DEMO_TICK_MS)
  return stream
}

// Identical caller contract to the router API; all effects stay in this module's memory.
export const api = {
  diagnosticCheck: async (connection: string) => demo.diagnostics(connection),
  catalog: async () => structuredClone(catalog),
  features: async () => ({ demo: true, dns: { route: 'direct', checked: new Date().toISOString(), fallbacks: 0, blocked: 18 }, filters: [] }),
  previewPreset: async (config: Config, id: string, target: string, index?: number) => demo.preview(config, id, target, index),
  refreshFilters: async () => ({ demo: true }),
  checkDNS: async (_config: Config) => ({ demo: true, rcode: 0, latency_ms: 12, route: 'simulated' }),
  previousConfig: async () => demo.previous(),
  importYAML: async (_yaml: string): Promise<Config> => { throw new Error('YAML import is available in the router application. This browser demo does not parse credentials or configuration files.') },
  domains: async () => demo.domains(),
  resolveDomains: async (names: string[], _preview = false) => demo.domains(names),
  login: async (_token: string) => ({ ok: true }),
  logout: async () => ({ ok: true }),
  session: async (_open = false) => ({ authed: true, trusted: false, ip: '192.0.2.100' }),
  status: async () => demo.status(),
  config: async () => demo.config(),
  saveConfig: async (config: Config) => demo.save(config),
  validateConfig: async (config: Config) => { demo.validate(config); return { ok: true } },
  devices: async () => [
    { mac: '02:00:00:00:00:10', ip: '192.168.7.10', client: 'Phone', mode: 'full' as const, hostname: 'phone.local', hostname_source: 'dhcp' as const },
    { mac: 'aa:bb:cc:dd:ee:01', ip: '192.168.7.20', client: 'TV', mode: 'split' as const },
    { mac: 'aa:bb:cc:dd:ee:02', ip: '192.168.7.30', client: 'Work laptop', mode: 'off' as const },
    { mac: '02:00:00:00:00:40', ip: '192.168.7.40', hostname: 'nas.local', hostname_source: 'dns' as const },
  ],
  parseLink: async (link: string) => parseLink(link),
  stats: async (series = false) => demo.stats(series),
  hosts: async (tag: string, q = '', limit = 200) => {
    const hosts = (demo.stats(false).outbounds?.find(o => o.tag === tag)?.top ?? []).filter(h => h.host.toLowerCase().includes(q.toLowerCase()))
    return { hosts: hosts.slice(0, limit), total: hosts.length }
  },
  journal: async (kind: JournalKind, params: Record<string, string | number | undefined>) => demo.journal(kind, params),
  journalStream,
  journalExportURL: (kind: JournalKind, _since: string) => download(demo.journal(kind, { limit: DEMO_JOURNAL_LIMIT }).entries, 'application/json'),
  configExportURL: (_redacted = false) => download(demo.config(), 'application/json'),
} satisfies typeof import('../api').api

// Demo sessions are always local and never request authentication.
export function setUnauthorizedHandler(_fn: () => void) {}
