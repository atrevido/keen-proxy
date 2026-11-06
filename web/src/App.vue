<script setup lang="ts">
import { NConfigProvider, NMessageProvider, NDialogProvider, NGlobalStyle } from 'naive-ui'
import { naiveTheme, overrides } from './theme'
import { naiveDateLocale, naiveLocale } from './i18n'
import Shell from './components/Shell.vue'
</script>

<template>
  <NConfigProvider :theme="naiveTheme" :theme-overrides="overrides" :locale="naiveLocale" :date-locale="naiveDateLocale">
    <NGlobalStyle />
    <NMessageProvider placement="bottom-right">
      <NDialogProvider>
        <Shell />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>
