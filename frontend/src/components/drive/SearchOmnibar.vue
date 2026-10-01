<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Search, X, Folder, FileText, Image, Film, Archive, Clock, Star, History, CornerDownLeft, Wand2, AlertCircle,
} from 'lucide-vue-next'
import { useFilesStore, buildSearchParams, filtersActive, type SearchFilters } from '@/stores/files'
import { api, type FileNode } from '@/lib/api'
import { metaFor } from '@/lib/icons'
import { fmtDate, fmtSize, kindFromName } from '@/lib/format'
import { loadRecents, pushRecent, removeRecent, type RecentSearch } from '@/lib/recentSearches'
import {
  VOCAB, filtersToTokens, parseWord, sameToken, splitQuery, suggest, toSyntax, toggleToken,
  tokensToFilters, withToken, type Op, type Suggestion, type Token,
} from '@/lib/searchSyntax'

const { t, locale } = useI18n()
const files = useFilesStore()
const vocab = computed(() => VOCAB[locale.value.startsWith('en') ? 'en' : 'fr'])

const QUICK: { token: Token; icon: typeof Folder }[] = [
  { token: { key: 'type', value: 'folder' }, icon: Folder },
  { token: { key: 'kind', value: 'documents' }, icon: FileText },
  { token: { key: 'kind', value: 'images' }, icon: Image },
  { token: { key: 'kind', value: 'media' }, icon: Film },
  { token: { key: 'kind', value: 'archives' }, icon: Archive },
  { token: { key: 'since', value: '7d' }, icon: Clock },
  { token: { key: 'starred' }, icon: Star },
]
const OPS: Op[] = ['type', 'date', 'size', 'is']
const PREVIEW_SIZE = 6

const input = ref<HTMLInputElement>()
const text = ref('')
const tokens = ref<Token[]>([])
const open = ref(false)
const hits = ref<FileNode[]>([])
const loading = ref(false)
const active = ref(-1) // keyboard-highlighted row; -1 = none, so Enter runs the search
const recents = ref<RecentSearch[]>(loadRecents())

/* Token labels */

const fmtDay = (d: string) =>
  new Date(d + 'T00:00:00').toLocaleDateString(locale.value, { day: 'numeric', month: 'short', year: 'numeric' })

// A token's chip: an operator name (dimmed) and its value.
function tokenParts(tk: Token): { key: string; value: string } {
  if (tk.key === 'type') return { key: t('search.tokType'), value: t(tk.value === 'folder' ? 'shell.folders' : 'shell.files') }
  if (tk.key === 'kind') return { key: t('search.tokType'), value: t('storage.cat.' + tk.value) }
  if (tk.key === 'since') return { key: t('search.tokDate'), value: t('search.since' + tk.value) }
  if (tk.key === 'range') {
    const value = tk.after && tk.after === tk.before ? fmtDay(tk.after)
      : tk.after && tk.before ? `${fmtDay(tk.after)} → ${fmtDay(tk.before)}`
        : tk.after ? `≥ ${fmtDay(tk.after)}` : `≤ ${fmtDay(tk.before)}`
    return { key: t('search.tokDate'), value }
  }
  if (tk.key === 'size') {
    const value = tk.min && tk.max ? `${fmtSize(tk.min)} – ${fmtSize(tk.max)}`
      : tk.max ? `< ${fmtSize(tk.max)}` : `> ${fmtSize(tk.min)}`
    return { key: t('search.tokSize'), value }
  }
  return { key: '', value: t('search.starredOnly') }
}
const tokenText = (tk: Token) => {
  const p = tokenParts(tk)
  return p.key ? `${p.key} ${p.value}` : p.value
}

/* Draft: committed tokens plus operators still sitting in the typed text */

