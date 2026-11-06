// Keep UI lookup keys compatible with model.ParseEntry (comments, wildcard,
// trailing dot and IDNA). Addresses remain explicit rules, never DNS lookups.
export function domainName(entry: string): string | null {
  const raw = entry.split('#')[0]!.trim().replace(/^\*?\./, '').replace(/\.$/, '')
  if (!raw || raw.includes('/') || raw.includes(':') || /^\d+\.\d+\.\d+\.\d+$/.test(raw)) return null
  try { return new URL(`http://${raw}`).hostname.toLowerCase() } catch { return null }
}
