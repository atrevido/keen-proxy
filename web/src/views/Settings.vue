<script setup lang="ts">
import { computed } from 'vue'
import DNSSettings from '../components/DNSSettings.vue'
import YAMLSettings from '../components/YAMLSettings.vue'
import { useI18n } from 'vue-i18n'
import { NButton, NCard, NForm, NFormItem, NInput, NInputNumber, NRadioGroup, NRadio, NDynamicInput, NAlert, NSelect } from 'naive-ui'
import { Settings, Globe, Shield, Network, Server, MapPin, Unlock, Languages } from '@lucide/vue'
import { session, store } from '../store'
import { LOCALES, locale, setLocale, type Locale } from '../i18n'

const { t } = useI18n()
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
</script>

<template>
  <div>
    <div class="page-head">
      <div>
        <h1><Settings :size="22" /> {{ t('nav.settings') }}</h1>
        <p>{{ t('settings.intro') }}</p>
      </div>
    </div>

    <div class="cols">
      <NCard size="small"><template #header>YAML</template><YAMLSettings /></NCard>
      <NCard size="small">
        <template #header><span class="hdr"><Globe :size="16" /> DNS</span></template>
        <DNSSettings />
      </NCard>

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
        <NForm label-placement="left" label-width="160" style="margin-top: 12px">
          <NFormItem :label="t('settings.tcpPort')"><NInputNumber v-model:value="cfg.intercept.tcp_port" :min="1" :max="65535" /></NFormItem>
          <NFormItem :label="t('settings.udpPort')"><NInputNumber v-model:value="cfg.intercept.udp_port" :min="1" :max="65535" /></NFormItem>
          <NFormItem :label="t('settings.routeTable')"><NInputNumber v-model:value="cfg.intercept.route_table" :min="1" :max="252" /></NFormItem>
        </NForm>
      </NCard>

      <NCard size="small">
        <template #header><span class="hdr"><Network :size="16" /> {{ t('settings.routerNet') }}</span></template>
        <NForm label-placement="left" label-width="160">
          <NFormItem :label="t('settings.lan')"><NInput v-model:value="cfg.lan.interface" class="mono" placeholder="br0" /></NFormItem>
          <NFormItem :label="t('settings.wan')"><NInput v-model:value="cfg.lan.wan" class="mono" placeholder="eth3" /></NFormItem>
        </NForm>
      </NCard>

      <NCard size="small">
        <template #header><span class="hdr"><MapPin :size="16" /> {{ t('settings.geo') }}</span></template>
        <div class="kv">
          <span class="muted">{{ t('settings.state') }}</span>
          <span :class="['pill', geo?.loaded ? 'ok' : 'warn']">{{ geo?.loaded ? t('settings.geoActive', { built: geo.built, n: geo.active ?? 1 }) : t('settings.geoMissing') }}</span>
        </div>
        <p v-if="geo?.error" class="hint bad">{{ geo.error }}</p>
        <p class="hint">{{ t('settings.geoHint') }}</p>
        <router-link to="/lists/geoip"><NButton size="small">{{ t('settings.geoManage') }}</NButton></router-link>
      </NCard>

      <NCard size="small">
        <template #header><span class="hdr"><Server :size="16" /> {{ t('settings.panel') }}</span></template>
        <NForm label-placement="left" label-width="160">
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

      <NCard size="small">
        <template #header><span class="hdr"><Languages :size="16" /> {{ t('settings.language') }}</span></template>
        <NSelect :value="locale" :options="langOptions" @update:value="(v: Locale) => setLocale(v)" />
        <p class="hint" style="margin-top: 10px">{{ t('settings.languageHint') }}</p>
      </NCard>
    </div>
  </div>
</template>

<style scoped>
.cols {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(360px, 100%), 1fr));
  gap: 12px;
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
