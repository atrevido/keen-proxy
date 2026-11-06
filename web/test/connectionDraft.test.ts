import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  manualConnection,
  importConnections,
  canFallback,
  connectionChain,
} from '../src/connectionDraft.ts'
import type { Connection } from '../src/types.ts'
const base = {
  protocol: 'vless',
  name: 'Example',
  id: 'example',
  server: 'vpn.example.test',
  port: '443',
  uuid: '00000000-0000-4000-8000-000000000000',
  security: 'reality',
  sni: 'cover.example.test',
  publicKey: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
  shortId: 'ab12',
  flow: 'xtls-rprx-vision',
  fingerprint: 'chrome',
}
test('manual REALITY produces editable sing-box outbound and rejects invalid port', () => {
  const c = manualConnection(base)
  assert.equal(c.outbound?.type, 'vless')
  assert.equal((c.outbound?.tls as any).reality.public_key, base.publicKey)
  assert.throws(() => manualConnection({ ...base, port: '443x' }))
  assert.throws(() => manualConnection({ ...base, uuid: 'invalid' }))
})
test('manual protocols have specific fields and no accidental credentials', () => {
  for (const protocol of ['shadowsocks', 'trojan', 'hysteria2', 'socks']) {
    const c = manualConnection({
      ...base,
      protocol,
      password: 'example-password',
      cipher: 'aes-128-gcm',
      version: '5',
    })
    assert.equal(c.outbound?.type, protocol)
    assert.equal(c.outbound?.uuid, undefined)
  }
  const c = manualConnection({
    ...base,
    protocol: 'wireguard',
    privateKey: 'a',
    peerKey: 'b',
    address: '10.1.0.2/32',
    endpoint: 'vpn.example.test:51820',
  })
  assert.equal(c.wireguard?.peer.endpoint, 'vpn.example.test:51820')
})
test('file import selects outbounds without importing global routing', () => {
  const profiles = importConnections(
    JSON.stringify({
      outbounds: [
        {
          type: 'vless',
          tag: 'one',
          server: 'vpn.example.test',
          uuid: 'example',
        },
        { type: 'trojan', tag: 'two', password: 'example' },
      ],
      route: { final: 'two' },
      inbounds: [{}],
    }),
    'config.json',
  )
  assert.equal(profiles.length, 2)
  assert.equal(profiles[0]!.name, 'one')
  assert.equal(profiles[0]!.outbound?.tag, undefined)
  assert.equal((profiles[0] as any).route, undefined)
  assert.throws(() =>
    importConnections('{"outbounds":[{"type":"direct"}]}', 'x.json'),
  )
  assert.throws(() =>
    importConnections('x'.repeat(2 * 1024 * 1024 + 1), 'x.txt'),
  )
  assert.throws(() => importConnections('[]', 'x.json'))
})
test('WireGuard import rejects commands, unknown options and multiple peers', () => {
  const conf =
    '[Interface]\nPrivateKey=a\nAddress=10.1.0.2/32\n[Peer]\nPublicKey=b\nEndpoint=vpn.example.test:51820'
  assert.equal(
    importConnections(conf, 'x.conf')[0]!.wireguard?.address[0],
    '10.1.0.2/32',
  )
  assert.throws(() => importConnections(conf + '\nPostUp=echo bad', 'x.conf'))
  assert.throws(() =>
    importConnections(conf + '\n[Peer]\nPublicKey=c', 'x.conf'),
  )
})
test('fallback rejects cycles and returns inherited chain without mutations', () => {
  const list: Connection[] = [
    { id: 'a', name: 'A', type: 'link', fallback: 'b' },
    { id: 'b', name: 'B', type: 'link', fallback: 'c' },
    { id: 'c', name: 'C', type: 'link' },
  ]
  assert.equal(canFallback(list, 'c', 'a'), false)
  assert.equal(canFallback(list, 'a', 'a'), false)
  assert.equal(canFallback(list, 'a', 'missing'), false)
  assert.equal(canFallback(list, 'c', 'direct'), true)
  assert.deepEqual(connectionChain(list, 'a'), ['a', 'b', 'c'])
  assert.equal(list[0]!.fallback, 'b')
})
test('import preserves source tag as display name but removes reserved tag from profile', () => {
  const c = importConnections(
    '{"type":"trojan","tag":"My server","server":"vpn.example.test","server_port":443,"password":"example"}',
    'x.json',
  )[0]!
  assert.equal(c.name, 'My server')
  assert.equal(c.outbound?.tag, undefined)
})
test('imported reactive profiles can be copied for validation and saving', async () => {
  const { reactive } = await import('vue')
  const { copyConnection } = await import('../src/connectionDraft.ts')
  const profile = reactive(
    importConnections(
      '{"type":"trojan","server":"vpn.example.test","server_port":443,"password":"example"}',
      'profile.json',
    )[0]!,
  )
  const copy = copyConnection(profile)
  assert.equal(copy.outbound?.password, 'example')
  copy.name = 'Changed'
  assert.notEqual(profile.name, copy.name)
})

