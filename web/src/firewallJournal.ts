import type { FirewallCheckFailure, FirewallEvent, JournalEntry } from './types'

type Translate = (key: string, values?: Record<string, string | number>) => string
type Detail = [string, string]

function rule(check: FirewallCheckFailure) {
  return `${check.table} / ${check.chain} → ${check.target}`
}

// Present structured evidence without guessing a historical trigger or outage time.
export function firewallJournal(event: FirewallEvent, t: Translate, fallbackTitle = '') {
  const titles = {
    restored: 'journal.firewallRestored',
    apply_failed: 'journal.firewallApplyFailed',
    verify_failed: 'journal.firewallVerifyFailed',
  }
  // Journals can contain outcomes written by a newer version.
  const title = Object.hasOwn(titles, event.outcome) ? t(titles[event.outcome]) : fallbackTitle
  const sourceKey = event.trigger === 'netfilter_hook' ? 'journal.firewallHook'
    : event.trigger === 'watchdog' ? 'journal.firewallWatchdog' : 'journal.firewallUnknown'
  const source = t(sourceKey)
  const duration = event.duration_ms != null && event.duration_ms >= 0
    ? t('journal.firewallDuration', { ms: event.duration_ms }) : ''
  const summary = [event.before ? rule(event.before) : '', duration].filter(Boolean).join(' · ')
  const details: Detail[] = [[t('journal.firewallTrigger'), source]]
  if (duration) details.push([t('journal.firewallTiming'), duration])
  details.push(...probeDetails(event.before, false, t))
  if (event.apply_error) details.push([t('journal.firewallApplyError'), event.apply_error])
  details.push(...probeDetails(event.after, true, t))
  return { title, source, summary, details }
}

// The first probe and failed verification retain the same fields with distinct labels.
function probeDetails(check: FirewallCheckFailure | null | undefined, after: boolean, t: Translate): Detail[] {
  if (!check) return []
  const keys = after
    ? ['firewallAfterRule', 'firewallAfterExit', 'firewallAfterReason', 'firewallAfterError']
    : ['rule', 'firewallExitCode', 'firewallReason', 'firewallBeforeError']
  const values = [rule(check), String(check.exit_code), check.reason, check.error]
  return values.flatMap((value, index): Detail[] => value ? [[t(`journal.${keys[index]}`), value]] : [])
}

// Older audit records have no structured payload; never infer their trigger from actor.
export function firewallJournalEntry(entry: JournalEntry, t: Translate) {
  if (entry.firewall) return firewallJournal(entry.firewall, t, entry.action)
  const titles: Record<string, string> = {
    'Interception rules restored': 'journal.firewallRestored',
    'Interception rules not restored': 'journal.firewallApplyFailed',
  }
  const title = titles[entry.action || '']
  if (entry.actor !== 'watchdog' || entry.target !== 'firewall' || !title) return undefined
  return { title: t(title), source: t('journal.firewallUnknown'), summary: '',
    details: entry.details ? [[t('journal.details'), entry.details] as Detail] : [] }
}
