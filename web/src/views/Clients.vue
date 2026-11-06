<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NInput, NSelect, NSwitch, NTooltip, useDialog } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Users, Plus, Trash2, RefreshCw, ChevronDown, ChevronRight, Network as NetIcon, Search, UserPlus } from '@lucide/vue'
import { modeOptions, store, targets, targetLabel } from '../store'
import type { Client, Device, Network } from '../types'

const dialog = useDialog()
const { t } = useI18n()
const cfg = computed(() => store.state.config!)
const q = ref('')
const closed = ref<Record<string, boolean>>({})
const tunnelTargets = computed(() => targets.value.filter((x) => x.value !== 'block' && x.value !== 'direct'))

// Convert IPv4 addresses to unsigned integers for subnet membership checks.
function ip4(s: string): number | null {
  const m = s.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)$/)
  return m ? ((+m[1] << 24) | (+m[2] << 16) | (+m[3] << 8) | +m[4]) >>> 0 : null
}
function inNet(ip: string, cidr: string): boolean {
  const [base, bitsStr] = cidr.split('/')
  const a = ip4(ip), b = ip4(base), bits = Number(bitsStr)
  if (a === null || b === null || !(bits >= 0 && bits <= 32)) return false
  const mask = bits === 0 ? 0 : (~0 << (32 - bits)) >>> 0
  return (a & mask) === (b & mask)
}

// Combines a configured client with its discovered address and online state.
interface Row {
  key: string
  client?: Client
  device?: Device
  ip: string
  mac: string
}

const online = computed(() => new Map(store.state.devices.map((d) => [d.mac.toLowerCase(), d])))

// Merge configured and ARP-discovered devices, deduplicating by MAC.
const rows = computed<Row[]>(() => {
  const out: Row[] = []
  const seen = new Set<string>()
  for (const c of cfg.value.clients!) {
    const mac = c.mac.toLowerCase()
    seen.add(mac)
    const d = online.value.get(mac)
    out.push({ key: 'c:' + mac + out.length, client: c, device: d, ip: c.ip || d?.ip || '', mac })
  }
  for (const d of store.state.devices) {
    const mac = d.mac.toLowerCase()
    if (!seen.has(mac)) out.push({ key: 'd:' + mac, device: d, ip: d.ip, mac })
  }
  const needle = q.value.trim().toLowerCase()
  return needle
    ? out.filter((r) => r.ip.includes(needle) || r.mac.includes(needle) || (r.client?.name ?? '').toLowerCase().includes(needle))
    : out
})

const sections = computed(() => {
  const nets = cfg.value.networks!
  const list = nets.map((n) => ({ net: n as Network | null, key: n.cidr || n.name, rows: [] as Row[] }))
  const other = { net: null as Network | null, key: '_other', rows: [] as Row[] }
  for (const r of rows.value) {
    const i = nets.findIndex((n) => r.ip && inNet(r.ip, n.cidr))
    ;(i >= 0 ? list[i] : other).rows.push(r)
  }
  return other.rows.length ? [...list, other] : list
})

function networkOf(r: Row): Network | undefined {
  return cfg.value.networks!.find((n) => r.ip && inNet(r.ip, n.cidr))
}

// Resolve participation and full-tunnel state from client overrides and network mode.
function effective(r: Row) {
  const net = networkOf(r)
  const mode = r.client?.mode || net?.mode || 'off'
  return { mode, inherited: !r.client?.mode, target: r.client?.target || net?.target }
}

// Create a draft override only when a discovered device is edited.
function ensureClient(r: Row): Client {
  if (r.client) return r.client
  const c: Client = { name: t('clients.deviceIP', { ip: r.ip }), mac: r.mac, mode: '' }
  cfg.value.clients!.push(c)
  return c
}

function setParticipation(r: Row, on: boolean) {
  const c = ensureClient(r)
  c.mode = on ? (networkOf(r)?.mode && networkOf(r)?.mode !== 'off' ? '' : 'split') : 'off'
}

function setFull(r: Row, on: boolean) {
  const c = ensureClient(r)
  if (on) {
    c.mode = 'full'
    c.target ||= networkOf(r)?.target || tunnelTargets.value[0]?.value
  } else {
    c.mode = ''
  }
}

function removeClient(r: Row) {
  if (!r.client) return
  dialog.warning({
    title: t('clients.removeTitle'),
    content: t('clients.removeText'),
    positiveText: t('clients.remove'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      cfg.value.clients!.splice(cfg.value.clients!.indexOf(r.client!), 1)
    },
  })
}

