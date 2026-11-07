<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NAlert, NButton, NCard, NCheckbox, NInput, NSelect, NSwitch, NTag, useMessage } from 'naive-ui'
import { ArrowDown, ArrowUp, Download, MapPin, Search, Trash2 } from '@lucide/vue'
import { api } from '../api'
import { store } from '../store'
import { countryName, flag } from '../countries'
import { MAX_GEO_DATABASES, customGeo, geoName, isCategoryDB, move } from '../geoip'
import type { Config, GeoBuiltin, GeoDatabaseInfo, GeoLookup, GeoStatus } from '../types'

const { t } = useI18n()
const message = useMessage()
const builtins = ref<GeoBuiltin[]>([])
const status = ref<GeoStatus>()
const error = ref('')
const busy = ref('') // database ID being downloaded, or '*' for all enabled
const name = ref('')
const source = ref('')
const category = ref(false) // new custom database holds category labels
const ip = ref('')
const target = ref('')
const lookup = ref<GeoLookup>()
const testing = ref(false)

// Edits go to the shared draft; downloads and tests act on the applied configuration.
const dbs = computed(() => store.state.config!.geoip.databases)
const applied = computed(() => (store.state.saved ? (JSON.parse(store.state.saved) as Config).geoip.databases : []))
const dirty = computed(() => store.dirty.value)
// Only country databases answer country groups; category databases serve category groups.
const enabled = computed(() => dbs.value.filter((d) => d.enabled && !isCategoryDB(d, builtins.value)).length)

function info(id: string): GeoDatabaseInfo | undefined {
  return status.value?.databases.find((d) => d.id === id)
}
function builtin(id: string) {
  return builtins.value.find((b) => b.id === id)
}
function when(s?: string) {
  return s ? new Date(s).toLocaleString() : ''
}
function updates(b: GeoBuiltin) {
  const key = { daily: 'daily', weekly: 'weekly', monthly: 'monthly', '6h': 'sixHours' }[b.updates]
  return key ? t(`geoip.updates.${key}`) : b.updates
}

// One status line per database, from the applied runtime state.
function stateLine(id: string): string {
  if (!applied.value.some((d) => d.id === id)) return t('geoip.draftOnly')
  const i = info(id)
  if (!i?.present) return t('geoip.notDownloaded')
  const parts = [i.loaded ? t('geoip.inUse', { built: i.built }) : t('geoip.presentOff', { built: i.built })]
  if (i.updated) parts.push(t('geoip.updated', { time: when(i.updated) }))
  else if (i.checked) parts.push(t('geoip.checked', { time: when(i.checked) }))
  if (i.next) parts.push(t('geoip.next', { time: when(i.next) }))
  return parts.join(' · ')
}

function add() {
  if (dbs.value.length >= MAX_GEO_DATABASES) return message.error(t('geoip.tooMany', { n: MAX_GEO_DATABASES }))
  try {
    const db = customGeo(name.value, source.value, dbs.value.map((d) => d.id))
    if (category.value) db.kind = 'category'
    dbs.value.push(db)
    name.value = source.value = ''
    category.value = false
  } catch (e) {
    message.error(t((e as Error).message))
  }
}

async function reload() {
  try {
    status.value = await api.geoip()
  } catch (e) {
    error.value = (e as Error).message
  }
}

// Download one database (it may be disabled) or every enabled one.
async function update(id = '') {
  busy.value = id || '*'
  error.value = ''
  try {
    status.value = await api.updateGeo(id)
    if (status.value.error) error.value = status.value.error
    else message.success(status.value.demo ? t('geoip.demo') : t('geoip.done'))
    await store.refreshStatus()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = ''
  }
}

async function test() {
  testing.value = true
  lookup.value = undefined
  try {
    lookup.value = await api.lookupGeo(ip.value.trim(), target.value)
  } catch (e) {
    message.error((e as Error).message)
  } finally {
    testing.value = false
  }
}

