import { emptyFilters, type SearchFilters, type SearchKind, type SearchSince } from '@/stores/files'

// Search operators typed in the omnibar ("type:image", "taille:>5Mo", ...) and
// the filter tokens they turn into. Parsing accepts French and English words,
// case- and accent-insensitively; suggestions and edits use the UI locale's.

export type Kind = Exclude<SearchKind, ''>
export type Since = Exclude<SearchSince, ''>

export type Token =
  | { key: 'type'; value: 'folder' | 'file' }
  | { key: 'kind'; value: Kind }
  | { key: 'since'; value: Since }
  | { key: 'range'; after: string; before: string } // yyyy-mm-dd, '' = open end
  | { key: 'size'; min: number; max: number } // bytes, 0 = open end
  | { key: 'starred' }

// Operator families. "type" and "kind" share one slot, as do the two date forms:
// adding a token replaces any other token of its slot.
export type Op = 'type' | 'date' | 'size' | 'is'
export const slotOf = (tk: Token): Op =>
  tk.key === 'type' || tk.key === 'kind' ? 'type'
    : tk.key === 'since' || tk.key === 'range' ? 'date'
      : tk.key === 'size' ? 'size' : 'is'

export const sameToken = (a: Token, b: Token) => JSON.stringify(a) === JSON.stringify(b)

// Replace the token's slot, or remove the token when it is already present.
export function toggleToken(list: Token[], tk: Token): Token[] {
  if (list.some((x) => sameToken(x, tk))) return list.filter((x) => !sameToken(x, tk))
  return withToken(list, tk)
}
export const withToken = (list: Token[], tk: Token): Token[] => [...list.filter((x) => slotOf(x) !== slotOf(tk)), tk]

/* Vocabulary */

export interface Vocab {
  ops: Record<Op, string>
  folder: string
  file: string
  kinds: Record<Kind, string>
  since: Record<Since, string>
  starred: string
  units: [string, string, string] // kilo, mega, giga
}

export const VOCAB: Record<'fr' | 'en', Vocab> = {
  fr: {
    ops: { type: 'type', date: 'modifié', size: 'taille', is: 'est' },
    folder: 'dossier',
    file: 'fichier',
    kinds: { documents: 'document', images: 'image', media: 'vidéo', archives: 'archive' },
    since: { '1d': '24h', '7d': '7j', '30d': '30j', '365d': '1an' },
    starred: 'favori',
    units: ['Ko', 'Mo', 'Go'],
  },
  en: {
    ops: { type: 'type', date: 'modified', size: 'size', is: 'is' },
    folder: 'folder',
    file: 'file',
    kinds: { documents: 'document', images: 'image', media: 'video', archives: 'archive' },
    since: { '1d': '24h', '7d': '7d', '30d': '30d', '365d': '1y' },
    starred: 'starred',
    units: ['KB', 'MB', 'GB'],
  },
}

// Lowercase and strip accents, so "Vidéo", "video" and "VIDEO" all match.
const norm = (s: string) => s.toLowerCase().normalize('NFD').replace(/\p{Diacritic}/gu, '')

const OP_ALIASES: Record<Op, string[]> = {
  type: ['type', 'kind'],
  date: ['modifie', 'modified', 'date'],
  size: ['taille', 'size'],
  is: ['est', 'is'],
}
const TYPE_ALIASES: [Token, string[]][] = [
  [{ key: 'type', value: 'folder' }, ['dossier', 'dossiers', 'folder', 'folders']],
  [{ key: 'type', value: 'file' }, ['fichier', 'fichiers', 'file', 'files']],
  [{ key: 'kind', value: 'documents' }, ['document', 'documents', 'doc', 'docs']],
  [{ key: 'kind', value: 'images' }, ['image', 'images', 'photo', 'photos', 'img']],
  [{ key: 'kind', value: 'media' }, ['video', 'videos', 'media', 'medias', 'audio', 'musique', 'music']],
  [{ key: 'kind', value: 'archives' }, ['archive', 'archives', 'zip']],
]
const SINCE_ALIASES: Record<Since, string[]> = {
  '1d': ['24h', '1j', '1d', 'jour', 'day', 'today'],
  '7d': ['7j', '7d', 'semaine', 'week', '1w'],
  '30d': ['30j', '30d', 'mois', 'month', '1m'],
  '365d': ['1an', '365j', '365d', 'an', 'annee', 'year', '1y'],
}
const STARRED_ALIASES = ['favori', 'favoris', 'fav', 'starred', 'star']

