<script setup lang="ts">
// Connection targets include direct routing and an optional block action.
import { computed, nextTick, onMounted, onUnmounted, onUpdated, ref, watch } from 'vue'
import { NButton, NDropdown } from 'naive-ui'
import { ArrowRightCircle, Ban, ChevronDown } from '@lucide/vue'
import { store } from '../store'
import { t } from '../i18n'
import { protocolOf } from '../conn'
import { flag } from '../countries'
import ConnectionHealth from './ConnectionHealth.vue'

const props = defineProps<{ modelValue: string; withBlock?: boolean; activeCounts?: Record<string, number> }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()
const strip = ref<HTMLElement>()
const overflowing = ref(false)
let observer: ResizeObserver | undefined
function measure() {
  if (strip.value) overflowing.value = strip.value.scrollWidth > strip.value.clientWidth + 1
}
// Wait for updated tab layout before scrolling the selected target into view.
async function reveal() {
  await nextTick()
  const selected = strip.value?.querySelector<HTMLElement>('[aria-selected="true"]')
  selected?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
}
onMounted(() => {
  observer = new ResizeObserver(measure)
  if (strip.value) observer.observe(strip.value)
  measure()
  void reveal()
})
onUpdated(measure)
onUnmounted(() => observer?.disconnect())
watch(() => props.modelValue, reveal)

const tabs = computed(() => {
  const conns = (store.state.config?.connections ?? []).map((c) => ({
    value: c.id, label: c.name || c.id, badge: protocolOf(c), kind: 'conn',
  }))
  const out = [...conns, { value: 'direct', label: t('target.direct'), badge: '', kind: 'direct' }]
  if (props.withBlock) out.push({ value: 'block', label: t('target.block'), badge: '', kind: 'block' })
  return out
})
</script>

<template>
  <!-- Scroll tabs independently so actions remain visible. -->
  <div class="conn-bar">
  <div ref="strip" class="tabs" role="tablist">
    <button
      v-for="t in tabs"
      :key="t.value"
      role="tab"
      :aria-selected="modelValue === t.value"
      :class="['tab', { active: modelValue === t.value }]"
      @click="emit('update:modelValue', t.value)"
    >
      <ArrowRightCircle v-if="t.kind === 'direct'" :size="15" />
      <Ban v-else-if="t.kind === 'block'" :size="15" class="bad" />
      <ConnectionHealth v-else :id="t.value" compact />
      <span v-if="t.kind === 'conn' && store.state.status?.conn_countries?.[t.value]" class="flag">{{ flag(store.state.status.conn_countries[t.value]) }}</span>
      <span class="tab-label">{{ t.label }}</span>
      <span v-if="t.badge" class="badge">{{ t.badge }}</span>
    </button>
  </div>
  <NDropdown v-if="overflowing" trigger="click" :options="tabs.map(tab => ({ key: tab.value, label: tab.label + (tab.badge ? ' · ' + tab.badge : '') }))" :value="modelValue" @select="(value: string) => emit('update:modelValue', value)">
    <NButton class="overflow-menu" quaternary :aria-label="t('conn.allConnections')" :title="t('conn.allConnections')"><ChevronDown :size="18" /></NButton>
  </NDropdown>
  <div v-if="$slots.default" class="actions"><slot /></div>
  </div>
</template>

<style scoped>
.conn-bar {
  display: flex;
  align-items: stretch;
  border-bottom: 1px solid var(--line);
  min-width: 0;
}
.tabs {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}
.overflow-menu {
  flex: none;
  align-self: center;
  margin: 0 4px;
}
.actions {
  flex: none;
  display: flex;
  align-items: center;
}
@media (max-width: 720px) {
  .tab {
    padding: 12px 10px;
  }
  .badge {
    display: none;
  }
}
.tab {
  flex: none;
  max-width: 100%;
  min-width: 0;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  background: none;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
}
.tab-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tab > :not(.tab-label) {
  flex-shrink: 0;
}
.tab:hover {
  color: var(--text);
}
.tab.active {
  color: var(--text);
  font-weight: 600;
  border-bottom-color: #3b82f6;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--muted);
  opacity: 0.6;
}
.dot.live {
  background: var(--ok);
  opacity: 1;
  box-shadow: 0 0 0 3px rgba(52, 211, 153, 0.15);
}
.badge {
  font-size: 10px;
  color: var(--muted);
  border: 1px solid var(--line);
  border-radius: 4px;
  padding: 0 4px;
}
.bad {
  color: var(--bad);
}
.flag {
  font-size: 15px;
  line-height: 1;
}
</style>
