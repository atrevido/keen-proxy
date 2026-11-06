import { computed, ref, watch } from 'vue'
import { darkTheme, type GlobalThemeOverrides } from 'naive-ui'

// Persist the user choice separately from the resolved system color scheme.
export type ThemePreference = 'dark' | 'light' | 'system'
const media = window.matchMedia('(prefers-color-scheme: light)')
const systemLight = ref(media.matches)
media.addEventListener('change', (e) => {
  systemLight.value = e.matches
})
const saved = (() => {
  try {
    return localStorage.getItem('keenproxy-theme')
  } catch {
    return null
  }
})()
export const themePreference = ref<ThemePreference>(
  saved === 'dark' || saved === 'light' ? saved : 'system',
)
export const themeName = computed(() =>
  themePreference.value === 'system'
    ? systemLight.value
      ? 'light'
      : 'dark'
    : themePreference.value,
)
watch(themePreference, (value) => {
  try {
    localStorage.setItem('keenproxy-theme', value)
  } catch {
    /* Private mode */
  }
})
watch(
  themeName,
  (value) => {
    document.documentElement.dataset.theme = value
  },
  { immediate: true },
)
export const palette = {
  accent: '#b2eb82',
  accentLight: '#35652b',
  sidebar: '#141b1c',
  sidebarLight: '#f6f8f4',
}
export const naiveTheme = computed(() =>
  themeName.value === 'dark' ? darkTheme : null,
)
// Apply the resolved palette to Naive UI components alongside the document theme.
export const overrides = computed<GlobalThemeOverrides>(() => {
  const dark = themeName.value === 'dark'
  const primary = dark ? '#b2eb82' : '#35652b'
  return {
    common: {
      primaryColor: primary,
      primaryColorHover: dark ? '#c5f3a0' : '#477d3a',
      primaryColorPressed: dark ? '#9bd76c' : '#294f22',
      primaryColorSuppl: primary,
      borderRadius: '6px',
      fontSize: '15px',
      fontFamily:
        'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
      bodyColor: dark ? '#141b1c' : '#f6f8f4',
      cardColor: dark ? '#1b2425' : '#ffffff',
      modalColor: dark ? '#1b2425' : '#ffffff',
      popoverColor: dark ? '#1b2425' : '#ffffff',
      tableColor: dark ? '#141b1c' : '#f6f8f4',
      textColorBase: dark ? '#e8eeeb' : '#202d27',
      textColor1: dark ? '#e8eeeb' : '#202d27',
      textColor2: dark ? '#d0d9d3' : '#34463a',
      textColor3: dark ? '#a8b4b0' : '#5b685f',
      borderColor: dark ? '#35433f' : '#cdd7ce',
      dividerColor: dark ? '#35433f' : '#cdd7ce',
      successColor: primary,
      warningColor: dark ? '#e4ae5d' : '#97600b',
      errorColor: dark ? '#f28e8e' : '#b32c35',
    },
    Button: { textColorPrimary: dark ? '#172318' : '#ffffff' },
    DataTable: {
      thColor: dark ? '#1b2425' : '#eef2eb',
      tdColor: dark ? '#141b1c' : '#f6f8f4',
    },
  }
})