export function opOf(key: string): Op | null {
  const k = norm(key)
  for (const op of Object.keys(OP_ALIASES) as Op[]) if (OP_ALIASES[op].includes(k)) return op
  return null
}

/* Values */

const DATE = /^\d{4}-\d{2}-\d{2}$/
const isDate = (s: string) => DATE.test(s) && !Number.isNaN(Date.parse(s + 'T00:00:00'))

function parseDate(v: string): Token | null {
  const range = v.split('..')
  if (range.length === 2) {
    const [a, b] = range
    if ((a && !isDate(a)) || (b && !isDate(b)) || (!a && !b)) return null
    return { key: 'range', after: a, before: b }
  }
  const m = /^([<>])=?(.+)$/.exec(v)
  if (m) {
    if (!isDate(m[2])) return null
    return m[1] === '>' ? { key: 'range', after: m[2], before: '' } : { key: 'range', after: '', before: m[2] }
  }
  return isDate(v) ? { key: 'range', after: v, before: v } : null // a single day
}

// SI units, matching fmtSize. A bare number is in megabytes.
const UNIT: Record<string, number> = {
  '': 1e6, o: 1, b: 1, k: 1e3, ko: 1e3, kb: 1e3, m: 1e6, mo: 1e6, mb: 1e6, g: 1e9, go: 1e9, gb: 1e9,
}
const NUM = '(\\d+(?:[.,]\\d+)?)([a-z]*)'
function bytes(n: string, unit: string): number | null {
  const mult = UNIT[unit]
  if (mult === undefined) return null
  return Math.round(parseFloat(n.replace(',', '.')) * mult)
}

function parseSize(v: string): Token | null {
  const s = norm(v)
  const range = new RegExp(`^${NUM}-${NUM}$`).exec(s)
  if (range) {
    // "1-100Mo": the first bound borrows the second's unit when it has none.
    const min = bytes(range[1], range[2] || range[4])
    const max = bytes(range[3], range[4])
    return min !== null && max !== null && max > min ? { key: 'size', min, max } : null
  }
  const one = new RegExp(`^([<>])=?${NUM}$`).exec(s)
  if (!one) return null
  const b = bytes(one[2], one[3])
  if (!b) return null
  return one[1] === '<' ? { key: 'size', min: 0, max: b } : { key: 'size', min: b, max: 0 }
}

// Parse one "operator:value" word into a token, or null when it isn't one.
export function parseWord(word: string): Token | null {
  const i = word.indexOf(':')
  if (i <= 0) return null
  const op = opOf(word.slice(0, i))
  const raw = word.slice(i + 1)
  const v = norm(raw)
  if (!op || !v) return null
  if (op === 'type') return TYPE_ALIASES.find(([, names]) => names.includes(v))?.[0] ?? null
  if (op === 'is') return STARRED_ALIASES.includes(v) ? { key: 'starred' } : null
  if (op === 'size') return parseSize(raw)
  const since = (Object.keys(SINCE_ALIASES) as Since[]).find((k) => SINCE_ALIASES[k].includes(v))
  return since ? { key: 'since', value: since } : parseDate(raw)
}

// Split typed text into its operator tokens and the remaining free text. Words
// naming a known operator with a value that doesn't parse are dropped from the
// text (they'd otherwise be searched as file names) and reported as invalid.
export function splitQuery(text: string): { q: string; tokens: Token[]; invalid: string[] } {
  const words: string[] = []
  const invalid: string[] = []
  let tokens: Token[] = []
  for (const w of text.split(/\s+/).filter(Boolean)) {
    const tk = parseWord(w)
    if (tk) tokens = withToken(tokens, tk)
    else if (w.includes(':') && opOf(w.slice(0, w.indexOf(':')))) invalid.push(w)
    else words.push(w)
  }
  return { q: words.join(' '), tokens, invalid }
}

/* Tokens ⇄ filters */

export function tokensToFilters(list: Token[]): SearchFilters {
  const f = emptyFilters()
  for (const tk of list) {
    if (tk.key === 'type') f.type = tk.value
    else if (tk.key === 'kind') f.kind = tk.value
    else if (tk.key === 'since') f.since = tk.value
    else if (tk.key === 'range') { f.after = tk.after; f.before = tk.before }
    else if (tk.key === 'size') { f.minSize = tk.min; f.maxSize = tk.max }
    else f.starred = true
  }
  return f
}

