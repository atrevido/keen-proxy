import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  sameAppliedConnection,
  safeDiagnosticsReport,
} from '../src/diagnostics.ts'
import type { Connection, Status, DiagnosticReport } from '../src/types.ts'
test('edited profile cannot borrow results from old applied definition', () => {
  const c: Connection = {
    id: 'one',
    name: 'One',
    type: 'link',
    link: 'vless://example',
  }
  assert.equal(sameAppliedConnection(c, { ...c }), true)
  assert.equal(
    sameAppliedConnection({ ...c, link: 'vless://changed' }, c),
    false,
  )
  assert.equal(sameAppliedConnection(c, undefined), false)
})
test('safe report cannot export raw GeoIP errors or arbitrary check errors', () => {
  const status = {
    geoip: { loaded: false, error: 'https://example.test/file?token=PRIVATE' },
    firewall: true,
    mode: 'redirect',
  } as Status
  const checks: DiagnosticReport = {
    connection: 'one',
    results: [
      {
        kind: 'https',
        ok: false,
        checked: '2026-11-06T10:00:00Z',
        latency_ms: 100,
        error: 'password=PRIVATE',
      },
    ],
  }
  const result = JSON.stringify(
    safeDiagnosticsReport(status, checks, 'one', true, false),
  )
  assert.equal(result.includes('PRIVATE'), false)
  assert.equal(result.includes('token='), false)
  assert.equal(JSON.parse(result).checks[0].ok, false)
})
test('safe report represents unavailable status and successful applied checks', () => {
  const empty = safeDiagnosticsReport(null, null, '', false, true)
  assert.equal(empty.status, null)
  assert.deepEqual(empty.checks, [])
  const status = {
    mode: 'tproxy',
    firewall: true,
    geoip: { loaded: true },
    restored: 0,
  } as Status
  const report: DiagnosticReport = {
    connection: 'one',
    results: [
      {
        kind: 'http',
        ok: true,
        checked: '2026-11-06T10:00:00Z',
        latency_ms: 42,
      },
    ],
  }
  const safe = safeDiagnosticsReport(status, report, 'one', false, false)
  assert.equal(safe.checks[0]!.error, undefined)
  assert.equal(safe.status?.geoip_loaded, true)
})
test('diagnostics never claims DNS route verification', () => {
  assert.equal(
    safeDiagnosticsReport(null, null, 'one', false, false).dns_route_verified,
    false,
  )
})