const targets = computed(() => [
  { label: t('geoip.allEnabled'), value: '' },
  ...applied.value.map((d) => ({ label: geoName(d, builtins.value) + (d.enabled ? '' : ` (${t('geoip.off')})`), value: d.id })),
])
const answeredBy = computed(() => lookup.value?.results.find((r) => r.id === lookup.value?.database)?.name ?? '')

// Status changes after Apply because databases may have been added or re-enabled;
// earlier test results describe the previous configuration.
watch(
  () => store.state.saved,
  () => {
    lookup.value = undefined
    reload()
  },
)
onMounted(async () => {
  try {
    builtins.value = (await api.catalog()).geoip ?? []
  } catch (e) {
    error.value = (e as Error).message
  }
  await reload()
})
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <h1><MapPin :size="22" /> {{ t('nav.geoip') }}</h1>
        <p>{{ t('geoip.intro') }}</p>
      </div>
      <NButton :loading="busy === '*'" :disabled="dirty || !!busy" @click="update()">
        <template #icon><Download :size="16" /></template>{{ t('geoip.updateAll') }}
      </NButton>
    </div>

    <NAlert v-if="dirty" type="warning" class="gap">{{ t('geoip.applyFirst') }}</NAlert>
    <NAlert v-if="status?.demo" type="info" class="gap">{{ t('geoip.demo') }}</NAlert>
    <NAlert v-if="!enabled" type="warning" class="gap">{{ t('geoip.noneEnabled') }}</NAlert>
    <NAlert v-if="error" type="error" class="gap"><pre class="err">{{ error }}</pre></NAlert>

    <NCard size="small" :title="t('geoip.sources')">
      <template #header-extra>
        <span class="muted">{{ t('geoip.active', { n: store.state.status?.geoip.active ?? 0, c: store.state.status?.geoip.categories ?? 0 }) }}</span>
      </template>
      <div v-for="(db, i) in dbs" :key="db.id" class="db">
        <div class="order">
          <span class="rank">{{ i + 1 }}</span>
          <NButton quaternary size="tiny" :disabled="i === 0" :title="t('geoip.up')" @click="move(dbs, i, -1)"><ArrowUp :size="14" /></NButton>
          <NButton quaternary size="tiny" :disabled="i === dbs.length - 1" :title="t('geoip.down')" @click="move(dbs, i, 1)"><ArrowDown :size="14" /></NButton>
        </div>
        <div class="body">
          <div class="title">
            <b>{{ geoName(db, builtins) }}</b>
            <NTag size="small" :bordered="false">{{ builtin(db.id) ? t('geoip.builtin') : t('geoip.custom') }}</NTag>
            <NTag v-if="isCategoryDB(db, builtins)" size="small" type="info" :bordered="false">{{ t('geoip.category') }}</NTag>
          </div>
          <div v-if="builtin(db.id)" class="muted small">
            <a :href="builtin(db.id)!.homepage" target="_blank" rel="noopener">{{ builtin(db.id)!.origin }}</a>
            · {{ t('geoip.license', { license: builtin(db.id)!.license }) }} · {{ updates(builtin(db.id)!) }}
          </div>
          <div v-if="builtin(db.id)?.categories?.length" class="muted small">{{ t('geoip.labels', { labels: builtin(db.id)!.categories!.join(', ') }) }}</div>
          <div v-else-if="!builtin(db.id)" class="muted small mono url">{{ db.url || db.file }}<template v-if="db.file"> · {{ t('geoip.localFile') }}</template></div>
          <div class="small">{{ stateLine(db.id) }}</div>
          <div v-if="info(db.id)?.failures" class="small warn-text">{{ t('geoip.failures', { n: info(db.id)!.failures }) }}</div>
          <div v-if="info(db.id)?.error" class="small bad">{{ info(db.id)!.error }}</div>
        </div>
        <div class="actions">
          <label class="switch"><NSwitch v-model:value="db.enabled" size="small" /> {{ t('geoip.enabled') }}</label>
          <NButton
            size="small"
            :loading="busy === db.id"
            :disabled="dirty || !!busy || !applied.some((d) => d.id === db.id) || (!!db.file && !db.enabled)"
            @click="update(db.id)"
            >{{ db.file ? t('geoip.reread') : t('geoip.download') }}</NButton
          >
          <NButton v-if="!builtin(db.id)" size="small" quaternary :title="t('geoip.remove')" @click="dbs.splice(i, 1)"><Trash2 :size="14" /></NButton>
        </div>
      </div>
    </NCard>

    <div class="cols">
      <NCard size="small" :title="t('geoip.testTitle')">
        <p class="muted small">{{ t('geoip.testHint') }}</p>
        <form class="test" @submit.prevent="test">
          <NInput v-model:value="ip" class="mono" :placeholder="t('geoip.ip')" clearable />
          <NSelect v-model:value="target" :options="targets" />
          <NButton attr-type="submit" type="primary" :loading="testing" :disabled="!ip.trim()">
            <template #icon><Search :size="16" /></template>{{ t('geoip.test') }}
          </NButton>
        </form>
        <div v-if="lookup" class="results">
          <p class="summary">
            <template v-if="lookup.country">{{ flag(lookup.country) }} {{ t('geoip.result', { country: `${countryName(lookup.country)} (${lookup.country})`, db: answeredBy }) }}</template>
            <template v-else>{{ t('geoip.unknown') }}</template>
          </p>
          <p v-if="lookup.categories?.length" class="summary">{{ t('geoip.categoriesResult', { labels: lookup.categories.join(', ') }) }}</p>
          <div v-for="r in lookup.results" :key="r.id" class="row">
            <span>{{ r.name }}<span v-if="!r.enabled" class="muted"> · {{ t('geoip.off') }}</span></span>
            <span v-if="r.error" class="bad">{{ r.error }}</span>
            <span v-else-if="r.country" class="mono">{{ flag(r.country) }} {{ r.country }}</span>
            <span v-else class="muted">{{ t('geoip.noData') }}</span>
          </div>
        </div>
      </NCard>

      <NCard size="small" :title="t('geoip.addTitle')">
        <p class="muted small">{{ t('geoip.addHint') }}</p>
        <form class="add" @submit.prevent="add">
          <NInput v-model:value="name" :placeholder="t('geoip.name')" :maxlength="64" />
          <NInput v-model:value="source" class="mono" :placeholder="t('geoip.source')" />
          <NCheckbox v-model:checked="category">{{ t('geoip.categoryDB') }}</NCheckbox>
          <NButton attr-type="submit" :disabled="!source.trim()">{{ t('geoip.add') }}</NButton>
        </form>
      </NCard>
    </div>
  </div>
