import { emptyFilters, filtersActive, type SearchFilters } from '@/stores/files'

// A search the user ran from the omnibar: the text plus every filter it
// carried. Kept in this browser only (localStorage), newest first.
export type RecentSearch = SearchFilters & { q: string }

const KEY = 'od-recent-searches'
const MAX = 5

const sameSearch = (a: RecentSearch, b: RecentSearch) =>
  JSON.stringify({ ...a, q: a.q.toLowerCase() }) === JSON.stringify({ ...b, q: b.q.toLowerCase() })

// Fill in fields an older entry may lack, in a fixed key order (sameSearch
// compares serialised entries).
const normalize = (r: Partial<RecentSearch>): RecentSearch => {
  const f = emptyFilters()
  for (const k of Object.keys(f) as (keyof SearchFilters)[]) if (r[k] !== undefined) Object.assign(f, { [k]: r[k] })
  return { ...f, q: String(r.q ?? '') }
}

// Storage can be missing or throw (private mode, blocked site data): every
// access falls back to an empty list rather than breaking the search box.
export function loadRecents(): RecentSearch[] {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '[]')
    return Array.isArray(raw) ? raw.slice(0, MAX).map(normalize) : []
  } catch {
    return []
  }
}

function save(list: RecentSearch[]): RecentSearch[] {
  try {
    localStorage.setItem(KEY, JSON.stringify(list))
  } catch {
    // Not persisted; the in-memory list is still returned.
  }
  return list
}

// Record a search at the top of the list, dropping an older identical entry.
export function pushRecent(r: RecentSearch): RecentSearch[] {
  if (!r.q.trim() && !filtersActive(r)) return loadRecents()
  const entry = normalize({ ...r, q: r.q.trim() })
  return save([entry, ...loadRecents().filter((x) => !sameSearch(x, entry))].slice(0, MAX))
}

export function removeRecent(index: number): RecentSearch[] {
  return save(loadRecents().filter((_, i) => i !== index))
}