// The box mirrors the committed search whenever it is not being edited, so the
// results page shows its text and tokens, and leaving search empties it.
function syncFromStore() {
  text.value = files.q
  tokens.value = filtersToTokens(files.filters)
}
syncFromStore()
watch(() => [files.q, files.filters], () => { if (!open.value) syncFromStore() }, { deep: true })

const parsed = computed(() => splitQuery(text.value))
const draftFilters = computed<SearchFilters>(() => tokensToFilters(parsed.value.tokens.reduce(withToken, tokens.value)))
const hasDraft = computed(() => parsed.value.q.length > 0 || filtersActive(draftFilters.value))

// The word under the caret (the end of the box): what autocompletion works on.
const currentWord = computed(() => (/\s$/.test(text.value) ? '' : text.value.split(/\s+/).pop() || ''))
const suggestions = computed(() => (currentWord.value ? suggest(currentWord.value, vocab.value) : { items: [], hint: null }))
// Operator words that won't parse, once the user has moved past them.
const invalid = computed(() => parsed.value.invalid.filter((w) => w !== currentWord.value))
const showSugg = computed(() => suggestions.value.items.length > 0 || !!suggestions.value.hint || invalid.value.length > 0)

// Live preview: a small page of hits, debounced, ignoring stale responses.
let timer: number | undefined
let seq = 0
watch([() => parsed.value.q, draftFilters, open], () => {
  clearTimeout(timer)
  if (!open.value) return
  if (!hasDraft.value) {
    hits.value = []
    loading.value = false
    return
  }
  loading.value = true
  timer = window.setTimeout(async () => {
    const mine = ++seq
    try {
      const page = await api.search({ ...buildSearchParams(parsed.value.q, draftFilters.value), per_page: String(PREVIEW_SIZE) })
      if (mine === seq) hits.value = page.items
    } catch {
      if (mine === seq) hits.value = []
    } finally {
      if (mine === seq) loading.value = false
    }
  }, 150)
}, { deep: true })
watch([text, tokens], () => { active.value = -1 }, { deep: true })

/* Rows (keyboard-navigable, in display order) */

type Row =
  | { kind: 'sugg'; s: Suggestion }
  | { kind: 'hit'; node: FileNode }
  | { kind: 'all' }
  | { kind: 'recent'; recent: RecentSearch; index: number }
const rows = computed<Row[]>(() => {
  const sugg = suggestions.value.items.map((s) => ({ kind: 'sugg' as const, s }))
  if (!hasDraft.value && !text.value.trim()) return recents.value.map((recent, index) => ({ kind: 'recent' as const, recent, index }))
  return [...sugg, ...hits.value.map((node) => ({ kind: 'hit' as const, node })), { kind: 'all' as const }]
})
const rowIndex = (pred: (r: Row) => boolean) => rows.value.findIndex(pred)
const suggIndex = (s: Suggestion) => rowIndex((r) => r.kind === 'sugg' && r.s === s)
const hitIndex = (n: FileNode) => rowIndex((r) => r.kind === 'hit' && r.node === n)
const allIndex = computed(() => rowIndex((r) => r.kind === 'all'))

/* Editing */

function focus() {
  input.value?.focus()
}
// Replace the word being typed: by a token, or by an operator prefix to go on.
function applySuggestion(s: Suggestion) {
  const base = text.value.slice(0, text.value.length - currentWord.value.length)
  if (s.token) {
    text.value = base
    tokens.value = withToken(tokens.value, s.token)
  } else {
    text.value = base + s.syntax
  }
  focus()
}
// A finished "operator:value" word followed by a space becomes a token.
function onInput() {
  const m = /(\S+)\s$/.exec(text.value)
  const tk = m && parseWord(m[1])
  if (m && tk) {
    text.value = text.value.slice(0, m.index)
    tokens.value = withToken(tokens.value, tk)
  }
}
function insertOp(op: Op) {
  if (op === 'is') {
    tokens.value = withToken(tokens.value, { key: 'starred' })
  } else {
    const sep = text.value && !/\s$/.test(text.value) ? ' ' : ''
    text.value += sep + vocab.value.ops[op] + ':'
  }
  focus()
}
function toggleQuick(tk: Token) {
  tokens.value = toggleToken(tokens.value, tk)
  focus()
}
const isOn = (tk: Token) => tokens.value.some((x) => sameToken(x, tk))
function removeToken(i: number) {
  tokens.value = tokens.value.filter((_, j) => j !== i)
  focus()
}
// Turn a token back into its typed form, at the end of the box, to edit it.
function editToken(i: number) {
  const tk = tokens.value[i]
  tokens.value = tokens.value.filter((_, j) => j !== i)
  const sep = text.value && !/\s$/.test(text.value) ? ' ' : ''
  text.value += sep + toSyntax(tk, vocab.value)
  focus()
}

