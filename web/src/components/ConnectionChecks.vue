<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { LockKeyhole, ArrowLeftRight, Database } from '@lucide/vue'
import ConnectionHealth from './ConnectionHealth.vue'
import type { DiagnosticReport } from '../types'
const props = defineProps<{ id: string; report?: DiagnosticReport | null }>()
const { t, locale } = useI18n()
// Keep HTTP, certificate-verified HTTPS, and transfer outcomes distinct.
const checks = computed(() =>
  [
    { kind: 'http', label: t('ui.http'), icon: ArrowLeftRight },
    { kind: 'https', label: t('ui.https'), icon: LockKeyhole },
    { kind: 'transfer', label: t('ui.transfer'), icon: Database },
  ].map((c) => ({
    ...c,
    result: props.report?.results.find((r) => r.kind === c.kind),
  })),
)
</script>
<template>
  <div class="checks">
    <div v-for="check in checks" :key="check.kind" class="data-row">
      <span><component :is="check.icon" :size="18" /> {{ check.label }}</span>
      <div v-if="check.result" class="check-result">
        <b :class="check.result.ok ? 'ok' : 'bad'">{{
          check.result.ok ? t('conn.probeLatency', {ms:check.result.latency_ms}) : t('ui.error')
        }}</b
        ><small>{{
          new Date(check.result.checked).toLocaleTimeString(locale)
        }}</small
        ><small v-if="check.result.error">{{ check.result.error }}</small>
      </div>
      <ConnectionHealth
        v-else-if="check.kind === 'http'"
        :id="id"
        compact
      /><span v-else class="muted">{{ t('ui.notChecked') }}</span>
    </div>
  </div>
</template>
<style scoped>
.checks .data-row > span {
  display: flex;
  align-items: center;
  gap: 8px;
}
.check-result {
  text-align: right;
  max-width: 55%;
  overflow-wrap: anywhere;
}
.check-result small {
  display: block;
  color: var(--muted);
  font-size: 12px;
}
.ok {
  color: var(--ok);
}
.bad {
  color: var(--bad);
}
</style>
