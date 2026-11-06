<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { store } from '../store'
import { useHealthState } from '../health'
const props = defineProps<{ id: string; compact?: boolean }>()
const { t, locale } = useI18n()
// Display HTTP probe freshness; a running process alone does not imply connectivity.
const health = computed(() => store.state.status?.health?.[props.id])
const tone = useHealthState(() => health.value, () => store.state.config?.failover?.interval)
const label = computed(() => tone.value === 'up' ? t('conn.probeLatency', { ms: health.value!.latency_ms }) : t(`conn.probe${tone.value === 'down' ? 'Down' : tone.value === 'stale' ? 'Stale' : 'Unknown'}`))
const checked = computed(() => health.value ? new Date(health.value.checked).toLocaleTimeString(locale.value) : '')
const title = computed(() => [t('conn.probeHint'), label.value, checked.value ? t('conn.probeChecked', { time: checked.value }) : '', health.value?.error ?? ''].filter(Boolean).join(' · '))
</script>
<template>
  <span :class="['connection-health', tone, { compact }]" :title="title" :aria-label="title">
    <span class="probe-dot" aria-hidden="true" />
    <span>{{ label }}</span>
    <template v-if="!compact">
      <small v-if="checked">{{ t('conn.probeChecked', { time: checked }) }}</small>
      <small v-else>{{ t('conn.probeHint') }}</small>
      <small v-if="health?.error" class="probe-error">{{ health.error }}</small>
    </template>
  </span>
</template>
<style scoped>
.connection-health { display:flex; align-items:center; gap:7px; flex-wrap:wrap; color:var(--muted); font-size:12px; }
.connection-health:not(.compact) { padding:10px 0; }
.compact { display:inline-flex; flex-wrap:nowrap; font-size:11px; }
.probe-dot { width:7px; height:7px; border-radius:50%; background:currentColor; flex:none; }
.up { color:var(--ok); }
.down { color:var(--bad); }
small { color:var(--muted); font-size:11px; }
.probe-error { width:100%; color:var(--bad); overflow-wrap:anywhere; }
</style>
