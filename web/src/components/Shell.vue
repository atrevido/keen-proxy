<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NDropdown, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  Settings,
  Sun,
  Moon,
  Menu,
  X,
  Undo2,
  Check,
  Languages,
  LogOut,
} from '@lucide/vue'
import { api, setUnauthorizedHandler } from '../api'
import { authed, session, store } from '../store'
import { LOCALES, locale, setLocale, type Locale } from '../i18n'
import { themeName, themePreference, type ThemePreference } from '../theme'
import mark from '../assets/keenproxy-mark.png'
const route = useRoute(),
  router = useRouter(),
  message = useMessage(),
  { t } = useI18n()
const drawer = ref(false)
const bare = computed(() => route.meta.bare === true)
const dbipAttribution = computed(
  () =>
    !!store.state.status?.geoip.loaded &&
    /^DB-?IP(?:[-_\s]|$)/i.test(store.state.status.geoip.type ?? ''),
)
const nav = computed(() => [
  { to: '/', label: t('ui.overview'), active: route.path === '/' },
  {
    to: '/lists/groups',
    label: t('ui.rules'),
    active: route.path.startsWith('/lists'),
  },
  {
    to: '/network/connections',
    label: t('nav.connections'),
    active: route.path.startsWith('/network'),
  },
  {
    to: '/service/journal',
    label: t('nav.journal'),
    active: route.path === '/service/journal',
  },
  {
    to: '/service/diagnostics',
    label: t('ui.diagnostics'),
    active: route.path === '/service/diagnostics',
  },
])
const secondary = computed(() =>
  route.path.startsWith('/lists')
    ? [
        { to: '/lists/groups', label: t('nav.whitelist') },
        { to: '/lists/presets', label: t('nav.presets') },
        { to: '/lists/filters', label: t('nav.filters') },
      ]
    : route.path.startsWith('/network')
      ? [
          { to: '/network/connections', label: t('nav.connections') },
          { to: '/network/clients', label: t('nav.clients') },
        ]
      : [],
)
const langOptions = LOCALES.map((l) => ({ key: l.value, label: l.label }))
const themeOptions = computed(() => [
  { key: 'dark', label: t('shell.darkTheme') },
  { key: 'light', label: t('shell.lightTheme') },
  { key: 'system', label: t('ui.systemTheme') },
])
watch(
  () => route.path,
  () => {
    drawer.value = false
  },
)
setUnauthorizedHandler(() => {
  authed.value = false
  if (route.name !== 'login')
    void router.push({ name: 'login', query: { next: route.fullPath } })
})
// Load session policy and the shared draft when entering authenticated screens.
async function boot() {
  if (bare.value) return
  try {
    await store.loadSession(true).catch(() => {})
    await store.load()
  } catch {
    /* Store or login displays error. */
  }
}
onMounted(boot)
watch(
  () => route.name,
  (n, old) => {
    if (old === 'login' && n !== 'login') void boot()
  },
)
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  timer = setInterval(() => {
    if (authed.value && !bare.value) void store.refreshStatus().catch(() => {})
  }, 5000)
})
onUnmounted(() => clearInterval(timer))
// Central Apply action persists edits accumulated across every screen.
async function apply() {
  try {
    await store.apply()
    message.success(t('shell.applied'))
  } catch (e) {
    message.error((e as Error).message, { duration: 10000, closable: true })
  }
}
async function logout() {
  await api.logout().catch(() => {})
  authed.value = false
  void router.push({ name: 'login' })
}
</script>
<template>
  <router-view v-if="bare" />
  <div v-else class="shell">
    <header class="topbar">
      <router-link to="/" class="brand" aria-label="KeenProxy"
        ><img :src="mark" alt="" /><b>KeenProxy</b></router-link
      >
      <nav class="desktop-nav" :aria-label="t('shell.menu')">
        <router-link
          v-for="item in nav"
          :key="item.to"
          :to="item.to"
          :class="{ active: item.active }"
          >{{ item.label }}</router-link
        >
      </nav>
      <div class="header-actions">
        <NDropdown
          trigger="click"
          :options="themeOptions"
          :value="themePreference"
          @select="(key: ThemePreference) => (themePreference = key)"
          ><button class="icon-btn" :aria-label="t('ui.systemTheme')">
            <Sun v-if="themeName === 'dark'" :size="22" /><Moon
              v-else
              :size="22"
            /></button
        ></NDropdown>
        <router-link
          to="/settings"
          class="icon-btn settings-link"
          :aria-label="t('nav.settings')"
          ><Settings :size="22"
        /></router-link>
        <NDropdown
          trigger="click"
          :options="langOptions"
          :value="locale"
          @select="(key: Locale) => setLocale(key)"
          ><button class="icon-btn language" :aria-label="t('shell.language')">
            <Languages :size="18" /></button
        ></NDropdown>
        <button
          class="icon-btn menu-toggle"
          :aria-label="t('shell.menu')"
          :aria-expanded="drawer"
          @click="drawer = !drawer"
        >
          <X v-if="drawer" :size="22" /><Menu v-else :size="22" />
        </button>
        <button
          v-if="!session.trusted"
          class="icon-btn logout"
          :aria-label="t('shell.logout')"
          @click="logout"
        >
          <LogOut :size="18" />
        </button>
      </div>
    </header>
    <nav v-if="drawer" class="mobile-nav" :aria-label="t('shell.menu')">
      <router-link
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        :class="{ active: item.active }"
        >{{ item.label }}</router-link
      ><router-link to="/settings">{{ t('nav.settings') }}</router-link
      ><router-link to="/network/clients">{{ t('nav.clients') }}</router-link>
    </nav>
    <nav v-if="secondary.length" class="subnav">
      <router-link
        v-for="item in secondary"
        :key="item.to"
        :to="item.to"
        :class="{ active: route.path === item.to }"
        >{{ item.label }}</router-link
      >
    </nav>
    <div v-if="store.dirty.value" class="draft-strip" role="status">
      <span>{{ t('ui.draft') }}</span>
      <div>
        <NButton
          quaternary
          :disabled="store.state.applying"
          :aria-label="t('common.discard')"
          @click="store.discard()"
          ><template #icon><Undo2 :size="16" /></template></NButton
        ><NButton type="primary" :loading="store.state.applying" @click="apply"
          ><template #icon><Check :size="16" /></template
          >{{ t('common.apply') }}</NButton
        >
      </div>
    </div>
    <main class="content">
      <div v-if="store.state.error" class="apply-error" role="alert">
        <b>{{ t('shell.applyFailed') }}</b>
        <pre>{{ store.state.error }}</pre>
      </div>
      <NSpin :show="!store.state.config && authed !== false"
        ><router-view v-if="store.state.config" />
        <div v-else class="empty-state">{{ t('ui.unknown') }}</div></NSpin
      >
    </main>
    <footer v-if="dbipAttribution" class="attribution">
      <a href="https://db-ip.com/" target="_blank" rel="noreferrer"
        >IP Geolocation by DB-IP</a
      >
    </footer>
  </div>
