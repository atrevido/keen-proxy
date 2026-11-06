import type { Connection } from './types'

// Derive a display protocol from the stored connection representation.
export function protocolOf(c: Connection): string {
  switch (c.type) {
    case 'wireguard':
      return 'WireGuard'
    case 'trusttunnel':
      return 'TrustTunnel'
    case 'singbox':
      return String((c.outbound ?? c.endpoint)?.type ?? 'sing-box')
    default: {
      const s = (c.link ?? '').split('://')[0].toLowerCase()
      return ({ vless: 'VLESS', vmess: 'VMess', trojan: 'Trojan', ss: 'Shadowsocks', hysteria2: 'Hysteria2', hy2: 'Hysteria2', tuic: 'TUIC', anytls: 'AnyTLS', socks5: 'SOCKS5', socks: 'SOCKS5' } as Record<string, string>)[s] ?? s.toUpperCase()
    }
  }
}

// Extract display-only endpoint details without exposing credentials.
export function serverOf(c: Connection): { host: string; port: string } {
  let hp = ''
  if (c.wireguard) hp = c.wireguard.peer.endpoint
  else if (c.link) hp = c.link.match(/@([^/?#]+)/)?.[1] ?? ''
  else {
    const o = c.outbound ?? c.endpoint
    if (o?.server) hp = `${o.server}:${o.server_port ?? ''}`
  }
  const i = hp.lastIndexOf(':')
  return i > 0 ? { host: hp.slice(0, i), port: hp.slice(i + 1) } : { host: hp, port: '' }
}

// Mask a secret for display, retaining only its edges.
export function mask(s: string | undefined): string {
  if (!s) return '—'
  return s.length <= 10 ? '••••' : `${s.slice(0, 4)}••••••${s.slice(-4)}`
}
