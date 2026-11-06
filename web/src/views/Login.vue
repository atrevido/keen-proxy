<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NButton, NCard, NInput, NAlert } from 'naive-ui'
import { KeyRound } from '@lucide/vue'
import { api } from '../api'
import { store } from '../store'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const token = ref('')
const error = ref('')
const loading = ref(false)

const next = () => (route.query.next as string) || '/'

// Trusted-network sessions can enter the panel without a token.
onMounted(async () => {
  try {
    if ((await store.loadSession()).authed) router.replace(next())
  } catch {
    /* Keep the login screen when the session check fails. */
  }
})

async function submit() {
  loading.value = true
  error.value = ''
  try {
    await api.login(token.value)
    router.push(next())
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login">
    <NCard class="box" :bordered="false">
      <div class="brand"><span class="logo">K</span><b>keenProxy</b></div>
      <h2>{{ t('login.title') }}</h2>
      <i18n-t keypath="login.hint" tag="p" class="muted">
        <template #file><span class="mono">/opt/etc/keenproxy/token</span></template>
      </i18n-t>
      <form @submit.prevent="submit">
        <NInput v-model:value="token" type="password" show-password-on="click" :placeholder="t('login.token')" size="large" autofocus>
          <template #prefix><KeyRound :size="16" /></template>
        </NInput>
        <NAlert v-if="error" type="error" :show-icon="false" style="margin-top: 12px">{{ error }}</NAlert>
        <NButton type="primary" size="large" block attr-type="submit" :loading="loading" :disabled="!token" style="margin-top: 16px">
          {{ t('login.submit') }}
        </NButton>
      </form>
    </NCard>
  </div>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 16px;
  background: radial-gradient(circle at 30% 20%, var(--sidebar), var(--page) 70%);
}
.box {
  width: min(400px, 100%);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 18px;
}
.logo {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: var(--accent);
  color: #172339;
  font-weight: 800;
  display: grid;
  place-items: center;
}
h2 {
  margin: 18px 0 6px;
}
p {
  font-size: 13px;
  margin: 0 0 16px;
}
</style>