test('manual TLS and unencrypted VLESS preserve explicit security choices', () => {
  const plain = manualConnection({ ...base, security: 'none', flow: '' })
  assert.equal(plain.outbound?.tls, undefined)
  assert.equal(plain.outbound?.flow, undefined)
  const tls = manualConnection({
    ...base,
    security: 'tls',
    sni: '',
    fingerprint: '',
  })
  assert.equal(
    (tls.outbound?.tls as { server_name: string }).server_name,
    base.server,
  )
  assert.equal((tls.outbound?.tls as { reality?: unknown }).reality, undefined)
  assert.throws(() => manualConnection({ ...base, publicKey: '' }), /publicKey/)
  assert.throws(
    () => manualConnection({ ...base, publicKey: 'bad' }),
    /publicKey/,
  )
  assert.throws(() => manualConnection({ ...base, shortId: 'abc' }), /shortId/)
  assert.throws(() => manualConnection({ ...base, port: '65536' }), /port/)
  assert.throws(
    () => manualConnection({ ...base, protocol: 'unknown' }),
    /protocol/,
  )
})
test('WireGuard optional values and SOCKS credentials survive manual entry', () => {
  const wg = manualConnection({
    ...base,
    protocol: 'wireguard',
    privateKey: 'a',
    peerKey: 'b',
    address: '10.1.0.2/32, 2001:db8::1/128',
    endpoint: 'vpn.example.test:51820',
    psk: 'c',
    mtu: '1400',
    keepalive: '25',
  }).wireguard!
  assert.equal(wg.mtu, 1400)
  assert.equal(wg.peer.keepalive, 25)
  assert.equal(wg.peer.preshared_key, 'c')
  assert.equal(wg.address.length, 2)
  const socks = manualConnection({
    ...base,
    protocol: 'socks',
    username: 'test',
    password: ' example ',
    version: '',
  }).outbound!
  assert.equal(socks.password, ' example ')
  assert.equal(socks.version, '5')
})
test('imports links and endpoint wrappers while rejecting malformed containers', () => {
  assert.equal(importConnections('vless://example', 'x.txt')[0]!.type, 'link')
  assert.equal(
    importConnections('tt://?example', 'x.txt')[0]!.type,
    'trusttunnel',
  )
  assert.equal(
    importConnections(
      '{"endpoint":{"type":"wireguard","tag":"one"}}',
      'x.json',
    )[0]!.endpoint?.type,
    'wireguard',
  )
  assert.equal(
    importConnections('{"type":"wireguard"}', 'x.json')[0]!.endpoint?.type,
    'wireguard',
  )
  for (const text of [
    '',
    '{"outbounds":{}}',
    '{"outbound":{}}',
    '{"endpoints":{}}',
    '{"type":2}',
    '{',
  ])
    assert.throws(() => importConnections(text, 'x.json'))
})
test('WireGuard file parsing preserves supported optional fields and rejects invalid input', () => {
  const file =
    '\uFEFF# test\n[Interface]\nPrivateKey=a\nAddress=10.1.0.2/32\nMTU=1400\n\n[Peer]\nPublicKey=b\nPresharedKey=c\nEndpoint=vpn.example.test:51820\nAllowedIPs=0.0.0.0/0, ::/0\nPersistentKeepalive=25'
  const wg = importConnections(file, 'x.CONF')[0]!.wireguard!
  assert.equal(wg.mtu, 1400)
  assert.equal(wg.peer.keepalive, 25)
  assert.equal(wg.peer.allowed_ips?.length, 2)
  assert.equal(wg.peer.preshared_key, 'c')
  for (const bad of [
    file.replace('MTU=1400', 'MTU=invalid'),
    file.replace('PersistentKeepalive=25', 'PersistentKeepalive=invalid'),
    file.replace('[Peer]', '[Unknown]'),
    '[Interface]\nInvalid line',
    '[Interface]\nAddress=10.1.0.2/32',
  ])
    assert.throws(() => importConnections(bad, 'x.conf'))
})
test('port bounds, TLS activation and REALITY options are preserved exactly', () => {
  for (const port of ['1', '65535'])
    assert.equal(
      manualConnection({ ...base, port }).outbound?.server_port,
      Number(port),
    )
  assert.throws(() => manualConnection({ ...base, port: '0' }), /port/)
  const c = manualConnection({ ...base, fingerprint: 'firefox' }).outbound!
    .tls as {
    enabled: boolean
    utls: { enabled: boolean; fingerprint: string }
    reality: { enabled: boolean; short_id: string }
  }
  assert.equal(c.enabled, true)
  assert.equal(c.utls.enabled, true)
  assert.equal(c.utls.fingerprint, 'firefox')
  assert.equal(c.reality.enabled, true)
  assert.equal(c.reality.short_id, 'ab12')
  const empty = manualConnection({ ...base, shortId: '' }).outbound!.tls as {
    reality: { short_id: string }
  }
  assert.equal(empty.reality.short_id, '')
  const { sni, ...withoutSni } = base
  assert.equal(
    (
      manualConnection({ ...withoutSni, security: 'tls' }).outbound!.tls as {
        server_name: string
      }
    ).server_name,
    base.server,
  )
})
test('WireGuard rejects each missing required field independently', () => {
  const lines = [
    '[Interface]',
    'PrivateKey=a',
    'Address=10.1.0.2/32',
    '[Peer]',
    'PublicKey=b',
    'Endpoint=vpn.example.test:51820',
  ]
  for (const index of [1, 2, 4, 5])
    assert.throws(() =>
      importConnections(
        lines.filter((_, i) => i !== index).join('\n'),
        'x.conf',
      ),
    )
})
test('file size boundary and valid fallback termination remain usable', () => {
  const link = 'vless://example'
  assert.equal(
    importConnections(link + ' '.repeat(2 * 1024 * 1024 - link.length), 'x.txt')
      .length,
    1,
  )
  const list: Connection[] = [
    { id: 'a', name: 'A', type: 'link' },
    { id: 'b', name: 'B', type: 'link' },
  ]
  assert.equal(canFallback(list, 'a', ''), true)
  assert.equal(canFallback(list, 'a', 'b'), true)
  assert.deepEqual(connectionChain(list, 'missing'), ['missing'])
})
test('fallback follows the selected profile rather than a different list entry', () => {
  const list: Connection[] = [
    { id: 'a', name: 'A', type: 'link' },
    { id: 'b', name: 'B', type: 'link', fallback: 'c' },
    { id: 'c', name: 'C', type: 'link', fallback: 'b' },
  ]
  assert.equal(canFallback(list, 'a', 'b'), false)
})
