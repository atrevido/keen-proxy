<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { NAlert, NButton, NCheckbox, NDynamicInput, NForm, NFormItem, NInput, NSelect } from 'naive-ui'
import { api } from '../api'
import { store } from '../store'
import { selectDNSResolver } from '../dnsSettings'
import type { DNSProfile, DNSCheck, FeatureStatus, DNSConfig } from '../types'
const { t } = useI18n()
const cfg = computed(() => store.state.config!)
const dns = computed(() => cfg.value.dns)
const invalidPair = store.invalidDNSPair
const profiles = ref<DNSProfile[]>([])
const result = ref<DNSCheck>()
const status = ref<FeatureStatus>()
const busy = ref(false)
const error = ref('')
const slots = [
  { index: 0, label: 'settings.primaryDNS', hint: 'settings.primaryDNSHint' },
  { index: 1, label: 'settings.secondaryDNS', hint: 'settings.secondaryDNSHint' },
] as const
const customResolver = 'custom'
const customSlots = ref(new Set<number>())
const resolverOptions = computed(() => profiles.value.flatMap((profile) => [
  { label: profile.name + ' · DoH', value: profile.doh, bootstrapIP: profile.bootstrap_ip ?? profile.ip, filtering: profile.filtering, source: profile.source },
  { label: profile.name + ' · DoT', value: profile.dot, bootstrapIP: profile.bootstrap_ip ?? profile.ip, filtering: profile.filtering, source: profile.source },
  { label: profile.name + ' · UDP', value: profile.ip, bootstrapIP: undefined, filtering: profile.filtering, source: profile.source },
]))
const additional = computed({
  get: () => dns.value.upstreams.slice(2),
  set: (servers: string[]) => { dns.value.upstreams = [...dns.value.upstreams.slice(0, 2), ...servers] },
})
const choices = computed(() => [
  { label: t('settings.customDNS'), value: customResolver },
  ...resolverOptions.value.map((option) => ({
    label: option.label + ' · ' + t('features.filterKinds.' + option.filtering),
    value: option.value,
  })),
])
const selectedResolvers = computed(() => slots.map(({ index }) =>
  selectedResolversFor(dns.value.upstreams[index] ?? ''),
))

function resolverSelection(slot: 0 | 1) {
  const endpoint = dns.value.upstreams[slot]
  if (customSlots.value.has(slot) || (endpoint && !selectedResolvers.value[slot])) return customResolver
  return endpoint || null
}

function choose(slot: 0 | 1, value: string) {
  if (value === customResolver) {
    customSlots.value.add(slot)
    return
  }
  customSlots.value.delete(slot)
  selectDNSResolver(dns.value, slot, value, selectedResolversFor(value)?.bootstrapIP)
}

function selectedResolversFor(endpoint: string) {
  return resolverOptions.value.find(({ value }) => value === endpoint)
}

watch(cfg, () => customSlots.value.clear())
watch(dns, () => { result.value = undefined }, { deep: true })
const conns = computed(() => (cfg.value.connections ?? []).map((c) => ({ value: c.id, label: c.name || c.id })))
const routes = computed(() => [
  { label: t('features.dnsLegacy'), value: '' },
  { label: t('features.dnsDirect'), value: 'direct' },
  { label: t('features.dnsTunnel'), value: 'connection' },
])
const failures = computed(() => [
  { label: t('features.dnsStrict'), value: 'strict' },
  { label: t('features.dnsFallback'), value: 'direct' },
])
const routeHint = computed(() => ({
  connection: 'features.tunnelExplain',
  direct: 'features.directExplain',
  '': 'features.legacyExplain',
})[dns.value.route ?? ''])
const bootstrapHost = ref('')
const bootstrapIP = ref('')

function addBootstrap() {
  const host = bootstrapHost.value.trim()
  const ip = bootstrapIP.value.trim()
  if (!host || !ip) return
  dns.value.bootstrap = { ...dns.value.bootstrap, [host]: ip }
  bootstrapHost.value = ''
  bootstrapIP.value = ''
}

