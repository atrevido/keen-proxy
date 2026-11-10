<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NModal, NCard, NTabs, NTabPane, NInput, NButton, NForm, NFormItem, NAlert, NSpace, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { api } from '../api'
import { store } from '../store'
import type { Connection, LinkPreview, WireGuard } from '../types'
import { parseWireGuardConf, slugify, wireGuardToConf } from '../wgconf'

// Edit a local form copy so cancellation leaves the shared draft unchanged.
const props = defineProps<{ show: boolean; edit?: Connection | null }>()
const emit = defineEmits<{ (e: 'update:show', v: boolean): void }>()
const message = useMessage()
const { t } = useI18n()

const tab = ref<'link' | 'wireguard' | 'singbox' | 'amneziawg'>('link')
const name = ref('')
const id = ref('')
const idTouched = ref(false)
const link = ref('')
const wgText = ref('')
const awgText = ref('')
const checkingAWG = ref(false)
let awgReadSequence = 0
const sbText = ref('')
const preview = ref<LinkPreview | null>(null)
const previewError = ref('')
const error = ref('')
const revealed = ref(false)
const fallback = ref<string | null>(null)
const isEdit = computed(() => !!props.edit)

watch(() => props.show, (open) => {
  awgReadSequence++
  if (!open) return
  error.value = ''
  revealed.value = false
  preview.value = null
  previewError.value = ''
  const c = props.edit
  name.value = c?.name ?? ''
  id.value = c?.id ?? ''
  idTouched.value = !!c
  fallback.value = c?.fallback ?? null
  link.value = c?.link ?? c?.trusttunnel?.link ?? ''
  awgText.value = c?.amneziawg?.profile ?? ''
  wgText.value = c?.wireguard ? wireGuardToConf(c.wireguard) : ''
  sbText.value = c?.outbound || c?.endpoint
    ? JSON.stringify({ [c.outbound ? 'outbound' : 'endpoint']: c.outbound ?? c.endpoint }, null, 2)
    : ''
  tab.value = c?.type === 'wireguard' ? 'wireguard' : c?.type === 'singbox' ? 'singbox' : c?.type === 'amneziawg' ? 'amneziawg' : 'link'
  if (link.value) checkLink()
})

watch(name, (v) => {
  if (!idTouched.value) id.value = slugify(v)
})

let seq = 0
// Ignore older preview responses after a newer request has started.
async function checkLink() {
  const my = ++seq
  preview.value = null
  previewError.value = ''
  if (!link.value.trim()) return
  try {
    const p = await api.parseLink(link.value)
    if (my !== seq) return
    preview.value = p
    if (!name.value && p.name) name.value = p.name
  } catch (e) {
    if (my === seq) previewError.value = (e as Error).message
  }
}

function build(): Connection {
  const base: Pick<Connection, 'id' | 'name' | 'fallback'> = { id: id.value.trim(), name: name.value.trim() || id.value.trim() }
  if (fallback.value) base.fallback = fallback.value
  if (tab.value === 'link') {
    const l = link.value.trim()
    if (l.startsWith('tt://')) return { ...base, type: 'trusttunnel', trusttunnel: { ...props.edit?.trusttunnel, link: l } }
    return { ...base, type: 'link', link: l }
  }
  if (tab.value === 'amneziawg') {
    if (!awgText.value) throw new Error(t('awg.select'))
    return { ...base, type: 'amneziawg', amneziawg: { ...props.edit?.amneziawg, profile: awgText.value } }
  }
  if (tab.value === 'wireguard') {
    const wg: WireGuard = parseWireGuardConf(wgText.value)
    return { ...base, type: 'wireguard', wireguard: wg }
  }
  const obj = JSON.parse(sbText.value || '{}')
  if (obj.outbound) return { ...base, type: 'singbox', outbound: obj.outbound }
  if (obj.endpoint) return { ...base, type: 'singbox', endpoint: obj.endpoint }
  // Infer endpoint versus outbound for an unwrapped raw profile.
  const endpointTypes = ['wireguard', 'tailscale', 'openvpn', 'openconnect']
  return endpointTypes.includes(obj.type) ? { ...base, type: 'singbox', endpoint: obj } : { ...base, type: 'singbox', outbound: obj }
}

async function readAWGFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const seq = ++awgReadSequence
  try {
    if (file.size > 128 * 1024) throw new Error(t('ui.largeFile'))
    const result = await api.previewAmneziaWG(await file.text())
    if (seq === awgReadSequence) {
      awgText.value = result.profile
      error.value = ''
    }
  } catch (e) {
    if (seq === awgReadSequence) error.value = (e as Error).message
  }
}

async function checkAWG() {
  checkingAWG.value = true
  error.value = ''
  try {
    await api.previewAmneziaWG(awgText.value, true)
    if (props.show) message.success(t('awg.probeOK'))
  } catch (e) {
    if (props.show) error.value = (e as Error).message
  } finally {
    checkingAWG.value = false
  }
}

