import type { JournalEntry } from './types'

// Remove a client port or IPv6 brackets while preserving bare IPv6 addresses.
export function clientIP(value = ''): string {
  if (value.startsWith('['))
    return value.slice(
      1,
      value.indexOf(']') >= 0 ? value.indexOf(']') : undefined,
    )
  return (value.match(/:/g)?.length ?? 0) === 1
    ? value.replace(/:\d+$/, '')
    : value
}

// A displayed route bucket retains raw members and the observed time range.
export interface RouteGroup extends JournalEntry {
  members: JournalEntry[]
  firstTime: string
  lastTime: string
}

// Fixed time buckets include routing identity; ambiguous legacy entries stay separate.
function routeKey(e: JournalEntry, windowMs: number): string {
  const time = Date.parse(e.time)
  return JSON.stringify([
    hasRouteIdentity(e) ? null : e.id,
    Number.isFinite(time) ? Math.floor(time / windowMs) : e.id,
    clientIP(e.client),
    e.host || e.ip,
    e.network,
    e.destination_port,
    e.outbound,
    e.rule,
    e.rule_raw,
    e.source,
  ])
}
function hasRouteIdentity(e: JournalEntry): boolean {
  return Boolean(
    e.destination_port &&
      e.network &&
      e.outbound &&
      e.client &&
      (e.rule || e.rule_raw),
  )
}
function appendRoute(group: RouteGroup, entry: JournalEntry): void {
  group.members.push(entry)
  if (Date.parse(entry.time) < Date.parse(group.firstTime))
    group.firstTime = entry.time
  if (Date.parse(entry.time) > Date.parse(group.lastTime))
    group.lastTime = entry.time
  group.count = group.members.length
}
// Group loaded entries in encounter order; counts represent members, not server totals.
export function groupRoutes(
  entries: JournalEntry[],
  windowMs = 30_000,
): RouteGroup[] {
  const groups = new Map<string, RouteGroup>()
  for (const entry of entries) {
    const key = routeKey(entry, windowMs),
      group = groups.get(key)
    if (group) appendRoute(group, entry)
    else
      groups.set(key, {
        ...entry,
        members: [entry],
        count: 1,
        firstTime: entry.time,
        lastTime: entry.time,
      })
  }
  return [...groups.values()]
}
