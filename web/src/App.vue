<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider, NGlobalStyle } from 'naive-ui'
import { naiveTheme, overrides } from './theme'
import { naiveDateLocale, naiveLocale } from './i18n'
import Shell from './components/Shell.vue'
import { locale, t } from './i18n'
const browserDemo = import.meta.env.MODE === 'demo'
</script>

<template>
  <aside v-if="browserDemo" class="browser-demo" aria-label="Browser demo">
    {{ t('browserDemo.notice') }}
    <a :href="locale === 'ru' ? '../ru/' : '../'">{{ t('browserDemo.documentation') }}</a>
  </aside>
  <NConfigProvider :theme="naiveTheme" :theme-overrides="overrides" :locale="naiveLocale" :date-locale="naiveDateLocale">
    <NGlobalStyle />
    <NMessageProvider placement="bottom-right">
      <NDialogProvider>
        <Shell />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

<style scoped>
.browser-demo{padding:8px 16px;background:#182d29;color:#e5f8ed;font:13px/1.5 system-ui;text-align:center}
.browser-demo a{color:#9df0c5;margin-left:12px;text-decoration:underline}
</style>
