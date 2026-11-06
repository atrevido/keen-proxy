<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { NButton, NInput, NTooltip, useDialog, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  Plug,
  Plus,
  Pencil,
  Trash2,
  Activity,
  Globe,
  Server,
  Hash,
  Clock,
  ListChecks,
  ArrowDown,
  ArrowUp,
  KeyRound,
  Search,
  ArrowRightCircle,
  Ban,
  Users,
  LifeBuoy,
  HeartPulse,
} from '@lucide/vue'
import { store } from '../store'
import { api } from '../api'
import { useStats } from '../useStats'
import { ago, bits, bytes, duration, mbit } from '../format'
import { mask, protocolOf, serverOf } from '../conn'
import { countryName, flag } from '../countries'
import type { Connection, HostStat } from '../types'
import ConnectionHealth from '../components/ConnectionHealth.vue'
import AreaChart from '../components/AreaChart.vue'
import ConnectionDialog from '../components/ConnectionDialog.vue'
import ConnectionWizard from '../components/ConnectionWizard.vue'
import FallbackPanel from '../components/FallbackPanel.vue'

const dialog = useDialog()
const message = useMessage()
const { t, locale } = useI18n()
const cfg = computed(() => store.state.config!)
const list = computed(() => cfg.value.connections!)
const { snap, byTag } = useStats(3000, true)

const tab = ref(list.value[0]?.id ?? 'direct')
watch(
  list,
  (l) => {
    if (
      !['direct', 'block'].includes(tab.value) &&
      !l.some((c) => c.id === tab.value)
    )
      tab.value = l[0]?.id ?? 'direct'
  },
  { deep: true },
)

const conn = computed<Connection | undefined>(() =>
  list.value.find((c) => c.id === tab.value),
)
const st = computed(() => byTag(tab.value))

const times = computed(() => (st.value?.series ?? []).map((p) => p.t))
const series = computed(() => [
  {
    name: t('conn.in'),
    color: 'var(--accent)',
    values: (st.value?.series ?? []).map((p) => p.down),
  },
  {
    name: t('conn.out'),
    color: 'var(--muted)',
    values: (st.value?.series ?? []).map((p) => p.up),
    fill: false,
  },
])

const usage = computed(() => {
  const id = tab.value
  return {
    groups: (cfg.value.groups ?? []).filter((g) => g.target === id),
    entries: (cfg.value.groups ?? [])
      .filter((g) => g.target === id)
      .reduce((n, g) => n + g.entries.length, 0),
    clients:
      (cfg.value.clients ?? []).filter((c) => c.target === id).length +
      (cfg.value.networks ?? []).filter((n) => n.target === id).length,
    backupFor: list.value.filter((c) => c.fallback === id),
  }
})

const connName = (id: string) =>
  id === 'direct'
    ? t('target.direct')
    : list.value.find((c) => c.id === id)?.name || id

// Show the currently active exit in the applied fallback chain.
const failover = computed(() =>
  conn.value?.fallback
    ? store.state.status?.failover?.[conn.value.id]
    : undefined,
)
const onBackup = computed(
  () => !!failover.value && failover.value.active !== conn.value?.id,
)
const health = computed(() =>
  conn.value ? store.state.status?.health?.[conn.value.id] : undefined,
)
const healthText = computed(() => {
  const h = health.value
  if (!h) return ''
  if (h.ok) return t('conn.healthOk', { ms: h.latency_ms })
  return t('conn.healthDown', {
    time: new Date(h.since).toLocaleTimeString(locale.value),
  })
})