/* Actions */

function close() {
  open.value = false
  active.value = -1
}
function remember(q: string, f: SearchFilters) {
  recents.value = pushRecent({ ...f, q })
}

// Show the full results page for the draft, or leave search when it is empty.
function commit() {
  const { q, tokens: typed } = splitQuery(text.value)
  const filters = tokensToFilters(typed.reduce(withToken, tokens.value))
  close()
  if (!q && !filtersActive(filters)) {
    files.clearSearch()
  } else {
    remember(q, filters)
    files.commitSearch(q, filters)
  }
  // Blur last: it resyncs the box from the store, which now holds this search.
  input.value?.blur()
}
function openHit(node: FileNode) {
  remember(parsed.value.q, draftFilters.value)
  close()
  input.value?.blur()
  // Rebuilds the breadcrumb to the item: enters a folder, or opens a file's
  // preview inside its parent folder.
  files.restoreNav(node.id, node.type === 'file')
}
function runRecent(r: RecentSearch) {
  text.value = r.q
  tokens.value = filtersToTokens(r)
  commit()
}
function forget(index: number) {
  recents.value = removeRecent(index)
  active.value = -1
}
function clearAll() {
  text.value = ''
  tokens.value = []
  if (files.searching) files.clearSearch()
  focus()
}

function activate(row: Row) {
  if (row.kind === 'sugg') applySuggestion(row.s)
  else if (row.kind === 'hit') openHit(row.node)
  else if (row.kind === 'recent') runRecent(row.recent)
  else commit()
}

function onKey(e: KeyboardEvent) {
  const n = rows.value.length
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    if (!open.value) { open.value = true; return }
    if (!n) return
    const step = e.key === 'ArrowDown' ? 1 : -1
    active.value = active.value < 0 ? (step > 0 ? 0 : n - 1) : (active.value + step + n) % n
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const row = rows.value[active.value]
    if (e.shiftKey || !row) commit()
    else activate(row)
  } else if (e.key === 'Tab' && open.value && suggestions.value.items.length) {
    // Tab completes the highlighted suggestion, or the first one.
    e.preventDefault()
    const row = rows.value[active.value]
    applySuggestion(row?.kind === 'sugg' ? row.s : suggestions.value.items[0])
  } else if (e.key === 'Escape') {
    // Handled here so the shell's global Escape (clear selection…) doesn't fire.
    e.stopPropagation()
    if (open.value) close()
    else input.value?.blur()
  } else if (e.key === 'Backspace' && !text.value && tokens.value.length) {
    e.preventDefault()
    editToken(tokens.value.length - 1)
  } else if (e.key !== 'Tab') {
    open.value = true
  }
}

function onBlur() {
  close()
  // Abandoned edits fall back to the committed search.
  syncFromStore()
}

/* Display helpers */

