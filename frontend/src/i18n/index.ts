import { createI18n } from 'vue-i18n'
import fr from './locales/fr.json'
import en from './locales/en.json'

// Locales OrionDrive ships with. French is the source/reference locale.
export const SUPPORTED = ['fr', 'en'] as const
export type Locale = (typeof SUPPORTED)[number]

export const LOCALE_LABELS: Record<Locale, string> = {
  fr: 'Français',
  en: 'English',
}

const STORAGE_KEY = 'od-lang'

function isSupported(v: string): v is Locale {
  return (SUPPORTED as readonly string[]).includes(v)
}

// Detect the initial locale: saved choice → browser language → French.
function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved && isSupported(saved)) return saved
  } catch {}
  const nav = (navigator.language || 'fr').slice(0, 2).toLowerCase()
  return isSupported(nav) ? nav : 'fr'
}

export const i18n = createI18n({
  legacy: false,
  locale: detectLocale(),
  fallbackLocale: 'fr',
  messages: { fr, en },
})

// Keep <html lang> in sync from the start.
document.documentElement.lang = i18n.global.locale.value

/** Switch the active locale, persist it and update <html lang>. */
export function setLocale(locale: Locale) {
  i18n.global.locale.value = locale
  try {
    localStorage.setItem(STORAGE_KEY, locale)
  } catch {}
  document.documentElement.lang = locale
}

/** The currently active locale. */
export function currentLocale(): Locale {
  return i18n.global.locale.value as Locale
}
