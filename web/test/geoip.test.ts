import { test } from 'node:test'
import assert from 'node:assert/strict'
import { customGeo, geoName, move } from '../src/geoip.ts'

test('custom databases accept HTTPS URLs and absolute files only', () => {
  const url = customGeo('', 'https://geo.example/db-{month}.mmdb.gz', [])
  assert.equal(url.url, 'https://geo.example/db-{month}.mmdb.gz')
  assert.equal(url.name, 'geo.example')
  assert.match(url.id, /^custom-[0-9a-f]{12}$/)
  assert.equal(url.enabled, true)
  const file = customGeo('Mine', '/opt/etc/geo.mmdb', [])
  assert.deepEqual([file.file, file.url, file.name], ['/opt/etc/geo.mmdb', undefined, 'Mine'])
  for (const bad of ['http://geo.example/db.mmdb', 'https://u:p@geo.example/db.mmdb', 'geo.mmdb', ''])
    assert.throws(() => customGeo('', bad, []), /geoip.badSource/)
  assert.throws(() => customGeo('x'.repeat(65), '/a.mmdb', []), /geoip.longName/)
})

test('names come from the catalog for built-ins', () => {
  const builtins = [{ id: 'dbip', name: 'DB-IP', homepage: '', license: '', updates: 'monthly', enabled: true, origin: '' }]
  assert.equal(geoName({ id: 'dbip', enabled: true }, builtins), 'DB-IP')
  assert.equal(geoName({ id: 'custom-1', name: 'Own', enabled: true }, builtins), 'Own')
  assert.equal(geoName({ id: 'custom-2', enabled: true }, builtins), 'custom-2')
})

test('move reorders priority within bounds', () => {
  const list = ['a', 'b', 'c']
  move(list, 2, -1)
  assert.deepEqual(list, ['a', 'c', 'b'])
  move(list, 0, -1)
  move(list, 2, 1)
  assert.deepEqual(list, ['a', 'c', 'b'])
})

test('category databases and options', async () => {
  const { categoryOptions, hasCategoryDatabase, isCategoryDB } = await import('../src/geoip.ts')
  const builtins = [{ id: 'rf', name: 'RF', kind: 'category' as const, categories: ['RU-BLOCKED', 'RE-FILTER'], homepage: '', license: '', updates: '6h', enabled: false, origin: '' }]
  assert.equal(isCategoryDB({ id: 'rf', enabled: true }, builtins), true)
  assert.equal(isCategoryDB({ id: 'c1', kind: 'category', enabled: true }, builtins), true)
  assert.equal(isCategoryDB({ id: 'dbip', enabled: true }, builtins), false)
  assert.equal(hasCategoryDatabase([{ id: 'rf', enabled: false }], builtins), false)
  assert.equal(hasCategoryDatabase([{ id: 'rf', enabled: true }], builtins), true)
  const opts = categoryOptions(builtins, [{ name: 'g', target: 'direct', entries: [], categories: ['TELEGRAM', 'RU-BLOCKED'] }])
  assert.deepEqual(opts.map((o) => o.value), ['RE-FILTER', 'RU-BLOCKED', 'TELEGRAM'])
})