function removeBootstrap(host: string) {
  const bootstrap = { ...dns.value.bootstrap }
  delete bootstrap[host]
  dns.value.bootstrap = bootstrap
}

// Reset route-specific settings when switching policies.
function changeRoute(route: DNSConfig['route']) {
  dns.value.route = route
  dns.value.connection = route === 'connection' ? dns.value.connection || conns.value[0]?.value : undefined
  dns.value.failure = route === 'connection' ? 'strict' : undefined
  if (route) dns.value.fallback = ''
}

// Probe the draft without applying it and ignore results from a superseded draft.
async function check() {
  const snapshot = JSON.stringify(cfg.value)
  busy.value = true
  error.value = ''
  try {
    const checked = await api.checkDNS(JSON.parse(snapshot))
    status.value = await api.features()
    if (snapshot === JSON.stringify(cfg.value)) result.value = checked
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  try {
    profiles.value = (await api.catalog()).dns
    status.value = await api.features()
  } catch (err) {
    error.value = (err as Error).message
  }
})
</script>
<template>
  <NForm label-placement="top">
    <section class="dns-section">
      <h2>{{ t('settings.dnsServers') }}</h2>
      <div class="resolver-pair">
        <div v-for="field in slots" :key="field.index" class="resolver-field">
          <div class="resolver-label">
            <span class="resolver-order" aria-hidden="true">0{{ field.index + 1 }}</span>
            {{ t(field.label) }} <span class="resolver-required" aria-hidden="true">*</span>
          </div>
          <NSelect
            :value="resolverSelection(field.index)"
            :options="choices"
            :input-props="{ 'aria-label': t(field.label), 'aria-required': true }"
            :placeholder="t('settings.dnsChoose')"
            filterable
            @update:value="choose(field.index, $event)"
          />
          <p class="hint">{{ t(field.hint) }}</p>
          <NInput
            v-if="resolverSelection(field.index) === customResolver"
            class="custom-resolver"
            :value="dns.upstreams[field.index] ?? ''"
            :input-props="{ 'aria-label': t(field.label) + ' · ' + t('settings.customDNS') }"
            placeholder="https://dns.example.com/dns-query"
            @update:value="selectDNSResolver(dns, field.index, $event.trim())"
          />
        </div>
      </div>
      <p class="hint">{{ t('settings.dnsCustomHint') }}</p>
      <NAlert v-if="invalidPair" type="warning" role="alert">{{ t('settings.dnsPairRequired') }}</NAlert>
    </section>
    <section class="dns-section">
      <h2>{{ t('settings.dnsRouteTitle') }}</h2>
      <NFormItem :show-label="false">
        <NSelect :input-props="{ 'aria-label': t('features.dnsRoute') }" :value="dns.route ?? ''" :options="routes" @update:value="changeRoute" />
      </NFormItem>
      <p class="hint">{{ t(routeHint) }}</p>
      <template v-if="dns.route === 'connection'">
        <NFormItem :label="t('features.connection')"><NSelect v-model:value="dns.connection" :options="conns" /></NFormItem>
        <NFormItem :label="t('features.onFailure')"><NSelect v-model:value="dns.failure" :options="failures" /></NFormItem>
        <NAlert :type="dns.failure === 'direct' ? 'warning' : 'info'">{{ dns.failure === 'direct' ? t('features.fallbackExplain') : t('features.strictExplain') }}</NAlert>
      </template>
    </section>
    <details class="dns-section">
      <summary>{{ t('settings.dnsAdvanced') }}</summary>
      <div class="advanced-fields">
        <NFormItem v-if="dns.upstreams[0] && dns.upstreams[1]" :label="t('settings.additionalDNS')">
          <NDynamicInput v-model:value="additional" :min="0" placeholder="https://dns.google/dns-query" />
        </NFormItem>
        <NFormItem v-if="!dns.route" :label="t('settings.fallbackDNS')">
          <NInput v-model:value="dns.fallback" placeholder="127.0.0.1:53" />
        </NFormItem>
        <NFormItem :label="t('settings.dnsListen')"><NInput v-model:value="dns.listen" /></NFormItem>
        <ul v-if="selectedResolvers.some(Boolean)" class="resolver-sources">
          <template v-for="(resolver, index) in selectedResolvers" :key="index">
            <li v-if="resolver">
              <a :href="resolver.source" target="_blank" rel="noreferrer">{{ resolver.label }} · {{ t('features.source') }}</a>
            </li>
          </template>
        </ul>
        <h3>{{ t('features.bootstrap') }}</h3>
        <p class="hint">{{ t('features.bootstrapHint') }}</p>
        <div v-for="(ip, host) in dns.bootstrap" :key="host" class="actions">
          <span class="mono">{{ host }} → {{ ip }}</span>
          <NButton size="tiny" @click="removeBootstrap(String(host))">{{ t('common.delete') }}</NButton>
        </div>
        <div class="actions bootstrap-inputs">
          <NInput v-model:value="bootstrapHost" :input-props="{ 'aria-label': t('settings.bootstrapHost') }" placeholder="dns.google" />
          <NInput v-model:value="bootstrapIP" :input-props="{ 'aria-label': t('features.bootstrap') }" placeholder="8.8.8.8" />
          <NButton @click="addBootstrap">{{ t('common.add') }}</NButton>
        </div>
        <NCheckbox v-model:checked="dns.block_encrypted">{{ t('features.block853') }}</NCheckbox>
        <NCheckbox v-model:checked="dns.firefox_canary">{{ t('features.canary') }}</NCheckbox>
        <p class="hint">{{ t('features.leakLimits') }}</p>
      </div>
    </details>
    <div class="dns-check actions">
      <NButton :loading="busy" :disabled="invalidPair" @click="check">{{ t('settings.dnsCheck') }}</NButton>
      <span v-if="status?.dns.route">{{ t('features.lastRoute') }}: {{ status.dns.route }}</span>
    </div>
    <NAlert v-if="result" :type="result.error ? 'warning' : 'success'">{{ result.demo ? t('features.demo') : `${result.route} · ${result.latency_ms} ms · ${result.error || 'NOERROR'}` }}</NAlert>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
  </NForm>