// Split a name around the first match of the query for highlighting.
function parts(name: string) {
  const q = parsed.value.q.toLowerCase()
  const i = q ? name.toLowerCase().indexOf(q) : -1
  if (i < 0) return { pre: name, match: '', post: '' }
  return { pre: name.slice(0, i), match: name.slice(i, i + q.length), post: name.slice(i + q.length) }
}
const metaOf = (n: FileNode) => metaFor(n.type === 'folder' ? 'folder' : kindFromName(n.name))
const locationOf = (n: FileNode) => t('shell.myDrive') + (n.location ? ' / ' + n.location : '')
const suggDesc = (s: Suggestion) => (s.token ? tokenText(s.token) : t('search.op_' + s.op))
const opLabel = (op: Op) => (op === 'is' ? `${vocab.value.ops.is}:${vocab.value.starred}` : vocab.value.ops[op] + ':')

defineExpose({ focus })
</script>

<template>
  <div class="omni" :class="{ open }">
    <label class="searchbox">
      <Search :size="16" />
      <span v-for="(tk, i) in tokens" :key="JSON.stringify(tk)" class="omni-token">
        <button
          type="button"
          tabindex="-1"
          class="omni-token-edit"
          :title="t('search.editToken', { label: tokenText(tk) })"
          @mousedown.prevent
          @click="editToken(i)"
        >
          <span v-if="tokenParts(tk).key" class="omni-token-key">{{ tokenParts(tk).key }}</span>{{ tokenParts(tk).value }}
        </button>
        <button
          type="button"
          tabindex="-1"
          :aria-label="t('search.removeToken', { label: tokenText(tk) })"
          @mousedown.prevent
          @click="removeToken(i)"
        >
          <X :size="12" />
        </button>
      </span>
      <input
        ref="input"
        v-model="text"
        type="search"
        role="combobox"
        aria-autocomplete="list"
        aria-controls="omni-list"
        :aria-expanded="open"
        :aria-activedescendant="active >= 0 ? 'omni-row-' + active : undefined"
        :placeholder="tokens.length ? '' : t('search.omniPlaceholder')"
        :aria-label="t('common.search')"
        spellcheck="false"
        autocomplete="off"
        @focus="open = true"
        @blur="onBlur"
        @input="onInput"
        @keydown="onKey"
      />
      <button
        v-if="hasDraft || text || files.searching"
        type="button"
        class="omni-clear"
        :aria-label="t('search.clearSearch')"
        @mousedown.prevent
        @click="clearAll"
      >
        <X :size="14" />
      </button>
      <kbd v-else>/</kbd>
    </label>

    <!-- mousedown.prevent keeps focus in the input, so clicks don't close the panel -->
    <div v-if="open" class="omni-panel" @mousedown.prevent>
      <ul id="omni-list" class="omni-list" role="listbox">
        <!-- Autocompletion of the word being typed -->
        <template v-if="showSugg">
          <li class="omni-sec" role="presentation">{{ t('search.omniSuggest') }}</li>
          <li
            v-for="s in suggestions.items"
            :id="'omni-row-' + suggIndex(s)"
            :key="s.syntax"
            class="omni-row omni-sugg"
            :class="{ active: active === suggIndex(s) }"
            role="option"
            :aria-selected="active === suggIndex(s)"
            @mouseenter="active = suggIndex(s)"
            @click="applySuggestion(s)"
          >
            <Wand2 :size="16" />
            <code class="omni-syntax">{{ s.syntax }}</code>
            <span class="omni-main omni-desc">{{ suggDesc(s) }}</span>
            <kbd v-if="active === suggIndex(s) || (active < 0 && suggIndex(s) === 0)">Tab</kbd>
          </li>
          <li v-if="suggestions.hint" class="omni-hint" role="presentation">
            {{ t(suggestions.hint === 'date' ? 'search.hintDate' : 'search.hintSize') }}
          </li>
          <li v-for="w in invalid" :key="w" class="omni-hint omni-bad" role="presentation">
            <AlertCircle :size="14" />{{ t('search.badValue', { w }) }}
          </li>
        </template>

        <template v-if="rows.some((r) => r.kind === 'all')">
          <li class="omni-sec" :class="{ 'omni-sep': showSugg }" role="presentation">{{ t('search.omniBest') }}</li>
          <li
            v-for="node in hits"
            :id="'omni-row-' + hitIndex(node)"
            :key="node.id"
            class="omni-row"
            :class="{ active: active === hitIndex(node) }"
            role="option"
            :aria-selected="active === hitIndex(node)"
            @mouseenter="active = hitIndex(node)"
            @click="openHit(node)"
          >
            <span class="omni-ico"><component :is="metaOf(node).icon" :size="16" :class="'tint-' + metaOf(node).tint" /></span>
            <span class="omni-main">
              <span class="omni-name">{{ parts(node.name).pre }}<mark>{{ parts(node.name).match }}</mark>{{ parts(node.name).post }}</span>
              <span class="omni-path">{{ locationOf(node) }}</span>
            </span>
            <span class="omni-meta">{{ node.type === 'folder' ? t('kinds.folder') : fmtDate(node.modified) }}</span>
          </li>
          <li v-if="!hits.length" class="omni-empty" role="presentation">
            {{ loading ? t('search.searching') : hasDraft ? t('search.omniNoHits') : t('search.omniTypeMore') }}
          </li>
          <li
            :id="'omni-row-' + allIndex"
            class="omni-row omni-all"
            :class="{ active: active === allIndex }"
            role="option"
            :aria-selected="active === allIndex"
            @mouseenter="active = allIndex"
            @click="commit"
          >
            <Search :size="16" />
            <span class="omni-main">{{ parsed.q ? t('search.seeAll', { q: parsed.q }) : t('search.seeAllFiltered') }}</span>
            <kbd>⇧ ↵</kbd>
          </li>
        </template>

        <template v-else-if="recents.length">
          <li class="omni-sec" role="presentation">{{ t('search.omniRecent') }}</li>
          <li
            v-for="(r, i) in recents"
            :id="'omni-row-' + i"
            :key="i"
            class="omni-row"
            :class="{ active: active === i }"
            role="option"
            :aria-selected="active === i"
            @mouseenter="active = i"
            @click="runRecent(r)"
          >
            <History :size="16" />
            <span class="omni-main omni-name">{{ r.q }}</span>
            <span v-for="tk in filtersToTokens(r)" :key="JSON.stringify(tk)" class="omni-token small">{{ tokenText(tk) }}</span>
            <button type="button" class="omni-forget" :aria-label="t('search.removeRecent')" @click.stop="forget(i)">
              <X :size="14" />
            </button>
          </li>
        </template>
      </ul>

      <!-- Quick filters, one click each -->
      <div class="omni-sec omni-sep">{{ t(hasDraft ? 'search.omniRefine' : 'search.omniBrowse') }}</div>
      <div class="omni-quick">
        <button
          v-for="qf in QUICK"
          :key="JSON.stringify(qf.token)"
          type="button"
          class="omni-chip"
          :aria-pressed="isOn(qf.token)"
          @click="toggleQuick(qf.token)"
        >
          <component :is="qf.icon" :size="14" />{{ tokenParts(qf.token).value }}
        </button>
      </div>

      <div class="omni-foot">
        <span class="omni-ops">
          {{ t('search.omniOperators') }}
          <button v-for="op in OPS" :key="op" type="button" class="omni-op" @click="insertOp(op)">{{ opLabel(op) }}</button>
        </span>
        <span class="omni-keys">
          <span><kbd>↑</kbd> <kbd>↓</kbd> {{ t('search.hintNav') }}</span>
          <span><kbd>Tab</kbd> {{ t('search.hintComplete') }}</span>
          <span><kbd><CornerDownLeft :size="10" /></kbd> {{ t('search.hintOpen') }}</span>
          <span><kbd>Esc</kbd> {{ t('search.hintClose') }}</span>
        </span>
      </div>
    </div>
  </div>
</template>