</template>
<style scoped>
.shell {
  min-height: 100dvh;
}
.attribution {
  max-width: 1600px;
  margin: auto;
  padding: 0 clamp(16px, 4vw, 42px) 24px;
  font-size: 12px;
}
.attribution a {
  color: var(--muted);
}
.attribution a:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 4px;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 28px;
  padding: 0 32px;
  min-height: 80px;
  border-bottom: 1px solid var(--line);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  text-decoration: none;
  color: var(--text);
  font-size: 28px;
  letter-spacing: -0.8px;
  white-space: nowrap;
}
.brand img {
  width: 44px;
  height: 44px;
  object-fit: contain;
}
.desktop-nav {
  display: flex;
  gap: 8px;
  align-self: stretch;
  flex: 1;
  min-width: 0;
}
.desktop-nav a {
  display: flex;
  align-items: center;
  padding: 0 15px;
  text-decoration: none;
  color: var(--text);
  border-bottom: 4px solid transparent;
  white-space: nowrap;
}
.desktop-nav a.active {
  font-weight: 650;
  border-color: var(--accent);
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}
.header-actions .icon-btn {
  border: 0;
}
.content {
  max-width: 1600px;
  padding: 28px 42px 48px;
  margin: auto;
  min-width: 0;
}
.subnav {
  display: flex;
  gap: 24px;
  padding: 0 42px;
  border-bottom: 1px solid var(--line);
  overflow-x: auto;
}
.subnav a {
  padding: 14px 0;
  text-decoration: none;
  color: var(--muted);
  white-space: nowrap;
  border-bottom: 2px solid transparent;
}
.subnav .active {
  color: var(--accent);
  border-color: var(--accent);
}
.draft-strip {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 12px 42px;
  background: color-mix(in srgb, var(--warn) 12%, var(--page));
  color: var(--warn);
}
.draft-strip > div {
  display: flex;
  gap: 10px;
}
.apply-error {
  padding: 16px;
  border: 1px solid var(--bad);
  color: var(--bad);
  margin-bottom: 24px;
}
.apply-error pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.menu-toggle {
  display: none;
}
.mobile-nav {
  display: flex;
  flex-direction: column;
  padding: 16px 24px;
  border-bottom: 1px solid var(--line);
}
.mobile-nav a {
  padding: 12px 0;
  text-decoration: none;
  color: var(--text);
}
.mobile-nav .active {
  color: var(--accent);
}
.brand img {
  filter: var(--brand-filter, none);
}
@media (max-width: 1200px) {
  .topbar {
    gap: 16px;
    padding: 0 24px;
  }
  .brand {
    font-size: 24px;
  }
  .desktop-nav {
    gap: 0;
  }
  .desktop-nav a {
    padding: 0 10px;
    font-size: 14px;
  }
  .language,
  .logout {
    display: none;
  }
}
@media (max-width: 950px) {
  .desktop-nav {
    display: none;
  }
  .menu-toggle {
    display: inline-flex;
  }
  .topbar {
    min-height: 72px;
  }
  .content {
    padding: 28px 24px 48px;
  }
}
@media (max-width: 540px) {
  .content {
    padding: 24px 18px 48px;
  }
  .topbar {
    padding: 0 16px;
    gap: 12px;
  }
  .brand {
    font-size: 24px;
  }
  .brand img {
    width: 36px;
    height: 36px;
  }
  .header-actions {
    gap: 0;
  }
  .settings-link {
    display: none;
  }
  .subnav {
    padding: 0 18px;
    gap: 20px;
  }
  .draft-strip {
    padding: 10px 18px;
    font-size: 13px;
  }
  .draft-strip span {
    max-width: 50%;
  }
}
</style>