export function filtersToTokens(f: SearchFilters): Token[] {
  const out: Token[] = []
  if (f.kind) out.push({ key: 'kind', value: f.kind })
  else if (f.type) out.push({ key: 'type', value: f.type })
  if (f.since) out.push({ key: 'since', value: f.since })
  else if (f.after || f.before) out.push({ key: 'range', after: f.after, before: f.before })
  if (f.minSize || f.maxSize) out.push({ key: 'size', min: f.minSize, max: f.maxSize })
  if (f.starred) out.push({ key: 'starred' })
  return out
}

/* Back to syntax */

function sizeText(b: number, vocab: Vocab): string {
  const [k, m, g] = vocab.units
  const fmt = (v: number) => String(Math.round(v * 10) / 10)
  if (b >= 1e9) return fmt(b / 1e9) + g
  if (b >= 1e6) return fmt(b / 1e6) + m
  return fmt(b / 1e3) + k
}

// The typed form of a token, in the locale's words (used to edit a token).
export function toSyntax(tk: Token, vocab: Vocab): string {
  const { ops } = vocab
  if (tk.key === 'type') return `${ops.type}:${tk.value === 'folder' ? vocab.folder : vocab.file}`
  if (tk.key === 'kind') return `${ops.type}:${vocab.kinds[tk.value]}`
  if (tk.key === 'since') return `${ops.date}:${vocab.since[tk.value]}`
  if (tk.key === 'starred') return `${ops.is}:${vocab.starred}`
  if (tk.key === 'range') {
    const v = tk.after && tk.after === tk.before ? tk.after
      : tk.after && tk.before ? `${tk.after}..${tk.before}`
        : tk.after ? `>${tk.after}` : `<${tk.before}`
    return `${ops.date}:${v}`
  }
  const v = tk.min && tk.max ? `${sizeText(tk.min, vocab)}-${sizeText(tk.max, vocab)}`
    : tk.max ? `<${sizeText(tk.max, vocab)}` : `>${sizeText(tk.min, vocab)}`
  return `${ops.size}:${v}`
}

/* Autocompletion */

export interface Suggestion {
  syntax: string // text replacing the word being typed
  token: Token | null // the token it makes, or null for an operator prefix ("taille:")
  op: Op
}

// Suggestions for the word being typed: operator names while typing a key,
// then values once the colon is in. `hint` names the operator whose free-form
// values (dates, sizes) deserve a format reminder.
export function suggest(word: string, vocab: Vocab): { items: Suggestion[]; hint: Op | null } {
  const i = word.indexOf(':')
  if (i < 0) {
    const w = norm(word)
    if (w.length < 2) return { items: [], hint: null }
    const items = (Object.keys(vocab.ops) as Op[])
      .filter((op) => norm(vocab.ops[op]).startsWith(w) || OP_ALIASES[op].some((a) => a.startsWith(w)))
      .map((op): Suggestion => op === 'is'
        ? { syntax: `${vocab.ops.is}:${vocab.starred}`, token: { key: 'starred' }, op }
        : { syntax: `${vocab.ops[op]}:`, token: null, op })
    return { items, hint: null }
  }
  const op = opOf(word.slice(0, i))
  if (!op) return { items: [], hint: null }
  const typed = word.slice(i + 1)
  const presets: Token[] =
    op === 'type' ? TYPE_ALIASES.map(([tk]) => tk)
      : op === 'date' ? (Object.keys(SINCE_ALIASES) as Since[]).map((value) => ({ key: 'since', value }))
        : op === 'size' ? [{ key: 'size', min: 0, max: 1e6 }, { key: 'size', min: 1e6, max: 1e8 }, { key: 'size', min: 1e8, max: 0 }]
          : [{ key: 'starred' }]
  const items: Suggestion[] = presets
    .map((token) => ({ syntax: toSyntax(token, vocab), token, op }))
    .filter((s) => norm(s.syntax.slice(s.syntax.indexOf(':') + 1)).startsWith(norm(typed)))
  // A complete custom value ("taille:>5Mo") is offered as-is, first.
  const exact = parseWord(word)
  if (exact && !items.some((s) => sameToken(s.token!, exact))) items.unshift({ syntax: toSyntax(exact, vocab), token: exact, op })
  return { items, hint: op === 'date' || op === 'size' ? op : null }
}
