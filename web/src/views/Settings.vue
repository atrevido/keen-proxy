<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import DNSSettings from '../components/DNSSettings.vue'
import YAMLSettings from '../components/YAMLSettings.vue'
import UpdateSettings from '../components/UpdateSettings.vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NCard, NForm, NFormItem, NInput, NInputNumber, NRadioGroup, NRadio, NDynamicInput, NAlert, NSelect } from 'naive-ui'
import { Shield, Network, MapPin, Unlock } from '@lucide/vue'
import { session, store } from '../store'
import { LOCALES, locale, setLocale, type Locale } from '../i18n'
import { themePreference, type ThemePreference } from '../theme'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const sectionOptions = computed(() => [
  { value: 'dns', label: 'DNS' },
  { value: 'network', label: t('settings.networkTab') },
  { value: 'access', label: t('settings.accessTab') },
  { value: 'geoip', label: t('settings.geoManage') },
  { value: 'yaml', label: t('settings.yamlTab') },
  { value: 'updates', label: t('settings.updatesTab') },
  { value: 'appearance', label: t('settings.appearanceTab') },
])
const section = computed(() => sectionOptions.value.find(({ value }) => value === route.query.section)?.value ?? 'dns')
const visited = reactive(new Set<string>())
watch(section, (value) => visited.add(value), { immediate: true })

function selectSection(value: string) {
  void router.push({ query: { ...route.query, section: value } })
}
const geo = computed(() => store.state.status?.geoip)

const cfg = computed(() => store.state.config!)

// Trusted networks bypass token login; an empty list always requires a token.
const noToken = computed({
  get: () => cfg.value.api.no_token_from ?? [],
  set: (v: string[]) => {
    cfg.value.api.no_token_from = v.length ? v : undefined
  },
})
// Suggest the current address as a /24 home-LAN subnet.
const myNet = computed(() => {
  const m = session.ip.match(/^(\d+\.\d+\.\d+)\.\d+$/)
  return m ? `${m[1]}.0/24` : ''
})
const wide = computed(() => noToken.value.some((n) => /\/(\d|1[0-5])$/.test(n.trim())))
function addMyNet() {
  if (myNet.value && !noToken.value.includes(myNet.value)) noToken.value = [...noToken.value, myNet.value]
}
const trustedNet = computed(() => (session.trusted ? session.network : ''))
const langOptions = LOCALES.map((l) => ({ label: l.label, value: l.value }))
const themeOptions = computed(() => [
  { value: 'system', label: t('ui.systemTheme') },
  { value: 'dark', label: t('shell.darkTheme') },
  { value: 'light', label: t('shell.lightTheme') },
])
</script>

