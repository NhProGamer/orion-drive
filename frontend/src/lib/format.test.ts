import { beforeAll, describe, expect, it } from 'vitest'
import { setLocale } from '@/i18n'
import {
  canThumbnail,
  ext,
  fmtDate,
  fmtEta,
  fmtSize,
  isArchive,
  isMarkdown,
  kindFromName,
  previewKind,
} from '@/lib/format'

// Pin the locale so size units / number + date formatting are deterministic.
beforeAll(() => setLocale('en'))

describe('kindFromName', () => {
  it('maps known extensions to their kind', () => {
    expect(kindFromName('photo.png')).toBe('image')
    expect(kindFromName('clip.mp4')).toBe('video')
    expect(kindFromName('song.MP3')).toBe('audio') // case-insensitive
    expect(kindFromName('book.pdf')).toBe('pdf')
    expect(kindFromName('notes.md')).toBe('text')
    expect(kindFromName('bundle.zip')).toBe('archive')
    expect(kindFromName('main.go')).toBe('code')
    expect(kindFromName('config.yaml')).toBe('config')
  })

  it('falls back to "file" for unknown or missing extensions', () => {
    expect(kindFromName('mystery.xyz')).toBe('file')
    expect(kindFromName('README')).toBe('file')
    expect(kindFromName('')).toBe('file')
    expect(kindFromName('.hidden')).toBe('file') // no name-then-ext match
  })
})

describe('ext', () => {
  it('returns the upper-cased extension', () => {
    expect(ext('archive.tar.gz')).toBe('GZ')
    expect(ext('image.JPeG')).toBe('JPEG')
  })
  it('returns empty string when there is no extension', () => {
    expect(ext('LICENSE')).toBe('')
    expect(ext('')).toBe('')
  })
})

describe('isArchive', () => {
  it('recognises supported archive names', () => {
    expect(isArchive('a.zip')).toBe(true)
    expect(isArchive('a.tar')).toBe(true)
    expect(isArchive('a.tar.gz')).toBe(true)
    expect(isArchive('a.tgz')).toBe(true)
    expect(isArchive('a.7z')).toBe(true)
    expect(isArchive('A.ZIP')).toBe(true)
  })
  it('rejects non-archives', () => {
    expect(isArchive('a.txt')).toBe(false)
    expect(isArchive('a.rar')).toBe(false) // listed in kind map but not the isArchive set
    expect(isArchive('noext')).toBe(false)
  })
})

describe('previewKind', () => {
  it('classifies each preview family', () => {
    expect(previewKind('a.png')).toBe('image')
    expect(previewKind('a.mp4')).toBe('video')
    expect(previewKind('a.mp3')).toBe('audio')
    expect(previewKind('a.pdf')).toBe('pdf')
    expect(previewKind('a.txt')).toBe('text')
    expect(previewKind('a.go')).toBe('text') // code previews as text
    expect(previewKind('a.json')).toBe('text') // config previews as text
    expect(previewKind('a.epub')).toBe('epub')
    expect(previewKind('a.cbz')).toBe('epub') // comic goes through the epub viewer
    expect(previewKind('a.zip')).toBe('archive')
  })
  it('returns "none" for unpreviewable files', () => {
    expect(previewKind('a.doc')).toBe('none')
    expect(previewKind('a.iso')).toBe('none')
    expect(previewKind('noext')).toBe('none')
  })
})

describe('canThumbnail', () => {
  it('is true for thumbnailable formats', () => {
    expect(canThumbnail('a.jpg')).toBe(true)
    expect(canThumbnail('a.cr2')).toBe(true) // raw
    expect(canThumbnail('a.mp4')).toBe(true)
    expect(canThumbnail('a.pdf')).toBe(true)
    expect(canThumbnail('a.epub')).toBe(true)
  })
  it('is false for formats with no thumbnail pipeline', () => {
    expect(canThumbnail('a.txt')).toBe(false)
    expect(canThumbnail('a.zip')).toBe(false)
    expect(canThumbnail('noext')).toBe(false)
  })
})

describe('isMarkdown', () => {
  it('matches markdown extensions only', () => {
    expect(isMarkdown('a.md')).toBe(true)
    expect(isMarkdown('a.markdown')).toBe(true)
    expect(isMarkdown('A.MD')).toBe(true)
    expect(isMarkdown('a.txt')).toBe(false)
    expect(isMarkdown('a.mdx')).toBe(false)
  })
})

describe('fmtSize', () => {
  it('returns a dash for empty / zero sizes', () => {
    expect(fmtSize(0)).toBe('—')
    expect(fmtSize(NaN)).toBe('—')
  })
  it('formats bytes with SI (1000-based) units', () => {
    expect(fmtSize(999)).toBe('999 B')
    expect(fmtSize(1000)).toBe('1 KB')
    expect(fmtSize(1500)).toBe('1.5 KB')
    expect(fmtSize(1_000_000)).toBe('1 MB')
    expect(fmtSize(1_500_000_000)).toBe('1.5 GB')
  })
  it('drops fractional digits once the value is >= 100 in a unit', () => {
    expect(fmtSize(150_000)).toBe('150 KB')
  })
})

describe('fmtEta', () => {
  it('returns empty string for invalid input', () => {
    expect(fmtEta(NaN)).toBe('')
    expect(fmtEta(Infinity)).toBe('')
    expect(fmtEta(-5)).toBe('')
  })
  it('formats seconds, minutes and hours', () => {
    expect(fmtEta(0)).toBe('0 s')
    expect(fmtEta(45)).toBe('45 s')
    expect(fmtEta(90)).toBe('2 min') // rounds to nearest minute
    expect(fmtEta(3600)).toBe('1 h')
    expect(fmtEta(3660)).toBe('1 h 1 min')
  })
})

describe('fmtDate', () => {
  it('returns the "now" label for very recent times', () => {
    expect(fmtDate(new Date().toISOString())).toBe('just now')
  })
  it('gives a relative label within the last week', () => {
    const fiveMinAgo = new Date(Date.now() - 5 * 60_000).toISOString()
    const out = fmtDate(fiveMinAgo)
    expect(out).not.toBe('just now')
    expect(out).toMatch(/5/)

    const twoDaysAgo = new Date(Date.now() - 2 * 86_400_000).toISOString()
    expect(fmtDate(twoDaysAgo)).toMatch(/2/)
  })
  it('falls back to an absolute date past a week', () => {
    const out = fmtDate('2020-01-15T10:00:00Z')
    expect(out).toMatch(/2020/)
  })
})
