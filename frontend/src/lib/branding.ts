// Brand assets, with light/dark variants. The "-dark" files are designed for a
// dark background (light ink), so they are used with the dark theme. An admin
// can replace them (Admin › Appearance); custom images win over built-in ones.
import { ref } from 'vue'
import bannerLight from '@/assets/branding/banner-light.svg'
import bannerDark from '@/assets/branding/banner-dark.svg'
import { api, type SiteBranding } from '@/lib/api'

type Theme = 'dark' | 'light'

// Custom image URLs by slot; reactive so banners re-render once loaded.
const custom = ref<SiteBranding>({})

/** The wide brand banner for the given theme. */
export function bannerFor(theme: Theme): string {
  const c = custom.value
  // A single custom banner serves both themes rather than mixing in ours.
  const own = theme === 'dark' ? c['banner-dark'] || c['banner-light'] : c['banner-light'] || c['banner-dark']
  return own || (theme === 'dark' ? bannerDark : bannerLight)
}

/** Fetch the custom branding and apply the favicon. Failures keep the defaults. */
export async function loadBranding() {
  try {
    custom.value = await api.siteBranding()
  } catch {
    custom.value = {}
  }
  applyFavicon(custom.value.favicon)
}

// The built-in <link rel="icon"> tags from index.html, captured once.
let builtInIcons: HTMLLinkElement[] | null = null

// A custom favicon replaces the built-in ones outright: left in place, the
// browser could still pick one of them by size or type.
function applyFavicon(url: string | undefined) {
  builtInIcons ??= [...document.querySelectorAll<HTMLLinkElement>('link[rel="icon"]')]
  document.getElementById('od-favicon')?.remove()
  builtInIcons.forEach((el) => (url ? el.remove() : document.head.appendChild(el)))
  if (!url) return
  const link = document.createElement('link')
  link.id = 'od-favicon'
  link.rel = 'icon'
  link.href = url
  document.head.appendChild(link)
}
