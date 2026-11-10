import type { FirewallEvent, JournalEntry } from '../types'

// Synthetic recoveries exercise both trigger paths and a failed verification.
export function firewallAudit(started: number): JournalEntry[] {
  const before = { table: 'nat', chain: 'PREROUTING', target: 'KEENPROXY_TCP', exit_code: 2, reason: 'command_failed', error: "iptables: Couldn't load target 'KEENPROXY_TCP': No such file or directory" }
  const samples: Partial<FirewallEvent>[] = [
    { outcome: 'restored' },
    { outcome: 'verify_failed', trigger: 'netfilter_hook', duration_ms: 84,
      after: { ...before, table: 'filter', chain: 'FORWARD', target: 'KEENPROXY_QUIC', error: 'iptables: synthetic verification failure' } },
    { outcome: 'restored', trigger: 'watchdog', duration_ms: 47 },
    { outcome: 'restored', trigger: 'netfilter_hook', duration_ms: 56 },
  ]
  return samples.map((sample, index) => ({
    id: index + 2, time: new Date(started - (4 - index) * 60000).toISOString(),
    actor: 'watchdog', target: 'firewall',
    action: sample.outcome === 'restored' ? 'Interception rules restored' : 'Interception rules not restored',
    result: sample.outcome === 'restored' ? 'warn' : 'error',
    details: `Synthetic diagnostic record; trigger: ${sample.trigger || 'not recorded'}; before: nat/PREROUTING -> KEENPROXY_TCP; ${before.error}; outcome: ${sample.outcome}`,
    firewall: index === 0 ? undefined : { before, outcome: 'restored', ...sample },
  }))
}
