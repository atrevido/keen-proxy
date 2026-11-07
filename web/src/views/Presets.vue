<script setup lang="ts">
import {computed,onMounted,ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {NAlert,NButton,NCard,NInput,NSelect,NModal,NCheckbox,useMessage} from 'naive-ui'
import {api} from '../api'
import {store,targets} from '../store'
import type {Config,Preset} from '../types'
const {t}=useI18n();const message=useMessage()
const catalog=ref<Preset[]>([]),q=ref(''),category=ref(''),target=ref('direct'),error=ref(''),busy=ref(false)
const preview=ref<{config:Config;diff:{added:string[];removed:string[]}}|null>(null)
const categories=computed(()=>[{label:t('features.all'),value:''},...Array.from(new Set(catalog.value.map(p=>p.category))).map(c=>({label:t('features.categories.'+c),value:c}))])
const rows=computed(()=>catalog.value.filter(p=>(!category.value||p.category===category.value)&&(!q.value||`${p.name} ${p.description} ${p.domains.join(' ')}`.toLowerCase().includes(q.value.toLowerCase()))))
function installed(p:Preset){return store.state.config?.groups?.find(g=>g.preset?.id===p.id)}
// Preview a preset against the current draft without writing configuration.
async function prepare(p:Preset){if(!store.state.config)return;busy.value=true;error.value='';try{const i=store.state.config.groups?.findIndex(g=>g.preset?.id===p.id)??-1;preview.value=await api.previewPreset(store.state.config,p.id,target.value,i<0?undefined:i)}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
// Adopt the preview into the shared draft; Apply performs persistence.
function accept(){if(preview.value){store.setDraft(preview.value.config);preview.value=null;message.success(t('features.draft'))}}
function remove(p:Preset){const c=store.state.config;if(c)c.groups=(c.groups??[]).filter(g=>g.preset?.id!==p.id)}
onMounted(async()=>{try{catalog.value=(await api.catalog()).presets}catch(e){error.value=(e as Error).message}})
</script>
<template>
 <div class="page-head"><div><h1>{{t('nav.presets')}}</h1><p>{{t('features.presetsIntro')}}</p></div></div>
 <NAlert type="info" style="margin-bottom:12px">{{t('features.presetSource')}}</NAlert>
 <NAlert v-if="error" type="error" closable @close="error=''">{{error}}</NAlert>
 <div class="tools"><NInput v-model:value="q" :placeholder="t('features.search')" clearable/><NSelect v-model:value="category" :options="categories"/><NSelect v-model:value="target" :options="targets" :placeholder="t('features.target')"/></div>
 <div class="grid">
 <NCard v-for="p in rows" :key="p.id" size="small" :title="p.name">
  <p>{{p.description}}</p><div class="muted">{{t('features.categories.'+p.category)}} · {{p.domains.length}} {{t('features.domains')}}</div>
  <details><summary>{{t('features.composition')}}</summary><pre>{{p.domains.join('\n')}}</pre></details>
  <template v-if="installed(p)">
   <div class="preset-options">
    <NCheckbox :checked="!installed(p)!.disabled" @update:checked="v=>{installed(p)!.disabled=!v}">{{t('features.enabled')}}</NCheckbox>
    <NCheckbox :checked="installed(p)!.preset!.auto_update" @update:checked="v=>{installed(p)!.preset!.auto_update=v}">{{t('features.presetAuto')}}</NCheckbox>
   </div>
   <NSelect v-model:value="installed(p)!.target" :options="targets" style="margin-top:8px"/>
  </template>
  <div class="actions"><NButton :loading="busy" @click="prepare(p)">{{installed(p)?t('features.previewUpdate'):t('features.install')}}</NButton><NButton v-if="installed(p)" @click="remove(p)">{{t('common.delete')}}</NButton></div>
 </NCard>
 </div>
 <NModal :show="!!preview" preset="card" :title="t('features.preview')" style="width:min(680px,94vw)" @update:show="v=>{if(!v)preview=null}">
  <p>{{t('features.previewHint')}}</p><div v-if="preview"><b>+{{preview.diff.added.length}} / −{{preview.diff.removed.length}}</b><pre>{{preview.diff.added.map(s=>'+ '+s).concat(preview.diff.removed.map(s=>'− '+s)).join('\n')||t('features.noChanges')}}</pre></div>
  <NButton type="primary" @click="accept">{{t('features.toDraft')}}</NButton>
 </NModal>
</template>
<style scoped>
.tools{display:grid;grid-template-columns:2fr 1fr 1fr;gap:10px;margin:12px 0}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(320px,100%),1fr));gap:12px}.actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:12px}p{margin:0 0 8px}pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:230px;overflow:auto;font-size:12px}summary{cursor:pointer;margin:8px 0}
.preset-options {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 8px;
  margin-top: 8px;
}
@media (max-width: 600px) {
  .tools { grid-template-columns: 1fr; }
  .preset-options { gap: 0; }
  .preset-options > .n-checkbox {
    box-sizing: border-box;
    min-height: 44px;
    padding: 10px 0;
  }
}
</style>
