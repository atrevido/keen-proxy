import { test } from 'node:test'
import assert from 'node:assert/strict'
import { firewallJournal, firewallJournalEntry } from '../src/firewallJournal.ts'
import type { FirewallEvent, JournalEntry } from '../src/types.ts'

const labels: Record<string, string> = {
  'journal.firewallRestored': 'Interception rules restored',
  'journal.firewallApplyFailed': 'Could not restore interception rules',
  'journal.firewallVerifyFailed': 'Rules failed verification after recovery',
  'journal.firewallHook': 'Netfilter hook',
  'journal.firewallWatchdog': 'Watchdog timer',
  'journal.firewallUnknown': 'Not recorded',
}
const t = (key: string, values?: Record<string, string | number>) =>
  key === 'journal.firewallDuration' ? `Check and recovery: ${values!.ms} ms` : labels[key] ?? key
const event = (extra: Partial<FirewallEvent> = {}): FirewallEvent => ({
  trigger: 'netfilter_hook', duration_ms: 56, outcome: 'restored',
  before: { table: 'nat', chain: 'PREROUTING', target: 'KEENPROXY_TCP', exit_code: 2, reason: 'command_failed', error: 'iptables: synthetic command error\nTry --help' },
  ...extra,
})

test('recovery row is concise, identifies its trigger and keeps raw evidence in details', () => {
  const view = firewallJournal(event(), t)
  assert.equal(view.title, 'Interception rules restored')
  assert.equal(view.source, 'Netfilter hook')
  assert.equal(view.summary, 'nat / PREROUTING → KEENPROXY_TCP · Check and recovery: 56 ms')
  assert.ok(view.details.some(([key, v]) => key === 'journal.firewallBeforeError' && v === event().before!.error))
  assert.ok(view.details.some(([, v]) => v === '2'))
  assert.ok(view.details.some(([, v]) => v === 'command_failed'))
  assert.ok(!view.summary.includes('iptables'))
})

test('timer and historical entries remain distinguishable, zero duration is valid', () => {
  const poll = firewallJournal(event({ trigger: 'watchdog', duration_ms: 0 }), t)
  assert.equal(poll.source, 'Watchdog timer')
  assert.match(poll.summary, /recovery: 0 ms/)
  const old = firewallJournal(event({ trigger: undefined, duration_ms: undefined }), t)
  assert.equal(old.source, 'Not recorded')
  assert.equal(old.summary, 'nat / PREROUTING → KEENPROXY_TCP')
  const unknown = firewallJournal(event({ trigger: 'future_source', duration_ms: -1 } as unknown as Partial<FirewallEvent>), t)
  assert.equal(unknown.source, 'Not recorded')
  assert.equal(unknown.summary, old.summary)
})

test('application and verification failures expose their distinct evidence', () => {
  const apply = firewallJournal(event({ outcome: 'apply_failed', apply_error: 'restore: synthetic permission denied' }), t)
  assert.equal(apply.title, 'Could not restore interception rules')
  assert.ok(apply.details.some(([, v]) => v === 'restore: synthetic permission denied'))
  const after = { ...event().before!, table: 'filter', chain: 'FORWARD', target: 'KEENPROXY_QUIC', error: 'still missing' }
  const verify = firewallJournal(event({ outcome: 'verify_failed', after }), t)
  assert.equal(verify.title, 'Rules failed verification after recovery')
  assert.ok(verify.details.some(([key, v]) => key === 'journal.firewallAfterRule' && v === 'filter / FORWARD → KEENPROXY_QUIC'))
  assert.ok(verify.details.some(([, v]) => v === 'still missing'))
})

test('partial historical records do not invent a rule or raw error', () => {
  const partial = firewallJournal(event({ before: null, duration_ms: undefined }), t)
  assert.equal(partial.summary, '')
  assert.ok(partial.details.every(([, v]) => v !== undefined))
})

test('pre-diagnostics audit entries retain legacy details without implying a timer trigger', () => {
  const old: JournalEntry = { id: 1, time: '2026-01-01T00:00:00Z', actor: 'watchdog', target: 'firewall', action: 'Interception rules restored', details: 'legacy raw error' }
  const view = firewallJournalEntry(old, t)!
  assert.equal(view.source, 'Not recorded')
  assert.equal(view.title, 'Interception rules restored')
  assert.equal(view.summary, '')
  assert.ok(view.details.some(([, value]) => value === 'legacy raw error'))
  assert.equal(firewallJournalEntry({ ...old, action: 'Interception rules not restored' }, t)!.title, 'Could not restore interception rules')
  assert.deepEqual(firewallJournalEntry({ ...old, details: undefined }, t)!.details, [])
  assert.equal(firewallJournalEntry({ ...old, target: 'core' }, t), undefined)
  assert.equal(firewallJournalEntry({ ...old, actor: 'user' }, t), undefined)
  assert.equal(firewallJournalEntry({ ...old, action: undefined }, t), undefined)
  assert.equal(firewallJournalEntry({ ...old, firewall: event() }, t)!.source, 'Netfilter hook')
})

test('missing error text and empty reason stay omitted, while exit code zero is retained', () => {
  const view = firewallJournal(event({ before: { ...event().before!, error: undefined, reason: '', exit_code: 0 } }), t)
  assert.ok(view.details.some(([key, value]) => key === 'journal.firewallExitCode' && value === '0'))
  assert.ok(!view.details.some(([key]) => key === 'journal.firewallBeforeError' || key === 'journal.firewallReason'))
})
