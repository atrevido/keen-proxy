import type { GeoBuiltin, GeoIPDatabase, Group } from './types'

// Mirrors model.MaxGeoIPDatabases; the backend validation is authoritative.
export const MAX_GEO_DATABASES = 12

// Built-ins show their catalog name; custom databases their name or ID.
export function geoName(db: GeoIPDatabase, builtins: GeoBuiltin[]): string {
  return builtins.find((b) => b.id === db.id)?.name ?? (db.name || db.id)
}

// Build a custom database from an absolute MMDB path or an HTTPS URL without
// credentials. Throws a message key from the geoip locale namespace on invalid input.
export function customGeo(name: string, source: string, taken: string[]): GeoIPDatabase {
  const value = source.trim()
  const db: GeoIPDatabase = { id: '', name: name.trim(), enabled: true }
  if (value.startsWith('/')) {
    db.file = value
    db.name ||= value.split('/').pop() || value
  } else {
    let u: URL
    try {
      u = new URL(value)
    } catch {
      throw new Error('geoip.badSource')
    }
    if (u.protocol !== 'https:' || !u.hostname || u.username || u.password) throw new Error('geoip.badSource')
    // URL normalization would encode the {month} placeholder.
    db.url = value
    db.name ||= u.hostname
  }
  if (db.name.length > 64) throw new Error('geoip.longName')
  do db.id = 'custom-' + Array.from(crypto.getRandomValues(new Uint8Array(6)), (b) => b.toString(16).padStart(2, '0')).join('')
  while (taken.includes(db.id))
  return db
}

// Move an item within the priority list in place; out-of-range moves are ignored.
export function move<T>(list: T[], index: number, delta: number) {
  const to = index + delta
  if (index < 0 || index >= list.length || to < 0 || to >= list.length) return
  const [item] = list.splice(index, 1)
  list.splice(to, 0, item)
}

// Category databases hold labels such as RU-BLOCKED; built-ins take their kind from the catalog.
export function isCategoryDB(db: GeoIPDatabase, builtins: GeoBuiltin[]): boolean {
  const b = builtins.find((x) => x.id === db.id)
  return b ? b.kind === 'category' : db.kind === 'category'
}

// Category groups require at least one enabled category database (backend validation agrees).
export function hasCategoryDatabase(dbs: GeoIPDatabase[], builtins: GeoBuiltin[]): boolean {
  return dbs.some((db) => db.enabled && isCategoryDB(db, builtins))
}

// Offer built-in labels plus labels already used by groups, sorted and unique.
export function categoryOptions(builtins: GeoBuiltin[], groups: Group[]) {
  const labels = new Set([...builtins.flatMap((b) => b.categories ?? []), ...groups.flatMap((g) => g.categories ?? [])])
  return [...labels].sort().map((l) => ({ label: l, value: l }))
}
