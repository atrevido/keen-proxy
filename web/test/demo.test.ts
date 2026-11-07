import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createDemo } from '../src/demo/model.ts'

test('demo Apply owns copies, retains previous state and resets per instance', () => {
  const demo = createDemo()
  const draft = demo.config()
  draft.groups![0]!.name = 'Edited draft'
  assert.notEqual(demo.config().groups![0]!.name, 'Edited draft')
  const original = demo.config()
  demo.save(draft)
  draft.groups![0]!.name = 'Later mutation'
  assert.equal(demo.config().groups![0]!.name, 'Edited draft')
  assert.deepEqual(demo.previous(), original)
  assert.deepEqual(createDemo().config(), original)
})

test('demo rejects invalid configurations without changing saved state', () => {
  const demo = createDemo()
  const before = demo.config()
  const invalid = demo.config()
  invalid.groups![0]!.target = 'missing'
  assert.throws(() => demo.save(invalid), /target/i)
  assert.deepEqual(demo.config(), before)
  const duplicate = demo.config()
  duplicate.connections!.push(duplicate.connections![0]!)
  assert.throws(() => demo.validate(duplicate), /unique/i)
})

test('preset preview changes only a returned draft and rejects unknown presets', () => {
  const demo = createDemo()
  const before = demo.config()
  const preview = demo.preview(before, 'sample-work', 'nl')
  assert.equal(preview.config.groups!.at(-1)!.target, 'nl')
  assert.equal(preview.config.groups!.at(-1)!.preset!.auto_update, false)
  assert.ok(preview.diff.added.includes('work.example.com'))
  assert.deepEqual(demo.config(), before)
  assert.deepEqual(before, demo.config())
  assert.throws(() => demo.preview(before, 'unknown', 'nl'), /preset/i)
})

test('all diagnostic and DNS data is explicitly simulated', () => {
  const demo = createDemo()
  assert.equal(demo.status().connections, 3)
  assert.equal(demo.status().groups, 6)
  assert.equal(demo.status().entries, 15)
  assert.equal(demo.status().health?.fi?.ok, true)
  assert.equal(demo.status().mode, 'tproxy')
  assert.ok(demo.status().geoip.built)
  assert.equal(demo.diagnostics('fi').demo, true)
  assert.deepEqual(demo.diagnostics('fi').results.map(r => r.kind), ['http', 'https', 'transfer'])
  assert.equal(demo.diagnostics('fi').results.at(-1)!.bytes, 65536)
  assert.ok(demo.diagnostics('fi').results.every(r => r.ok && Number.isFinite(r.latency_ms)))
  assert.throws(() => demo.diagnostics('missing'), /connection/i)
  assert.equal(demo.domains(['work.example.com']).demo, true)
  assert.equal(demo.domains(['bad.invalid']).records[0]!.error, 'Synthetic NXDOMAIN')
  assert.deepEqual(demo.domains(['example.com', 'example.com']).records.map(r => r.name), ['example.com'])
  assert.equal(demo.domains(['bad.invalid']).records[0]!.addresses.length, 0)
  const record = demo.domains(['example.com']).records[0]!
  assert.ok(Date.parse(record.addresses[0]!.expires_at) > Date.parse(record.checked_at))
})

test('empty optional collections still render status, history and a preset preview', () => {
  const demo = createDemo()
  const config = demo.config()
  config.connections = null
  config.groups = null
  config.clients = null
  demo.save(config)
  const status = demo.status()
  assert.equal(status.connections, 0)
  assert.equal(status.groups, 0)
  assert.equal(status.clients, 0)
  assert.equal(status.entries, 0)
  assert.equal(demo.domains().records.length, 0)
  assert.deepEqual(demo.stats().outbounds!.map(o => o.tag), ['direct'])
  assert.throws(() => demo.diagnostics('fi'), /connection/)
  assert.equal(demo.preview(config, 'sample-work', 'direct', 0).config.groups!.length, 1)
})

test('synthetic traffic and journal records stay consistent with configured profiles', () => {
  const demo = createDemo()
  const stats = demo.stats()
  assert.deepEqual(stats.outbounds!.map(o => o.tag), ['fi', 'nl', 'de', 'direct'])
  assert.equal(stats.outbounds![0]!.series!.length, 60)
  for (const kind of ['routes', 'audit', 'system'] as const) assert.ok(demo.journal(kind).entries.length > 0)
  assert.ok(demo.domains().records.length > 0)
  const config = demo.config()
  config.connections = []
  config.groups = []
  demo.save(config)
  assert.equal(demo.status().connections, 0)
  assert.deepEqual(demo.status().health, {})
})

