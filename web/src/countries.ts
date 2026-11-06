// Country groups use Intl names in the current UI language.
import { computed } from 'vue'
import { locale } from './i18n'

// Convert a two-letter country code to regional indicator emoji.
export function flag(cc?: string): string {
  if (!cc || cc.length !== 2) return ''
  const up = cc.toUpperCase()
  return String.fromCodePoint(0x1f1e6 + up.charCodeAt(0) - 65, 0x1f1e6 + up.charCodeAt(1) - 65)
}

const names = computed(() => {
  try {
    return new Intl.DisplayNames([locale.value], { type: 'region', fallback: 'none' })
  } catch {
    return null
  }
})

// Localize a region code, keeping the original when Intl has no name.
export function countryName(cc: string): string {
  return names.value?.of(cc.toUpperCase()) ?? cc.toUpperCase()
}

// Enumerate two-letter region codes recognized by Intl.DisplayNames.
export const countries = computed(() => {
  const out: { code: string; name: string }[] = []
  for (let a = 65; a <= 90; a++) {
    for (let b = 65; b <= 90; b++) {
      const code = String.fromCharCode(a, b)
      const name = names.value?.of(code)
      if (name && name !== code && !/^(EU|EZ|UN|ZZ|QO|XA|XB)$/.test(code)) out.push({ code, name })
    }
  }
  return out.sort((x, y) => x.name.localeCompare(y.name, locale.value))
})
