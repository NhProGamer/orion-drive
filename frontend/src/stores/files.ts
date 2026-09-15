import { defineStore } from 'pinia'
import { api, type FileNode, type ShareInfo } from '@/lib/api'
import { uploadInChunks } from '@/lib/upload'
import { i18n } from '@/i18n'
import { useUiStore } from './ui'

// Translate with the global i18n instance (Pinia stores run outside setup).
const t = (key: string, ...args: any[]) => i18n.global.t(key, ...(args as [])) as string

export type View = 'drive' | 'shares' | 'trash' | 'storage'

export interface Upload {
  id: number
  name: string
  size: number
  progress: number
  loaded: number // bytes uploaded so far
  speed: number // bytes/s (smoothed), 0 when done
  done: boolean
}

// A tracked background job (archive extraction / compression). Progress is a
// percentage, or -1 when indeterminate (streaming formats report a running count).
export interface BgTask {
  id: string
  type: string // 'extract' | 'compress'
  progress: number
  message: string
}

interface Crumb {
  id: number | null
  name: string
}

export type SearchType = '' | 'file' | 'folder'
export type SearchKind = '' | 'images' | 'media' | 'documents' | 'archives'
export type SearchSince = '' | '1d' | '7d' | '30d' | '365d'

export interface SearchFilters {
  type: SearchType
  kind: SearchKind
  starred: boolean
  since: SearchSince
  after: string // custom range start (yyyy-mm-dd); '' = none. Ignored when `since` is set.
  before: string // custom range end (yyyy-mm-dd); '' = none.
  minSize: number // bytes; 0 = none
  maxSize: number // bytes; 0 = none
}

const emptyFilters = (): SearchFilters => ({
  type: '', kind: '', starred: false, since: '', after: '', before: '', minSize: 0, maxSize: 0,
})

// Map a "modified since" preset to an ISO timestamp cutoff.
const SINCE_DAYS: Record<Exclude<SearchSince, ''>, number> = { '1d': 1, '7d': 7, '30d': 30, '365d': 365 }

// Breadcrumb/view labels reuse the sidebar (shell.*) translations.
const VIEW_LABEL_KEY: Record<View, string> = {
  drive: 'shell.myDrive',
  shares: 'shell.myShares',
  trash: 'shell.trash',
  storage: 'shell.storage',
}

let uploadSeq = 0