function addNetwork() {
  cfg.value.networks!.push({ name: t('clients.newNetwork'), cidr: '', mode: 'split' })
}

function addClient() {
  cfg.value.clients!.push({ name: t('clients.newDevice'), mac: '', mode: '' })
}

function modeText(m: string) {
  return m === 'full' ? t('clients.mFull') : m === 'split' ? t('clients.mSplit') : t('clients.mOff')
}
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <h1><Users :size="24" class="title-icon" /> {{ t('nav.clients') }}</h1>
        <p>{{ t('clients.intro') }}</p>
      </div>
    </div>

    <div class="toolbar">
      <NInput v-model:value="q" :placeholder="t('clients.search')" clearable size="small">
        <template #prefix><Search :size="14" /></template>
      </NInput>
      <NTooltip><template #trigger><NButton size="small" type="info" :aria-label="t('clients.addDevice')" @click="addClient"><template #icon><UserPlus :size="15" /></template></NButton></template>{{ t('clients.addDevice') }}</NTooltip>
      <NTooltip><template #trigger><NButton size="small" :aria-label="t('clients.newNetwork')" @click="addNetwork"><template #icon><Plus :size="15" /></template></NButton></template>{{ t('clients.newNetwork') }}</NTooltip>
      <NTooltip><template #trigger><NButton size="small" :aria-label="t('clients.refresh')" @click="store.refreshDevices()"><template #icon><RefreshCw :size="15" /></template></NButton></template>{{ t('clients.refresh') }}</NTooltip>
    </div>

    <div class="table">
      <div class="thead">
        <span>{{ t('clients.thName') }}</span><span>{{ t('clients.thIP') }}</span><span>{{ t('clients.thMAC') }}</span><span>{{ t('groups.colTarget') }}</span>
        <span class="c">{{ t('mode.full') }}</span><span class="c">{{ t('clients.thPart') }}</span><span class="r">{{ t('clients.thActions') }}</span>
      </div>

      <template v-for="sec in sections" :key="sec.key">
        <div class="section">
          <button class="toggle" :aria-label="closed[sec.key] ? t('clients.expand') : t('clients.collapse')" @click="closed[sec.key] = !closed[sec.key]">
            <ChevronRight v-if="closed[sec.key]" :size="15" /><ChevronDown v-else :size="15" />
          </button>
          <NetIcon :size="16" class="net-icon" />
          <template v-if="sec.net">
            <NInput v-model:value="sec.net.name" size="tiny" class="net-name" />
            <span class="muted">({{ sec.rows.length }})</span>
            <NInput v-model:value="sec.net.cidr" size="tiny" class="mono net-cidr" placeholder="192.168.1.0/24" />
            <span class="spacer" />
            <NSelect v-model:value="sec.net.mode" :options="modeOptions" size="tiny" class="net-mode" />
            <NSelect
              v-if="sec.net.mode === 'full'"
              v-model:value="sec.net.target"
              :options="tunnelTargets"
              size="tiny"
              class="net-target"
              :placeholder="t('clients.connection')"
            />
            <NButton size="tiny" quaternary type="error" :aria-label="t('clients.deleteNetwork')" @click="cfg.networks!.splice(cfg.networks!.indexOf(sec.net!), 1)">
              <template #icon><Trash2 :size="14" /></template>
            </NButton>
          </template>
          <template v-else>
            <b>{{ t('clients.outside') }}</b>
            <span class="muted">({{ sec.rows.length }})</span>
          </template>
        </div>

        <template v-if="!closed[sec.key]">
          <div v-for="r in sec.rows" :key="r.key" class="trow">
            <span class="name">
              <i :class="['dot', r.device ? 'on' : 'off']" :title="r.device ? t('clients.online') : t('clients.offline')" />
              <NInput v-if="r.client" v-model:value="r.client.name" size="tiny" />
              <span v-else class="muted">{{ t('clients.noName') }}</span>
            </span>
            <span><span class="chip mono">{{ r.ip || '—' }}</span></span>
            <span>
              <NInput v-if="r.client" v-model:value="r.client.mac" size="tiny" class="mono" placeholder="aa:bb:cc:dd:ee:ff" />
              <span v-else class="chip mono">{{ r.mac }}</span>
            </span>
            <span>
              <NSelect
                v-if="effective(r).mode === 'full' && r.client?.mode === 'full'"
                :value="r.client.target"
                :options="tunnelTargets"
                size="tiny"
                @update:value="(v: string) => (r.client!.target = v)"
              />
              <span v-else class="conn-text">
                → {{ effective(r).mode === 'full' ? targetLabel(effective(r).target) : effective(r).mode === 'split' ? t('clients.byWhitelist') : t('clients.directNoTunnel') }}
                <small v-if="effective(r).inherited" class="muted"> · {{ t('clients.asNetwork') }}</small>
              </span>
            </span>
            <span class="c full">
              <small class="m-label muted">{{ t('mode.full') }}</small>
              <NSwitch size="small" :value="effective(r).mode === 'full'" :disabled="effective(r).mode === 'off'" @update:value="(v: boolean) => setFull(r, v)" />
            </span>
            <span class="c part">
              <NSwitch size="small" :value="effective(r).mode !== 'off'" @update:value="(v: boolean) => setParticipation(r, v)" />
              <small :class="effective(r).mode !== 'off' ? 'ok' : 'muted'">{{ effective(r).mode !== 'off' ? t('clients.participates') : t('mode.off') }}</small>
            </span>
            <span class="r">
              <NButton v-if="r.client" size="tiny" quaternary type="error" :aria-label="t('clients.removeAria')" @click="removeClient(r)">
                <template #icon><Trash2 :size="14" /></template>
              </NButton>
            </span>
          </div>
          <div v-if="!sec.rows.length" class="trow empty muted">
            {{ t('clients.noDevices') }}<template v-if="sec.net"> · {{ t('clients.netMode', { mode: modeText(sec.net.mode) }) }}</template>
          </div>
        </template>
      </template>
    </div>
    <p class="hint">{{ t('clients.hint') }}</p>
  </div>