</template>

<style scoped>
.gap {
  margin-bottom: 10px;
}
.err {
  margin: 0;
  white-space: pre-wrap;
  font: inherit;
}
.db {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: 12px;
  align-items: start;
  padding: 12px 0;
  border-bottom: 1px solid var(--line);
}
.db:last-child {
  border-bottom: 0;
}
.order {
  display: flex;
  align-items: center;
  gap: 2px;
}
.rank {
  min-width: 18px;
  font-weight: 600;
  color: var(--muted);
}
.body {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.small {
  font-size: 12px;
}
.url {
  overflow-wrap: anywhere;
}
.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}
.switch {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}
.bad {
  color: var(--bad);
}
.warn-text {
  color: var(--warn);
}
a {
  color: var(--accent);
}
.cols {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(360px, 100%), 1fr));
  gap: 12px;
  margin-top: 12px;
}
.test,
.add {
  display: grid;
  gap: 8px;
}
.results {
  margin-top: 12px;
}
.summary {
  font-weight: 600;
  margin: 0 0 8px;
}
.row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 0;
  border-top: 1px solid var(--line);
  font-size: 13px;
}
@media (max-width: 640px) {
  .db {
    grid-template-columns: 1fr;
  }
  .actions {
    justify-content: flex-start;
  }
}
</style>
