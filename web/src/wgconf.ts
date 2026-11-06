import { t } from './i18n'
import type { WireGuard } from './types'

// Parse the supported wg-quick fields and require connection essentials.
export function parseWireGuardConf(text: string): WireGuard {
  const wg: WireGuard = { private_key: '', address: [], peer: { endpoint: '', public_key: '' } }
  let section = ''
  for (const raw of text.split('\n')) {
    const line = raw.replace(/#.*/, '').trim()
    if (!line) continue
    const sec = line.match(/^\[(\w+)\]$/)
    if (sec) {
      section = sec[1].toLowerCase()
      continue
    }
    const eq = line.indexOf('=')
    if (eq < 0) continue
    const key = line.slice(0, eq).trim().toLowerCase()
    const value = line.slice(eq + 1).trim()
    const list = () => value.split(',').map((s) => s.trim()).filter(Boolean)
    if (section === 'interface') {
      if (key === 'privatekey') wg.private_key = value
      else if (key === 'address') wg.address = list()
      else if (key === 'mtu') wg.mtu = Number(value)
    } else if (section === 'peer') {
      if (key === 'publickey') wg.peer.public_key = value
      else if (key === 'presharedkey') wg.peer.preshared_key = value
      else if (key === 'endpoint') wg.peer.endpoint = value
      else if (key === 'allowedips') wg.peer.allowed_ips = list()
      else if (key === 'persistentkeepalive') wg.peer.keepalive = Number(value)
    }
  }
  if (!wg.private_key) throw new Error(t('wg.noPrivateKey'))
  if (!wg.peer.public_key) throw new Error(t('wg.noPublicKey'))
  if (!wg.peer.endpoint) throw new Error(t('wg.noEndpoint'))
  if (!wg.address.length) throw new Error(t('wg.noAddress'))
  return wg
}

// Export one peer, defaulting AllowedIPs to both address families.
export function wireGuardToConf(wg: WireGuard): string {
  const lines = ['[Interface]', `PrivateKey = ${wg.private_key}`, `Address = ${wg.address.join(', ')}`]
  if (wg.mtu) lines.push(`MTU = ${wg.mtu}`)
  lines.push('', '[Peer]', `PublicKey = ${wg.peer.public_key}`)
  if (wg.peer.preshared_key) lines.push(`PresharedKey = ${wg.peer.preshared_key}`)
  lines.push(`Endpoint = ${wg.peer.endpoint}`)
  lines.push(`AllowedIPs = ${(wg.peer.allowed_ips?.length ? wg.peer.allowed_ips : ['0.0.0.0/0', '::/0']).join(', ')}`)
  if (wg.peer.keepalive) lines.push(`PersistentKeepalive = ${wg.peer.keepalive}`)
  return lines.join('\n')
}

const translit: Record<string, string> = {
  а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z', и: 'i', й: 'y', к: 'k', л: 'l', м: 'm',
  н: 'n', о: 'o', п: 'p', р: 'r', с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'c', ч: 'ch', ш: 'sh', щ: 'sch',
  ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
}

// Transliterate a display name into a connection ID of at most 32 characters.
export function slugify(s: string): string {
  return s
    .toLowerCase()
    .split('')
    .map((ch) => translit[ch] ?? ch)
    .join('')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
}
