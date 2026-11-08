import { computed, reactive, ref } from 'vue'
import { api } from './api'
import { dnsPairIncomplete } from './dnsSettings'
import { t } from './i18n'
import type { Config, Device, Status } from './types'

// All screens edit this shared draft; Apply sends it in one daemon reload.
const state = reactive({
  config: null as Config | null,
  saved: '',
  status: null as Status | null,
  devices: [] as Device[],
  applying: false,
  error: '',
})

const invalidDNSPair = computed(() => state.config !== null && dnsPairIncomplete(
  state.config.dns.upstreams,
  state.saved ? (JSON.parse(state.saved) as Config).dns.upstreams : [],
))

export const authed = ref<boolean | null>(null)

// Records whether the current session entered from a trusted network.
export const session = reactive({ trusted: false, network: '', ip: '' })

// Fill optional collections in place so draft editors can mutate them safely.
export function normalize(c: Config): Config {
  c.connections ??= []
  c.groups ??= []
  c.networks ??= []
  c.clients ??= []
  c.dns.upstreams ??= []
  for (const g of c.groups) g.entries ??= []
  c.filters ??= { sources:[],exceptions:[] }
 c.filters.sources ??= []
 c.filters.exceptions ??= []
 c.geoip ??= { databases: [] }
 c.geoip.databases ??= []
  return c
}

// Owns the editable draft, last saved snapshot, and daemon status.
export const store = {
  state,
  dirty: computed(() => state.config !== null && JSON.stringify(state.config) !== state.saved),
  invalidDNSPair,

  // Replace only the draft; the saved snapshot remains the dirty-state baseline.
  setDraft(c:Config){state.config=normalize(c);state.error=''},

  async load() {
    const cfg = normalize(await api.config())
    state.config = cfg
    state.saved = JSON.stringify(cfg)
    state.error = ''
    authed.value = true
    await Promise.all([this.refreshStatus(), this.refreshDevices()])
  },

  async loadSession(open = false) {
    const s = await api.session(open)
    session.trusted = s.trusted
    session.network = s.network ?? ''
    session.ip = s.ip
    return s
  },

  async refreshStatus() {
    state.status = await api.status()
  },

  async refreshDevices() {
    state.devices = await api.devices()
  },

  // Persist the whole draft and advance the baseline only after a successful save.
  async apply() {
    if (!state.config) return
    if (invalidDNSPair.value) throw new Error(t('settings.dnsPairRequired'))
    state.applying = true
    state.error = ''
    try {
      const cfg = normalize(await api.saveConfig(state.config))
      state.config = cfg
      state.saved = JSON.stringify(cfg)
      await this.refreshStatus()
    } catch (e) {
      state.error = (e as Error).message
      throw e
    } finally {
      state.applying = false
    }
  },

  // Restore a fresh copy of the last saved configuration.
  discard() {
    if (state.saved) state.config = JSON.parse(state.saved)
    state.error = ''
  },
}

// Route choices include configured connections and built-in actions.
export const targets = computed(() => {
  const conns = (state.config?.connections ?? []).map((c) => ({ label: c.name || c.id, value: c.id }))
  return [...conns, { label: t('target.direct'), value: 'direct' }, { label: t('target.block'), value: 'block' }]
})

// Resolve built-in and configured targets, preserving unknown IDs for display.
export function targetLabel(id: string | undefined): string {
  if (!id) return '—'
  if (id === 'direct') return t('target.direct')
  if (id === 'block') return t('target.block')
  const c = state.config?.connections?.find((x) => x.id === id)
  return c ? c.name || c.id : id
}

export const modeOptions = computed(() => [
  { label: t('mode.split'), value: 'split' },
  { label: t('mode.full'), value: 'full' },
  { label: t('mode.off'), value: 'off' },
])