// Connection summary row with an icon and optional monospace formatting.
interface Row {
  icon: unknown
  label: string
  value: string
  mono?: boolean
}
const rows = computed<Row[]>(() => {
  const c = conn.value
  const base: Row[] = []
  if (c) {
    const sv = serverOf(c)
    base.push({
      icon: Server,
      label: t('conn.server'),
      value: sv.host || '—',
      mono: true,
    })
    const cc = store.state.status?.conn_countries?.[c.id]
    if (cc)
      base.push({
        icon: Globe,
        label: t('conn.country'),
        value: `${flag(cc)} ${countryName(cc)}`,
      })
    if (sv.port)
      base.push({
        icon: Hash,
        label: t('conn.port'),
        value: sv.port,
        mono: true,
      })
    if (c.wireguard)
      base.push({
        icon: Globe,
        label: t('conn.localIP'),
        value: c.wireguard.address.join(', '),
        mono: true,
      })
  }
  base.push(
    {
      icon: Activity,
      label: t('conn.activeNow'),
      value: String(st.value?.active ?? 0),
    },
    {
      icon: ArrowDown,
      label: t('conn.received'),
      value: bytes(st.value?.down_total ?? 0),
    },
    {
      icon: ArrowUp,
      label: t('conn.sent'),
      value: bytes(st.value?.up_total ?? 0),
    },
    {
      icon: ListChecks,
      label: t('conn.rules'),
      value: t('conn.rulesValue', {
        n: usage.value.entries,
        g: usage.value.groups.length,
      }),
    },
    {
      icon: Users,
      label: t('conn.devicesNets'),
      value: String(usage.value.clients),
    },
  )
  if (c?.fallback) {
    const chain = failover.value?.chain ?? [c.id, c.fallback]
    base.push({
      icon: LifeBuoy,
      label: t('conn.backups'),
      value: chain.slice(1).map(connName).join(' → '),
    })
  }
  if (usage.value.backupFor.length) {
    base.push({
      icon: LifeBuoy,
      label: t('conn.backupFor'),
      value: usage.value.backupFor.map((x) => x.name || x.id).join(', '),
    })
  }
  if (healthText.value)
    base.push({
      icon: HeartPulse,
      label: t('conn.check'),
      value: healthText.value,
    })
  if (store.state.status?.started)
    base.push({
      icon: Clock,
      label: t('conn.uptime'),
      value: duration(store.state.status.started),
    })
  return base
})

const specific = computed<Row[]>(() => {
  const c = conn.value
  if (!c) return []
  if (c.wireguard) {
    return [
      {
        icon: KeyRound,
        label: t('conn.publicKey'),
        value: mask(c.wireguard.peer.public_key),
        mono: true,
      },
      {
        icon: Server,
        label: 'Endpoint',
        value: c.wireguard.peer.endpoint,
        mono: true,
      },
      {
        icon: Globe,
        label: 'Allowed IPs',
        value: (c.wireguard.peer.allowed_ips ?? ['0.0.0.0/0', '::/0']).join(
          ', ',
        ),
        mono: true,
      },
    ]
  }
  if (c.link) {
    const q = new URLSearchParams(c.link.split('?')[1]?.split('#')[0] ?? '')
    const out: Row[] = []
    if (q.get('security'))
      out.push({
        icon: KeyRound,
        label: t('conn.security'),
        value: q.get('security')!,
      })
    if (q.get('type'))
      out.push({
        icon: Activity,
        label: t('conn.transport'),
        value: q.get('type')!,
      })
    if (q.get('sni'))
      out.push({ icon: Globe, label: 'SNI', value: q.get('sni')!, mono: true })
    out.push({
      icon: KeyRound,
      label: t('conn.link'),
      value: mask(c.link),
      mono: true,
    })
    return out
  }
  if (c.trusttunnel)
    return [
      {
        icon: KeyRound,
        label: t('conn.ttLink'),
        value: mask(c.trusttunnel.link),
        mono: true,
      },
    ]
  return [
    {
      icon: Activity,
      label: t('conn.sbType'),
      value: String((c.outbound ?? c.endpoint)?.type ?? '—'),
    },
  ]
})

// Host-level traffic for the selected connection.
const hosts = ref<HostStat[]>([])
const hostsTotal = ref(0)
const q = ref('')
async function loadHosts() {
  try {
    const r = await api.hosts(tab.value, q.value, 300)
    hosts.value = r.hosts
    hostsTotal.value = r.total
  } catch {
    /* Preserve existing host rows when refresh fails. */
  }
}
watch([tab, q], loadHosts)
let hostTimer: number | undefined
onMounted(() => {
  loadHosts()
  hostTimer = window.setInterval(loadHosts, 5000)
})
onUnmounted(() => clearInterval(hostTimer))

