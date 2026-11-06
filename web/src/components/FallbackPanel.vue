<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NSelect, NButton, NAlert } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { store, targetLabel } from '../store'
import { canFallback, connectionChain } from '../connectionDraft'
import type { Connection } from '../types'
// Edit only the immediate fallback; inherited links and global timing are read-only here.
const props = defineProps<{ connection: Connection }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const target = ref<string | null>(null),
  error = ref('')
watch(
  () => props.connection,
  (c) => {
    target.value = c.fallback ?? null
    error.value = ''
  },
  { immediate: true },
)
const list = computed(() => store.state.config?.connections ?? [])
const options = computed(() => [
  ...list.value
    .filter((c) => canFallback(list.value, props.connection.id, c.id))
    .map((c) => ({ value: c.id, label: c.name || c.id })),
  { value: 'direct', label: t('dialog.directFallback') },
])
const inherited = computed(() =>
  target.value ? connectionChain(list.value, target.value) : [],
)
const active = computed(
  () => store.state.status?.failover?.[props.connection.id]?.active,
)
const timing = computed(
  () =>
    store.state.config?.failover ?? {
      interval: 15,
      down_after: 60,
      up_after: 120,
    },
)
// Recheck the current graph before changing the shared draft fallback.
function save() {
  const actual = list.value.find((c) => c.id === props.connection.id)
  if (
    !actual ||
    (target.value && !canFallback(list.value, actual.id, target.value))
  ) {
    error.value = t('ui.invalid')
    return
  }
  if (target.value) actual.fallback = target.value
  else delete actual.fallback
  emit('close')
}
</script>
<template>
  <section class="fallback-panel">
    <h2>{{ t('ui.fallback') }} · {{ connection.name }}</h2>
    <p v-if="active" class="muted">
      {{ t('ui.activeExit') }}: <b>{{ targetLabel(active) }}</b>
    </p>
    <div class="data-row">
      <b>1 · {{ connection.name }}</b
      ><span class="pill ok">{{ t('ui.main') }}</span>
    </div>
    <div class="fallback-select">
      <label>{{ t('ui.backup') }}</label
      ><NSelect
        v-model:value="target"
        :options="options"
        clearable
        :placeholder="t('dialog.noFallback')"
        :aria-label="t('ui.backup')"
      />
    </div>
    <template v-if="inherited.length"
      ><h3>{{ t('ui.inherited') }}</h3>
      <div v-for="(id, i) in inherited" :key="id" class="data-row">
        <span>{{ i + 2 }} · {{ targetLabel(id) }}</span
        ><small class="muted">{{ t('ui.backup') }}</small>
      </div></template
    >
    <h3>{{ t('ui.globalTiming') }}</h3>
    <div class="timing">
      <span>{{ t('ui.interval', { n: timing.interval }) }}</span
      ><span>{{ t('ui.down', { n: timing.down_after }) }}</span
      ><span>{{ t('ui.up', { n: timing.up_after }) }}</span>
    </div>
    <router-link to="/settings">{{ t('nav.settings') }}</router-link>
    <NAlert
      v-if="!inherited.includes('direct')"
      type="info"
      :bordered="false"
      style="margin: 16px 0"
      ><b>{{ t('ui.noDirect') }}</b>
      <p>{{ t('ui.allDown') }}</p></NAlert
    >
    <p class="muted">{{ t('ui.secret') }}</p>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <div class="actions">
      <NButton type="primary" @click="save">{{ t('ui.saveDraft') }}</NButton
      ><NButton @click="emit('close')">{{ t('common.cancel') }}</NButton>
    </div>
    <p class="muted">{{ t('ui.afterApply') }}</p>
  </section>
</template>
<style scoped>
.fallback-panel h2 {
  font-size: 24px;
  margin: 0 0 12px;
}
.fallback-panel h3 {
  font-size: 16px;
  margin: 16px 0 8px;
}
.fallback-select {
  margin: 16px 0;
}
.fallback-select label {
  display: block;
  margin-bottom: 8px;
  color: var(--muted);
}
.timing {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.timing span {
  padding: 4px 0;
  flex: 1;
  min-width: 150px;
}
.actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 16px;
}
.fallback-panel p {
  margin: 8px 0;
  overflow-wrap: anywhere;
}
.fallback-panel .data-row {
  padding-block: 12px;
}
</style>
