import samples from '../../../internal/api/demo_firewall.json' with { type: 'json' }
import type { JournalEntry } from '../types'

// Share synthetic evidence with the Go demo; each call owns its entry objects.
export function firewallAudit(started: number): JournalEntry[] {
  return (structuredClone(samples) as Omit<JournalEntry, 'id' | 'time'>[]).map((sample, index) => ({
    ...sample, id: index + 2, time: new Date(started - (samples.length - index) * 60000).toISOString(),
  }))
}
