import { beforeEach, describe, expect, it, vi } from 'vitest'
import { loadRecents, pushRecent, removeRecent, type RecentSearch } from '@/lib/recentSearches'
import { emptyFilters } from '@/stores/files'

const r = (over: Partial<RecentSearch>): RecentSearch => ({ ...emptyFilters(), q: '', ...over })

describe('recent searches', () => {
  // Node 26 ships its own (unconfigured, undefined) localStorage global that
  // shadows jsdom's, so each test gets a fresh in-memory one.
  beforeEach(() => {
    const data = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
    })
  })

  it('starts empty and survives corrupt storage', () => {
    expect(loadRecents()).toEqual([])
    localStorage.setItem('od-recent-searches', '{not json')
    expect(loadRecents()).toEqual([])
  })

  it('puts the newest first and dedupes case-insensitively', () => {
    pushRecent(r({ q: 'rapport' }))
    pushRecent(r({ q: 'facture' }))
    const list = pushRecent(r({ q: 'Rapport ' }))
    expect(list.map((x) => x.q)).toEqual(['Rapport', 'facture'])
  })

  it('keeps the same text with different filters as separate entries', () => {
    pushRecent(r({ q: 'plan' }))
    const list = pushRecent(r({ q: 'plan', kind: 'images' }))
    expect(list).toHaveLength(2)
  })

  it('ignores an empty search and caps the list at five', () => {
    expect(pushRecent(r({}))).toEqual([])
    for (const q of ['a', 'b', 'c', 'd', 'e', 'f']) pushRecent(r({ q }))
    expect(loadRecents().map((x) => x.q)).toEqual(['f', 'e', 'd', 'c', 'b'])
  })

  it('keeps custom sizes and fills fields missing from older entries', () => {
    pushRecent(r({ q: 'x', minSize: 5e6 }))
    expect(loadRecents()[0].minSize).toBe(5e6)
    localStorage.setItem('od-recent-searches', JSON.stringify([{ q: 'old', kind: 'images' }]))
    expect(loadRecents()[0]).toEqual(r({ q: 'old', kind: 'images' }))
  })

  it('removes an entry by index', () => {
    pushRecent(r({ q: 'a' }))
    pushRecent(r({ q: 'b' }))
    expect(removeRecent(0).map((x) => x.q)).toEqual(['a'])
  })
})
