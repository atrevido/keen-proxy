<script setup lang="ts">
import {ref} from 'vue'
import {useI18n} from 'vue-i18n'
import {NAlert,NButton,NInput} from 'naive-ui'
import {api} from '../api'
import {store} from '../store'
import type {Config} from '../types'
const {t}=useI18n();const text=ref(''),preview=ref<Config>(),error=ref(''),file=ref<HTMLInputElement>()
async function read(event:Event){const f=(event.target as HTMLInputElement).files?.[0];if(!f)return;if(f.size>1024*1024){error.value=t('features.tooLarge');return}text.value=await f.text();preview.value=undefined}
// Parse and validate on the server into a preview without replacing the active draft.
async function validate(){error.value='';preview.value=undefined;try{preview.value=await api.importYAML(text.value)}catch(e){error.value=(e as Error).message}}
async function previous(){error.value='';try{preview.value=await api.previousConfig()}catch(e){error.value=(e as Error).message}}
// Adopt the preview as the shared draft; persistence still requires Apply.
function accept(){if(preview.value){store.setDraft(preview.value);preview.value=undefined;text.value=''}}
</script>
<template>
 <NAlert type="warning">{{t('features.yamlSecrets')}}</NAlert><p>{{t('features.yamlHint')}}</p>
 <div class="actions"><a href="/api/config/yaml" download="keenproxy-backup.yaml">{{t('features.exportYAML')}}</a><a href="/api/config/yaml?redacted=1" download="keenproxy-support-redacted.yaml">{{t('features.exportSupport')}}</a><NButton @click="previous">{{t('features.previous')}}</NButton><NButton @click="file?.click()">{{t('features.importFile')}}</NButton><input ref="file" type="file" accept=".yaml,.yml" style="display:none" @change="read"/></div>
 <NInput v-model:value="text" type="textarea" :placeholder="t('features.pasteYAML')" :rows="5" @update:value="preview=undefined"/>
 <div class="actions"><NButton :disabled="!text.trim()" @click="validate">{{t('features.validate')}}</NButton><NButton v-if="preview" type="primary" @click="accept">{{t('features.toDraft')}}</NButton></div>
 <NAlert v-if="preview" type="info">{{t('features.yamlPreview',{groups:preview.groups?.length??0,connections:preview.connections?.length??0})}}</NAlert><NAlert v-if="error" type="error">{{error}}</NAlert>
</template>
<style scoped>.actions{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:12px 0}a{color:var(--accent)}</style>
