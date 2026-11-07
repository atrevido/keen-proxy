<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {NAlert,NButton,NCheckbox,NDynamicInput,NForm,NFormItem,NInput,NSelect} from 'naive-ui'
import {api} from '../api'
import {store} from '../store'
import type {DNSProfile,DNSCheck,FeatureStatus} from '../types'
const {t}=useI18n();const cfg=computed(()=>store.state.config!);const profiles=ref<DNSProfile[]>([]),profile=ref<string|null>(null),transport=ref('doh'),result=ref<DNSCheck>(),status=ref<FeatureStatus>(),busy=ref(false),error=ref('')
const selected=computed(()=>profiles.value.find(p=>p.id===profile.value))
const conns=computed(()=>(cfg.value.connections??[]).map(c=>({value:c.id,label:c.name||c.id})))
const routes=computed(()=>[{label:t('features.dnsLegacy'),value:''},{label:t('features.dnsDirect'),value:'direct'},{label:t('features.dnsTunnel'),value:'connection'}])
const failures=computed(()=>[{label:t('features.dnsStrict'),value:'strict'},{label:t('features.dnsFallback'),value:'direct'}])
const bootstrapHost=ref(''),bootstrapIP=ref('')
function addBootstrap(){const host=bootstrapHost.value.trim(),ip=bootstrapIP.value.trim();if(host&&ip){cfg.value.dns.bootstrap={...(cfg.value.dns.bootstrap??{}),[host]:ip};bootstrapHost.value='';bootstrapIP.value=''}}
function removeBootstrap(host:string){const b={...(cfg.value.dns.bootstrap??{})};delete b[host];cfg.value.dns.bootstrap=b}

// Stage the selected resolver and bootstrap address together in the shared draft.
function choose(){const p=selected.value;if(!p)return;const endpoint=transport.value==='dot'?p.dot:transport.value==='udp'?p.ip:p.doh;cfg.value.dns.upstreams=[endpoint];const host=new URL(p.doh).hostname;cfg.value.dns.bootstrap={...(cfg.value.dns.bootstrap??{}),[host]:p.ip};if(!cfg.value.dns.route)cfg.value.dns.route='direct';cfg.value.dns.fallback='';result.value=undefined}
// Keep route-dependent fields consistent and reset the previous check result.
function changeRoute(v:''|'direct'|'connection'){cfg.value.dns.route=v;cfg.value.dns.connection=v==='connection'?(cfg.value.dns.connection||conns.value[0]?.value):undefined;cfg.value.dns.failure=v==='connection'?'strict':undefined;if(v)cfg.value.dns.fallback='';result.value=undefined}
// Probe the draft DNS configuration without applying it.
async function check(){busy.value=true;error.value='';try{result.value=await api.checkDNS(cfg.value);status.value=await api.features()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
onMounted(async()=>{try{profiles.value=(await api.catalog()).dns;status.value=await api.features()}catch(e){error.value=(e as Error).message}})
</script>
<template>
 <NForm label-placement="top">
 <NFormItem :label="t('features.publicDNS')"><NSelect v-model:value="profile" :options="profiles.map(p=>({value:p.id,label:p.name+' · '+t('features.filterKinds.'+p.filtering)}))"/></NFormItem>
 <div class="actions"><NSelect v-model:value="transport" :options="[{label:'DoH',value:'doh'},{label:'DoT',value:'dot'},{label:'UDP',value:'udp'}]" style="width:120px"/><NButton :disabled="!selected" @click="choose">{{t('features.useProfile')}}</NButton><a v-if="selected" :href="selected.source" target="_blank" rel="noopener">{{t('features.source')}}</a></div>
 <NFormItem :label="t('settings.upstreams')"><NDynamicInput v-model:value="cfg.dns.upstreams" :min="0" placeholder="https://dns.google/dns-query"/></NFormItem>
 <NFormItem :label="t('features.dnsRoute')"><NSelect :value="cfg.dns.route??''" :options="routes" @update:value="changeRoute"/></NFormItem>
 <NAlert type="info" :show-icon="false">{{cfg.dns.route==='connection'?t('features.tunnelExplain'):cfg.dns.route==='direct'?t('features.directExplain'):t('features.legacyExplain')}}</NAlert>
 <template v-if="cfg.dns.route==='connection'"><NFormItem :label="t('features.connection')"><NSelect v-model:value="cfg.dns.connection" :options="conns"/></NFormItem><NFormItem :label="t('features.onFailure')"><NSelect v-model:value="cfg.dns.failure" :options="failures"/></NFormItem><NAlert :type="cfg.dns.failure==='direct'?'warning':'info'">{{cfg.dns.failure==='direct'?t('features.fallbackExplain'):t('features.strictExplain')}}</NAlert></template>
 <NFormItem v-if="!cfg.dns.route" :label="t('settings.fallbackDNS')"><NInput v-model:value="cfg.dns.fallback" placeholder="127.0.0.1:53"/></NFormItem>
 <details><summary>{{t('features.bootstrap')}}</summary><p class="hint">{{t('features.bootstrapHint')}}</p><div v-for="(ip,host) in cfg.dns.bootstrap" :key="host" class="actions"><span class="mono">{{host}} → {{ip}}</span><NButton size="tiny" @click="removeBootstrap(String(host))">{{t('common.delete')}}</NButton></div><div class="actions"><NInput v-model:value="bootstrapHost" placeholder="dns.google"/><NInput v-model:value="bootstrapIP" placeholder="8.8.8.8"/><NButton @click="addBootstrap">{{t('common.add')}}</NButton></div></details>
 <NFormItem :label="t('settings.dnsListen')"><NInput v-model:value="cfg.dns.listen"/></NFormItem>
 <NCheckbox v-model:checked="cfg.dns.block_encrypted">{{t('features.block853')}}</NCheckbox><NCheckbox v-model:checked="cfg.dns.firefox_canary">{{t('features.canary')}}</NCheckbox>
 <p class="hint">{{t('features.leakLimits')}}</p>
 <div class="actions"><NButton class="dns-check" :loading="busy" @click="check">{{t('features.checkDNS')}}</NButton><span v-if="status?.dns.route">{{t('features.lastRoute')}}: {{status.dns.route}}</span></div>
 <NAlert v-if="result" :type="result.error?'warning':'success'">{{result.demo?t('features.demo'):`${result.route} · ${result.latency_ms} ms · ${result.error||'NOERROR'}`}}</NAlert><NAlert v-if="error" type="error">{{error}}</NAlert>
 </NForm>
</template>
<style scoped>
.actions{display:flex;align-items:center;flex-wrap:wrap;gap:10px;margin-bottom:14px}.n-checkbox{display:block;margin:10px 0}.hint{font-size:12px;color:var(--muted);margin:10px 0}summary{cursor:pointer;margin:12px 0}a{color:var(--accent)}
.dns-check {
  max-width: 100%;
  height: auto;
  min-height: var(--n-height);
  padding-block: 6px;
}
.dns-check :deep(.n-button__content) {
  white-space: normal;
}
</style>
