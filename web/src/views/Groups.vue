<script setup lang="ts">
import { computed, h, ref, reactive, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  NCheckbox,
  NButton,
  NDataTable,
  NInput,
  NSelect,
  NTabs,
  NTab,
  NAlert,
  useDialog,
  type DataTableColumns,
} from 'naive-ui'
import { ListChecks, Plus, Trash2, Search, Globe, RefreshCw } from '@lucide/vue'
import { store, targets, targetLabel } from '../store'
import type { Group, DomainStatus } from '../types'
import { countries, flag } from '../countries'

import DomainEntries from '../components/DomainEntries.vue'
import { api } from '../api'
import { domainName } from '../domain'
import { groupKey, changeGroupTargets } from '../groups'
const domainStatus = ref<DomainStatus | null>(null)
const domainError = ref('')
const previews = reactive(new Map<Group, DomainStatus>())
const refreshing = reactive(new Set<Group>())
let timer: ReturnType<typeof setInterval> | undefined
async function loadDomains() {
  try {
    domainStatus.value = await api.domains()
    domainError.value = ''
  } catch (e) {
    domainError.value = (e as Error).message
  }
}
onMounted(() => {
  void loadDomains()
  timer = setInterval(loadDomains, 3000)
})
onUnmounted(() => clearInterval(timer))
watch(
  () => store.state.saved,
  () => {
    previews.clear()
    void loadDomains()
  },
)
// Compare against the saved group to keep draft resolution isolated from applied state.
function isPreview(g: Group) {
  return !JSON.parse(store.state.saved || '{"groups":[]}').groups?.some(
    (s: Group) => JSON.stringify(s) === JSON.stringify(g),
  )
}
// Resolve at most 100 names per request; merge preview batches separately from live data.
async function refresh(g: Group, names?: string[]) {
  if (g.disabled || (g.preset && isPreview(g))) {
    domainError.value = t('features.presetApplyFirst')
    return
  }
  const wanted =
    names ?? g.entries.map(domainName).filter((n): n is string => n !== null)
  if (!wanted.length) return
  refreshing.add(g)
  domainError.value = ''
  try {
    const preview = isPreview(g)
    for (let i = 0; i < wanted.length; i += 100) {
      const result = await api.resolveDomains(wanted.slice(i, i + 100), preview)
      if (preview) {
        const records = new Map(
          (previews.get(g)?.records ?? []).map((r) => [r.name, r]),
        )
        for (const r of result.records) records.set(r.name, r)
        previews.set(g, { ...result, records: [...records.values()] })
      } else domainStatus.value = result
    }
  } catch (e) {
    domainError.value = (e as Error).message
  } finally {
    refreshing.delete(g)
  }
}
const dialog = useDialog()
const { t } = useI18n()
const search = ref('')
const tab = ref('all')
const expanded = ref<string[]>([])

const groups = computed(() => store.state.config!.groups!)

// Filter groups by all targets, one connection, direct routing, or blocking.
const tabs = computed(() => [
  { label: t('groups.all'), value: 'all' },
  ...targets.value,
])

const isCountry = (g: Group) => g.countries !== undefined

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return groups.value
    .map((g) => ({ g, key: groupKey(g) }))
    .filter(({ g }) => tab.value === 'all' || g.target === tab.value)
    .filter(
      ({ g }) =>
        !q ||
        g.name.toLowerCase().includes(q) ||
        g.entries.some((e) => e.toLowerCase().includes(q)) ||
        (g.countries ?? []).some((c) => c.toLowerCase() === q),
    )
})
const rows = computed(() => filtered.value.filter(({ g }) => !isCountry(g)))
const countryRows = computed(() =>
  filtered.value.filter(({ g }) => isCountry(g)),
)

