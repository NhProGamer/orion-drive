/** Formatting helpers ported from the OrionDrive prototype (French locale). */

const EXT_KIND: Record<string, string> = {
  md: 'text', txt: 'text', rtf: 'text',
  pdf: 'pdf',
  doc: 'doc', docx: 'doc', odt: 'doc',
  xls: 'sheet', xlsx: 'sheet', csv: 'sheet', ods: 'sheet',
  ppt: 'slides', pptx: 'slides', key: 'slides',
  jpg: 'image', jpeg: 'image', png: 'image', gif: 'image', webp: 'image', svg: 'image',
  mp4: 'video', mov: 'video', mkv: 'video', webm: 'video',
  mp3: 'audio', wav: 'audio', flac: 'audio',
  zip: 'archive', gz: 'archive', tar: 'archive', rar: 'archive', '7z': 'archive',
  js: 'code', ts: 'code', css: 'code', html: 'code', py: 'code', sh: 'code', go: 'code', rs: 'code',
  yml: 'config', yaml: 'config', toml: 'config', env: 'config', conf: 'config', ini: 'config', json: 'config',
  iso: 'disc', img: 'disc',
  fig: 'design', sketch: 'design',
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

export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'text' | 'epub' | 'none'

/** How a file can be previewed inline in the browser. */
export function previewKind(name: string): PreviewKind {
  if (/\.epub$/i.test(String(name))) return 'epub'
  const k = kindFromName(name)
  if (k === 'image' || k === 'video' || k === 'audio' || k === 'pdf') return k
  if (k === 'text' || k === 'code' || k === 'config') return 'text'
  return 'none'
}

/** Whether the file's text content can be edited in the browser. */
export function isMarkdown(name: string): boolean {
  return /\.(md|markdown)$/i.test(String(name))
}

/** Whether the file is an Office document editable via WOPI. */
export function isOffice(name: string): boolean {
  return /\.(docx?|xlsx?|pptx?|odt|ods|odp)$/i.test(String(name))
}

// Extensions the backend can generate a thumbnail for (image built-in / vips /
// libraw / ffmpeg / poppler / libreoffice). A 404 falls back to the type icon.
const THUMB_EXT =
  /\.(jpe?g|png|gif|webp|tiff?|bmp|heic|heif|avif|jxl|jp2|jpx|cr2|cr3|nef|nrw|arw|sr2|srf|dng|raf|orf|rw2|pef|srw|k25|kdc|dcr|mrw|x3f|3fr|mef|iiq|mos|raw|mp4|mov|webm|mkv|m4v|avi|mp3|flac|m4a|aac|ogg|opus|pdf|docx?|odt|rtf|xlsx?|ods|pptx?|odp)$/i

/** Whether OrionDrive may have a thumbnail for this file. */
export function canThumbnail(name: string): boolean {
  return THUMB_EXT.test(String(name))
}

/** Human-readable size, French style (comma decimal, narrow no-break space). */
export function fmtSize(bytes: number): string {
  if (!bytes) return '—'
  const units = ['o', 'Ko', 'Mo', 'Go', 'To']
  let v = bytes
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i += 1
  }
  const s =
    v >= 100 || i === 0
      ? Math.round(v).toString()
      : v.toFixed(1).replace('.', ',').replace(',0', '')
  return s + ' ' + units[i]
}

const MONTHS = ['janv.', 'févr.', 'mars', 'avr.', 'mai', 'juin', 'juil.', 'août', 'sept.', 'oct.', 'nov.', 'déc.']

export function fmtDate(iso: string): string {
  const d = new Date(iso)
  const diff = Date.now() - d.getTime()
  const min = Math.floor(diff / 60000)
  if (min < 1) return 'à l’instant'
  if (min < 60) return 'il y a ' + min + ' min'
  const h = Math.floor(min / 60)
  if (h < 24) return 'il y a ' + h + ' h'
  const j = Math.floor(h / 24)
  if (j < 7) return 'il y a ' + j + ' j'
  return d.getDate() + ' ' + MONTHS[d.getMonth()] + ' ' + d.getFullYear()
}
