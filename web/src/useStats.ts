import { onMounted, onUnmounted, ref } from 'vue'
import { api } from './api'
import type { OutboundStats, StatsSnapshot } from './types'

// Poll while mounted and retain the last successful snapshot on errors.
export function useStats(intervalMs = 3000, series = true) {
  const snap = ref<StatsSnapshot | null>(null)
  let timer: number | undefined
  async function load() {
    try {
      snap.value = await api.stats(series)
    } catch {
      /* The shell handles unauthorized responses. */
    }
  }
  onMounted(() => {
    load()
    timer = window.setInterval(load, intervalMs)
  })
  onUnmounted(() => clearInterval(timer))
  const byTag = (tag: string): OutboundStats | undefined => snap.value?.outbounds?.find((o) => o.tag === tag)
  return { snap, byTag, reload: load }
}
