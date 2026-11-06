<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {NAlert,NButton,NCard,NCheckbox,NDynamicInput,NInput,useMessage} from 'naive-ui'
import {api} from '../api'
import {store} from '../store'
import type {FilterCatalog,FeatureStatus} from '../types'
const {t}=useI18n(),message=useMessage();const catalog=ref<FilterCatalog[]>([]),status=ref<FeatureStatus>(),error=ref(''),busy=ref(false),name=ref(''),url=ref('')
const filters=computed(()=>store.state.config!.filters!)
function add(p:FilterCatalog){if(!filters.value.sources.some(s=>s.id===p.id))filters.value.sources.push({id:p.id,name:p.name,url:p.url,enabled:true,auto_update:false})}
// Accept HTTPS sources only and assign a fresh local draft identifier.
function addCustom(){try{const u=new URL(url.value);if(u.protocol!=='https:')throw new Error('HTTPS');filters.value.sources.push({id:'custom-'+Array.from(crypto.getRandomValues(new Uint8Array(12)),b=>b.toString(16).padStart(2,'0')).join(''),name:name.value||u.hostname,url:u.href,enabled:true,auto_update:false});name.value='';url.value=''}catch{message.error(t('features.httpsOnly'))}}
// Refresh applied filter data and then reload its runtime status.
async function refresh(){busy.value=true;error.value='';try{const r=await api.refreshFilters();status.value=await api.features();if((r as {demo?:boolean}).demo)message.info(t('features.demo'))}catch(e){error.value=(e as Error).message;status.value=await api.features().catch(()=>undefined)}finally{busy.value=false}}
function info(id:string){return status.value?.filters.find(s=>s.id===id)}
onMounted(async()=>{try{catalog.value=(await api.catalog()).filters;status.value=await api.features()}catch(e){error.value=(e as Error).message}})
</script>
<template>
 <div class="page-head"><div><h1>{{t('nav.filters')}}</h1><p>{{t('features.filtersIntro')}}</p></div><NButton :loading="busy" :disabled="store.dirty.value" @click="refresh">{{t('features.download')}}</NButton></div>
 <NAlert type="info">{{t('features.filtersHint')}}</NAlert><NAlert v-if="store.dirty.value" type="warning" style="margin-top:10px">{{t('features.applyBeforeDownload')}}</NAlert><NAlert v-if="status?.demo" type="info" style="margin-top:10px">{{t('features.demo')}}</NAlert><NAlert v-if="error" type="error">{{error}}</NAlert>
 <div class="grid">
 <NCard v-for="p in catalog" :key="p.id" :title="p.name" size="small"><p>{{t('features.filterKinds.'+p.category)}}</p><p><a :href="p.source" target="_blank" rel="noopener">{{t('features.source')}}</a> · {{p.license}}</p><NButton :disabled="filters.sources.some(s=>s.id===p.id)" @click="add(p)">{{t('features.install')}}</NButton></NCard>
 </div>
 <NCard size="small" :title="t('features.installed')" style="margin-top:12px">
 <div v-for="(s,i) in filters.sources" :key="s.id" class="source">
  <b>{{s.name}}</b><div class="muted url">{{s.url}}</div><div class="actions"><NCheckbox v-model:checked="s.enabled">{{t('features.enabled')}}</NCheckbox><NCheckbox v-model:checked="s.auto_update">{{t('features.daily')}}</NCheckbox><NButton size="small" @click="filters.sources.splice(i,1)">{{t('common.delete')}}</NButton></div>
  <p v-if="info(s.id)">{{info(s.id)!.rules}} {{t('features.rules')}} · {{(info(s.id)!.bytes/1048576).toFixed(1)}} MiB {{t('features.estimated')}} · {{info(s.id)!.skipped}} {{t('features.skipped')}}</p><p>{{info(s.id)?.updated && !info(s.id)!.updated.startsWith('0001') ? new Date(info(s.id)!.updated).toLocaleString():t('features.notDownloaded')}}</p><NAlert v-if="info(s.id)?.error" type="error">{{info(s.id)!.error}}</NAlert>
 </div><p v-if="!filters.sources.length" class="muted">{{t('features.emptyFilters')}}</p>
 </NCard>
 <NCard size="small" :title="t('features.custom')" style="margin-top:12px"><div class="custom"><NInput v-model:value="name" :placeholder="t('features.name')"/><NInput v-model:value="url" placeholder="https://…/domains.txt"/><NButton @click="addCustom">{{t('common.add')}}</NButton></div></NCard>
 <NCard size="small" :title="t('features.exceptions')" style="margin-top:12px"><p>{{t('features.exceptionsHint')}}</p><NDynamicInput v-model:value="filters.exceptions" placeholder="example.com"/></NCard>
</template>
<style scoped>.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(240px,100%),1fr));gap:12px;margin-top:12px}.actions{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:10px 0}.source{padding:12px 0;border-bottom:1px solid var(--line)}.url{overflow-wrap:anywhere;font-size:12px}.custom{display:flex;gap:10px;flex-wrap:wrap}.custom .n-input{flex:1;min-width:180px}a{color:var(--accent)}p{margin:6px 0}</style>
