// Brand assets, with light/dark variants. The "-dark" files are designed for a
// dark background (light ink), so they are used with the dark theme.
import logoLight from '@/assets/branding/logo-light.svg'
import logoDark from '@/assets/branding/logo-dark.svg'
import bannerLight from '@/assets/branding/banner-light.webp'
import bannerDark from '@/assets/branding/banner-dark.webp'

type Theme = 'dark' | 'light'

/** The square logo mark for the given theme. */
export function logoFor(theme: Theme): string {
  return theme === 'dark' ? logoDark : logoLight
}

/** The wide brand banner for the given theme. */
export function bannerFor(theme: Theme): string {
  return theme === 'dark' ? bannerDark : bannerLight
}
