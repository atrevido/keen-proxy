import { computed, onScopeDispose, ref } from 'vue'

// Last HTTP probe result, with an ISO check time and latency in milliseconds.
export interface ProbeHealth { ok: boolean; checked: string; latency_ms: number; error?: string }
// Expire old or future-skewed probes before interpreting their success flag.
export function healthState(health: ProbeHealth | null | undefined, interval = 15, now = Date.now()): 'unknown' | 'stale' | 'up' | 'down' {
  if (!health || !Number.isFinite(Date.parse(health.checked))) return 'unknown'
  if (Math.abs(now - Date.parse(health.checked)) > Math.max(30, interval * 3 + 20) * 1000) return 'stale'
  return health.ok ? 'up' : 'down'
}

// Tick independently of successful API polls so cached green results expire offline.
export function useHealthState(health: () => ProbeHealth | null | undefined, interval: () => number | undefined) {
  const now = ref(Date.now())
  const timer = setInterval(() => { now.value = Date.now() }, 1000)
  onScopeDispose(() => clearInterval(timer))
  return computed(() => healthState(health(), interval(), now.value))
}
