<script setup lang="ts">
// Route, audit, and system journals combine SSE updates with server-filtered pages.
import { computed, h, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  NCheckbox,
  NButton,
  NDataTable,
  NDropdown,
  NInput,
  NSelect,
  NTab,
  NTabs,
  NTooltip,
  useMessage,
  type DataTableColumns,
  type DataTableFilterState,
} from 'naive-ui'
import {
  Copy,
  Download,
  FileText,
  Globe,
  Maximize2,
  Minimize2,
  Pause,
  Play,
  Search,
  Server,
  Shield,
} from '@lucide/vue'
import { api } from '../api'
import { clientIP, groupRoutes, type RouteGroup } from '../journal'
import { firewallJournalEntry } from '../firewallJournal'
import { store, targetLabel } from '../store'
import type { JournalEntry, JournalKind } from '../types'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const { t, locale } = useI18n()

// label is a translation key.
const tabs: { value: JournalKind; label: string; icon: typeof Globe }[] = [
  { value: 'routes', label: 'journal.routes', icon: Globe },
  { value: 'audit', label: 'journal.audit', icon: Shield },
  { value: 'system', label: 'journal.system', icon: Server },
]
const kind = ref<JournalKind>(
  tabs.some((tb) => tb.value === route.query.tab)
    ? (route.query.tab as JournalKind)
    : 'routes',
)
watch(kind, (k) => router.replace({ query: { tab: k } }))

const search = ref('')
const limit = ref(50)
const filters = reactive<Record<string, string>>({})
const entries = ref<JournalEntry[]>([])
const facets = ref<Record<string, string[]>>({})
const hasMore = ref(false)
const loading = ref(false)
const live = ref(true)
const missed = ref(0) // New entries received while paused.
const fresh = ref(new Set<number>())
const expanded = ref<number[]>([])
const full = ref(false)
const grouped = ref(true)
const routeGroups = computed(() => groupRoutes(entries.value))
const displayed = computed(() =>
  kind.value === 'routes' && grouped.value ? routeGroups.value : entries.value,
)
watch(grouped, () => (expanded.value = []))

const actorLabels = computed<Record<string, string>>(() => ({
  user: t('journal.actorUser'),
  system: t('journal.system'),
  watchdog: t('monitor.watchdog'),
}))
const resultLabels = computed<Record<string, [string, string]>>(() => ({
  ok: [t('journal.rOk'), 'ok'],
  info: [t('journal.rInfo'), 'dim'],
  warn: [t('journal.rWarn'), 'warn'],
  error: [t('journal.rError'), 'bad'],
}))
const actorLabel = (a?: string) => actorLabels.value[a ?? ''] ?? a
const firewallView = (e: JournalEntry) => firewallJournalEntry(e, t)
const resultLabel = (r?: string) => resultLabels.value[r ?? '']?.[0] ?? r
const levelClass: Record<string, string> = {
  INFO: 'dim',
  WARN: 'warn',
  ERROR: 'bad',
}

// Resolve device names from configured clients and discovered addresses.
const deviceNames = computed(() => {
  const m = new Map<string, string>()
  for (const d of store.state.devices) if (d.client) m.set(d.ip, d.client)
  for (const c of store.state.config?.clients ?? [])
    if (c.ip) m.set(c.ip, c.name)
  return m
})
const deviceIP = clientIP
function deviceLabel(client?: string) {
  const ip = deviceIP(client)
  return deviceNames.value.get(ip) ?? ip
}
function outLabel(tag?: string) {
  return tag === 'direct' ? t('target.direct') : targetLabel(tag)
}