test('the demo advances one coherent sample and route event per polling interval', () => {
  let now = Date.UTC(2026, 0, 1)
  const demo = createDemo(() => now)
  const first = demo.stats().outbounds![0]!
  const latest = demo.journal('routes').entries[0]!
  assert.equal(first.series!.at(-1)!.t, now)
  assert.deepEqual(demo.stats().outbounds![0], first)
  now += 3000
  const next = demo.stats().outbounds![0]!
  assert.deepEqual(next.series!.slice(0, -1), first.series!.slice(1))
  assert.equal(next.series!.at(-1)!.down, next.down)
  assert.equal(next.down_total - first.down_total, next.down * 3)
  assert.notEqual(next.down, first.down)
  const route = demo.journal('routes').entries[0]!
  assert.ok(route.id > latest.id)
  assert.equal(Date.parse(route.time), next.series!.at(-1)!.t)
  assert.ok(demo.stats().outbounds!.find(o => o.tag === route.outbound)!.top!.some(h => h.host === route.host))
  now -= 3000
  assert.deepEqual(demo.stats().outbounds![0], next)
  assert.equal(demo.stats(false).outbounds![0]!.series, null)
})

test('simulated samples stay in range and counters grow across traffic peaks', () => {
  let now = Date.UTC(2026, 0, 1)
  const demo = createDemo(() => now)
  for (let i = 0; i < 30; i++) {
    const before = demo.stats(false)
    now += 3000
    const after = demo.stats(false)
    assert.ok(after.system.cpu >= 0 && after.system.cpu <= 100)
    assert.ok(after.system.mem_pct > 0 && after.system.mem_pct < 100)
    for (const [index, outbound] of after.outbounds!.entries()) {
      assert.ok(outbound.down > outbound.up && outbound.up > 0)
      assert.ok(outbound.peak_down >= outbound.down && outbound.peak_unique >= outbound.unique)
      assert.ok(outbound.active >= outbound.unique && outbound.unique > 0)
      assert.ok(outbound.down_total > before.outbounds![index]!.down_total)
      assert.ok(outbound.up_total > before.outbounds![index]!.up_total)
      assert.ok(outbound.top!.every(host => host.last_seen === now && host.requests > 0))
      assert.ok(outbound.top!.reduce((sum, host) => sum + host.down, 0) <= outbound.down_total)
    }
  }
})

test('demo journal filters, paginates and keeps prior routes after Apply', () => {
  let now = Date.UTC(2026, 0, 1)
  const demo = createDemo(() => now)
  const first = demo.journal('routes', { limit: 1 })
  assert.equal(first.entries.length, 1)
  assert.equal(first.has_more, true)
  assert.ok(demo.journal('routes', { before: first.entries[0]!.id }).entries.every(e => e.id < first.entries[0]!.id))
  assert.ok(demo.journal('routes', { q: 'missing.example' }).entries.length === 0)
  assert.ok(first.facets.outbound!.length > 1)
  const filtered = demo.journal('routes', { outbound: first.entries[0]!.outbound, q: 'EXAMPLE.COM' })
  assert.ok(filtered.entries.length > 0)
  assert.ok(filtered.entries.every(e => e.outbound === first.entries[0]!.outbound))
  assert.equal(demo.journal('routes', { limit: 12 }).has_more, false)
  const config = demo.config()
  config.connections = []
  config.groups = []
  demo.save(config)
  now += 3000
  const newest = demo.journal('routes').entries
  assert.equal(newest[0]!.outbound, 'direct')
  assert.deepEqual(newest[1], first.entries[0])
  assert.equal(demo.journal('audit').entries[0]!.action, 'config.apply')
  const firstApply = demo.journal('audit').entries[0]!
  demo.save(config)
  assert.ok(demo.journal('audit').entries[0]!.id > firstApply.id)
  now += 24 * 60 * 60 * 1000
  const history = demo.journal('routes', { limit: 500 }).entries
  assert.equal(history.length, 120)
  assert.equal(Date.parse(history[0]!.time), now)
  assert.equal(Date.parse(history.at(-1)!.time), now - 119 * 3000)
})

test('preset updates report removed entries and retain their position', () => {
  const demo = createDemo()
  const config = demo.config()
  const old = config.groups![0]!.entries.slice()
  const result = demo.preview(config, 'sample-work', 'direct', 0)
  assert.equal(result.config.groups!.length, config.groups!.length)
  assert.equal(result.config.groups![0]!.preset!.id, 'sample-work')
  assert.deepEqual(result.diff.removed, old)
  const emptyId = demo.config()
  emptyId.connections![0]!.id = ''
  assert.throws(() => demo.validate(emptyId), /non-empty/i)
})
