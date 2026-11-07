<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Server,
  Layers,
  Users,
  ArrowDown,
  ArrowUp,
  ChevronRight,
  ShieldCheck,
  AlertTriangle,
  Cpu,
  MemoryStick,
} from '@lucide/vue'
import { api } from '../api'
import { store } from '../store'
import { useStats } from '../useStats'
import { bits, mbit } from '../format'
import { protocolOf } from '../conn'
import type { FeatureStatus } from '../types'
import AreaChart from '../components/AreaChart.vue'
import ConnectionHealth from '../components/ConnectionHealth.vue'
import ConnectionChecks from '../components/ConnectionChecks.vue'
const { n, t } = useI18n()
const cfg = computed(() => store.state.config!),
  st = computed(() => store.state.status)
const { snap, byTag } = useStats(3000, true)
const system = computed(() => snap.value?.system)
const memoryUsage = computed(() => system.value?.mem_mb ? system.value.mem_pct : undefined)

function formatUsage(value: number | undefined) {
  return value === undefined
    ? t('ui.unknown')
    : n(value / 100, { style: 'percent', maximumFractionDigits: 1 })
}

const tab = ref(cfg.value.connections?.[0]?.id ?? '')
watch(
  () => cfg.value.connections,
  () => {
    if (!cfg.value.connections?.some((c) => c.id === tab.value))
      tab.value = cfg.value.connections?.[0]?.id ?? ''
  },
  { deep: true },
)
const conn = computed(() =>
  cfg.value.connections?.find((c) => c.id === tab.value),
)
const tunnels = computed(() =>
  (snap.value?.outbounds ?? []).filter(
    (o) => !['direct', 'block'].includes(o.tag),
  ),
)
// Merge tunnel samples by timestamp; direct and blocked traffic are excluded.
const points = computed(() => {
  const byTime = new Map<number, { down: number; up: number }>()
  for (const outbound of tunnels.value)
    for (const p of outbound.series ?? []) {
      const existing = byTime.get(p.t) ?? { down: 0, up: 0 }
      existing.down += p.down
      existing.up += p.up
      byTime.set(p.t, existing)
    }
  return [...byTime].sort((a, b) => a[0] - b[0])
})
const times = computed(() => points.value.map((p) => p[0]))
const series = computed(() => [
  {
    name: t('conn.in'),
    color: 'var(--accent)',
    values: points.value.map((p) => p[1].down),
  },
  {
    name: t('conn.out'),
    color: 'var(--muted)',
    values: points.value.map((p) => p[1].up),
    fill: false,
  },
])
const down = computed(() => tunnels.value.reduce((sum, o) => sum + o.down, 0)),
  up = computed(() => tunnels.value.reduce((sum, o) => sum + o.up, 0))
