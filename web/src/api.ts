import type { DiagnosticReport, Catalog, FeatureStatus, DNSCheck, Config, Device, DomainStatus, Session, HostStat, JournalKind, JournalPage, LinkPreview, StatsSnapshot, Status } from './types'

// Keeps the HTTP status available to callers alongside the server error.
export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

// The shell uses this callback to show login after an unauthorized request.
let onUnauthorized: () => void = () => {}
// Register the application-wide handler for expired or missing authentication.
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

// Send cookie-authenticated JSON requests and route non-login 401s to the shell.
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    if (res.status === 401 && path !== '/api/login') onUnauthorized()
    throw new ApiError(res.status, data?.error ?? `HTTP ${res.status}`)
  }
  return data as T
}

// Typed API boundary; callers own draft changes and EventSource cleanup.
export const api = {
 diagnosticCheck:(connection:string)=>request<DiagnosticReport>('POST','/api/diagnostics/check',{connection}),
 catalog:()=>request<Catalog>('GET','/api/catalog'),
 features:()=>request<FeatureStatus>('GET','/api/features'),
 previewPreset:(config:Config,id:string,target:string,index?:number)=>request<{config:Config;diff:{added:string[];removed:string[]}}>('POST','/api/presets/preview',{config,id,target,index}),
 refreshFilters:()=>request<unknown>('POST','/api/filters/refresh'),
 checkDNS:(cfg:Config)=>request<DNSCheck>('POST','/api/dns/check',cfg),
 previousConfig:()=>request<Config>('GET','/api/config/previous'),
 importYAML:(yaml:string)=>request<Config>('POST','/api/config/import',{yaml}),

  domains: () => request<DomainStatus>('GET', '/api/domains'),
  resolveDomains: (names: string[], preview = false) => request<DomainStatus>('POST', '/api/domains/resolve', { names, preview }),
  login: (token: string) => request<{ ok: boolean }>('POST', '/api/login', { token }),
  logout: () => request<{ ok: boolean }>('POST', '/api/logout'),
  // open=true audits trusted-network access when the panel is opened.
  session: (open = false) => request<Session>('GET', `/api/session${open ? '?open=1' : ''}`),
  status: () => request<Status>('GET', '/api/status'),
  config: () => request<Config>('GET', '/api/config'),
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
