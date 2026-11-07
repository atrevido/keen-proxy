<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NButton, NAlert } from 'naive-ui'
import {
  Activity,
  Server,
  Shield,
  Globe,
  Database,
  FileCheck,
  RefreshCw,
  Download,
} from '@lucide/vue'
import { store, targetLabel } from '../store'
import { api } from '../api'
import { protocolOf } from '../conn'
import { duration } from '../format'
import { sameAppliedConnection, safeDiagnosticsReport } from '../diagnostics'
import type { DiagnosticReport, FeatureStatus } from '../types'
import ConnectionHealth from '../components/ConnectionHealth.vue'
import ConnectionChecks from '../components/ConnectionChecks.vue'
const { t, locale } = useI18n(),
  route = useRoute()
const cfg = computed(() => store.state.config!)
// Compare against the saved configuration, rather than treating the draft as live state.
const applied = computed(
  () => JSON.parse(store.state.saved || 'null') as typeof cfg.value | null,
)
const st = computed(() => store.state.status)
const selected = ref(
  typeof route.query.connection === 'string'
    ? route.query.connection
    : (cfg.value.connections?.[0]?.id ?? ''),
)
const conn = computed(() =>
  cfg.value.connections?.find((c) => c.id === selected.value),
)
const features = ref<FeatureStatus | null>(null),
  report = ref<DiagnosticReport | null>(null),
  loading = ref(false),
  error = ref(''),
  updated = ref('')
const demo = computed(() => features.value?.demo || report.value?.demo)
const exportingBundle = ref(false)
const bundleError = ref(false)
watch(
  () => store.state.saved,
  () => {
    report.value = null
  },
)
watch(selected, () => {
  report.value = null
  error.value = ''
})
watch(
  conn,
  () => {
    report.value = null
  },
  { deep: true },
)
watch(
  () => cfg.value.connections,
  () => {
    if (!conn.value) selected.value = cfg.value.connections?.[0]?.id ?? ''
  },
  { deep: true },
)
async function refresh() {
  error.value = ''
  try {
    await store.refreshStatus()
    features.value = await api.features()
    updated.value = new Date().toISOString()
  } catch {
    error.value = t('ui.unavailable')
  }
}
onMounted(refresh)
const rows = computed(() => [
  {
    key: 'core',
    icon: Server,
    label: t('monitor.core'),
    value: st.value?.started
      ? t('monitor.running', { d: duration(st.value.started) })
      : t('ui.unknown'),
    tone: st.value?.started ? 'ok' : 'muted',
    to: '/service/journal',
  },
  {
    key: 'config',
    icon: FileCheck,
    label: t('ui.configuration'),
    value: store.dirty.value ? t('ui.unsaved') : t('ui.applied'),
    tone: store.dirty.value ? 'warn' : 'ok',
    to: '/settings',
  },
  {
    key: 'intercept',
    icon: Shield,
    label: t('monitor.intercept'),
    value: st.value
      ? `${st.value.mode} · ${t(st.value.firewall ? 'ui.available' : 'ui.unavailable')}`
      : t('ui.unknown'),
    tone: st.value?.firewall ? 'ok' : 'warn',
    to: '/settings',
  },
  {
    key: 'dns',
    icon: Globe,
    label: 'DNS',
    value: `${applied.value?.dns.upstreams.length ?? 0} upstream · ${features.value?.dns.route || t('ui.unknown')}`,
    tone: 'muted',
    to: '/settings',
  },
  {
    key: 'geoip',
    icon: Database,
    label: t('ui.geoip'),
    value: st.value?.geoip.loaded
      ? `${st.value.geoip.type} · ${st.value.geoip.built}${(st.value.geoip.active ?? 1) > 1 ? ` · +${(st.value.geoip.active ?? 1) - 1}` : ''}`
      : st.value?.geoip.error
        ? t('ui.unavailable')
        : t('ui.notChecked'),
    tone: st.value?.geoip.loaded && !st.value.geoip.error ? 'ok' : 'warn',
    to: '/lists/geoip',
  },
  {
    key: 'watchdog',
    icon: RefreshCw,
    label: t('ui.watchdog'),
    value: `${t('ui.watchdogUnknown')} · ${t('monitor.restored', { n: st.value?.restored ?? 0 })}`,
    tone: 'muted',
    to: '/service/journal',
  },
])
const pending = computed(
  () =>
    !sameAppliedConnection(
      conn.value,
      applied.value?.connections?.find((c) => c.id === selected.value),
    ),
)
// Retain results only while selection, applied snapshot, and profile definition still match.
async function check() {
  if (!conn.value || pending.value) return
  loading.value = true
  error.value = ''
  const id = selected.value,
    snapshot = store.state.saved,
    definition = JSON.stringify(conn.value)
  try {
    const value = await api.diagnosticCheck(id)
    if (
      selected.value === id &&
      store.state.saved === snapshot &&
      JSON.stringify(conn.value) === definition
    )
      report.value = value
    await refresh()
  } catch {
    error.value = t('ui.unavailable')
  } finally {
    loading.value = false
  }
}
function saveDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// Export only the sanitized machine-result allowlist, excluding raw errors and secrets.
function downloadReport() {
  const data = safeDiagnosticsReport(
    st.value,
    report.value,
    selected.value,
    store.dirty.value,
    features.value?.demo ?? false,
  )
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  saveDownload(blob, 'keenproxy-diagnostics.json')
}

