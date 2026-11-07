<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NAlert, NButton, NCard, NInput, NModal, NSpace, NSwitch, NTag, NTimePicker, useDialog } from 'naive-ui'
import { Download } from '@lucide/vue'
import { api } from '../api'
import { ApiError } from '../apiError'
import { store } from '../store'
import type { UpdatePreferences, UpdateSelection, UpdateStatus } from '../types'

type Mutation = { kind: 'schedule'; prefs: UpdatePreferences } | { kind: 'install'; selection: UpdateSelection }
type Action = Mutation | { kind: 'check' }

const { t } = useI18n()
const dialog = useDialog()
const status = ref<UpdateStatus>()
const enabled = ref(false)
const time = ref<string | null>(null)
const busy = ref<Action['kind']>()
const error = ref('')
const readFailed = ref(false)
const olderPanel = ref(false)
const tokenAction = ref<Mutation>()
const token = ref('')
const routerClock = computed(() => status.value?.router_time?.slice(11, 16))
const routingDraft = computed(() => store.dirty.value || store.state.applying)
const scheduleDraft = computed(() => {
  const prefs = status.value?.prefs
  return prefs !== undefined && (prefs.enabled !== enabled.value || prefs.time !== time.value)
})
const running = computed(() => status.value?.job?.state === 'running')
const locked = computed(() => busy.value !== undefined || running.value)

let timer: ReturnType<typeof setInterval> | undefined
let refreshing = false
let refreshEpoch = 0

// Keep the draft when a poll returns a newer installer snapshot.
function accept(next: UpdateStatus, syncSchedule = false) {
  if (!scheduleDraft.value || syncSchedule) {
    enabled.value = next.prefs.enabled
    time.value = next.prefs.time
  }
  status.value = next
}

async function refresh() {
  if (refreshing || busy.value) return
  refreshing = true
  const epoch = refreshEpoch
  try {
    const next = await api.updates()
    if (epoch !== refreshEpoch) return
    readFailed.value = false
    olderPanel.value = false
    accept(next)
  } catch (err) {
    if (epoch !== refreshEpoch) return
    // A successful rollback may restart a version with no update endpoint.
    if (err instanceof ApiError && err.status === 404) olderPanel.value = true
    else if (!status.value) readFailed.value = true
  } finally {
    refreshing = false
  }
}

onMounted(() => {
  void refresh()
  timer = setInterval(refresh, 5000)
})
onUnmounted(() => {
  clearInterval(timer)
  refreshEpoch++
  token.value = ''
})

function closeToken() {
  tokenAction.value = undefined
  token.value = ''
}

function canInstall(selection: UpdateSelection) {
  const target = selection.rollback ? status.value?.previous : status.value?.latest
  return !routingDraft.value && !running.value && target === selection.target
}

function submit(action: Action) {
  switch (action.kind) {
    case 'check': return api.checkUpdates()
    case 'schedule': return api.saveUpdatePreferences(action.prefs)
    case 'install': return api.installUpdate(action.selection)
  }
}

function handleError(err: unknown, action: Action, signIn: boolean) {
  const code = err instanceof ApiError ? err.status : undefined
  if (code === 403 && action.kind !== 'check') {
    tokenAction.value = action
    if (signIn) error.value = t('updates.tokenRejected')
    return
  }
  if (signIn && code === 401) {
    error.value = t('updates.tokenRejected')
    return
  }
  closeToken()
  error.value = t(code === 409 ? 'updates.stateChanged' : 'updates.requestFailed')
}

async function run(action: Action, signIn = false) {
  if (busy.value) return
  busy.value = action.kind
  refreshEpoch++
  error.value = ''
  try {
    if (signIn) {
      const credential = token.value.trim()
      token.value = ''
      await api.login(credential)
    }
    if (action.kind === 'install' && !canInstall(action.selection)) {
      closeToken()
      error.value = t('updates.stateChanged')
      return
    }
    accept(await submit(action), action.kind === 'schedule')
    closeToken()
  } catch (err) {
    handleError(err, action, signIn)
  } finally {
    busy.value = undefined
  }
}

