import type { DiagnosticReport, Catalog, FeatureStatus, DNSCheck, Config, GeoLookup, GeoStatus, Device, DomainStatus, Session, HostStat, JournalKind, JournalPage, LinkPreview, StatsSnapshot, Status } from './types'
import type { UpdatePreferences, UpdateSelection, UpdateStatus } from './types'
import { ApiError } from './apiError.ts'

export { ApiError } from './apiError.ts'

// The shell uses this callback to show login after an unauthorized request.
let onUnauthorized: () => void = () => {}
// Register the application-wide handler for expired or missing authentication.
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

// Preserve status even when an older build or intermediary sends plain text.
function httpError(status: number, text: string): ApiError {
  let detail: unknown
  try { ({ error: detail } = JSON.parse(text)) } catch { /* Use the status below. */ }
  const message = typeof detail === 'string' && detail ? detail : `HTTP ${status}`
  return new ApiError(status, message)
}

// Share cookie authentication and the login handler across JSON and file requests.
async function send(method: string, path: string, body?: unknown): Promise<Response> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    if (res.status === 401 && path !== '/api/login') onUnauthorized()
    throw httpError(res.status, await res.text())
  }
  return res
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const text = await (await send(method, path, body)).text()
  return (text ? JSON.parse(text) : undefined) as T
}

// Reject an intermediary's HTML response before the browser saves it as a ZIP.
async function diagnosticBundle(): Promise<Blob> {
  const res = await send('GET', '/api/diagnostics/bundle')
  const mime = res.headers.get('Content-Type')?.split(';', 1)[0]
  if (mime?.trim().toLowerCase() !== 'application/zip') {
    throw new ApiError(res.status, 'Expected a diagnostics ZIP archive')
  }
  return res.blob()
}

// Typed API boundary; callers own draft changes and EventSource cleanup.
export const api = {
 diagnosticCheck:(connection:string)=>request<DiagnosticReport>('POST','/api/diagnostics/check',{connection}),
 diagnosticBundle,
 catalog:()=>request<Catalog>('GET','/api/catalog'),
 features:()=>request<FeatureStatus>('GET','/api/features'),
 previewPreset:(config:Config,id:string,target:string,index?:number)=>request<{config:Config;diff:{added:string[];removed:string[]}}>('POST','/api/presets/preview',{config,id,target,index}),
 refreshFilters:()=>request<unknown>('POST','/api/filters/refresh'),
 checkDNS:(cfg:Config)=>request<DNSCheck>('POST','/api/dns/check',cfg),
 // Country databases of the applied configuration; an id targets one database, even a disabled one.
 geoip:()=>request<GeoStatus>('GET','/api/geoip'),
 updateGeo:(id='')=>request<GeoStatus>('POST','/api/geoip/update',{id}),
 lookupGeo:(ip:string,id='')=>request<GeoLookup>('POST','/api/geoip/lookup',{ip,id}),
 previousConfig:()=>request<Config>('GET','/api/config/previous'),
 importYAML:(yaml:string)=>request<Config>('POST','/api/config/import',{yaml}),

  domains: () => request<DomainStatus>('GET', '/api/domains'),
  resolveDomains: (names: string[], preview = false) => request<DomainStatus>('POST', '/api/domains/resolve', { names, preview }),
  login: (token: string) => request<{ ok: boolean }>('POST', '/api/login', { token }),
  logout: () => request<{ ok: boolean }>('POST', '/api/logout'),
  // open=true audits trusted-network access when the panel is opened.
  session: (open = false) => request<Session>('GET', `/api/session${open ? '?open=1' : ''}`),
  status: () => request<Status>('GET', '/api/status'),
  updates: () => request<UpdateStatus>('GET', '/api/updates'),
  checkUpdates: () => request<UpdateStatus>('POST', '/api/updates/check', {}),
  saveUpdatePreferences: (prefs: UpdatePreferences) => request<UpdateStatus>('PUT', '/api/updates/preferences', prefs),
  installUpdate: (selection: UpdateSelection) => request<UpdateStatus>('POST', '/api/updates/install', selection),
  config: () => request<Config>('GET', '/api/config'),
  previewAmneziaWG: (profile: string, probe = false) => request<{ profile: string; endpoint: string }>('POST', '/api/connections/amneziawg/preview', { profile, probe }),
  saveConfig: (cfg: Config) => request<Config>('PUT', '/api/config', cfg),
  validateConfig: (cfg: Config) => request<{ ok: boolean }>('POST', '/api/config/validate', cfg),
  devices: () => request<Device[]>('GET', '/api/devices'),
  parseLink: (link: string) => request<LinkPreview>('POST', '/api/links/parse', { link }),
  stats: (series = false) => request<StatsSnapshot>('GET', `/api/stats${series ? '?series=1' : ''}`),
  hosts: (tag: string, q = '', limit = 200) =>
    request<{ hosts: HostStat[]; total: number }>(
      'GET',
      `/api/stats/hosts?tag=${encodeURIComponent(tag)}&q=${encodeURIComponent(q)}&limit=${limit}`,
    ),
  // Accepts q, before, limit, and column filters such as outbound or level.
  journal: (kind: JournalKind, params: Record<string, string | number | undefined>) => {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') qs.set(k, String(v))
    return request<JournalPage>('GET', `/api/journal/${kind}?${qs}`)
  },
  journalStream: (kind: JournalKind): Pick<EventSource, 'onmessage' | 'close'> => new EventSource(`/api/journal/${kind}/stream`),
  journalExportURL: (kind: JournalKind, since: string) => `/api/journal/${kind}/export?since=${since}`,
  configExportURL: (redacted = false) => `/api/config/yaml${redacted ? '?redacted=1' : ''}`,
}