function time(iso: string) {
  return new Date(iso).toLocaleTimeString(locale.value, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}
function dateTime(iso: string) {
  return new Date(iso).toLocaleString(locale.value)
}

let loadSequence = 0
// Ignore older page responses after filters change or the component unmounts.
async function load(more = false) {
  const seq = ++loadSequence
  loading.value = true
  try {
    const page = await api.journal(kind.value, {
      q: search.value.trim(),
      limit: limit.value,
      before: more ? entries.value.at(-1)?.id : undefined,
      ...filters,
    })
    if (seq !== loadSequence) return
    entries.value = more ? [...entries.value, ...page.entries] : page.entries
    hasMore.value = page.has_more
    facets.value = page.facets
    missed.value = 0
  } catch (e) {
    if (seq === loadSequence) message.error((e as Error).message)
  } finally {
    if (seq === loadSequence) loading.value = false
  }
}

// Apply the current server-style filters to incoming stream entries.
function matches(e: JournalEntry) {
  for (const [k, v] of Object.entries(filters)) {
    if (!v) continue
    const got =
      k === 'client'
        ? deviceIP(e.client)
        : (e as unknown as Record<string, unknown>)[k]
    if (got !== v) return false
  }
  const q = search.value.trim().toLowerCase()
  if (!q) return true
  return Object.values(e).some(
    (v) => typeof v === 'string' && v.toLowerCase().includes(q),
  )
}

let source: ReturnType<typeof api.journalStream> | null = null
// Replace the stream for the selected journal; unmount closes the active connection.
function connect() {
  source?.close()
  source = api.journalStream(kind.value)
  source.onmessage = (ev) => {
    const e = JSON.parse(ev.data) as JournalEntry
    if (!matches(e)) return
    // Repeated messages reuse their ID with an updated count.
    const i = entries.value.findIndex((x) => x.id === e.id)
    if (i >= 0) {
      entries.value[i] = e
      return
    }
    if (!live.value) {
      missed.value++
      return
    }
    entries.value = [e, ...entries.value].slice(
      0,
      Math.max(limit.value, entries.value.length),
    )
    fresh.value.add(e.id)
    setTimeout(() => fresh.value.delete(e.id), 1500)
  }
}
onUnmounted(() => {
  source?.close()
  clearTimeout(timer)
  loadSequence++
})

watch(
  kind,
  () => {
    for (const k of Object.keys(filters)) delete filters[k]
    expanded.value = []
    load()
    connect()
  },
  { immediate: true },
)
let timer: number | undefined
watch(search, () => {
  clearTimeout(timer)
  timer = window.setTimeout(() => load(), 300)
})
watch(limit, () => load())

function toggleLive() {
  live.value = !live.value
  if (live.value && missed.value) load()
}

function onFilters(state: DataTableFilterState) {
  for (const [k, v] of Object.entries(state)) {
    const val = Array.isArray(v) ? v[0] : v
    if (val === null || val === undefined || val === '') delete filters[k]
    else filters[k] = String(val)
  }
  load()
}

function filterColumn(key: string, label: (v: string) => string) {
  return {
    filter: true as const,
    filterMultiple: false,
    filterOptionValue: filters[key] ?? null,
    filterOptions: (facets.value[key] ?? []).map((v) => ({
      label: label(v),
      value: v,
    })),
  }
}

function line(e: JournalEntry) {
  const ts = dateTime(e.time)
  const view = firewallView(e)
  switch (kind.value) {
    case 'routes':
      return `${ts}  ${deviceLabel(e.client)} (${e.client}) → ${e.host}${e.ip && e.ip !== e.host ? ` (${e.ip})` : ''}  ${outLabel(e.outbound)}  ${e.rule}`
    case 'audit':
      if (view) {
        return `${ts}  ${view.source}  ${view.title}  ${view.summary}\n${e.details ?? view.details.map(([key, value]) => `${key}: ${value}`).join('\n')}`
      }
      return `${ts}  ${actorLabel(e.actor)}  ${e.action} [${e.target}]  ${resultLabel(e.result)}  ${e.details ?? ''}`
    default:
      return `${ts}  ${e.level}  ${e.source}  ${e.message}${(e.count ?? 0) > 1 ? ` (×${e.count})` : ''}`
  }
}
async function copy(e: JournalEntry) {
  try {
    await navigator.clipboard.writeText(line(e))
    message.success(t('journal.copied'))
  } catch {
    message.error(t('journal.noClipboard'))
  }
}

const exportOptions = computed(() => [
  { label: t('journal.exp1h'), key: '1h' },
  { label: t('journal.exp24h'), key: '24h' },
  { label: t('journal.exp7d'), key: '168h' },
  { label: t('journal.expAll'), key: '8760h' },
])
function download(since: string) {
  const a = document.createElement('a')
  a.href = api.journalExportURL(kind.value, since)
  a.download = `keenproxy-${kind.value}.txt`
  a.click()
}

const timeCol = () => ({
  title: t('journal.time'),
  key: 'time',
  width: 175,
  render: (e: JournalEntry) =>
    h(
      'span',
      { class: 'mono', title: dateTime(e.time) },
      grouped.value && 'members' in e
        ? `${time((e as RouteGroup).firstTime)}–${time((e as RouteGroup).lastTime)}`
        : time(e.time),
    ),
})
const copyCol = () => ({
  title: '',
  key: 'copy',
  width: 48,
  render: (e: JournalEntry) =>
    h(
      NButton,
      {
        size: 'tiny',
        quaternary: true,
        title: t('journal.copy'),
        'aria-label': t('journal.copy'),
        onClick: () => copy(e),
      },
      { icon: () => h(Copy, { size: 14 }) },
    ),
})
function details(rows: [string, string | undefined][]) {
  return h(
    'dl',
    { class: 'details' },
    rows
      .filter(([, v]) => v)
      .flatMap(([k, v]) => [h('dt', k), h('dd', { class: 'mono' }, v)]),
  )
}

const columns = computed<DataTableColumns<JournalEntry>>(() => {
  switch (kind.value) {
    case 'routes':
      return [
        {
          type: 'expand',
          renderExpand: (e) =>
            h('div', [
              details([
                [t('journal.time'), dateTime(e.time)],
                [t('journal.device'), `${deviceLabel(e.client)} · ${e.client}`],
                [
                  t('journal.destination'),
                  e.ip && e.ip !== e.host ? `${e.host} · ${e.ip}` : e.host,
                ],
                [
                  t('journal.protocol'),
                  `${e.network?.toUpperCase() ?? '—'} ${e.destination_port ?? ''}`,
                ],
                [t('groups.colTarget'), outLabel(e.outbound)],
                [t('journal.rule'), e.rule],
                [t('journal.ruleRaw'), e.rule_raw || t('journal.final')],
              ]),
              'members' in e
                ? h(
                    'div',
                    { class: 'raw-members' },
                    (e as RouteGroup).members.map((m) =>
                      h('div', { class: 'data-row', key: m.id }, [
                        h('span', { class: 'mono' }, dateTime(m.time)),
                        h('span', m.client),
                        h(
                          'span',
                          `${m.network ?? ''} ${m.destination_port ?? ''}`,
                        ),
                        h('span', { class: 'muted' }, t('ui.chosen')),
                      ]),
                    ),
                  )
                : null,
            ]),
        },
        timeCol(),
        ...(grouped.value
          ? [
              {
                title: t('ui.connections'),
                key: 'count',
                width: 120,
                render: (e: JournalEntry) =>
                  h('b', { class: 'pill ok' }, String(e.count ?? 1)),
              },
            ]
          : []),
        {
          title: t('journal.host'),
          key: 'host',
          width: 230,
          ellipsis: { tooltip: true },
          render: (e) => h('span', { class: 'mono' }, e.host),
        },
        {
          title: 'IP',
          key: 'ip',
          width: 125,
          render: (e) => h('span', { class: 'mono muted' }, e.ip),
        },
        {
          title: t('journal.device'),
          key: 'client',
          width: 140,
          ellipsis: { tooltip: true },
          ...filterColumn('client', (v) => deviceNames.value.get(v) ?? v),
          render: (e) => h('span', { title: e.client }, deviceLabel(e.client)),
        },
        {
          title: t('groups.colTarget'),
          key: 'outbound',
          width: 165,
          ...filterColumn('outbound', outLabel),
          render: (e) =>
            h('span', { class: 'out' }, [
              h('i', {
                class: ['tdot', e.outbound === 'direct' ? 'direct' : 'tunnel'],
              }),
              outLabel(e.outbound),
            ]),
        },
        {
          title: t('journal.rule'),
          key: 'rule',
          minWidth: 280,
          ellipsis: { tooltip: true },
          // Show the matching group first so truncation preserves the routing decision.
          render: (e) => {
            const i = e.rule?.indexOf(': ') ?? -1
            if (i < 0) return h('span', { class: 'rule' }, e.rule)
            return h('span', { class: 'rule' }, [
              e.rule!.slice(i + 2),
              h('span', { class: 'muted' }, ' · ' + e.rule!.slice(0, i)),
            ])
          },
        },
        {
          title: t('features.exceptions'),
          key: 'filter_exception',
          width: 150,
          render: (e) =>
            e.network === 'dns' &&
            e.outbound === 'block' &&
            e.source &&
            e.source !== 'manual' &&
            e.source !== 'firefox-canary'
              ? h(
                  NButton,
                  {
                    size: 'tiny',
                    onClick: () => {
                      const name = e.host?.replace(/\.$/, '')
                      const f = store.state.config?.filters
                      if (name && f && !f.exceptions.includes(name)) {
                        f.exceptions.push(name)
                        message.success(t('features.draft'))
                      }
                    },
                  },
                  () => t('features.addException'),
                )
              : null,
        },
        copyCol(),
      ]
    case 'audit':
      return [
        {
          type: 'expand',
          renderExpand: (e) =>
            h('div', [details([
              [t('journal.time'), dateTime(e.time)],
              [t('journal.who'), actorLabel(e.actor)],
              [t('journal.object'), e.target],
              ...(firewallView(e)?.details ?? [[t('journal.details'), e.details]] as [string, string | undefined][]),
            ]), e.firewall && e.details ? h('details', { class: 'raw-evidence' }, [
              h('summary', t('journal.firewallRaw')),
              h('pre', e.details),
            ]) : null]),
        },
        timeCol(),
        {
          title: t('journal.action'),
          key: 'action',
          render: (e) => {
            const summary = firewallView(e)?.summary || e.details
            return h('div', [
              h('div', firewallView(e)?.title ?? e.action),
              summary ? h('div', { class: 'sub' }, summary) : null,
            ])
          },
        },
        {
          title: t('journal.who'),
          key: 'actor',
          width: 170,
          ...filterColumn('actor', (v) => actorLabel(v) ?? v),
          render: (e) => {
            const source = firewallView(e)?.source
            return h('div', [
              h('div', actorLabel(e.actor)),
              source ? h('div', { class: 'sub' }, source) : null,
            ])
          },
        },
        {
          title: t('journal.object'),
          key: 'target',
          width: 120,
          ...filterColumn('target', (v) => v),
          render: (e) => h('span', { class: 'mono muted' }, e.target),
        },
        {
          title: t('journal.result'),
          key: 'result',
          width: 150,
          ...filterColumn('result', (v) => resultLabel(v) ?? v),
          render: (e) => {
            const [label, cls] = resultLabels.value[e.result ?? ''] ?? [
              e.result,
              'dim',
            ]
            return h('span', { class: ['pill', cls] }, label)
          },
        },
        copyCol(),
      ]
    default:
      return [
        timeCol(),
        {
          title: t('journal.level'),
          key: 'level',
          width: 110,
          ...filterColumn('level', (v) => v),
          render: (e) =>
            h(
              'span',
              { class: ['pill', levelClass[e.level ?? ''] ?? 'dim'] },
              e.level,
            ),
        },
        {
          title: t('journal.source'),
          key: 'source',
          width: 120,
          ...filterColumn('source', (v) => v),
          render: (e) => h('span', { class: 'muted' }, e.source),
        },
        {
          title: t('journal.message'),
          key: 'message',
          render: (e) =>
            h('span', { class: 'mono msg' }, [
              e.message,
              (e.count ?? 0) > 1
                ? h(
                    'span',
                    { class: 'pill dim count', title: t('journal.repeated') },
                    `×${e.count}`,
                  )
                : null,
            ]),
        },
        copyCol(),
      ]
  }
})

const emptyText = computed(() =>
  search.value || Object.keys(filters).length
    ? t('journal.notFound')
    : kind.value === 'routes'
      ? t('journal.noConnections')
      : t('journal.noEntries'),
)
</script>

<template>
  <div :class="{ fullscreen: full }">
    <div class="page-head">
      <div>
        <h1><FileText :size="22" /> {{ t('nav.journal') }}</h1>
        <p>{{ t('journal.intro') }}</p>
      </div>
    </div>

    <NTabs v-model:value="kind" type="line" style="margin-bottom: 12px">
      <NTab v-for="tb in tabs" :key="tb.value" :name="tb.value">
        <span class="tab"
          ><component :is="tb.icon" :size="15" /> {{ t(tb.label) }}</span
        >
      </NTab>
    </NTabs>

    <div class="toolbar">
      <NInput
        v-model:value="search"
        :placeholder="t('journal.search')"
        clearable
        class="search"
      >
        <template #prefix><Search :size="16" /></template>
      </NInput>
      <NSelect
        v-model:value="limit"
        :options="
          [50, 100, 200].map((n) => ({
            label: t('journal.rows', { n }),
            value: n,
          }))
        "
        style="width: 120px"
      />
      <NTooltip>
        <template #trigger>
          <NButton
            :type="live ? 'default' : 'warning'"
            :aria-label="live ? t('journal.pause') : t('journal.resume')"
            @click="toggleLive"
          >
            <template #icon
              ><Pause v-if="live" :size="16" /><Play v-else :size="16"
            /></template>
            {{
              live
                ? t('journal.pause')
                : missed
                  ? t('journal.newN', { n: missed })
                  : t('journal.resume')
            }}
          </NButton>
        </template>
        {{ live ? t('journal.pauseHint') : t('journal.resumeHint') }}
      </NTooltip>
      <NDropdown :options="exportOptions" trigger="click" @select="download">
        <NButton
          :title="t('journal.downloadHint')"
          :aria-label="t('journal.download')"
        >
          <template #icon><Download :size="16" /></template>
        </NButton>
      </NDropdown>
      <NButton
        :title="full ? t('clients.collapse') : t('journal.fullscreen')"
        :aria-label="full ? t('clients.collapse') : t('journal.fullscreen')"
        @click="full = !full"
      >
        <template #icon
          ><Minimize2 v-if="full" :size="16" /><Maximize2 v-else :size="16"
        /></template>
      </NButton>
    </div>
    <div v-if="kind === 'routes'" class="grouping">
      <NCheckbox v-model:checked="grouped">{{ t('ui.grouped') }}</NCheckbox
      ><span class="muted">{{ t('ui.groupHint') }}</span>
    </div>
    <p class="live muted">
      <i class="dot" :class="{ on: live }" />
      {{ live ? t('common.realtime') : t('journal.paused') }}
      <template v-if="kind === 'routes'">
        · {{ t('journal.routesNote') }}</template
      >
      <template v-else-if="kind === 'system'">
        · {{ t('journal.systemNote') }}</template
      >
    </p>

    <NDataTable
      v-model:expanded-row-keys="expanded"
      :columns="columns"
      :data="displayed"
      :row-key="(e: JournalEntry) => e.id"
      :row-class-name="(e: JournalEntry) => (fresh.has(e.id) ? 'fresh' : '')"
      :loading="loading"
      :bordered="false"
      remote
      :scroll-x="kind === 'routes' ? (grouped ? 1480 : 1360) : 820"
      size="small"
      class="table"
      @update:filters="onFilters"
    >
      <template #empty
        ><span class="muted">{{ emptyText }}</span></template
      >
    </NDataTable>
    <div v-if="hasMore" class="more">
      <NButton :loading="loading" @click="load(true)">{{
        t('journal.more')
      }}</NButton>
    </div>
    <p class="muted note">{{ t('journal.storage') }}</p>
  </div>
</template>

<style scoped>
.fullscreen {
  position: fixed;
  inset: 0;
  z-index: 50;
  background: var(--page);
  padding: 20px 24px;
  overflow: auto;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.toolbar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.search {
  flex: 1;
  min-width: 220px;
}
.live {
  font-size: 12px;
  margin: 8px 0 10px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--muted);
}
.dot.on {
  background: var(--ok);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 25%, transparent);
}
.more {
  display: flex;
  justify-content: center;
  margin-top: 12px;
}
.note {
  font-size: 12px;
  margin-top: 14px;
}
:deep(.fresh td) {
  animation: fresh 1.5s ease-out;
}
@keyframes fresh {
  from {
    background: color-mix(in srgb, var(--accent) 18%, transparent);
  }
}
:deep(.out) {
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  gap: 8px;
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
:deep(.rule) {
  font-size: 12px;
}
:deep(.sub) {
  font-size: 12px;
  color: var(--muted);
  margin-top: 2px;
}
:deep(.msg) {
  white-space: pre-wrap;
  word-break: break-word;
}
:deep(.count) {
  margin-left: 8px;
  padding: 0 6px;
}
.raw-evidence { margin-top: 16px; }
.raw-evidence summary { cursor: pointer; color: var(--muted); }
.raw-evidence pre { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 12px; }
:deep(.details) {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 16px;
  margin: 2px 0 2px 8px;
  font-size: 13px;
}
:deep(.details dt) {
  color: var(--muted);
}
:deep(.details dd) {
  margin: 0;
  word-break: break-all;
}
</style>

<style scoped>
.grouping {
  display: flex;
  align-items: center;
  gap: 20px;
  flex-wrap: wrap;
  margin: 22px 0 10px;
  font-size: 13px;
}
:deep(.raw-members) {
  padding: 12px 24px;
  max-height: 320px;
  overflow: auto;
}
:deep(.raw-members .data-row) {
  font-size: 13px;
  flex-wrap: wrap;
}
:deep(.raw-members span) {
  overflow-wrap: anywhere;
}
</style>