</template>
<style scoped>
.dns-section {
  padding: 24px 0;
  border-bottom: 1px solid var(--line);
}
.dns-section:first-child { padding-top: 0; }
.dns-section h2 { margin: 0 0 20px; font-size: 24px; }
.resolver-pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 28px;
}
.resolver-field { width: 100%; min-width: 0; }
.resolver-label { margin-bottom: 8px; font-weight: 600; }
.resolver-required { color: var(--accent); }
.custom-resolver { margin-top: 8px; }
.resolver-sources a { color: var(--accent); }
.resolver-order {
  margin-right: 8px;
  padding: 2px 6px;
  border: 1px solid var(--line);
  border-radius: 5px;
  color: var(--muted);
  font: 12px monospace;
}
.hint { margin: 8px 0; color: var(--muted); font-size: 13px; }
.advanced-fields { margin-top: 18px; max-width: 760px; }
.advanced-fields .n-checkbox { display: block; margin: 12px 0; }
summary { cursor: pointer; font-weight: 600; }
.actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.dns-check { margin: 24px 0; }
.bootstrap-inputs .n-input { flex: 1 1 140px; }
.advanced-fields .actions { margin: 12px 0; }
.advanced-fields .mono { overflow-wrap: anywhere; }
@media (max-width: 640px) {
  .resolver-pair { grid-template-columns: 1fr; gap: 0; }
  .dns-section { padding: 18px 0; }
}
</style>
