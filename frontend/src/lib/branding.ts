// Brand assets, with light/dark variants. The "-dark" files are designed for a
// dark background (light ink), so they are used with the dark theme.
import bannerLight from '@/assets/branding/banner-light.svg'
import bannerDark from '@/assets/branding/banner-dark.svg'

type Theme = 'dark' | 'light'

/** The wide brand banner for the given theme. */
export function bannerFor(theme: Theme): string {
  return theme === 'dark' ? bannerDark : bannerLight
}