// Validate local identity constraints before replacing or appending the draft profile.
function save() {
  error.value = ''
  let conn: Connection
  try {
    conn = build()
  } catch (e) {
    error.value = (e as Error).message
    return
  }
  if (!/^[a-z0-9][a-z0-9_-]*$/.test(conn.id)) {
    error.value = t('dialog.errId')
    return
  }
  if (conn.id === 'direct' || conn.id === 'block') {
    error.value = t('dialog.errReserved')
    return
  }
  const list = store.state.config!.connections!
  const clash = list.find((c) => c.id === conn.id && c !== props.edit)
  if (clash) {
    error.value = t('dialog.errDup', { id: conn.id })
    return
  }
  if (props.edit) {
    const i = list.indexOf(props.edit)
    // Keep draft references consistent when the connection ID changes.
    if (props.edit.id !== conn.id) renameTarget(props.edit.id, conn.id)
    list.splice(i, 1, conn)
  } else {
    list.push(conn)
  }
  message.info(t('dialog.added'))
  emit('update:show', false)
}

// Rewrite routing, fallback, and DNS references within the shared draft.
function renameTarget(from: string, to: string) {
  const cfg = store.state.config!
  for (const g of cfg.groups ?? []) if (g.target === from) g.target = to
  for (const n of cfg.networks ?? []) if (n.target === from) n.target = to
  for (const c of cfg.clients ?? []) if (c.target === from) c.target = to
  for (const c of cfg.connections ?? []) if (c.fallback === from) c.fallback = to
  if (cfg.dns.connection === from) cfg.dns.connection = to
}
</script>

<template>
  <NModal :show="show" @update:show="emit('update:show', $event)">
    <NCard :title="isEdit ? t('dialog.editTitle') : t('conn.new')" style="width: min(720px, 96vw)" closable @close="emit('update:show', false)">
      <NForm label-placement="top">
        <NSpace :wrap="false" style="width: 100%">
          <NFormItem :label="t('dialog.name')" style="flex: 1">
            <NInput v-model:value="name" :placeholder="t('dialog.namePlaceholder')" />
          </NFormItem>
          <NFormItem :label="t('dialog.id')" style="width: 220px">
            <NInput v-model:value="id" placeholder="fi" @input="idTouched = true" />
          </NFormItem>
        </NSpace>
      </NForm>

      <NButton v-if="isEdit" block @click="revealed=!revealed">{{revealed?t('ui.hide'):t('ui.reveal')}}</NButton>
      <p v-if="isEdit&&!revealed" class="hint">{{t('ui.secret')}}</p>
      <NTabs v-if="!isEdit||revealed" v-model:value="tab" type="segment" animated>
        <NTabPane name="link" :tab="t('conn.link')">
          <NInput
            v-model:value="link"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 6 }"
            placeholder="vless://… vmess://… trojan://… ss://… hysteria2://… tuic://… anytls://… socks5://… tt://?…"
            class="mono"
            @blur="checkLink"
          />
          <div class="hint">{{ t('dialog.linkHint') }}</div>
          <NAlert v-if="previewError" type="error" :show-icon="false" style="margin-top: 10px">{{ previewError }}</NAlert>
          <div v-if="preview" class="preview">
            <span class="pill ok">{{ preview.protocol }}</span>
            <span class="mono">{{ preview.server }}<template v-if="preview.port">:{{ preview.port }}</template></span>
            <span v-if="preview.tls" class="pill dim">TLS</span>
            <span v-if="preview.addresses" class="mono muted">{{ preview.addresses.join(', ') }}</span>
          </div>
          <div v-if="!preview && link" style="margin-top: 10px">
            <NButton size="small" @click="checkLink">{{ t('dialog.checkLink') }}</NButton>
          </div>
        </NTabPane>
        <NTabPane name="wireguard" tab="WireGuard (.conf)">
          <NInput
            v-model:value="wgText"
            type="textarea"
            :autosize="{ minRows: 10, maxRows: 16 }"
            class="mono"
            placeholder="[Interface]&#10;PrivateKey = …&#10;Address = 10.13.13.2/32&#10;&#10;[Peer]&#10;PublicKey = …&#10;Endpoint = 203.0.113.10:51820&#10;AllowedIPs = 0.0.0.0/0"
          />
          <div class="hint">{{ t('dialog.wgHint') }}</div>
        </NTabPane>
        <NTabPane name="amneziawg" tab="AmneziaWG (.conf)">
          <label>{{ t('awg.select') }} <input type="file" accept=".conf" @change="readAWGFile" /></label>
          <p class="hint">{{ t('awg.tcpOnly') }}</p>
          <NButton :disabled="!awgText || checkingAWG" @click="checkAWG">{{ t('awg.probe') }}</NButton>
        </NTabPane>
        <NTabPane name="singbox" tab="sing-box JSON">
          <NInput
            v-model:value="sbText"
            type="textarea"
            :autosize="{ minRows: 10, maxRows: 18 }"
            class="mono"
            placeholder='{"type": "shadowtls", "server": "…", "server_port": 443, …}'
          />
          <div class="hint">{{ t('dialog.sbHint') }}</div>
        </NTabPane>
      </NTabs>

      <NAlert v-if="error" type="error" :show-icon="false" style="margin-top: 12px">{{ error }}</NAlert>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="emit('update:show', false)">{{ t('common.cancel') }}</NButton>
          <NButton type="primary" :disabled="!id" @click="save">{{ isEdit ? t('common.save') : t('common.add') }}</NButton>
        </NSpace>
      </template>
    </NCard>
  </NModal>
</template>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--muted);
  margin-top: 8px;
}
.preview {
  display: flex;
  gap: 10px;
  align-items: center;
  flex-wrap: wrap;
  margin-top: 10px;
}
</style>
