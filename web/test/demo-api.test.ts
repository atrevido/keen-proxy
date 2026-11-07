import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api, setUnauthorizedHandler } from '../src/demo/api.ts'

test('demo API drives the UI without HTTP or EventSource operations', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async () => { throw new Error('Unexpected network request') }
  try {
    setUnauthorizedHandler(() => assert.fail('Demo must not request login'))
    assert.equal((await api.session()).authed, true)
    assert.equal((await api.login('synthetic')).ok, true)
    assert.equal((await api.logout()).ok, true)
    assert.equal((await api.catalog()).presets[0]!.id, 'sample-work')
    assert.equal((await api.features()).demo, true)
    assert.equal((await api.refreshFilters()).demo, true)
    const config = await api.config()
    assert.equal((await api.checkDNS(config)).demo, true)
    assert.equal((await api.validateConfig(config)).ok, true)
    assert.equal((await api.devices()).length, 3)
    assert.equal((await api.status()).connections, 3)
    assert.ok((await api.stats()).outbounds!.length)
    assert.equal((await api.hosts('fi', 'nonexistent')).total, 0)
    assert.equal((await api.hosts('fi', 'video', 1)).hosts.length, 1)
    assert.equal((await api.hosts('unknown')).total, 0)
    assert.equal((await api.diagnosticCheck('fi')).demo, true)
    assert.equal((await api.domains()).demo, true)
    assert.equal((await api.resolveDomains(['example.com'])).records.length, 1)
    assert.equal((await api.journal('routes', {})).entries.length, 1)
    const stream = api.journalStream('routes')
    assert.equal(stream.onmessage, null)
    stream.close()
    const preview = await api.previewPreset(config, 'sample-work', 'nl')
    await api.saveConfig(preview.config)
    assert.deepEqual(await api.previousConfig(), config)
    assert.equal((await api.config()).groups!.length, config.groups!.length + 1)
    await api.saveConfig(config)
    await assert.rejects(api.importYAML('synthetic'), /browser demo/)
    const link = await api.parseLink('vless://example@vpn.example.com:8443?security=tls#Example')
    assert.equal(link.server, 'vpn.example.com')
    assert.equal(link.name, 'Example')
    assert.equal(link.protocol, 'vless')
    assert.equal(link.type, 'link')
    assert.equal(link.port, 8443)
    assert.equal(link.tls, true)
    assert.equal((await api.parseLink('tt://vpn.example.com?security=none')).type, 'trusttunnel')
    assert.equal((await api.parseLink('tt://vpn.example.com?security=none')).tls, false)
    assert.equal((await api.parseLink('vless://vpn.example.com')).name, 'Demo connection')
    assert.equal((await api.parseLink('vless://vpn.example.com')).port, 443)
    for (const url of [api.configExportURL(), api.configExportURL(true), api.journalExportURL('system', '')]) {
      assert.ok(url.startsWith('blob:'))
      URL.revokeObjectURL(url)
    }
  } finally { globalThis.fetch = original }
})
