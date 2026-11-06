import { test } from 'node:test'
import assert from 'node:assert/strict'
import { clientIP, groupRoutes } from '../src/journal.ts'
import type { JournalEntry } from '../src/types.ts'
const entry = (
  id: number,
  extra: Partial<JournalEntry> = {},
): JournalEntry => ({
  id,
  time: '2026-11-06T10:24:01Z',
  client: '192.168.7.4:5000',
  host: 'example.test',
  network: 'tcp',
  destination_port: 443,
  outbound: 'one',
  rule: 'Steam',
  ...extra,
})
test('same route in fixed window merges ephemeral ports without mutating input', () => {
  const a = entry(1),
    b = entry(2, { client: '192.168.7.4:6000', time: '2026-11-06T10:24:20Z' })
  const r = groupRoutes([b, a])
  assert.equal(r.length, 1)
  assert.equal(r[0]!.members.length, 2)
  assert.equal(r[0]!.firstTime, a.time)
  assert.equal(r[0]!.lastTime, b.time)
  assert.equal(a.count, undefined)
})
test('different devices paths reasons transport and destination ports stay separate', () => {
  const rows = [
    entry(1),
    entry(2, { client: '192.168.7.5:5000' }),
    entry(3, { outbound: 'two' }),
    entry(4, { rule: 'Other' }),
    entry(5, { network: 'udp' }),
    entry(6, { destination_port: 80 }),
    entry(7, { time: '2026-11-06T10:24:30Z' }),
  ]
  assert.equal(groupRoutes(rows).length, 7)
})
test('IPv6 source normalization never truncates an unbracketed address', () => {
  assert.equal(clientIP('[2001:db8::1]:443'), '2001:db8::1')
  assert.equal(clientIP('2001:db8::1'), '2001:db8::1')
  assert.equal(clientIP('192.0.2.1:123'), '192.0.2.1')
  assert.equal(clientIP('[2001:db8::1]'), '2001:db8::1')
})
test('ambiguous legacy records without port or rule identity stay separate', () => {
  assert.equal(
    groupRoutes([
      entry(1, { destination_port: undefined }),
      entry(2, { destination_port: undefined }),
    ]).length,
    2,
  )
  assert.equal(
    groupRoutes([
      entry(1, { rule: undefined, rule_raw: undefined }),
      entry(2, { rule: undefined, rule_raw: undefined }),
    ]).length,
    2,
  )
})
test('unadorned source addresses and absent source values are preserved', () => {
  assert.equal(clientIP('192.0.2.1'), '192.0.2.1')
  assert.equal(clientIP(), '')
})
test('destinations are part of identity and a single route has count one', () => {
  assert.equal(groupRoutes([entry(1)])[0]!.count, 1)
  assert.equal(
    groupRoutes([
      entry(1, { host: 'one.test' }),
      entry(2, { host: 'two.test' }),
    ]).length,
    2,
  )
  assert.equal(
    groupRoutes([
      entry(1, { host: undefined, ip: '203.0.113.1' }),
      entry(2, { host: undefined, ip: '203.0.113.2' }),
    ]).length,
    2,
  )
})