<template>
  <div class="settings-page">
    <div class="page-head">
      <h1>{{ t('nav.settings') }}</h1>
    </div>
    <nav class="settings-tabs" :aria-label="t('settings.sectionPicker')">
      <router-link
        v-for="option in sectionOptions"
        :key="option.value"
        :to="{ query: { ...route.query, section: option.value } }"
        :aria-current="section === option.value ? 'page' : undefined"
      >{{ option.label }}</router-link>
    </nav>
    <select
      class="settings-section-picker"
      :aria-label="t('settings.sectionPicker')"
      :value="section"
      @change="selectSection(($event.target as HTMLSelectElement).value)"
    >
      <option v-for="option in sectionOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
    </select>
    <div class="settings-panel">
      <section v-if="visited.has('dns')" v-show="section === 'dns'">
        <DNSSettings />
      </section>
      <section v-if="visited.has('network')" v-show="section === 'network'">
        <div class="settings-fields">
          <NCard size="small">
            <template #header><span class="hdr"><Shield :size="16" /> {{ t('settings.intercept') }}</span></template>
            <NRadioGroup v-model:value="cfg.intercept.mode" class="modes">
              <NRadio value="redirect">
                <b>{{ t('monitor.redirect') }}</b>
                <div class="hint">{{ t('settings.redirectHint') }}</div>
              </NRadio>
              <NRadio value="tproxy">
                <b>{{ t('monitor.tproxy') }}</b>
                <div class="hint">{{ t('settings.tproxyHint') }}</div>
              </NRadio>
            </NRadioGroup>
            <NAlert type="info" :show-icon="false" style="margin-top: 12px">{{ t('settings.interceptNote') }}</NAlert>
            <NForm label-placement="top" style="margin-top: 12px">
              <NFormItem :label="t('settings.tcpPort')"><NInputNumber v-model:value="cfg.intercept.tcp_port" :min="1" :max="65535" /></NFormItem>
              <NFormItem :label="t('settings.udpPort')"><NInputNumber v-model:value="cfg.intercept.udp_port" :min="1" :max="65535" /></NFormItem>
              <NFormItem :label="t('settings.routeTable')"><NInputNumber v-model:value="cfg.intercept.route_table" :min="1" :max="252" /></NFormItem>
            </NForm>
          </NCard>

          <NCard size="small">
            <template #header><span class="hdr"><Network :size="16" /> {{ t('settings.routerNet') }}</span></template>
            <NForm label-placement="top">
              <NFormItem :label="t('settings.lan')"><NInput v-model:value="cfg.lan.interface" class="mono" placeholder="br0" /></NFormItem>
              <NFormItem :label="t('settings.wan')"><NInput v-model:value="cfg.lan.wan" class="mono" placeholder="eth3" /></NFormItem>
            </NForm>
          </NCard>

        </div>
      </section>
      <section v-if="visited.has('access')" v-show="section === 'access'">
        <NCard class="settings-fields" size="small" :title="t('settings.panel')">
          <NForm label-placement="top">
            <NFormItem :label="t('settings.panelListen')"><NInput v-model:value="cfg.api.listen" class="mono" /></NFormItem>
            <NFormItem :label="t('settings.tokenFile')"><NInput v-model:value="cfg.api.token_file" class="mono" /></NFormItem>
          </NForm>
          <p class="hint">{{ t('settings.panelHint') }}</p>

          <div class="sub"><Unlock :size="14" /> {{ t('settings.noToken') }}</div>
          <p class="hint">{{ t('settings.noTokenHint') }}</p>
          <NDynamicInput v-model:value="noToken" placeholder="192.168.1.0/24" :min="0">
            <template #create-button-default>{{ t('settings.addNetwork') }}</template>
          </NDynamicInput>
          <div class="row">
            <NButton v-if="myNet && !noToken.includes(myNet)" size="small" @click="addMyNet">{{ t('settings.addMyNet', { net: myNet }) }}</NButton>
            <span v-if="trustedNet" class="pill ok">{{ t('settings.youTrusted', { net: trustedNet }) }}</span>
          </div>
          <NAlert v-if="wide" type="warning" :show-icon="false" style="margin-top: 10px">{{ t('settings.wideWarn') }}</NAlert>
        </NCard>

      </section>
      <section v-if="visited.has('geoip')" v-show="section === 'geoip'">
        <NCard class="settings-fields" size="small">
          <template #header><span class="hdr"><MapPin :size="16" /> {{ t('settings.geo') }}</span></template>
          <div class="kv">
            <span class="muted">{{ t('settings.state') }}</span>
            <span :class="['pill', geo?.loaded ? 'ok' : 'warn']">{{ geo?.loaded ? t('settings.geoActive', { built: geo.built, n: geo.active ?? 1 }) : t('settings.geoMissing') }}</span>
          </div>
          <p v-if="geo?.error" class="hint bad">{{ geo.error }}</p>
          <p class="hint">{{ t('settings.geoHint') }}</p>
          <router-link to="/lists/geoip"><NButton size="small">{{ t('settings.geoManage') }}</NButton></router-link>
        </NCard>

      </section>
      <section v-if="visited.has('yaml')" v-show="section === 'yaml'">
        <div class="settings-fields"><YAMLSettings /></div>
      </section>
      <section v-if="visited.has('updates')" v-show="section === 'updates'">
        <div class="settings-fields"><UpdateSettings /></div>
      </section>
      <section v-if="visited.has('appearance')" v-show="section === 'appearance'">
        <NForm class="settings-fields" label-placement="top">
          <NFormItem :label="t('settings.language')">
            <NSelect :value="locale" :options="langOptions" @update:value="(v: Locale) => setLocale(v)" />
          </NFormItem>
          <p class="hint">{{ t('settings.languageHint') }}</p>
          <NFormItem :label="t('settings.theme')">
            <NSelect :value="themePreference" :options="themeOptions" @update:value="(v: ThemePreference) => themePreference = v" />
          </NFormItem>
        </NForm>
      </section>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  max-width: 1320px;
  margin: 0 auto;
}
.settings-fields {
  display: grid;
  gap: 12px;
  max-width: 760px;
}
.settings-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 0 22px;
  border-bottom: 1px solid var(--line);
}
.settings-tabs a {
  padding: 12px 6px;
  color: var(--text);
  text-decoration: none;
  border-bottom: 2px solid transparent;
}
.settings-tabs a:hover, .settings-tabs a[aria-current='page'] { color: var(--accent); }
.settings-tabs a[aria-current='page'] { border-bottom-color: var(--accent); }
.settings-tabs a:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.settings-panel { padding-top: 28px; }
.settings-section-picker { display: none; }
@media (max-width: 820px) {
  .settings-tabs { display: none; }
  .settings-section-picker {
    display: block;
    width: 100%;
    min-height: 40px;
    padding: 8px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--card);
    color: var(--text);
    font: inherit;
  }
  .settings-panel { padding-top: 20px; }
}
.hdr {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.hint {
  font-size: 12px;
  color: var(--muted);
  margin: 0 0 12px;
  font-weight: normal;
}
.kv {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 10px;
}
.bad {
  color: var(--bad);
}
.hint a {
  color: var(--accent);
}
.sub {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 13px;
  margin: 14px 0 6px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
}
.row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-top: 10px;
}
.modes {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