// Local editor and wizard visibility.
const show = ref(false)
const wizard = ref(false)
const backupOpen = ref(false)
const showSpecific = ref(false)
watch(tab, () => {
  backupOpen.value = false
  showSpecific.value = false
})
const editing = ref<Connection | null>(null)
function add() {
  wizard.value = true
}
function edit() {
  if (!conn.value) return
  editing.value = conn.value
  show.value = true
}
function remove() {
  const c = conn.value
  if (!c) return
  const used =
    usage.value.groups.length +
    usage.value.clients +
    usage.value.backupFor.length
  if (used) {
    const backup = usage.value.backupFor.length
      ? t('conn.inUseBackup', { n: usage.value.backupFor.length })
      : ''
    message.warning(
      t('conn.inUse', {
        g: usage.value.groups.length,
        c: usage.value.clients,
        backup,
      }),
    )
    return
  }
  dialog.warning({
    title: t('conn.deleteTitle'),
    content: t('conn.deleteText', { name: c.name || c.id }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      list.value.splice(list.value.indexOf(c), 1)
    },
  })
}
</script>

<template>
  <ConnectionWizard
    v-if="wizard"
    @close="wizard = false"
    @saved="
      (id) => {
        tab = id
        wizard = false
      }
    "
  />
  <div v-else>
    <div class="page-head">
      <div>
        <h1>{{ t('nav.connections') }}</h1>
        <p>{{ t('conn.intro') }}</p>
      </div>
      <NButton type="primary" @click="add"
        ><template #icon><Plus :size="18" /></template
        >{{ t('common.add') }}</NButton
      >
    </div>
    <div class="connection-layout">
      <nav class="connection-list" :aria-label="t('nav.connections')">
        <button
          v-for="c in list"
          :key="c.id"
          class="connection-item"
          :class="{ selected: tab === c.id }"
          @click="tab = c.id"
        >
          <Plug :size="22" /><span
            ><b>{{ c.name || c.id }}</b
            ><small>{{ protocolOf(c) }}</small></span
          ><ConnectionHealth :id="c.id" compact />
        </button>
        <button
          class="connection-item"
          :class="{ selected: tab === 'direct' }"
          @click="tab = 'direct'"
        >
          <ArrowRightCircle :size="22" /><span>{{ t('target.direct') }}</span>
        </button>
        <button
          class="connection-item"
          :class="{ selected: tab === 'block' }"
          @click="tab = 'block'"
        >
          <Ban :size="22" /><span>{{ t('target.block') }}</span>
        </button>
      </nav>
      <section class="connection-detail">
        <FallbackPanel
          v-if="conn && backupOpen"
          :key="conn.id"
          :connection="conn"
          @close="backupOpen = false"
        />
        <template v-else>
          <div class="section-head">
            <div>
              <h2>
                {{
                  conn
                    ? conn.name
                    : tab === 'direct'
                      ? t('target.direct')
                      : t('target.block')
                }}
              </h2>
              <p class="muted">
                {{
                  conn
                    ? protocolOf(conn)
                    : tab === 'direct'
                      ? t('conn.directSub')
                      : t('conn.blockSub')
                }}
              </p>
            </div>
            <div v-if="conn" class="actions">
              <NButton @click="backupOpen = true"
                ><template #icon><LifeBuoy :size="16" /></template
                >{{ t('ui.fallback') }}</NButton
              ><NButton :aria-label="t('common.edit')" @click="edit"
                ><template #icon><Pencil :size="16" /></template></NButton
              ><NButton
                type="error"
                quaternary
                :aria-label="t('common.delete')"
                @click="remove"
                ><template #icon><Trash2 :size="16" /></template
              ></NButton>
            </div>
          </div>
          <ConnectionHealth v-if="conn" :id="conn.id" />
          <div v-if="onBackup" class="backup-note">
            {{ t('ui.activeExit') }}: {{ connName(failover!.active) }}
          </div>
          <div v-for="r in rows" :key="r.label" class="data-row">
            <span class="muted"
              ><component :is="r.icon" :size="16" /> {{ r.label }}</span
            ><b :class="{ mono: r.mono }">{{ r.value }}</b>
          </div>
          <NButton
            v-if="specific.length"
            text
            style="margin: 24px 0"
            @click="showSpecific = !showSpecific"
            >{{ showSpecific ? t('ui.hide') : t('ui.reveal') }}</NButton
          >
          <template v-if="showSpecific"
            ><div v-for="r in specific" :key="r.label" class="data-row">
              <span class="muted">{{ r.label }}</span
              ><span :class="{ mono: r.mono }">{{ r.value }}</span>
            </div></template
          >
          <router-link
            v-if="conn"
            :to="{
              path: '/service/diagnostics',
              query: { connection: conn.id },
            }"
            >{{ t('ui.diagnostics') }} →</router-link
          >
        </template>
      </section>
    </div>
    <section class="traffic-section">
      <div class="section-head">
        <h2>{{ t('conn.speed') }}</h2>
        <span class="muted">{{ t('common.realtime') }}</span>
      </div>
      <AreaChart :times="times" :series="series" :height="240" :format="mbit" />
      <div class="traffic-labels">
        <span
          >{{ t('conn.in') }}: <b>{{ bits(st?.down ?? 0) }}</b></span
        ><span
          >{{ t('conn.out') }}: <b>{{ bits(st?.up ?? 0) }}</b></span
        >
      </div>
    </section>
    <section class="hosts">
      <div class="section-head">
        <h2>{{ t('conn.hosts') }}</h2>
        <NInput
          v-model:value="q"
          clearable
          :placeholder="t('conn.hostsSearch')"
          style="max-width: 360px"
        />
      </div>
      <div class="hosts-table">
        <table>
          <thead>
            <tr>
              <th>{{ t('conn.thHost') }}</th>
              <th>{{ t('conn.received') }}</th>
              <th>{{ t('conn.sent') }}</th>
              <th>{{ t('conn.thRequests') }}</th>
              <th>{{ t('conn.thSeen') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="h in hosts" :key="h.host">
              <td>{{ h.host }}</td>
              <td>{{ bytes(h.down) }}</td>
              <td>{{ bytes(h.up) }}</td>
              <td>{{ h.requests }}</td>
              <td>{{ ago(h.last_seen) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="!hosts.length" class="muted">{{ t('monitor.noTraffic') }}</p>
    </section>
    <ConnectionDialog v-model:show="show" :edit="editing" />
  </div>
</template>
<style scoped>
.connection-layout {
  display: grid;
  grid-template-columns: minmax(250px, 0.8fr) minmax(0, 1.6fr);
  gap: 36px;
}
.connection-list {
  min-width: 0;
}
.connection-item {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
  text-align: left;
  padding: 20px 16px;
  border: 0;
  border-bottom: 1px solid var(--line);
  border-left: 4px solid transparent;
  background: transparent;
  color: var(--text);
  cursor: pointer;
  min-height: 84px;
}
.connection-item.selected {
  border-left-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  border-radius: 6px;
}
.connection-item > span {
  min-width: 0;
  flex: 1;
  overflow-wrap: anywhere;
}
.connection-item small {
  display: block;
  color: var(--muted);
  margin-top: 4px;
}
.connection-detail {
  border-left: 1px solid var(--line);
  padding-left: 36px;
  min-width: 0;
}
.connection-detail h2 {
  margin: 0;
  font-size: 26px;
  overflow-wrap: anywhere;
}
.connection-detail .data-row svg {
  vertical-align: middle;
  margin-right: 8px;
}
.connection-detail .data-row b {
  max-width: 55%;
  text-align: right;
}
.connection-detail .section-head p {
  margin: 6px 0 14px;
}
.actions {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.traffic-section,
.hosts {
  margin-top: 32px;
  padding-top: 24px;
  border-top: 1px solid var(--line);
}
h2 {
  font-size: 22px;
}
.traffic-labels {
  display: flex;
  gap: 24px;
  color: var(--muted);
  flex-wrap: wrap;
}
.traffic-labels b {
  color: var(--text);
}
.hosts-table {
  overflow: auto;
  margin-top: 18px;
}
table {
  border-collapse: collapse;
  width: 100%;
  font-size: 14px;
  text-align: left;
}
td,
th {
  padding: 12px;
  border-bottom: 1px solid var(--line);
}
th {
  color: var(--muted);
}
.backup-note {
  color: var(--warn);
  padding: 12px 0;
}
@media (max-width: 900px) {
  .connection-layout {
    grid-template-columns: 1fr;
  }
  .connection-detail {
    padding: 24px 0 0;
    border-left: 0;
    border-top: 1px solid var(--line);
  }
  .connection-list {
    display: flex;
    overflow: auto;
    padding-bottom: 8px;
  }
  .connection-item {
    min-width: 220px;
    width: auto;
    flex: none;
    max-width: 85vw;
  }
  .connection-item :deep(.connection-health) {
    white-space: normal;
    max-width: 100px;
  }
  .connection-detail .section-head {
    align-items: flex-start;
  }
  .hosts-table {
    font-size: 13px;
  }
}
</style>