</template>

<style scoped>
.title-icon {
  color: #60a5fa;
}
.toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.table {
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow: hidden;
  font-size: 13px;
}
.thead,
.trow {
  display: grid;
  grid-template-columns: 2.2fr 1fr 1.3fr 1.8fr 0.9fr 1.1fr 0.6fr;
  gap: 12px;
  align-items: center;
  padding: 8px 14px;
}
.thead {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--muted);
  border-bottom: 1px solid var(--line);
}
.trow {
  border-bottom: 1px solid var(--line);
}
.trow:hover {
  background: rgba(148, 163, 184, 0.05);
}
.section {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: rgba(59, 130, 246, 0.06);
  border-bottom: 1px solid var(--line);
}
.toggle {
  background: none;
  border: 0;
  color: var(--muted);
  cursor: pointer;
  display: grid;
  place-items: center;
}
.net-icon {
  color: #60a5fa;
}
.net-name {
  max-width: 200px;
}
.net-cidr {
  max-width: 150px;
}
.net-mode {
  width: 170px;
}
.net-target {
  width: 180px;
}
.spacer {
  flex: 1;
}
.name {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
.dot.on {
  background: var(--ok);
}
.dot.off {
  background: var(--muted);
  opacity: 0.5;
}
.chip {
  border: 1px solid var(--line);
  border-radius: 4px;
  padding: 2px 6px;
}
.conn-text {
  font-size: 12px;
}
.c {
  text-align: center;
}
.r {
  text-align: right;
}
.part {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.ok {
  color: var(--ok);
}
.empty {
  display: block;
  font-size: 12px;
}
.hint {
  font-size: 12px;
  color: var(--muted);
  margin-top: 12px;
}
.m-label {
  display: none;
}
/* Narrow screens show device rows as labeled, two-column cards. */
@media (max-width: 1000px) {
  .thead {
    display: none;
  }
  .trow {
    grid-template-columns: 1fr 1fr;
    padding: 10px 12px;
    border-bottom: 1px solid var(--line);
  }
  .c,
  .part {
    text-align: left;
    justify-content: flex-start;
  }
  .full {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .m-label {
    display: inline;
  }
  .section {
    flex-wrap: wrap;
    row-gap: 8px;
  }
  .net-name {
    max-width: none;
    flex: 1 1 140px;
  }
  .net-cidr {
    max-width: none;
    flex: 1 1 130px;
  }
  .section .spacer {
    display: none;
  }
  .net-mode,
  .net-target {
    flex: 1 1 150px;
    width: auto;
  }
}
</style>
