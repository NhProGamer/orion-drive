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
