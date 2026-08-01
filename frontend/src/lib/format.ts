/** Formatting helpers. Size units and dates follow the active i18n locale. */
import { i18n, currentLocale } from '@/i18n'

const EXT_KIND: Record<string, string> = {
  md: 'text', txt: 'text', rtf: 'text',
  pdf: 'pdf',
  doc: 'doc', docx: 'doc', odt: 'doc',
  xls: 'sheet', xlsx: 'sheet', csv: 'sheet', ods: 'sheet',
  ppt: 'slides', pptx: 'slides', key: 'slides',
  jpg: 'image', jpeg: 'image', png: 'image', gif: 'image', webp: 'image', svg: 'image',
  mp4: 'video', mov: 'video', mkv: 'video', webm: 'video', m4v: 'video', avi: 'video',
  flv: 'video', wmv: 'video', mpg: 'video', mpeg: 'video', '3gp': 'video', ogv: 'video',
  mp3: 'audio', wav: 'audio', flac: 'audio', m4a: 'audio', aac: 'audio', ogg: 'audio', opus: 'audio', wma: 'audio', aiff: 'audio',
  zip: 'archive', gz: 'archive', tar: 'archive', rar: 'archive', '7z': 'archive',
  js: 'code', ts: 'code', css: 'code', html: 'code', py: 'code', sh: 'code', go: 'code', rs: 'code',
  yml: 'config', yaml: 'config', toml: 'config', env: 'config', conf: 'config', ini: 'config', json: 'config',
  iso: 'disc', img: 'disc',
  fig: 'design', sketch: 'design', psd: 'design', psb: 'design', ai: 'design', eps: 'design', xcf: 'design',
}

export function kindFromName(name: string): string {
  const m = String(name).toLowerCase().match(/\.([a-z0-9]+)$/)
  return (m && EXT_KIND[m[1]]) || 'file'
}

export function ext(name: string): string {
  const m = String(name).match(/\.([a-zA-Z0-9]+)$/)
  return m ? m[1].toUpperCase() : ''
}

/** Whether the file name is a supported archive (zip, tar, tar.gz, tgz, 7z). */
export function isArchive(name: string): boolean {
  return /\.(zip|tar|tar\.gz|tgz|7z)$/i.test(String(name))
}

export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'epub' | 'archive' | 'none'

/** How a file can be previewed inline in the browser. */
export function previewKind(name: string): PreviewKind {
  // foliate-js renders these e-book / comic formats through the same viewer.
  if (/\.(epub|mobi|azw3?|fb2|cbz)$/i.test(String(name))) return 'epub'
  // Archives are browsed as a tree (listed from their index, never extracted).
  if (isArchive(name)) return 'archive'
  const k = kindFromName(name)
  if (k === 'image' || k === 'video' || k === 'audio' || k === 'pdf') return k
  if (k === 'text' || k === 'code' || k === 'config') return 'text'
  return 'none'
}

/** Whether the file's text content can be edited in the browser. */
export function isMarkdown(name: string): boolean {
  return /\.(md|markdown)$/i.test(String(name))
}

// Extensions the backend can generate a thumbnail for (image built-in / vips /
// libraw / ffmpeg / poppler / libreoffice). A 404 falls back to the type icon.
const THUMB_EXT =
  /\.(jpe?g|png|gif|webp|tiff?|bmp|svgz?|heic|heif|avif|jxl|jp2|jpx|ico|tga|xcf|dds|qoi|pcx|hdr|cr2|cr3|nef|nrw|arw|sr2|srf|dng|raf|orf|rw2|pef|srw|k25|kdc|dcr|mrw|x3f|3fr|mef|iiq|mos|raw|mp4|mov|webm|mkv|m4v|avi|flv|wmv|mpe?g|3gp|3g2|ts|m2ts|mts|ogv|asf|mp3|flac|m4a|aac|ogg|opus|wma|aiff|wav|pdf|docx?|odt|rtf|xlsx?|ods|pptx?|odp|eps|ai|psd|psb|ttf|otf|ttc|otc|epub|cbz|cbt|fb2|mobi|azw3?)$/i

/** Whether OrionDrive may have a thumbnail for this file. */
export function canThumbnail(name: string): boolean {
  return THUMB_EXT.test(String(name))
}

// Size unit suffixes per locale (SI/decimal: 1000-based).
const SIZE_UNITS: Record<string, string[]> = {
  fr: ['o', 'Ko', 'Mo', 'Go', 'To'],
  en: ['B', 'KB', 'MB', 'GB', 'TB'],
}

/** Human-readable size, formatted for the active locale. */
export function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  const loc = currentLocale()
  const units = SIZE_UNITS[loc] || SIZE_UNITS.fr
  let v = bytes
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i += 1
  }
  const digits = v >= 100 || i === 0 ? 0 : 1
  const s = new Intl.NumberFormat(loc, { maximumFractionDigits: digits }).format(v)
  return s + ' ' + units[i]
}

/** Coarse ETA for a live estimate: whole seconds under a minute, whole minutes
 * above (dropping the noisy seconds component so the label stops flickering). */
export function fmtEta(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return ''
  const s = Math.round(seconds)
  if (s < 60) return `${s} s`
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return rm ? `${h} h ${rm} min` : `${h} h`
}

/** Absolute date + time (short), formatted for the active locale. */
export function fmtDateTime(iso: string): string {
  return new Date(iso).toLocaleString(currentLocale(), { dateStyle: 'short', timeStyle: 'short' })
}

/** Relative-then-absolute date, formatted for the active locale. */
export function fmtDate(iso: string): string {
  const loc = currentLocale()
  const d = new Date(iso)
  const diff = Date.now() - d.getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) return i18n.global.t('format.now')
  const rtf = new Intl.RelativeTimeFormat(loc, { numeric: 'always', style: 'short' })
  if (min < 60) return rtf.format(-min, 'minute')
  const h = Math.floor(min / 60)
  if (h < 24) return rtf.format(-h, 'hour')
  const j = Math.floor(h / 24)
  if (j < 7) return rtf.format(-j, 'day')
  return new Intl.DateTimeFormat(loc, { day: 'numeric', month: 'short', year: 'numeric' }).format(d)
}
