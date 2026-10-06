// The admin-defined custom stylesheet (/api/v1/site/custom.css), applied to
// every page — drive and public share links — except the admin panel, so a
// broken stylesheet can always be fixed from there. Opening any URL with
// ?safe=1 skips it for the rest of the session (recovery escape hatch).
const LINK_ID = 'od-custom-css'
const CSS_URL = '/api/v1/site/custom.css'

const safeMode = new URLSearchParams(location.search).has('safe')

/** Enable or disable the custom stylesheet, creating its <link> on first use. */
export function setCustomCssEnabled(enabled: boolean) {
  let link = document.getElementById(LINK_ID) as HTMLLinkElement | null
  if (!enabled || safeMode) {
    if (link) link.disabled = true
    return
  }
  if (!link) {
    link = document.createElement('link')
    link.id = LINK_ID
    link.rel = 'stylesheet'
    link.href = CSS_URL
    // Appended last so it overrides the app's own styles.
    document.head.appendChild(link)
  }
  link.disabled = false
}

/** Re-fetch the stylesheet after an admin edit (busts the cached copy). */
export function reloadCustomCss() {
  const link = document.getElementById(LINK_ID) as HTMLLinkElement | null
  if (link) link.href = `${CSS_URL}?v=${Date.now()}`
}
