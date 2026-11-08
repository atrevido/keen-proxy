import type { DNSConfig } from './types'

// Keep existing resolver lists usable until the operator changes their order or contents.
export function dnsPairIncomplete(current: string[], saved: string[]): boolean {
  if (JSON.stringify(current) === JSON.stringify(saved)) return false
  const primary = current[0]?.trim()
  const secondary = current[1]?.trim()
  return !primary || !secondary || primary === secondary
}

// Replace one priority slot while preserving the route, extra servers, and manual pins.
export function selectDNSResolver(dns: DNSConfig, slot: 0 | 1, endpoint: string, bootstrapIP?: string) {
  const upstreams = [...dns.upstreams]
  if (!upstreams.length && slot === 1) upstreams.push('')
  upstreams[slot] = endpoint
  dns.upstreams = upstreams
  if (bootstrapIP) {
    const host = new URL(endpoint).hostname
    dns.bootstrap = { [host]: bootstrapIP, ...dns.bootstrap }
  }
}
