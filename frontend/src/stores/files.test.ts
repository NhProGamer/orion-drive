import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { useFilesStore } from '@/stores/files'
import type { FileNode } from '@/lib/api'

// Minimal FileNode factory for state-only getter tests (no network).
function node(over: Partial<FileNode> & { id: number }): FileNode {
  return {
    parent_id: null,
    name: 'n',
    type: 'file',
    size: 0,
    starred: false,
    locked: false,
    owner: 'me',
    modified: '2020-01-01T00:00:00Z',
    ...over,
  }
}

describe('files store getters', () => {
  beforeEach(() => setActivePinia(createPinia()))

  describe('folderId / currentParentParam', () => {
    it('are root when the path is empty', () => {
      const s = useFilesStore()
      expect(s.folderId).toBeNull()
      expect(s.currentParentParam).toBe('root')
    })
    it('reflect the deepest crumb in the path', () => {
      const s = useFilesStore()
      s.path = [
        { id: 3, name: 'a' },
        { id: 7, name: 'b' },
      ]
      expect(s.folderId).toBe(7)
      expect(s.currentParentParam).toBe('7')
    })
  })

  describe('folders / files / orderedNodes split', () => {
    it('partitions nodes and lists folders before files', () => {
      const s = useFilesStore()
      s.nodes = [
        node({ id: 1, type: 'file', name: 'f1' }),
        node({ id: 2, type: 'folder', name: 'd1' }),
        node({ id: 3, type: 'file', name: 'f2' }),
        node({ id: 4, type: 'folder', name: 'd2' }),
      ]
      expect(s.folders.map((n) => n.id)).toEqual([2, 4])
      expect(s.files.map((n) => n.id)).toEqual([1, 3])
      expect(s.orderedNodes.map((n) => n.id)).toEqual([2, 4, 1, 3])
      expect(s.orderedIds).toEqual([2, 4, 1, 3])
    })
  })

  describe('crumbs', () => {
    it('prepends the view root to the path in drive view', () => {
      const s = useFilesStore()
      s.view = 'drive'
      s.path = [{ id: 5, name: 'Sub' }]
      const c = s.crumbs
      expect(c).toHaveLength(2)
      expect(c[0].id).toBeNull()
      expect(c[1]).toEqual({ id: 5, name: 'Sub' })
    })
    it('collapses to a single root crumb in flat views', () => {
      const s = useFilesStore()
      s.path = [{ id: 5, name: 'Sub' }] // should be ignored
      for (const v of ['trash', 'storage', 'shares'] as const) {
        s.view = v
        expect(s.crumbs).toHaveLength(1)
        expect(s.crumbs[0].id).toBeNull()
      }
    })
  })

  describe('hasFilters', () => {
    it('is false with the default (empty) filters', () => {
      const s = useFilesStore()
      expect(s.hasFilters).toBe(false)
    })

    const facets = [
      { type: 'file' as const },
      { kind: 'images' as const },
      { starred: true },
      { since: '7d' as const },
      { after: '2024-01-01' },
      { before: '2024-02-01' },
      { minSize: 1 },
      { maxSize: 1000 },
    ]
    it.each(facets)('is true when a single facet is set: %o', (patch) => {
      const s = useFilesStore()
      s.filters = { ...s.filters, ...patch }
      expect(s.hasFilters).toBe(true)
    })
  })

  describe('searching', () => {
    it('is false with no query and no filters', () => {
      const s = useFilesStore()
      expect(s.searching).toBe(false)
    })
    it('is true when the query is non-blank', () => {
      const s = useFilesStore()
      s.q = '  hello '
      expect(s.searching).toBe(true)
    })
    it('ignores a whitespace-only query', () => {
      const s = useFilesStore()
      s.q = '   '
      expect(s.searching).toBe(false)
    })
    it('is true when only a filter facet is set', () => {
      const s = useFilesStore()
      s.filters = { ...s.filters, starred: true }
      expect(s.searching).toBe(true)
    })
  })

  describe('view-derived flags', () => {
    it('readOnly / dndEnabled track the active view', () => {
      const s = useFilesStore()
      s.view = 'drive'
      expect(s.readOnly).toBe(false)
      expect(s.dndEnabled).toBe(true)
      s.view = 'shares'
      expect(s.readOnly).toBe(true)
      expect(s.dndEnabled).toBe(false)
    })
  })

  describe('node lookups', () => {
    it('selNodes resolves selected ids and drops missing ones', () => {
      const s = useFilesStore()
      s.nodes = [node({ id: 1 }), node({ id: 2 })]
      s.sel = [2, 99] // 99 does not exist
      expect(s.selNodes.map((n) => n.id)).toEqual([2])
    })
    it('previewNode / overlayNode resolve by id or are null', () => {
      const s = useFilesStore()
      s.nodes = [node({ id: 1, name: 'a' })]
      expect(s.previewNode).toBeNull()
      s.previewId = 1
      expect(s.previewNode?.name).toBe('a')
      s.overlayId = 1
      expect(s.overlayNode?.id).toBe(1)
    })
  })

  describe('quotaPct', () => {
    it('is 0 when total is 0 and clamps to 100', () => {
      const s = useFilesStore()
      expect(s.quotaPct).toBe(0)
      s.quota = { used: 50, total: 200 }
      expect(s.quotaPct).toBe(25)
      s.quota = { used: 500, total: 200 }
      expect(s.quotaPct).toBe(100) // clamped
    })
  })
})