const features = ref<FeatureStatus | null>(null)
const updateVersion = ref('')
let timer: ReturnType<typeof setInterval> | undefined
let updateTimer: ReturnType<typeof setInterval> | undefined
async function loadFeatures() {
  try {
    features.value = await api.features()
  } catch {
    /* Keep previous value; no fabricated status. */
  }
}
async function loadUpdateNotice() {
  try {
    const status = await api.updates()
    updateVersion.value = status.job?.state === 'running' ? '' : (status.latest ?? '')
  } catch {
    // Release checks are independent of the routing dashboard.
  }
}
onMounted(() => {
  void loadFeatures()
  timer = setInterval(loadFeatures, 5000)
  void loadUpdateNotice()
  updateTimer = setInterval(loadUpdateNotice, 60_000)
})
onUnmounted(() => {
  clearInterval(timer)
  clearInterval(updateTimer)
})
const routingOK = computed(() => !!st.value?.firewall)
const selectedStats = computed(() => byTag(tab.value))
</script>
<template>
  <div>
    <div class="page-head">
      <div>
        <h1>{{ t('ui.overview') }}</h1>
        <p>{{ t('monitor.intro') }}</p>
      </div>
      <router-link
        to="/service/diagnostics"
        class="routing-status"
        :class="routingOK ? 'ok' : 'warn'"
        ><ShieldCheck v-if="routingOK" :size="22" /><AlertTriangle
          v-else
          :size="22"
        />{{ t(routingOK ? 'ui.running' : 'ui.attention') }}</router-link
      >
    </div>

    <router-link v-if="updateVersion" to="/settings" class="update-notice">
      {{ t('updates.newRelease', { version: updateVersion }) }} <ChevronRight :size="16" />
    </router-link>

    <div class="metrics">
      <router-link to="/network/connections"
        ><Server :size="28" /><b>{{ cfg.connections?.length ?? 0 }}</b
        >{{ t('monitor.cConnections') }}</router-link
      ><router-link to="/lists/groups"
        ><Layers :size="28" /><b>{{ cfg.groups?.length ?? 0 }}</b
        >{{ t('monitor.cGroups') }}</router-link
      ><router-link to="/network/clients"
        ><Users :size="28" /><b>{{ st?.online_devices ?? '—' }}</b
        >{{ t('monitor.cOnline') }}</router-link
      >
      <dl class="resources" :aria-label="t('shell.load')">
        <div>
          <dt><Cpu :size="16" aria-hidden="true" />CPU</dt>
          <dd>{{ formatUsage(system?.cpu) }}</dd>
        </div>
        <div>
          <dt><MemoryStick :size="16" aria-hidden="true" />RAM</dt>
          <dd>{{ formatUsage(memoryUsage) }}</dd>
        </div>
      </dl>
    </div>
    <section class="traffic">
      <div class="section-head">
        <h2>{{ t('ui.traffic') }}</h2>
        <span class="muted"
          >{{ t('ui.window') }} · {{ t('common.realtime') }}</span
        >
      </div>
      <div class="traffic-body">
        <div class="traffic-totals">
          <div>
            <ArrowDown :size="26" /><span
              >{{ t('conn.in')
              }}<b>{{ snap ? bits(down) : t('ui.unknown') }}</b></span
            >
          </div>
          <div>
            <ArrowUp :size="26" /><span
              >{{ t('conn.out')
              }}<b>{{ snap ? bits(up) : t('ui.unknown') }}</b></span
            >
          </div>
        </div>
        <div class="traffic-chart">
          <AreaChart
            v-if="points.length"
            :times="times"
            :series="series"
            :height="250"
            :format="mbit"
          />
          <p v-else class="empty-state">{{ t('monitor.noTraffic') }}</p>
        </div>
      </div>
    </section>
    <div class="split-view">
      <section>
        <h2>{{ t('nav.connections') }}</h2>
        <button
          v-for="c in cfg.connections"
          :key="c.id"
          class="connection-row"
          :class="{ selected: tab === c.id }"
          @click="tab = c.id"
        >
          <Server :size="24" /><span
            ><b>{{ c.name }}</b
            ><small>{{ protocolOf(c) }}</small></span
          ><ConnectionHealth :id="c.id" compact /><ChevronRight
            :size="18"
          /></button
        ><router-link
          v-if="!cfg.connections?.length"
          to="/network/connections"
          >{{ t('ui.noProfiles') }}</router-link
        >
      </section>
      <aside class="detail-pane">
        <template v-if="conn"
          ><h2>{{ conn.name }} · {{ protocolOf(conn) }}</h2>
          <ConnectionChecks :id="conn.id" />
          <p class="muted">{{ t('ui.httpOnly') }}</p>
          <router-link
            :to="{
              path: '/service/diagnostics',
              query: { connection: conn.id },
            }"
            >{{ t('ui.diagnostics') }} →</router-link
          >
          <div class="data-row">
            <span class="muted">{{ t('conn.activeNow') }}</span
            ><b>{{ selectedStats?.active ?? 0 }}</b>
          </div></template
        >
      </aside>
    </div>
    <p v-if="features?.demo" class="muted demo-note">{{ t('ui.demo') }}</p>
    <section class="more-metrics">
      <router-link to="/lists/groups"
        >{{ t('nav.whitelist') }} <b>{{ st?.entries ?? '—' }}</b></router-link
      ><router-link to="/settings"
        >{{ t('monitor.cDNS') }}
        <b>{{ cfg.dns.upstreams.length }}</b></router-link
      ><router-link to="/lists/filters"
        >{{ t('monitor.cBlocked') }}
        <b>{{ features?.dns.blocked ?? '—' }}</b></router-link
      ><router-link to="/service/diagnostics"
        >{{ t('monitor.core') }} · {{ st?.mode ?? '—' }}</router-link
      >
    </section>
  </div>