const selected = ref(new Set<string>())
const newTarget = ref<string | null>(null)
const selectedGroups = computed(() =>
  groups.value.filter((g) => selected.value.has(groupKey(g))),
)
const hiddenSelected = computed(
  () =>
    selectedGroups.value.filter((g) => !filtered.value.some((r) => r.g === g))
      .length,
)
watch(
  groups,
  () => {
    const alive = new Set(groups.value.map(groupKey))
    selected.value = new Set([...selected.value].filter((k) => alive.has(k)))
  },
  { deep: true },
)
function selectGroup(g: Group, checked: boolean) {
  const next = new Set(selected.value)
  if (checked) next.add(groupKey(g))
  else next.delete(groupKey(g))
  selected.value = next
}
const allVisible = computed(
  () =>
    filtered.value.length > 0 &&
    filtered.value.every((r) => selected.value.has(r.key)),
)
function selectVisible(checked: boolean) {
  for (const { g } of filtered.value) selectGroup(g, checked)
}
// Change all selected draft groups, including selections hidden by the current filter.
function saveBulk() {
  if (!newTarget.value || !selectedGroups.value.length) return
  changeGroupTargets(
    groups.value,
    selected.value,
    newTarget.value,
    targets.value.map((t) => t.value),
  )
  selected.value = new Set()
  newTarget.value = null
}
const selectionColumn = () => ({
  title: '',
  key: 'selected',
  width: 44,
  render: ({ g }: { g: Group }) =>
    h(NCheckbox, {
      checked: selected.value.has(groupKey(g)),
      'aria-label': g.name,
      'onUpdate:checked': (v: boolean) => selectGroup(g, v),
    }),
})

const total = computed(() =>
  groups.value.reduce((n, g) => n + g.entries.length, 0),
)

function addGroup() {
  const target =
    tab.value !== 'all' ? tab.value : (targets.value[0]?.value ?? 'direct')
  groups.value.push({ name: t('groups.newGroup'), target, entries: [] })
  expanded.value = [groupKey(groups.value.at(-1)!)]
}

// Default country routing sends everything except Russia to the first connection.
function addCountryGroup() {
  const target =
    (tab.value !== 'all' ? tab.value : undefined) ??
    targets.value.find((t) => t.value !== 'direct' && t.value !== 'block')
      ?.value ??
    'direct'
  groups.value.push({
    name: t('groups.exceptRussia'),
    target,
    entries: [],
    countries: ['RU'],
    except: true,
  })
}

function removeGroup(g: Group) {
  dialog.warning({
    title: t('groups.deleteTitle'),
    content: isCountry(g)
      ? t('groups.deleteCountry', { name: g.name })
      : t('groups.deleteText', { name: g.name, n: g.entries.length }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      groups.value.splice(groups.value.indexOf(g), 1)
    },
  })
}

function renameGroup(g: Group) {
  const name = ref(g.name)
  dialog.create({
    title: t('domains.editGroup'),
    content: () =>
      h(NInput, {
        value: name.value,
        'onUpdate:value': (v: string) => (name.value = v),
      }),
    positiveText: t('common.save'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      if (!name.value.trim()) return false
      g.name = name.value.trim()
    },
  })
}
watch(search, (q) => {
  if (q.trim()) expanded.value = rows.value.map((r) => r.key)
})

const nameCell = (g: Group) =>
  h('div', { class: 'gname' }, [
    // Use blue for tunnels, green for direct routing, and red for blocking.
    h('i', {
      class: [
        'tdot',
        g.target === 'block'
          ? 'block'
          : g.target === 'direct'
            ? 'direct'
            : 'tunnel',
      ],
      title: targetLabel(g.target),
    }),
    h(
      NButton,
      { text: true, strong: true, onClick: () => renameGroup(g) },
      { default: () => g.name },
    ),
  ])
const targetCell = (g: Group) =>
  h(NSelect, {
    value: g.target,
    options: targets.value,
    size: 'small',
    'onUpdate:value': (v: string) => (g.target = v),
  })
