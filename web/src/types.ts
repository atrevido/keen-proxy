// Wire types mirror the backend JSON models in internal/model.

// Routing policy; an empty value inherits the enclosing/default policy.
export type Mode = '' | 'split' | 'full' | 'off'
// Kernel interception mechanism selected by the backend.
export type InterceptMode = 'redirect' | 'tproxy'
// Selects which protocol-specific connection payload is populated.
export type ConnType = 'wireguard' | 'link' | 'singbox' | 'trusttunnel'

// Single WireGuard peer, including optional route and keepalive settings.
export interface WGPeer {
  endpoint: string
  public_key: string
  preshared_key?: string
  allowed_ips?: string[]
  keepalive?: number
}

// WireGuard interface credentials and its single peer.
export interface WireGuard {
  private_key: string
  address: string[]
  mtu?: number
  peer: WGPeer
}

// External TrustTunnel link and optional process settings.
export interface TrustTunnel {
  link: string
  binary?: string
  socks_port?: number
}

// Named routing target with protocol payload and optional fallback.
export interface Connection {
  id: string
  name: string
  type: ConnType
  wireguard?: WireGuard
  link?: string
  outbound?: Record<string, unknown>
  endpoint?: Record<string, unknown>
  trusttunnel?: TrustTunnel
  // Immediate fallback: another connection ID or direct.
  fallback?: string
}

// Route group containing explicit domain/address entries or country selectors.
export interface Group {
 disabled?: boolean
 preset?: { id:string; version:string; base:string[]; auto_update:boolean }
  name: string
  target: string
  entries: string[]
  countries?: string[]
  // Labels from category databases, such as RU-BLOCKED; combined with countries.
  categories?: string[]
  except?: boolean
}

// Subnet-level default routing policy for matching devices.
export interface Network {
  name: string
  cidr: string
  mode: Mode
  target?: string
}

// MAC-based routing override, optionally tied to a configured IP.
export interface Client {
  name: string
  mac: string
  ip?: string
  mode?: Mode
  target?: string
}

// Whole configuration exchanged with the API and edited as one shared draft.
export interface Config {
 schema_version?: number
 filters?: {sources: FilterSource[]; exceptions:string[]}
  api: { listen: string; token_file: string; no_token_from?: string[] }
  lan: { interface: string; wan: string }
  dns: DNSConfig
  intercept: { mode: InterceptMode; tcp_port: number; udp_port: number; fwmark: number; route_table: number }
  geoip: GeoIPConfig
  failover?: { url: string; interval: number; down_after: number; up_after: number }
  connections: Connection[] | null
  groups: Group[] | null
  networks: Network[] | null
  clients: Client[] | null
}

// Authentication and trusted-network policy for the requesting address.
export interface Session {
  authed: boolean
  trusted: boolean // Address matches api.no_token_from; no token required.
  network?: string
  ip: string
}

export interface UpdatePreferences {
  enabled: boolean
  time: string
}

export interface UpdateSelection {
  rollback: boolean
  target: string
}

export interface UpdateStatus {
  available: boolean
  demo?: boolean
  current?: string
  previous?: string
  latest?: string
  checked_at?: string
  check_failed?: boolean
  prefs: UpdatePreferences
  job?: { state: 'running' | 'success' | 'failed'; target: string }
  router_time?: string
}

// Public identity of the executable serving the panel.
export interface BuildIdentity {
  version?: string
  variant: 'full' | 'lite'
  arch: string
  revision?: string
  dirty?: boolean
}

// Applied daemon state, runtime capabilities, and connection observations.
export interface Status {
  build?: BuildIdentity
  started: string
  mode: string
  sources: number
  connections: number
  groups: number
  entries: number
  clients: number
  online_devices: number
  dns: string
  firewall: boolean
  restored: number
  trusttunnel: { id: string; running: boolean; starts: number }[] | null
  // Primary database build and type; active counts databases answering lookups.
  geoip: { loaded: boolean; built?: string; type?: string; error?: string; active?: number; categories?: number; attributions?: GeoAttribution[] | null }
  conn_countries: Record<string, string> | null
  // Maps a connection ID to its applied chain and currently active exit.
  failover?: Record<string, { active: string; chain: string[] }> | null
  // Latest HTTP probe through each connection; does not verify HTTPS or DNS.
  health?: Record<string, { ok: boolean; checked: string; since: string; latency_ms: number; error?: string }> | null
}

// Discovered LAN device and its resolved client/routing identity.
export interface Device {
  mac: string
  ip: string
  client?: string
  mode?: Mode
  hostname?: string
  hostname_source?: 'dhcp' | 'dns'
}

// Display metadata returned by the server-side connection-link parser.
export interface LinkPreview {
  type: ConnType
  name: string
  protocol: string
  server: string
  port?: number
  tls?: boolean
  addresses?: string[]
}

export const TARGET_DIRECT = 'direct'
export const TARGET_BLOCK = 'block'

// One traffic-history sample; t is an epoch timestamp in milliseconds.
export interface StatPoint {
  t: number
  unique: number
  down: number
  up: number
}

// Per-host traffic totals and last activity reported by the backend.
export interface HostStat {
  host: string
  down: number
  up: number
  requests: number
  last_seen: number
}

// Current rates, cumulative traffic, and optional history for one outbound tag.
export interface OutboundStats {
  tag: string
  active: number
  unique: number
  down: number
  up: number
  down_total: number
  up_total: number
  peak_down: number
  peak_unique: number
  series: StatPoint[] | null
  top: HostStat[] | null
}

