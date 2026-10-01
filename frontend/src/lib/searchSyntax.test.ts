import { describe, expect, it } from 'vitest'
import {
  VOCAB, filtersToTokens, parseWord, splitQuery, suggest, toSyntax, toggleToken, tokensToFilters, type Token,
} from '@/lib/searchSyntax'
import { emptyFilters } from '@/stores/files'

const fr = VOCAB.fr

describe('parseWord', () => {
  it('reads types in French and English, ignoring case and accents', () => {
    expect(parseWord('type:Vidéo')).toEqual({ key: 'kind', value: 'media' })
    expect(parseWord('kind:video')).toEqual({ key: 'kind', value: 'media' })
    expect(parseWord('type:dossier')).toEqual({ key: 'type', value: 'folder' })
    expect(parseWord('est:favori')).toEqual({ key: 'starred' })
    expect(parseWord('is:starred')).toEqual({ key: 'starred' })
  })

  it('reads date presets, bounds, ranges and single days', () => {
    expect(parseWord('modifie:7j')).toEqual({ key: 'since', value: '7d' })
    expect(parseWord('modified:1y')).toEqual({ key: 'since', value: '365d' })
    expect(parseWord('modifié:>2026-09-01')).toEqual({ key: 'range', after: '2026-09-01', before: '' })
    expect(parseWord('date:<=2026-01-31')).toEqual({ key: 'range', after: '', before: '2026-01-31' })
    expect(parseWord('date:2026-01-01..2026-03-31')).toEqual({ key: 'range', after: '2026-01-01', before: '2026-03-31' })
    expect(parseWord('date:2026-02-14')).toEqual({ key: 'range', after: '2026-02-14', before: '2026-02-14' })
    expect(parseWord('date:2026-13-45')).toBeNull()
  })

  it('reads sizes with units, defaulting to megabytes', () => {
    expect(parseWord('taille:>5Mo')).toEqual({ key: 'size', min: 5e6, max: 0 })
    expect(parseWord('size:<500KB')).toEqual({ key: 'size', min: 0, max: 5e5 })
    expect(parseWord('taille:1-100Mo')).toEqual({ key: 'size', min: 1e6, max: 1e8 })
    expect(parseWord('taille:500Ko-2Mo')).toEqual({ key: 'size', min: 5e5, max: 2e6 })
    expect(parseWord('taille:>1,5Go')).toEqual({ key: 'size', min: 1.5e9, max: 0 })
    expect(parseWord('taille:>10')).toEqual({ key: 'size', min: 1e7, max: 0 })
    expect(parseWord('taille:10-5Mo')).toBeNull()
    expect(parseWord('taille:gros')).toBeNull()
  })

  it('ignores plain words and unknown operators', () => {
    expect(parseWord('rapport')).toBeNull()
    expect(parseWord('http://x')).toBeNull()
    expect(parseWord('type:')).toBeNull()
  })
})

describe('splitQuery', () => {
  it('separates tokens from free text and flags bad operator values', () => {
    const r = splitQuery('rapport type:pdf annuel taille:>5Mo type:image')
    expect(r.q).toBe('rapport annuel')
    expect(r.invalid).toEqual(['type:pdf'])
    expect(r.tokens).toEqual([{ key: 'size', min: 5e6, max: 0 }, { key: 'kind', value: 'images' }])
  })
})

describe('tokens and filters', () => {
  it('round-trips every filter', () => {
    const f = { ...emptyFilters(), kind: 'images' as const, after: '2026-01-01', before: '', minSize: 5e6, starred: true }
    expect(tokensToFilters(filtersToTokens(f))).toEqual(f)
  })
  it('keeps one token per slot when toggling', () => {
    let list: Token[] = [{ key: 'type', value: 'folder' }]
    list = toggleToken(list, { key: 'kind', value: 'images' })
    expect(list).toEqual([{ key: 'kind', value: 'images' }])
    expect(toggleToken(list, { key: 'kind', value: 'images' })).toEqual([])
  })
  it('writes tokens back in the locale words', () => {
    expect(toSyntax({ key: 'size', min: 1e6, max: 1e8 }, fr)).toBe('taille:1Mo-100Mo')
    expect(toSyntax({ key: 'range', after: '2026-09-01', before: '' }, fr)).toBe('modifié:>2026-09-01')
    expect(toSyntax({ key: 'kind', value: 'media' }, VOCAB.en)).toBe('type:video')
    for (const tk of [{ key: 'size', min: 0, max: 5e5 }, { key: 'since', value: '30d' }] as Token[]) {
      expect(parseWord(toSyntax(tk, fr))).toEqual(tk)
    }
  })
})

describe('suggest', () => {
  it('offers operators while typing a key', () => {
    expect(suggest('ta', fr).items.map((s) => s.syntax)).toEqual(['taille:'])
    expect(suggest('es', fr).items[0].token).toEqual({ key: 'starred' })
    expect(suggest('r', fr).items).toEqual([])
  })
  it('offers matching values after the colon, with a format hint', () => {
    expect(suggest('type:ima', fr).items.map((s) => s.syntax)).toEqual(['type:image'])
    const size = suggest('taille:', fr)
    expect(size.items).toHaveLength(3)
    expect(size.hint).toBe('size')
  })
  it('puts a complete custom value first', () => {
    expect(suggest('taille:>5Mo', fr).items[0]).toMatchObject({ syntax: 'taille:>5Mo', token: { key: 'size', min: 5e6, max: 0 } })
  })
})