const removeCell = (g: Group) =>
  h(
    NButton,
    {
      size: 'small',
      quaternary: true,
      type: 'error',
      title: t('groups.deleteGroup'),
      'aria-label': t('groups.deleteGroup'),
      onClick: () => removeGroup(g),
    },
    { icon: () => h(Trash2, { size: 15 }) },
  )

const countryOptions = computed(() =>
  countries.value.map((c) => ({
    label: `${flag(c.code)} ${c.name}`,
    value: c.code,
  })),
)
const modeOptions = computed(() => [
  { label: t('groups.onlyThese'), value: 'only' },
  { label: t('groups.allExcept'), value: 'except' },
])

const countryColumns = computed<DataTableColumns<{ g: Group; key: string }>>(
  () => [
    selectionColumn(),
    {
      title: t('groups.colGroup'),
      key: 'name',
      minWidth: 200,
      render: ({ g }) => nameCell(g),
    },
    {
      title: t('groups.colRule'),
      key: 'mode',
      width: 190,
      render: ({ g }) =>
        h(NSelect, {
          value: g.except ? 'except' : 'only',
          options: modeOptions.value,
          size: 'small',
          'onUpdate:value': (v: string) =>
            (g.except = v === 'except' || undefined),
        }),
    },
    {
      title: t('groups.colCountries'),
      key: 'countries',
      minWidth: 260,
      render: ({ g }) =>
        h(NSelect, {
          value: g.countries,
          options: countryOptions.value,
          multiple: true,
          filterable: true,
          size: 'small',
          placeholder: t('groups.pickCountries'),
          status: g.countries?.length ? undefined : 'error',
          renderTag: ({ option, handleClose }) =>
            h('span', { class: ['cchip', { except: g.except }] }, [
              `${flag(String(option.value))} ${option.value}`,
              h(
                'button',
                {
                  class: 'cchip-x',
                  title: t('groups.removeCountry'),
                  'aria-label': t('groups.removeCountry'),
                  onClick: (e: Event) => {
                    e.stopPropagation()
                    handleClose()
                  },
                },
                '×',
              ),
            ]),
          'onUpdate:value': (v: string[]) => (g.countries = v),
        }),
    },
    {
      title: t('groups.colTarget'),
      key: 'target',
      width: 230,
      render: ({ g }) => targetCell(g),
    },
    { title: '', key: 'actions', width: 56, render: ({ g }) => removeCell(g) },
  ],
)