function confirmRelease(target: string, rollback: boolean) {
  dialog.warning({
    title: t(rollback ? 'updates.rollback' : 'updates.install'),
    content: t(status.value?.demo ? 'updates.demoConfirm' : 'updates.confirm', { version: target }),
    positiveText: t(rollback ? 'updates.rollback' : 'updates.install'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => { void run({ kind: 'install', selection: { rollback, target } }) },
  })
}

function saveSchedule() {
  if (time.value) void run({ kind: 'schedule', prefs: { enabled: enabled.value, time: time.value } })
}

function signInAndContinue() {
  if (tokenAction.value && token.value.trim()) void run(tokenAction.value, true)
}

function reloadPanel() {
  window.location.reload()
}
</script>

<template>
  <NCard size="small">
    <template #header><span class="update-heading"><Download :size="16" /> {{ t('updates.title') }}</span></template>
    <NAlert v-if="olderPanel" type="info">
      {{ t('updates.olderPanel') }}
      <div><NButton text size="small" :disabled="routingDraft || scheduleDraft" @click="reloadPanel">{{ t('updates.reload') }}</NButton></div>
    </NAlert>
    <p v-else-if="!status" class="muted">{{ t(readFailed ? 'updates.readFailed' : 'updates.loading') }}</p>
    <p v-else-if="!status.available" class="muted">{{ t('updates.unavailable') }}</p>
    <div v-else class="update-body">
      <NAlert v-if="status.demo" type="info">{{ t('updates.demoHint') }}</NAlert>
      <div class="release-versions">
        <span>{{ t('updates.current') }} <strong>{{ status.current ?? t('ui.unknown') }}</strong></span>
        <span v-if="status.previous">{{ t('updates.previous') }} <strong>{{ status.previous }}</strong></span>
        <NTag v-if="status.latest" type="warning" size="small" :bordered="false">
          {{ t('updates.newRelease', { version: status.latest }) }}
        </NTag>
      </div>
      <span v-if="status.checked_at && !status.check_failed && !status.latest" class="muted">{{ t('updates.upToDate') }}</span>
      <NAlert v-if="status.check_failed" type="warning">{{ t('updates.checkFailed') }}</NAlert>

      <NSpace>
        <NButton size="small" :disabled="locked" :loading="busy === 'check'" @click="run({ kind: 'check' })">
          {{ t('updates.check') }}
        </NButton>
        <NButton v-if="status.latest" type="primary" size="small" :disabled="locked || routingDraft" @click="confirmRelease(status.latest, false)">
          {{ t('updates.install') }}
        </NButton>
        <NButton v-if="status.previous" size="small" :disabled="locked || routingDraft" @click="confirmRelease(status.previous, true)">
          {{ t('updates.rollback') }}
        </NButton>
      </NSpace>
      <p v-if="routingDraft" class="muted">{{ t('updates.unsavedRouting') }}</p>

      <NAlert v-if="status.job" :type="status.job.state === 'failed' ? 'error' : status.job.state === 'success' ? 'success' : 'info'">
        {{ t(status.demo && status.job.state === 'running' ? 'updates.demoRunning' : `updates.job.${status.job.state}`, { version: status.job.target }) }}
        <div v-if="status.job.state === 'success' && !status.demo">
          <NButton text size="small" :disabled="routingDraft || scheduleDraft" @click="reloadPanel">{{ t('updates.reload') }}</NButton>
        </div>
      </NAlert>

      <div class="update-schedule">
        <NSpace align="center">
          <NSwitch v-model:value="enabled" :disabled="locked" :aria-label="t('updates.auto')" />
          <strong>{{ t('updates.auto') }}</strong>
        </NSpace>
        <p class="muted">{{ t(enabled ? (status.demo ? 'updates.demoOnHint' : 'updates.onHint') : 'updates.offHint') }}</p>
        <NSpace align="center">
          <NTimePicker v-model:formatted-value="time" size="small" format="HH:mm" :clearable="false" :disabled="locked || !enabled" :aria-label="t('updates.time')" />
          <NButton size="small" :disabled="locked || !scheduleDraft || !time" :loading="busy === 'schedule'" @click="saveSchedule">
            {{ t('updates.save') }}
          </NButton>
        </NSpace>
        <span v-if="routerClock" class="muted">{{ t(status.demo ? 'updates.demoClock' : 'updates.routerTime', { time: routerClock }) }}</span>
      </div>
      <NAlert v-if="error && !tokenAction" type="error">{{ error }}</NAlert>
    </div>
    <NModal
      :show="tokenAction !== undefined"
      preset="card"
      :title="t('updates.tokenTitle')"
      :mask-closable="!busy"
      :close-on-esc="!busy"
      :closable="!busy"
      style="width: min(420px, 94vw)"
      @update:show="value => { if (!value) closeToken() }"
    >
      <form class="update-body" @submit.prevent="signInAndContinue">
        <p class="muted">{{ t('updates.tokenHint') }}</p>
        <NInput v-model:value="token" type="password" :placeholder="t('login.token')" :aria-label="t('login.token')" :disabled="!!busy" autofocus />
        <NAlert v-if="error" type="error">{{ error }}</NAlert>
        <NButton type="primary" attr-type="submit" :loading="!!busy" :disabled="!token.trim()">{{ t('updates.authorize') }}</NButton>
      </form>
    </NModal>
  </NCard>
</template>

<style scoped>
.update-heading,
.release-versions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 16px;
}
.update-body,
.update-schedule {
  display: grid;
  gap: 12px;
}
.update-body p {
  margin: 0;
}
.update-schedule {
  border-top: 1px solid var(--line);
  padding-top: 12px;
}
</style>