async function downloadBundle() {
  if (demo.value || exportingBundle.value) return
  exportingBundle.value = true
  bundleError.value = false
  try {
    saveDownload(await api.diagnosticBundle(), 'keenproxy-diagnostics.zip')
  } catch {
    bundleError.value = true
  } finally {
    exportingBundle.value = false
  }
}
</script>
<template>
  <div>
    <div class="page-head">
      <div>
        <h1>{{ t('ui.diagnostics') }}</h1>
        <p>{{ t('ui.summary') }}</p>
      </div>
      <div class="actions">
        <NButton
          type="primary"
          :loading="loading"
          :disabled="!conn || pending"
          @click="check"
          ><template #icon><Activity :size="18" /></template
          >{{ t('ui.check') }}</NButton
        ><NButton @click="downloadReport"
          ><template #icon><Download :size="18" /></template
          >{{ t('ui.report') }}</NButton
        >
      </div>
    </div>
    <NAlert
      v-if="demo"
      type="info"
      style="margin-bottom: 24px"
      >{{ t('ui.demo') }}</NAlert
    ><NAlert v-if="error" type="error" style="margin-bottom: 24px">{{
      error
    }}</NAlert>
    <section class="bundle-panel" aria-labelledby="diagnostics-bundle-heading">
      <div>
        <h2 id="diagnostics-bundle-heading">{{ t('ui.bundleTitle') }}</h2>
        <p class="muted">{{ t(demo ? 'ui.bundleDemo' : 'ui.bundleContents') }}</p>
        <p v-if="!demo" id="diagnostics-bundle-privacy" class="muted">{{ t('ui.bundlePrivacy') }}</p>
      </div>
      <NButton
        v-if="!demo"
        aria-describedby="diagnostics-bundle-privacy"
        :loading="exportingBundle"
        @click="downloadBundle"
      >
        <template #icon><Download :size="18" /></template>
        {{ t('ui.bundleDownload') }}
      </NButton>
      <NAlert v-if="bundleError" type="error" class="bundle-error">
        {{ t('ui.bundleFailed') }}
      </NAlert>
    </section>
    <div class="split-view">
      <section>
        <div class="section-head">
          <h2>{{ t('ui.summary') }}</h2>
          <NButton quaternary :aria-label="t('ui.refreshed')" @click="refresh"
            ><template #icon><RefreshCw :size="18" /></template
          ></NButton>
        </div>
        <router-link
          v-for="row in rows"
          :key="row.key"
          :to="row.to"
          class="diagnostic-row"
          ><component :is="row.icon" :size="22" /><b>{{ row.label }}</b
          ><span :class="row.tone">{{ row.value }}</span></router-link
        >
        <p v-if="updated" class="muted">
          {{ t('ui.refreshed') }}:
          {{ new Date(updated).toLocaleTimeString(locale) }}
        </p>
        <h2 class="connection-heading">{{ t('nav.connections') }}</h2>
        <button
          v-for="c in cfg.connections"
          :key="c.id"
          class="diagnostic-connection"
          :class="{ selected: selected === c.id }"
          @click="selected = c.id"
        >
          <Server :size="22" /><span
            ><b>{{ c.name }}</b
            ><small>{{ protocolOf(c) }}</small></span
          ><ConnectionHealth :id="c.id" compact />
        </button>
        <p v-if="!cfg.connections?.length" class="muted">
          {{ t('ui.noProfiles') }}
        </p>
      </section>
      <aside class="detail-pane">
        <template v-if="conn"
          ><h2>{{ conn.name }}</h2>
          <p class="muted">{{ protocolOf(conn) }}</p>
          <p v-if="st?.failover?.[conn.id]">
            {{ t('ui.activeExit') }}:
            {{ targetLabel(st.failover[conn.id]!.active) }}
          </p>
          <ConnectionChecks :id="conn.id" :report="report" />
          <p class="muted">{{ t('ui.httpOnly') }}</p>
          <p class="muted">{{ t('ui.checkDisclaimer') }}</p>
          <NAlert v-if="pending" type="warning">{{
            t('ui.pendingConnection')
          }}</NAlert
          ><router-link :to="'/network/connections'"
            >{{ t('ui.parameters') }} →</router-link
          ></template
        >
        <div class="dns-note">
          <Globe :size="22" />
          <h3>{{ t('ui.dnsPath') }}</h3>
          <p class="muted">{{ t('ui.dnsNote') }}</p>
        </div>
      </aside>
    </div>
  </div>