const columns = computed<DataTableColumns<{ g: Group; key: string }>>(() => [
  selectionColumn(),
  {
    type: 'expand',
    renderExpand: ({ g }) =>
      h(DomainEntries, {
        group: g,
        status: isPreview(g) ? (previews.get(g) ?? null) : domainStatus.value,
        query: search.value,
        preview: isPreview(g),
        refreshing: refreshing.has(g),
        onRefresh: (names: string[]) => {
          void refresh(g, names)
        },
      }),
  },
  {
    title: t('groups.colGroup'),
    key: 'name',
    minWidth: 200,
    render: ({ g }) => nameCell(g),
  },
  {
    title: t('groups.colEntries'),
    key: 'count',
    width: 90,
    render: ({ g }) =>
      h('span', { class: 'pill dim' }, String(g.entries.length)),
  },
  {
    title: t('groups.colTarget'),
    key: 'target',
    width: 230,
    render: ({ g }) => targetCell(g),
  },
  {
    title: '',
    key: 'actions',
    width: 92,
    render: ({ g }) =>
      h('div', { style: 'display:flex;gap:4px' }, [
        h(
          NButton,
          {
            size: 'small',
            quaternary: true,
            loading: refreshing.has(g),
            title: t('domains.refreshGroup'),
            'aria-label': `${t('domains.refreshGroup')}: ${g.name}`,
            onClick: () => refresh(g),
          },
          { icon: () => h(RefreshCw, { size: 15 }) },
        ),
        removeCell(g),
      ]),
  },
])
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <h1><ListChecks :size="22" /> {{ t('nav.whitelist') }}</h1>
        <p>{{ t('groups.intro', { n: total }) }}</p>
      </div>
      <div class="head-actions">
        <NButton type="primary" @click="addGroup">
          <template #icon><Plus :size="16" /></template>
          {{ t('groups.group') }}
        </NButton>
        <NButton @click="addCountryGroup">
          <template #icon><Globe :size="16" /></template>
          {{ t('groups.byCountry') }}
        </NButton>
      </div>
    </div>

    <p v-if="domainStatus?.demo" class="demo-note muted">
      {{ t('domains.demo') }}
    </p>
    <NAlert
      v-if="domainError || domainStatus?.error"
      type="error"
      style="margin-bottom: 12px"
      >{{ domainError || domainStatus?.error }}</NAlert
    >
    <NAlert
      v-if="domainStatus?.limited"
      type="warning"
      style="margin-bottom: 12px"
      >{{ t('domains.limited') }}</NAlert
    >
    <p
      v-if="domainStatus?.application === 'files_written_reload_pending'"
      class="muted"
    >
      {{ t('domains.pending') }}
    </p>
    <NTabs
      class="target-tabs"
      v-model:value="tab"
      type="line"
      style="margin-bottom: 12px"
    >
      <NTab v-for="tb in tabs" :key="tb.value" :name="tb.value">
        {{ tb.label }}
        <span class="count">{{
          tb.value === 'all'
            ? groups.length
            : groups.filter((g) => g.target === tb.value).length
        }}</span>
      </NTab>
    </NTabs>

    <NSelect
      class="mobile-target-filter"
      v-model:value="tab"
      :options="tabs.map((tb) => ({ label: tb.label, value: tb.value }))"
      :aria-label="t('groups.colTarget')"
    />
    <NInput
      v-model:value="search"
      :placeholder="t('groups.search')"
      clearable
      style="margin-bottom: 12px"
    >
      <template #prefix><Search :size="16" /></template>
    </NInput>

    <div
      class="bulk-layout"
      :class="{ 'has-selection': selectedGroups.length }"
    >
      <div class="group-tables">
        <div class="selection-toolbar">
          <NCheckbox :checked="allVisible" @update:checked="selectVisible">{{
            t('ui.visible')
          }}</NCheckbox
          ><span v-if="selectedGroups.length" class="selection-summary"
            >{{ t('ui.selected', { n: selectedGroups.length }) }}
            <NButton text @click="selected = new Set()">{{
              t('ui.clear')
            }}</NButton></span
          ><small v-if="hiddenSelected" class="muted">{{
            t('ui.hidden', { n: hiddenSelected })
          }}</small>
        </div>
        <div class="desktop-groups">
          <NDataTable
            v-model:expanded-row-keys="expanded"
            :columns="columns"
            :data="rows"
            :row-key="(r) => r.key"
            :bordered="false"
            :scroll-x="600"
            size="small"
          />
          <p
            v-if="tab !== 'all' && !rows.length && !countryRows.length"
            class="muted"
          >
            {{ t('groups.noneForTarget', { target: targetLabel(tab) }) }}
          </p>

          <section v-if="countryRows.length" class="countries">
            <h2><Globe :size="16" /> {{ t('groups.byCountry') }}</h2>
            <p class="muted">{{ t('groups.countryIntro') }}</p>
            <NDataTable
              :columns="countryColumns"
              :data="countryRows"
              :row-key="(r) => r.key"
              :bordered="false"
              :scroll-x="900"
              size="small"
            />
          </section>
        </div>
        <div class="mobile-groups">
          <section
            v-for="section in [
              { label: t('ui.domainGroups'), items: rows },
              { label: t('ui.countryGroups'), items: countryRows },
            ]"
            :key="section.label"
          >
            <h2>{{ section.label }}</h2>
            <article
              v-for="{ g, key } in section.items"
              :key="key"
              class="mobile-group"
            >
              <div class="mobile-group-head">
                <NCheckbox
                  :checked="selected.has(key)"
                  :aria-label="g.name"
                  @update:checked="(v) => selectGroup(g, v)"
                /><NButton
                  text
                  @click="
                    expanded = expanded.includes(key)
                      ? expanded.filter((k) => k !== key)
                      : [...expanded, key]
                  "
                  >{{ g.name }}</NButton
                ><small class="muted">{{ targetLabel(g.target) }}</small>
              </div>
              <div v-if="expanded.includes(key)" class="mobile-group-body">
                <NSelect
                  v-model:value="g.target"
                  :options="targets"
                  :aria-label="t('groups.colTarget')"
                />
                <template v-if="isCountry(g)"
                  ><NSelect
                    :value="g.except ? 'except' : 'only'"
                    :options="modeOptions"
                    @update:value="
                      (v) => (g.except = v === 'except' || undefined)
                    " /><NSelect
                    v-model:value="g.countries"
                    multiple
                    filterable
                    :options="countryOptions"
                /></template>
                <DomainEntries
                  v-else
                  :group="g"
                  :status="
                    isPreview(g) ? (previews.get(g) ?? null) : domainStatus
                  "
                  :query="search"
                  :preview="isPreview(g)"
                  :refreshing="refreshing.has(g)"
                  @refresh="(names) => refresh(g, names)"
                />
                <NButton size="small" @click="renameGroup(g)">{{
                  t('common.edit')
                }}</NButton>
                <NButton size="small" type="error" @click="removeGroup(g)">{{
                  t('common.delete')
                }}</NButton>
              </div>
            </article>
          </section>
          <p class="muted">{{ t('groups.countryIntro') }}</p>
        </div>
      </div>
      <aside v-if="selectedGroups.length" class="bulk-panel">
        <h2>{{ t('ui.changeRoute') }}</h2>
        <p>{{ t('ui.selected', { n: selectedGroups.length }) }}</p>
        <NSelect
          v-model:value="newTarget"
          :options="targets"
          :placeholder="t('groups.colTarget')"
          :aria-label="t('groups.colTarget')"
        />
        <div class="bulk-preview">
          <div v-for="g in selectedGroups" :key="groupKey(g)" class="data-row">
            <div>
              <b>{{ g.name }}</b
              ><small
                >{{ targetLabel(g.target) }} →
                {{ newTarget ? targetLabel(newTarget) : '—' }}</small
              >
            </div>
          </div>
        </div>
        <p class="muted">{{ t('ui.unchanged') }}</p>
        <NButton
          type="primary"
          block
          :disabled="!newTarget || !targets.some((t) => t.value === newTarget)"
          @click="saveBulk"
          >{{ t('ui.saveDraft') }}</NButton
        >
        <NButton block quaternary @click="selected = new Set()">{{
          t('ui.clear')
        }}</NButton>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.count {
  margin-left: 6px;
  font-size: 11px;
  color: var(--muted);
}
.head-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.page-head > div:first-child {
  flex: 1;
  min-width: 0;
}
:deep(.cchip) {
  display: inline-flex;
  align-items: center;
  margin: 2px 4px 2px 0;
  white-space: nowrap;
  font-size: 11px;
  border: 1px solid #3b82f6;
  color: #60a5fa;
  border-radius: 999px;
  padding: 1px 8px;
}
:deep(.cchip-x) {
  margin-left: 4px;
  border: 0;
  background: none;
  color: inherit;
  cursor: pointer;
  padding: 0 2px;
  font-size: 13px;
  line-height: 1;
}
.countries {
  margin-top: 24px;
}
.countries h2 {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  margin: 0 0 4px;
}
.countries p {
  margin: 0 0 10px;
  font-size: 13px;
}
:deep(.cchip.except) {
  border-color: var(--accent);
  color: var(--accent);
}
:deep(.gname) {
  display: flex;
  align-items: center;
  gap: 10px;
}
:deep(.tdot) {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
:deep(.tdot.tunnel) {
  background: #3b82f6;
}
:deep(.tdot.direct) {
  background: var(--ok);
}
:deep(.tdot.block) {
  background: var(--bad);
}
:deep(.entries) {
  padding: 4px 8px 8px 40px;
}
:deep(.hint) {
  font-size: 12px;
  color: var(--muted);
  margin-top: 6px;
}
</style>

<style scoped>
.bulk-layout.has-selection {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 28px;
}
.group-tables {
  min-width: 0;
}
.selection-toolbar {
  display: flex;
  gap: 16px;
  align-items: center;
  flex-wrap: wrap;
  padding: 12px 0 20px;
}
.selection-summary {
  display: inline-flex;
  align-items: center;
  gap: 12px;
}
.bulk-panel {
  border-left: 1px solid var(--line);
  padding-left: 28px;
}
.bulk-panel h2 {
  font-size: 22px;
  margin: 10px 0;
}
.bulk-preview {
  max-height: 400px;
  overflow: auto;
}
.bulk-preview small {
  display: block;
  color: var(--muted);
  margin-top: 3px;
}
.mobile-groups {
  display: none;
}
@media (max-width: 1100px) {
  .bulk-layout.has-selection {
    grid-template-columns: minmax(0, 1fr) 280px;
    gap: 18px;
  }
  .bulk-panel {
    padding-left: 18px;
  }
}
@media (max-width: 800px) {
  .page-head {
    flex-direction: column;
  }
  .bulk-layout.has-selection {
    display: block;
  }
  .desktop-groups {
    display: none;
  }
  .mobile-groups {
    display: block;
  }
  .mobile-groups h2 {
    font-size: 18px;
    margin: 20px 0 6px;
  }
  .mobile-group {
    border-bottom: 1px solid var(--line);
  }
  .mobile-group-head {
    display: flex;
    gap: 12px;
    align-items: center;
    padding: 16px 0;
  }
  .mobile-group-head :deep(.n-button) {
    white-space: normal;
    height: auto;
    text-align: left;
    flex: 1;
    justify-content: flex-start;
  }
  .mobile-group-head small {
    max-width: 100px;
    overflow-wrap: anywhere;
  }
  .mobile-group-body {
    display: grid;
    gap: 12px;
    padding: 0 0 18px;
  }
  .bulk-panel {
    position: sticky;
    bottom: 0;
    background: var(--page);
    padding: 14px 0 0;
    border-left: 0;
    border-top: 1px solid var(--line);
    z-index: 5;
  }
  .bulk-panel h2,
  .bulk-preview,
  .bulk-panel > p {
    display: none;
  }
  .bulk-panel :deep(.n-select) {
    margin-bottom: 12px;
  }
  .bulk-panel :deep(.n-button) {
    min-height: 44px;
  }
  .bulk-panel:before {
    content: '';
    position: absolute;
    inset: 0 -18px;
    z-index: -1;
    background: var(--page);
    border-top: 1px solid var(--line);
  }
}
</style>
<style scoped>
.mobile-target-filter {
  display: none;
}
.demo-note {
  font-size: 12px;
  margin: 0 0 12px;
}
@media (max-width: 800px) {
  .page-head p {
    display: none;
  }
  .target-tabs {
    display: none;
  }
  .mobile-target-filter {
    display: block;
    margin-bottom: 12px;
  }
  .selection-toolbar {
    padding: 8px 0 14px;
    gap: 8px;
  }
  .bulk-panel {
    padding-bottom: env(safe-area-inset-bottom);
  }
  .mobile-groups {
    padding-bottom: 12px;
  }
}
</style>
