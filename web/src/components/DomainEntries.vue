<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NButton, NInput, NModal, NAlert, useDialog } from 'naive-ui'
import { Plus, Pencil, Trash2, RefreshCw, ChevronRight, ChevronDown } from '@lucide/vue'
import type { Group, DomainStatus, DomainRecord } from '../types'
import { store } from '../store'
import { api } from '../api'
import { domainName } from '../domain'

// Edit group entries in the shared draft and display live or isolated preview DNS records.
const props = defineProps<{ group: Group; status: DomainStatus | null; query: string; preview: boolean; refreshing: boolean }>()
const emit = defineEmits<{ refresh: [names: string[]] }>()
const { t, locale } = useI18n()
const dialog = useDialog()
const expanded = ref(new Set<string>())
const show = ref(false)
const editing = ref<number | null>(null)
const value = ref('')
const error = ref('')
const saving = ref(false)
const isDomain = (s: string) => domainName(s) !== null
const normalize = (s: string) => domainName(s) ?? s
const rows = computed(() => props.group.entries.map((entry, index) => ({ entry, index })).filter(r => !props.query || r.entry.toLowerCase().includes(props.query.toLowerCase()) || props.group.name.toLowerCase().includes(props.query.toLowerCase())))
function records(entry: string): DomainRecord[] {
  const name = normalize(entry)
  return (props.status?.records ?? []).filter(r => r.name === name || r.name.endsWith('.' + name))
}
function ips(entry: string) { return records(entry).flatMap(r => r.addresses.filter(a => Date.parse(a.expires_at) > Date.now()).map(a => ({ ...a, name: r.name }))) }
function date(v: string) { return !v || v.startsWith('0001') ? '—' : new Date(v).toLocaleString(locale.value) }
function toggle(s: string) { if (expanded.value.has(s)) expanded.value.delete(s); else expanded.value.add(s) }
function open(index: number | null) { editing.value = index; value.value = index === null ? '' : props.group.entries[index]!; error.value = ''; show.value = true }
// Validate a copied configuration before updating entries and requesting a domain preview.
async function save() {
  error.value = ''
  const lines = value.value.split('\n').map(s => s.split('#')[0]!.trim()).filter(Boolean)
  if (!lines.length || (editing.value !== null && lines.length !== 1)) { error.value = t('domains.oneOnly'); return }
  saving.value = true
  try {
    const cfg = JSON.parse(JSON.stringify(store.state.config))
    const groupIndex = store.state.config!.groups!.indexOf(props.group)
    const next = [...props.group.entries]
    if (editing.value === null) next.push(...lines.filter(s => !next.includes(s)))
    else next[editing.value] = lines[0]!
    cfg.groups[groupIndex].entries = next
    await api.validateConfig(cfg)
    props.group.entries = next
    show.value = false
    const domains = lines.filter(isDomain).map(normalize)
    if (domains.length && !props.group.preset && !props.group.disabled) emit('refresh', domains)
  } catch (e) { error.value = (e as Error).message || t('domains.saveError') }
  finally { saving.value = false }
}
function remove(index: number) {
  dialog.warning({ title: t('domains.remove'), content: props.group.entries[index], positiveText: t('common.delete'), negativeText: t('common.cancel'), onPositiveClick: () => props.group.entries.splice(index, 1) })
}
</script>

