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
  for (const kind of ['routes', 'audit', 'system'] as const) assert.equal(demo.journal(kind).entries.length, 1)
  assert.ok(demo.domains().records.length > 0)
  const config = demo.config()
  config.connections = []
  config.groups = []
  demo.save(config)
  assert.equal(demo.status().connections, 0)
  assert.deepEqual(demo.status().health, {})
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