export const useFilesStore = defineStore('files', {
  state: () => ({
    view: 'drive' as View,
    path: [] as Crumb[], // breadcrumb trail within the current view
    q: '',
    filters: emptyFilters(), // active search filters (type/kind/starred/since)
    nodes: [] as FileNode[],
    loading: false,
    sel: [] as number[],
    anchor: null as number | null, // last plain/ctrl click, for shift-range select
    selectionMode: false, // touch multi-select mode (entered via long-press)
    previewId: null as number | null,
    overlayId: null as number | null, // full-screen content preview
    uploads: [] as Upload[],
    tasks: [] as BgTask[], // tracked background jobs (extract/compress), shown live
    quota: { used: 0, total: 0 },
    trashCount: 0,
    storageFiles: [] as FileNode[], // full non-trashed file list for the storage view
    shares: [] as ShareInfo[], // the user's own share links (My shares view)
    dragIds: [] as number[], // ids currently being drag-moved
    dragOverId: null as number | null, // folder highlighted as a drop target
    searchHasMore: false, // a further page of search results exists
    searchNextOffset: 0, // offset to request the next search page with
    loadingMore: false, // a "load more" page fetch is in flight
  }),

  getters: {
    viewLabel: (s) => t(VIEW_LABEL_KEY[s.view]),
    hasFilters: (s) => {
      const f = s.filters
      return !!(f.type || f.kind || f.starred || f.since || f.after || f.before || f.minSize || f.maxSize)
    },
    searching(): boolean {
      return this.q.trim().length > 0 || this.hasFilters
    },
    readOnly: (s) => s.view === 'shares',
    dndEnabled: (s) => s.view === 'drive', // drag-to-move only in Mon Drive
    folderId: (s) => (s.path.length ? s.path[s.path.length - 1].id : null),
    currentParentParam(): string {
      const id = this.folderId
      return id == null ? 'root' : String(id)
    },
    folders: (s) => s.nodes.filter((n) => n.type === 'folder'),
    files: (s) => s.nodes.filter((n) => n.type === 'file'),
    orderedNodes(): FileNode[] {
      return this.folders.concat(this.files)
    },
    orderedIds(): number[] {
      return this.orderedNodes.map((n) => n.id)
    },
    // O(1) membership for the per-item `:selected` binding: a plain array made
    // each of N items run includes() on every selection change (O(N²)).
    selSet(): Set<number> {
      return new Set(this.sel)
    },
    // id → node map so selNodes / preview lookups are O(1) instead of a linear
    // find over all nodes per id.
    nodeById(): Map<number, FileNode> {
      return new Map(this.nodes.map((n) => [n.id, n]))
    },
    selNodes(): FileNode[] {
      return this.sel.map((id) => this.nodeById.get(id)).filter(Boolean) as FileNode[]
    },
    previewNode(): FileNode | null {
      return this.previewId ? this.nodes.find((n) => n.id === this.previewId) || null : null
    },
    overlayNode(): FileNode | null {
      return this.overlayId ? this.nodes.find((n) => n.id === this.overlayId) || null : null
    },
    crumbs(): Crumb[] {
      const base: Crumb = { id: null, name: this.viewLabel }
      if (this.view === 'trash' || this.view === 'storage' || this.view === 'shares') return [base]
      return [base, ...this.path]
    },
    quotaPct: (s) => (s.quota.total ? Math.min(100, Math.round((s.quota.used / s.quota.total) * 100)) : 0),
  },

  actions: {
    ui() {
      return useUiStore()
    },

    async load() {
      this.loading = true
      try {
        if (this.view === 'storage') {
          // Storage view needs every non-trashed file to compute the breakdown.
          this.storageFiles = await api.list({ all: '1' })
          return
        }
        if (this.view === 'shares') {
          // My shares view lists the share links the user created.
          this.shares = await api.listShares()
          return
        }
        if (this.searching) {
          const page = await api.search(this.searchParams(0))
          this.nodes = page.items
          this.searchHasMore = page.has_more
          this.searchNextOffset = page.next_offset
          return
        }
        const params: Record<string, string> = {}
        if (this.view === 'trash') params.view = 'trash'
        else params.parent = this.currentParentParam
        this.nodes = await api.list(params)
      } finally {
        this.loading = false
      }
    },

    // searchParams builds the query for the active search at a given row offset.
    searchParams(offset: number): Record<string, string> {
      const params: Record<string, string> = {}
      if (this.q.trim()) params.q = this.q.trim()
      const f = this.filters
      if (f.type) params.type = f.type
      if (f.kind) params.kind = f.kind
      if (f.starred) params.starred = '1'
      if (f.since) {
        const cutoff = new Date(Date.now() - SINCE_DAYS[f.since] * 86400000)
        params.after = cutoff.toISOString()
      } else {
        // Custom date range (only when no relative preset is active).
        if (f.after) params.after = new Date(f.after + 'T00:00:00').toISOString()
        if (f.before) params.before = new Date(f.before + 'T23:59:59').toISOString()
      }
      if (f.minSize > 0) params.min_size = String(f.minSize)
      if (f.maxSize > 0) params.max_size = String(f.maxSize)
      if (offset > 0) params.offset = String(offset)
      return params
    },

    // loadMoreSearch appends the next page of search results in place.
    async loadMoreSearch() {
      if (!this.searchHasMore || this.loadingMore) return
      this.loadingMore = true
      try {
        const page = await api.search(this.searchParams(this.searchNextOffset))
        this.nodes = this.nodes.concat(page.items)
        this.searchHasMore = page.has_more
        this.searchNextOffset = page.next_offset
      } finally {
        this.loadingMore = false
      }
    },

    async loadCapacity() {
      this.quota = await api.capacity()
    },

    async refreshTrashCount() {
      try {
        const trashed = await api.list({ view: 'trash' })
        this.trashCount = trashed.length
      } catch {
        this.trashCount = 0
      }
    },

    async init() {
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
    },

    gotoView(v: View) {
      this.view = v
      this.path = []
      this.q = ''
      this.filters = emptyFilters()
      this.previewId = null
      this.clearSel()
      this.load()
    },

    openFolder(node: FileNode) {
      if (this.view === 'trash') return
      this.path.push({ id: node.id, name: node.name })
      this.q = ''
      this.clearSel()
      this.load()
    },

    // Rebuild drive navigation from a bookmarked/refreshed URL: resolve the
    // breadcrumb trail of a folder (or a file's parent) from the server, load it,
    // and optionally open the file's preview. id null returns to the drive root.
    async restoreNav(id: number | null, preview: boolean) {
      this.view = 'drive'
      this.q = ''
      this.filters = emptyFilters()
      this.clearSel()
      this.previewId = null
      this.overlayId = null
      if (id == null) {
        this.path = []
      } else {
        try {
          this.path = await api.ancestors(id)
        } catch {
          this.path = []
        }
      }
      await this.load()
      if (preview && id != null) this.overlayId = id // open the file full-screen
    },

    crumbTo(index: number) {
      // index 0 is the view root; 1.. map into the path trail.
      this.path = this.path.slice(0, index)
      this.clearSel()
      this.load()
    },

    openNode(node: FileNode) {
      // Double click: navigate into a folder, or open a file's full-screen
      // preview. The side details panel is closed so only the popup shows.
      if (node.type === 'folder') {
        this.openFolder(node)
        return
      }
      this.overlayId = node.id
      this.previewId = null
    },

    closeOverlay() {
      this.overlayId = null
    },

    async saveText(id: number, content: string) {
      await api.saveText(id, content)
      await Promise.all([this.load(), this.loadCapacity()])
      this.ui().toast(t('files.fileSaved'), 'check')
    },

    setQuery(q: string) {
      this.q = q
      this.clearSel()
      this.load()
    },

    // Merge new search filter values and reload the results.
    setFilters(patch: Partial<SearchFilters>) {
      this.filters = { ...this.filters, ...patch }
      this.clearSel()
      this.load()
    },
    clearFilters() {
      this.filters = emptyFilters()
      this.clearSel()
      this.load()
    },
    // Leave search entirely: clear the text query and every filter.
    clearSearch() {
      this.q = ''
      this.filters = emptyFilters()
      this.clearSel()
      this.load()
    },

    /* Selection */
    select(node: FileNode, ev?: MouseEvent) {
      const id = node.id
      // Shift+click: select the range between the anchor and this item.
      if (ev?.shiftKey && this.anchor != null) {
        const ids = this.orderedIds
        const a = ids.indexOf(this.anchor)
        const b = ids.indexOf(id)
        if (a > -1 && b > -1) {
          const [s, e] = a < b ? [a, b] : [b, a]
          this.sel = ids.slice(s, e + 1)
          return
        }
      }
      // Ctrl/Cmd+click: toggle this item in the current selection.
      if (ev && (ev.metaKey || ev.ctrlKey)) {
        this.sel = this.sel.includes(id) ? this.sel.filter((x) => x !== id) : [...this.sel, id]
        this.anchor = id
        return
      }
      // Plain click: only select. Left-click never touches the details panel —
      // it is opened deliberately via the context menu and closed from its header.
      this.sel = [id]
      this.anchor = id
    },
    clearSel() {
      this.sel = []
      this.anchor = null
      this.previewId = null
      this.selectionMode = false
    },

    // Touch: long-press enters selection mode and selects the pressed item.
    enterSelection(node: FileNode) {
      this.selectionMode = true
      this.previewId = null
      if (!this.sel.includes(node.id)) this.sel = [...this.sel, node.id]
      this.anchor = node.id
    },
    // Touch: in selection mode a tap toggles the item; emptying the selection
    // leaves the mode.
    toggleSel(node: FileNode) {
      const id = node.id
      this.sel = this.sel.includes(id) ? this.sel.filter((x) => x !== id) : [...this.sel, id]
      this.anchor = id
      if (this.sel.length === 0) this.selectionMode = false
    },

    async revokeShare(token: string) {
      await api.deleteShare(token)
      this.shares = this.shares.filter((s) => s.token !== token)
      this.ui().toast(t('files.shareRevoked'), 'trash')
    },

    /* Mutations */
    async createFolder(name: string) {
      await api.createFolder(this.currentParentParam, name)
      await this.load()
      this.ui().toast(t('files.folderCreated', { name }), 'folder-plus')
    },

    // Create a blank Office document in the current folder and return it, so the
    // caller can open it in the online editor.
    async createOffice(name: string) {
      const node = await api.officeNew(this.currentParentParam, name)
      await this.load()
      return node
    },

    // Create a blank whiteboard in the current folder and return it, so the
    // caller can open it straight away.
    async createBoard(name: string) {
      const node = await api.newBoard(this.currentParentParam, name)
      await this.load()
      return node
    },

    async rename(id: number, name: string) {
      await api.rename(id, name)
      await this.load()
    },

    async toggleStar(node: FileNode) {
      await api.star(node.id)
      await this.load()
    },

    async setLock(node: FileNode, lock: boolean) {
      if (lock) await api.lock(node.id)
      else await api.unlock(node.id)
      await this.load()
      this.ui().toast(
        lock ? t('files.locked', { name: node.name }) : t('files.unlocked', { name: node.name }),
        lock ? 'lock' : 'unlock',
      )
    },

    async createDirectLink(node: FileNode) {
      const link = await api.createDirectLink(node.id)
      try {
        await navigator.clipboard.writeText(link.url)
        this.ui().toast(t('files.directLinkCopied'), 'link')
      } catch {
        this.ui().toast(t('files.directLink', { url: link.url }), 'link')
      }
      return link
    },

    async trash(ids: number[]) {
      await api.trash(ids)
      if (this.previewId && ids.includes(this.previewId)) this.previewId = null
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      this.ui().toast(t('files.trashed', ids.length), 'trash', {
        label: t('common.cancel'),
        fn: () => this.restore(ids),
      })
    },

    async restore(ids: number[]) {
      await api.restore(ids)
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      this.ui().toast(t('files.restored', ids.length), 'restore')
    },

    /* Drag & drop move: pick up an item (or the whole selection if it is part of
       it) and drop it onto a folder to move it there. */
    beginDrag(node: FileNode) {
      this.dragIds = this.sel.includes(node.id) && this.sel.length ? [...this.sel] : [node.id]
    },
    endDrag() {
      this.dragIds = []
      this.dragOverId = null
    },
    canDropInto(folder: FileNode): boolean {
      return (
        this.dndEnabled &&
        folder.type === 'folder' &&
        this.dragIds.length > 0 &&
        !this.dragIds.includes(folder.id)
      )
    },
    async dropInto(folder: FileNode) {
      const ids = this.dragIds.filter((id) => id !== folder.id)
      this.endDrag()
      if (!ids.length || folder.type !== 'folder') return
      await this.move(ids, String(folder.id))
    },

    async move(ids: number[], parent: string) {
      await api.move(ids, parent)
      this.clearSel()
      await this.load()
      this.ui().toast(t('files.moved', ids.length), 'move')
    },

    async emptyTrash() {
      const r = await api.emptyTrash()
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      this.ui().toast(r.purged ? t('files.trashEmptied', { n: r.purged }) : t('files.trashAlreadyEmpty'), 'trash')
    },

    async purge(ids: number[]) {
      await api.purge(ids)
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      this.ui().toast(t('files.purged', ids.length), 'trash')
    },

    download(node: FileNode) {
      if (node.type === 'folder') return
      window.open(api.contentUrl(node.id), '_blank')
    },

    downloadArchive(ids: number[]) {
      if (!ids.length) return
      window.open(api.archiveUrl(ids), '_blank')
    },

    // Track a background job in `tasks` (so its progress shows live), polling
    // until it finishes. Idempotent: re-tracking an already-tracked id is a no-op,
    // so restoreTasks() can safely re-attach after a page refresh.
    trackTask(id: string, type: string, onDone: () => void) {
      if (this.tasks.some((t) => t.id === id)) return
      this.tasks.push({ id, type, progress: 0, message: '' })
      const remove = () => {
        this.tasks = this.tasks.filter((t) => t.id !== id)
      }
      const started = Date.now()
      const tick = async () => {
        try {
          const task = await api.taskStatus(id)
          const cur = this.tasks.find((t) => t.id === id)
          if (cur) {
            cur.progress = task.progress
            cur.message = task.message
          }
          if (task.status === 'done') {
            remove()
            return onDone()
          }
          if (task.status === 'failed') {
            remove()
            this.ui().toast(t('files.taskFailed') + (task.error ? ` : ${task.error}` : ''), 'x')
            return
          }
        } catch {
          /* keep polling */
        }
        // Bound the poll; extraction may take a while, so allow up to 30 min.
        if (Date.now() - started < 1_800_000) setTimeout(tick, 500)
        else remove()
      }
      setTimeout(tick, 300)
    },

    // Re-attach to the user's still-running background jobs after a page refresh,
    // so their progress reappears (survives reload). Called on startup.
    async restoreTasks() {
      try {
        const jobs = await api.taskList()
        for (const j of jobs) {
          if (j.status === 'running' || j.status === 'pending') {
            const done =
              j.type === 'extract'
                ? async () => {
                    await Promise.all([this.load(), this.loadCapacity()])
                    this.ui().toast(t('files.archiveExtracted'), 'file-archive')
                  }
                : async () => {
                    await Promise.all([this.load(), this.loadCapacity()])
                    this.ui().toast(t('files.archiveCreated'), 'file-archive')
                  }
            this.trackTask(j.id, j.type, done)
          }
        }
      } catch {
        /* no tasks / not reachable */
      }
    },

    async compress(ids: number[]) {
      if (!ids.length) return
      const task = await api.compress(this.currentParentParam, ids)
      this.ui().toast(t('files.compressing'), 'file-archive')
      this.trackTask(task.id, 'compress', async () => {
        await Promise.all([this.load(), this.loadCapacity()])
        this.ui().toast(t('files.archiveCreated'), 'file-archive')
      })
    },

    openOffice(node: FileNode) {
      // The launch page auto-submits the WOPI POST form and hosts the editor in
      // an iframe; opening it as a top-level navigation carries the session cookie.
      window.open(api.officeUrl(node.id), '_blank')
    },

    async extract(node: FileNode) {
      const task = await api.extract(node.id, this.currentParentParam)
      this.ui().toast(t('files.extracting'), 'file-archive')
      this.trackTask(task.id, 'extract', async () => {
        await Promise.all([this.load(), this.loadCapacity()])
        this.ui().toast(t('files.archiveExtracted'), 'file-archive')
      })
    },

    /* Chunked resumable upload */
    async upload(files: File[]) {
      const parent = this.view === 'drive' ? this.currentParentParam : 'root'
      if (this.view !== 'drive') this.ui().toast(t('files.uploadToRoot'), 'info')

      await Promise.all(files.map((file) => this.uploadOne(file, parent)))

      await Promise.all([this.load(), this.loadCapacity()])
      setTimeout(() => {
        if (this.uploads.length && this.uploads.every((u) => u.done)) this.uploads = []
      }, 2400)
    },

    /* Folder upload: recreate the directory tree, then upload each file into it.
       Each entry's `path` is the file's full relative path (e.g. "docs/sub/a.txt"). */
    async uploadTree(entries: { file: File; path: string }[]) {
      if (!entries.length) return
      const base = this.view === 'drive' ? this.currentParentParam : 'root'
      if (this.view !== 'drive') this.ui().toast(t('files.uploadToRoot'), 'info')

      const dirOf = (p: string) => {
        const i = p.lastIndexOf('/')
        return i < 0 ? '' : p.slice(0, i)
      }
      const dirParam = new Map<string, string>([['', base]])

      // Create each distinct directory once, ancestors before descendants (sorted)
      // and sequentially, so concurrent files never race on a shared parent folder.
      const dirs = [...new Set(entries.map((e) => dirOf(e.path)))].filter(Boolean).sort()
      for (const dir of dirs) {
        if (dirParam.has(dir)) continue
        try {
          const leaf = await api.ensureFolderPath(base, dir)
          dirParam.set(dir, String(leaf.id))
        } catch (e: any) {
          this.ui().toast(t('files.folderFailed', { dir }) + (e?.message ? ` : ${e.message}` : ''), 'x')
        }
      }

      await Promise.all(
        entries.map((e) => {
          const parent = dirParam.get(dirOf(e.path))
          return parent ? this.uploadOne(e.file, parent) : Promise.resolve()
        }),
      )

      await Promise.all([this.load(), this.loadCapacity()])
      setTimeout(() => {
        if (this.uploads.length && this.uploads.every((u) => u.done)) this.uploads = []
      }, 2400)
    },

    async uploadOne(file: File, parent: string) {
      this.uploads.push({ id: ++uploadSeq, name: file.name, size: file.size, progress: 0, loaded: 0, speed: 0, done: false })
      // Mutate the reactive array element (not the raw object we just pushed),
      // otherwise Vue never sees the progress changes.
      const up = this.uploads[this.uploads.length - 1]
      const pct = (bytes: number) => (file.size ? Math.min(99, Math.round((bytes / file.size) * 100)) : 99)
      // Throughput smoothing. Upload progress callbacks fire at irregular
      // intervals, so a fixed-weight average jumps around. Instead use an
      // exponential moving average with a *time constant* (TAU): the weight of
      // each sample is derived from how much time actually elapsed, which makes
      // the smoothing independent of the callback cadence and steadies the rate
      // (and therefore the ETA). ~3s of memory rides out chunk-boundary bursts.
      const TAU = 3000
      let lastT = performance.now()
      let lastLoaded = 0
      const onProgress = (loaded: number) => {
        up.loaded = loaded
        up.progress = pct(loaded)
        const now = performance.now()
        const dt = now - lastT
        if (dt >= 200) {
          const inst = ((loaded - lastLoaded) / dt) * 1000
          const alpha = 1 - Math.exp(-dt / TAU)
          up.speed = up.speed ? up.speed + alpha * (inst - up.speed) : inst
          lastT = now
          lastLoaded = loaded
        }
      }
      try {
        await uploadInChunks(
          file,
          {
            init: () => api.initUpload(parent, file.name, file.size),
            putChunk: (sid, i, blob, cb) => api.putChunk(sid, i, blob, cb),
            complete: (sid) => api.completeUpload(sid),
          },
          onProgress,
        )
        up.progress = 100
        up.loaded = file.size
      } catch (e: any) {
        this.ui().toast(t('files.uploadFailed', { name: file.name }) + (e?.message ? ` : ${e.message}` : ''), 'x')
      } finally {
        up.done = true
        up.speed = 0
      }
    },
  },
})