// Outbound traffic and process resource usage captured by the daemon.
export interface StatsSnapshot {
  outbounds: OutboundStats[] | null
  system: { cpu: number; mem_pct: number; mem_mb: number; process?: { cpu?: number; rss_bytes: number } }
}

// Selects one of the independent route, audit, or system journals.
export type JournalKind = 'routes' | 'audit' | 'system'

// Sparse journal wire record; available fields depend on journal kind.
export interface JournalEntry {
  destination_port?: number
  id: number
  time: string
  host?: string
  ip?: string
  client?: string
  network?: string
  outbound?: string
  rule?: string
  rule_raw?: string
  actor?: string
  action?: string
  target?: string
  result?: string
  details?: string
  level?: string
  source?: string
  message?: string
  count?: number
}

// Server-filtered entries with pagination state and column-filter choices.
export interface JournalPage {
  entries: JournalEntry[]
  has_more: boolean
  facets: Record<string, string[]>
}

// Resolved IP address with its expiration time.
export interface DomainAddress { ip: string; expires_at: string }
// Cached DNS answers and the outcome of the latest resolution attempt.
export interface DomainRecord { name: string; addresses: DomainAddress[]; checked_at: string; attempted_at: string; error?: string; source: string; limited?: boolean }
// DNS cache, routing conflicts, and application status for live or preview data.
export interface DomainStatus { records: DomainRecord[]; conflicts: Record<string, string[]>; limited: boolean; error?: string; written_at: string; application: string; demo?: boolean }

// Resolver endpoints, routing policy, bootstrap addresses, and blocking options.
export interface DNSConfig {listen:string;upstreams:string[];fallback:string;route?:''|'direct'|'connection';connection?:string;failure?:''|'strict'|'direct';bootstrap?:Record<string,string>;block_encrypted?:boolean;firefox_canary?:boolean}
// Versioned catalog entry used to preview and maintain a routing group.
export interface Preset {id:string;name:string;category:string;domains:string[];prefixes:string[];description:string}
// Catalog resolver endpoints and bootstrap IP for a selectable DNS provider.
export interface DNSProfile {id:string;name:string;filtering:string;doh:string;dot:string;ip:string;bootstrap_ip?:string;source:string}
// Configured downloadable blocklist and its update policy.
export interface FilterSource {id:string;name:string;url:string;enabled:boolean;auto_update:boolean}
// Available blocklist with provenance and license metadata.
export interface FilterCatalog extends FilterSource {category:string;source:string;license:string}
// Bundled choices for routing presets, DNS resolvers, and blocklists.
export interface Catalog {presets:Preset[];dns:DNSProfile[];filters:FilterCatalog[];geoip:GeoBuiltin[]}
// Applied blocklist counts, version, and latest refresh outcome.
export interface FilterState {id:string;rules:number;skipped:number;bytes:number;updated:string;error?:string;version?:string;enabled:boolean}
// Runtime DNS and filter observations, optionally marked as synthetic demo data.
export interface FeatureStatus {demo?:boolean;dns:{route:string;checked:string;fallbacks:number;blocked:number};filters:FilterState[]}
// One DNS probe outcome; success alone does not verify the entire DNS path.
export interface DNSCheck {rcode:number;latency_ms:number;route:string;error?:string;demo?:boolean}

// Independent HTTP, verified HTTPS, or transfer test through an applied profile.
export interface DiagnosticResult {kind:'http'|'https'|'transfer';ok:boolean;checked:string;latency_ms:number;bytes?:number;error?:string}
// Connectivity results for a selected profile, separate from failover health.
export interface DiagnosticReport {connection:string;results:DiagnosticResult[];demo?:boolean}

// Country database entry: a built-in referenced by ID, or a custom HTTPS URL or local MMDB.
// List order is lookup priority; the first enabled database that knows an address wins.
// kind 'category' marks a custom database of labels used by group categories.
export interface GeoIPDatabase { id: string; name?: string; url?: string; file?: string; kind?: 'category'; enabled: boolean }
// Country databases; url and file are legacy single-database fields migrated by the backend.
export interface GeoIPConfig { databases: GeoIPDatabase[]; url?: string; file?: string }
// License credit shown while a database is in use.
export interface GeoAttribution { text: string; url: string }
// Bundled country database source with provenance and license metadata.
export interface GeoBuiltin { id: string; name: string; kind: 'country' | 'category'; categories?: string[]; homepage: string; license: string; attribution?: string; attribution_url?: string; updates: 'daily' | 'weekly' | 'monthly' | string; enabled: boolean; origin: string }
// Runtime state of one applied database; next is the next automatic check.
export interface GeoDatabaseInfo {
  id: string
  name: string
  builtin: boolean
  kind: 'country' | 'category'
  enabled: boolean
  local: boolean
  loaded: boolean
  present: boolean
  built?: string
  type?: string
  version?: string
  checked?: string
  updated?: string
  next?: string
  failures?: number
  error?: string
}
// Applied database states; error carries the latest manual download failure.
export interface GeoStatus { demo?: boolean; databases: GeoDatabaseInfo[]; error?: string }
// Per-database answers of a test lookup and the effective (first known) country.
// Categories are the address labels across category databases; country comes only from country databases.
export interface GeoLookup { ip: string; country?: string; database?: string; categories?: string[]; results: { id: string; name: string; enabled: boolean; kind: 'country' | 'category'; country?: string; error?: string }[] }