<template>
  <div class="domain-entries">
    <div class="entries-toolbar">
      <span class="hint">{{ t('domains.hint') }}</span>
      <NButton size="small" @click="open(null)"><template #icon><Plus :size="14" /></template>{{ t('domains.add') }}</NButton>
    </div>
    <div v-if="preview" class="hint preview">{{ t('domains.preview') }}</div>
    <p v-if="!rows.length" class="hint">{{ t('domains.emptyGroup') }}</p>
    <div v-for="row in rows" :key="`${row.index}:${row.entry}`" class="entry">
      <div class="entry-line">
        <button class="entry-label" :aria-label="`${t('domains.details')}: ${row.entry}`" :aria-expanded="expanded.has(row.entry)" @click="toggle(row.entry)">
          <ChevronDown v-if="expanded.has(row.entry)" :size="14" /><ChevronRight v-else :size="14" />
          <span class="entry-type">{{ t(isDomain(row.entry) ? 'domains.domain' : 'domains.address') }}</span>
          <strong>{{ row.entry }}</strong>
          <span v-if="isDomain(row.entry)" class="ip-count">{{ t('domains.ipCount', { n: ips(row.entry).length }) }}</span>
        </button>
        <div class="entry-actions">
          <NButton v-if="isDomain(row.entry) && !group.disabled && !(group.preset && preview)" size="tiny" quaternary :loading="refreshing" :title="t('domains.refresh')" :aria-label="`${t('domains.refresh')}: ${row.entry}`" @click="emit('refresh', [normalize(row.entry)])"><template #icon><RefreshCw :size="14" /></template></NButton>
          <NButton size="tiny" quaternary :title="t('common.edit')" :aria-label="`${t('common.edit')}: ${row.entry}`" @click="open(row.index)"><template #icon><Pencil :size="14" /></template></NButton>
          <NButton size="tiny" quaternary type="error" :title="t('common.delete')" :aria-label="`${t('common.delete')}: ${row.entry}`" @click="remove(row.index)"><template #icon><Trash2 :size="14" /></template></NButton>
        </div>
      </div>
      <div v-if="expanded.has(row.entry)" class="entry-detail">
        <template v-if="isDomain(row.entry)">
          <div v-if="!records(row.entry).length" class="hint">{{ t('domains.waiting') }}</div>
          <div v-for="r in records(row.entry)" :key="r.name">
            <div class="record-meta">{{ r.name }} · {{ t('domains.checked') }}: {{ date(r.checked_at) }} · {{ t('domains.source') }}: {{ t(r.source === 'observed' ? 'domains.observed' : 'domains.resolved') }}</div>
            <div v-if="r.error" class="dns-error">{{ t('domains.error') }}: {{ r.error }}</div>
          </div>
          <div v-if="records(row.entry).some(r => r.limited)" class="dns-error">{{ t('domains.limited') }}</div>
          <div class="addresses">
            <span v-for="a in ips(row.entry)" :key="`${a.name}:${a.ip}`" class="address" :title="`${a.name} · ${t('domains.expires')}: ${date(a.expires_at)}`">{{ a.ip }} <small>{{ t('domains.expires') }} {{ date(a.expires_at) }}</small><span v-if="status?.conflicts?.[a.ip]" class="dns-error">{{ t('domains.conflict') }}</span></span>
          </div>
          <div v-if="!ips(row.entry).length" class="hint">{{ t('domains.empty') }}</div>
        </template>
        <span v-else class="hint">{{ t('domains.noDNS') }}</span>
      </div>
    </div>
    <NModal v-model:show="show" preset="card" :title="t(editing === null ? 'domains.add' : 'domains.edit')" style="width: min(560px, 94vw)">
      <NInput v-model:value="value" type="textarea" :autosize="{ minRows: 4, maxRows: 12 }" :placeholder="t('domains.placeholder')" />
      <NAlert v-if="error" type="error" style="margin-top: 12px">{{ error }}</NAlert>
      <template #footer><div class="dialog-actions"><NButton @click="show = false">{{ t('common.cancel') }}</NButton><NButton type="primary" :loading="saving" @click="save">{{ t('common.save') }}</NButton></div></template>
    </NModal>
  </div>
</template>

<style scoped>
.domain-entries { padding: 10px 16px 14px; }
.entries-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.entry { border-top: 1px solid var(--line, #ffffff12); }
.entry-line { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 9px 0; }
.entry-label { display: flex; align-items: center; gap: 8px; border: 0; padding: 0; background: none; color: inherit; cursor: pointer; text-align: left; min-width: 0; }
.entry-label strong { font: 600 13px ui-monospace, monospace; overflow-wrap: anywhere; }
.entry-type, .ip-count { font-size: 10px; border-radius: 3px; padding: 2px 5px; background: #739ee91a; color: var(--muted); white-space: nowrap; }
.entry-actions, .dialog-actions { display: flex; justify-content: flex-end; gap: 6px; flex-shrink: 0; }
.entry-detail { padding: 0 0 12px 22px; }
.record-meta, .hint { color: var(--muted); font-size: 12px; }
.preview { margin-bottom: 8px; color: #c39b49; }
.addresses { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.address { padding: 4px 8px; background: #739ee915; border: 1px solid #739ee925; border-radius: 4px; font-family: ui-monospace, monospace; }
.address small { display: block; font: 10px system-ui; color: var(--muted); }
.dns-error { color: #e78484; font-size: 12px; }
@media(max-width: 720px) { .entries-toolbar { align-items: flex-start; flex-direction: column; } .entry-type { display: none; } .domain-entries { padding: 8px; } }
</style>