</template>
<style scoped>
h2 {
  font-size: 25px;
  line-height: 1.3;
  margin: 0 0 22px;
}
.routing-status {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  border-radius: 6px;
  text-decoration: none;
  white-space: nowrap;
  font-weight: 650;
}
.routing-status.ok {
  background: var(--accent);
  color: var(--page);
}
.routing-status.warn {
  color: var(--warn);
  border: 1px solid var(--warn);
}
.update-notice {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px solid var(--line);
  border-radius: 6px;
  color: var(--accent);
  text-decoration: none;
}
.update-notice:hover {
  border-color: var(--accent);
}
.metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  padding: 20px 0 30px;
  border-bottom: 1px solid var(--line);
  margin-bottom: 28px;
  gap: 24px;
}
.metrics a {
  display: flex;
  align-items: center;
  gap: 16px;
  color: var(--text);
  text-decoration: none;
  border-right: 1px solid var(--line);
}
.metrics a:last-of-type {
  border: 0;
}
.metrics b {
  font-size: 30px;
}
.resources {
  grid-column: 1 / -1;
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
  margin: 0;
}
.resources > div,
.resources dt {
  display: flex;
  align-items: center;
  gap: 8px;
}
.resources dt {
  color: var(--muted);
}
.resources dd {
  margin: 0;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}
.traffic {
  border-bottom: 1px solid var(--line);
  padding-bottom: 28px;
  margin-bottom: 30px;
}
.traffic-body {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  align-items: center;
  gap: 24px;
}
.traffic-totals > div {
  display: flex;
  gap: 18px;
  align-items: center;
  margin: 24px 0;
}
.traffic-totals svg {
  color: var(--accent);
  flex: none;
}
.traffic-totals span {
  font-size: 14px;
  color: var(--muted);
}
.traffic-totals b {
  font-size: 26px;
  display: block;
  color: var(--text);
  margin-top: 6px;
}
.traffic-chart {
  min-width: 0;
}
.connection-row {
  display: flex;
  align-items: center;
  gap: 18px;
  width: 100%;
  text-align: left;
  padding: 20px 16px;
  color: var(--text);
  border: 0;
  border-left: 4px solid transparent;
  border-bottom: 1px solid var(--line);
  background: transparent;
  cursor: pointer;
}
.connection-row.selected {
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  border-left-color: var(--accent);
  border-radius: 6px;
}
.connection-row > span {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}
.connection-row small {
  display: block;
  color: var(--muted);
}
.connection-row :deep(.connection-health) {
  max-width: 150px;
  white-space: normal;
  flex-wrap: wrap;
}
.detail-pane h2 {
  overflow-wrap: anywhere;
}
.more-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
  padding-top: 28px;
  border-top: 1px solid var(--line);
  margin-top: 30px;
}
.more-metrics a {
  color: var(--muted);
  text-decoration: none;
}
.more-metrics b {
  color: var(--text);
  margin-left: 8px;
}
@media (max-width: 720px) {
  .metrics {
    gap: 12px;
  }
  .metrics a {
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    font-size: 12px;
  }
  .metrics a svg {
    display: none;
  }
  .metrics b {
    font-size: 26px;
  }
  .traffic-body {
    grid-template-columns: 1fr;
    gap: 0;
  }
  .traffic-totals {
    display: flex;
    gap: 24px;
    flex-wrap: wrap;
  }
  .traffic-totals > div {
    margin: 0 0 12px;
  }
  .traffic-totals b {
    font-size: 22px;
  }
  .traffic-totals svg {
    width: 20px;
  }
  .connection-row {
    gap: 10px;
    padding: 16px 10px;
  }
  .connection-row :deep(.connection-health) {
    max-width: 105px;
  }
  .routing-status {
    font-size: 14px;
  }
}
</style>

<style scoped>
.metrics {
  padding: 12px 0 22px;
  margin-bottom: 22px;
}
.traffic {
  padding-bottom: 20px;
  margin-bottom: 24px;
}
.traffic-body {
  grid-template-columns: 200px minmax(0, 1fr);
}
.demo-note {
  font-size: 12px;
  margin-top: 24px;
}
@media (max-width: 720px) {
  .traffic-body {
    grid-template-columns: 1fr;
  }
}
</style>
