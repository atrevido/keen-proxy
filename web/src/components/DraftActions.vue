<script setup lang="ts">
import { NButton, useMessage } from 'naive-ui'
import { Check, Undo2 } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import { store } from '../store'

defineProps<{ settings?: boolean }>()
const { t } = useI18n()
const message = useMessage()

async function apply() {
  try {
    await store.apply()
    message.success(t('shell.applied'))
  } catch (error) {
    message.error((error as Error).message, { duration: 10000, closable: true })
  }
}
</script>

<template>
  <div v-if="store.dirty.value" class="draft-strip" :class="{ 'settings-draft': settings }">
    <span role="status">{{ t('ui.draft') }}</span>
    <div class="draft-buttons">
      <NButton type="primary" :loading="store.state.applying" @click="apply">
        <template #icon><Check :size="16" /></template>
        {{ t('common.apply') }}
      </NButton>
      <NButton :quaternary="!settings" :disabled="store.state.applying" :aria-label="t('common.discard')" @click="store.discard()">
        <template #icon><Undo2 :size="16" /></template>
        <template v-if="settings">{{ t('common.discard') }}</template>
      </NButton>
    </div>
  </div>
</template>

<style scoped>
.draft-strip, .draft-buttons {
  display: flex;
  align-items: center;
  gap: 10px;
}
.draft-strip {
  justify-content: space-between;
  padding: 12px 42px;
  background: color-mix(in srgb, var(--warn) 12%, var(--page));
  color: var(--warn);
}
.draft-buttons { flex-direction: row-reverse; }
.settings-draft {
  justify-content: flex-end;
  flex-direction: row-reverse;
  flex-wrap: wrap;
  gap: 16px;
  max-width: 1320px;
  margin: 24px auto 0;
  padding: 20px 0 0;
  border-top: 1px solid var(--line);
  background: transparent;
  color: var(--muted);
}
.settings-draft .draft-buttons { flex-direction: row; }
@media (max-width: 540px) {
  .draft-strip { font-size: 13px; }
  .draft-strip:not(.settings-draft) { padding: 10px 18px; }
  .settings-draft { justify-content: flex-start; flex-direction: column; align-items: flex-start; }
}
</style>
