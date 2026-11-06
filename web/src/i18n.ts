// Prefer the saved UI language, then supported browser languages, then English.
import { computed } from 'vue'
import { createI18n } from 'vue-i18n'
import { dateEnUS, dateRuRU, enUS, ruRU } from 'naive-ui'
import ru from './locales/ru'
import en from './locales/en'

export const LOCALES = [
  { value: 'ru', label: 'Русский' },
  { value: 'en', label: 'English' },
] as const
export type Locale = (typeof LOCALES)[number]['value']

const KEY = 'keenproxy.locale'

function initial(): Locale {
  try {
    const saved = localStorage.getItem(KEY)
    if (saved === 'ru' || saved === 'en') return saved
  } catch {
    /* Fall back to browser preferences when storage is unavailable. */
  }
  for (const l of navigator.languages ?? [navigator.language]) {
    const base = l.toLowerCase().split('-')[0]
    if (base === 'ru' || base === 'en') return base
  }
  return 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: initial(),
  fallbackLocale: 'ru',
  messages: { ru, en },
})

// Shared translator for modules outside component setup (formatters, store, router).
export const t = i18n.global.t
export const locale = i18n.global.locale

// Update the UI and document language, persisting the choice when possible.
export function setLocale(l: Locale) {
  locale.value = l
  document.documentElement.lang = l
  try {
    localStorage.setItem(KEY, l)
  } catch {
    /* The active language still changes when persistence is unavailable. */
  }
}
document.documentElement.lang = locale.value

// Keep Naive UI labels and date formatting aligned with the selected locale.
export const naiveLocale = computed(() => (locale.value === 'en' ? enUS : ruRU))
export const naiveDateLocale = computed(() => (locale.value === 'en' ? dateEnUS : dateRuRU))
