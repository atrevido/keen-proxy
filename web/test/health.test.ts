import { test } from 'node:test'
import assert from 'node:assert/strict'
import { healthState } from '../src/health.ts'
const now = Date.parse('2026-11-06T12:00:00Z')
test('a running process without a probe is not a healthy VPN', () => {
  assert.equal(healthState(undefined, 15, now), 'unknown')
})
test('a failed HTTP probe is down even if it has a latency', () => {
  assert.equal(healthState({ok:false, checked:'2026-11-06T11:59:58Z', latency_ms:10},15,now),'down')
})
test('an expired successful probe cannot keep a green status', () => {
  assert.equal(healthState({ok:true, checked:'2026-11-06T11:57:00Z',latency_ms:20},15,now),'stale')
})
test('a fresh successful probe is up', () => {
  assert.equal(healthState({ok:true, checked:'2026-11-06T11:59:58Z',latency_ms:0},15,now),'up')
})
test('invalid probe time is unknown', () => {
  assert.equal(healthState({ok:true, checked:'bad',latency_ms:20},15,now),'unknown')
})

test('a successful result expires even when no new status arrives', async (t) => {
  const { effectScope } = await import('vue')
  const { useHealthState } = await import('../src/health.ts')
  t.mock.timers.enable({ apis: ['Date', 'setInterval'], now })
  const scope = effectScope()
  try {
    const state = scope.run(() => useHealthState(() => ({ok:true, checked:'2026-11-06T12:00:00Z', latency_ms:20}), () => 15))!
    assert.equal(state.value, 'up')
    t.mock.timers.tick(120_000)
    assert.equal(state.value, 'stale')
  } finally {
    scope.stop()
    t.mock.timers.reset()
  }
})