</template>
<style scoped>
h2 {
  font-size: 23px;
  margin: 0 0 12px;
}
.actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
.bundle-panel {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: 12px 20px;
  margin-bottom: 24px;
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: 10px;
}
.bundle-panel > div:first-child {
  flex: 1 1 390px;
  min-width: 0;
}
.bundle-panel h2 {
  font-size: 18px;
  margin-bottom: 4px;
}
.bundle-panel p {
  margin: 0;
}
.bundle-panel p + p {
  margin-top: 6px;
}
.bundle-error {
  flex-basis: 100%;
}
.diagnostic-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px 0;
  border-bottom: 1px solid var(--line);
  text-decoration: none;
  color: var(--text);
}
.diagnostic-row b {
  min-width: 115px;
}
.diagnostic-row span {
  margin-left: auto;
  text-align: right;
  overflow-wrap: anywhere;
  font-size: 14px;
  max-width: 58%;
}
.ok {
  color: var(--ok);
}
.warn {
  color: var(--warn);
}
.connection-heading {
  margin-top: 36px;
}
.diagnostic-connection {
  display: flex;
  align-items: center;
  gap: 16px;
  width: 100%;
  padding: 18px 14px;
  text-align: left;
  color: var(--text);
  background: transparent;
  border: 0;
  border-bottom: 1px solid var(--line);
  border-left: 4px solid transparent;
  cursor: pointer;
}
.diagnostic-connection > span {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}
.diagnostic-connection small {
  display: block;
  color: var(--muted);
}
.diagnostic-connection.selected {
  border-left-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  border-radius: 6px;
}
.dns-note {
  margin-top: 36px;
  padding-top: 24px;
  border-top: 1px solid var(--line);
}
.detail-pane > p {
  overflow-wrap: anywhere;
}
.diagnostic-connection :deep(.connection-health) {
  max-width: 130px;
  white-space: normal;
  flex-wrap: wrap;
}
@media (max-width: 540px) {
  .diagnostic-row {
    gap: 10px;
    flex-wrap: wrap;
  }
  .diagnostic-row span {
    max-width: 100%;
    flex-basis: 100%;
    text-align: left;
    margin-left: 32px;
  }
  .diagnostic-row b {
    min-width: 0;
  }
  .diagnostic-connection {
    gap: 8px;
    padding: 14px 8px;
  }
  .actions .n-button {
    min-height: 44px;
  }
}
</style>
