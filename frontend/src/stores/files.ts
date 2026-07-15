import { defineStore } from 'pinia'
import { api, type FileNode } from '@/lib/api'
import { useUiStore } from './ui'

export type View = 'drive' | 'shared' | 'trash' | 'storage'

export interface Upload {
  id: number
  name: string
  size: number
  progress: number
  done: boolean
}

interface Crumb {
  id: number | null
  name: string
}

const VIEW_LABEL: Record<View, string> = {
  drive: 'Mon Drive',
  shared: 'Partagés avec moi',
  trash: 'Corbeille',
  storage: 'Stockage',
}

let uploadSeq = 0

export const useFilesStore = defineStore('files', {
  state: () => ({
    view: 'drive' as View,
    path: [] as Crumb[], // breadcrumb trail within the current view
    q: '',
    nodes: [] as FileNode[],
    loading: false,
    sel: [] as number[],
    anchor: null as number | null,
    previewId: null as number | null,
    overlayId: null as number | null, // full-screen content preview
    uploads: [] as Upload[],
    quota: { used: 0, total: 0 },
    trashCount: 0,
    storageFiles: [] as FileNode[], // full non-trashed file list for the storage view
  }),

  getters: {
    viewLabel: (s) => VIEW_LABEL[s.view],
    searching: (s) => s.q.trim().length > 0,
    readOnly: (s) => s.view === 'shared',
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
    selNodes(): FileNode[] {
      return this.sel.map((id) => this.nodes.find((n) => n.id === id)).filter(Boolean) as FileNode[]
    },
    previewNode(): FileNode | null {
      return this.previewId ? this.nodes.find((n) => n.id === this.previewId) || null : null
    },
    overlayNode(): FileNode | null {
      return this.overlayId ? this.nodes.find((n) => n.id === this.overlayId) || null : null
    },
    crumbs(): Crumb[] {
      const base: Crumb = { id: null, name: this.viewLabel }
      if (this.view === 'trash' || this.view === 'storage') return [base]
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
        const params: { parent?: string; view?: string; q?: string } = {}
        if (this.searching) params.q = this.q.trim()
        else if (this.view === 'trash') params.view = 'trash'
        else params.parent = this.currentParentParam
        this.nodes = await api.list(params)
      } finally {
        this.loading = false
      }
    },

    async loadCapacity() {
      this.quota = await api.capacity()
    },

    async refreshTrashCount() {
      try {
        const t = await api.list({ view: 'trash' })
        this.trashCount = t.length
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

    crumbTo(index: number) {
      // index 0 is the view root; 1.. map into the path trail.
      this.path = this.path.slice(0, index)
      this.clearSel()
      this.load()
    },

    openNode(node: FileNode) {
      if (node.type === 'folder') this.openFolder(node)
      else {
        this.previewId = node.id
        this.overlayId = node.id
      }
    },

    closeOverlay() {
      this.overlayId = null
    },

    async saveText(id: number, content: string) {
      await api.saveText(id, content)
      await Promise.all([this.load(), this.loadCapacity()])
      this.ui().toast('Fichier enregistré', 'check')
    },

    setQuery(q: string) {
      this.q = q
      this.clearSel()
      this.load()
    },

    /* Selection */
    select(node: FileNode, ev?: MouseEvent) {
      const id = node.id
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
      if (ev && (ev.metaKey || ev.ctrlKey)) {
        this.sel = this.sel.includes(id) ? this.sel.filter((x) => x !== id) : [...this.sel, id]
        this.anchor = id
        return
      }
      this.sel = [id]
      this.anchor = id
    },
    clearSel() {
      this.sel = []
      this.anchor = null
    },

    /* Mutations */
    async createFolder(name: string) {
      await api.createFolder(this.currentParentParam, name)
      await this.load()
      this.ui().toast(`Dossier « ${name} » créé`, 'folder-plus')
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
      this.ui().toast(lock ? `« ${node.name} » verrouillé` : `« ${node.name} » déverrouillé`, lock ? 'lock' : 'unlock')
    },

    async createDirectLink(node: FileNode) {
      const link = await api.createDirectLink(node.id)
      try {
        await navigator.clipboard.writeText(link.url)
        this.ui().toast('Lien direct copié dans le presse-papiers', 'link')
      } catch {
        this.ui().toast(`Lien direct : ${link.url}`, 'link')
      }
      return link
    },

    async trash(ids: number[]) {
      await api.trash(ids)
      if (this.previewId && ids.includes(this.previewId)) this.previewId = null
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      const msg = ids.length > 1 ? `${ids.length} éléments déplacés vers la corbeille` : 'Élément déplacé vers la corbeille'
      this.ui().toast(msg, 'trash', {
        label: 'Annuler',
        fn: () => this.restore(ids),
      })
    },

    async restore(ids: number[]) {
      await api.restore(ids)
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      this.ui().toast(ids.length > 1 ? `${ids.length} éléments restaurés` : 'Élément restauré', 'restore')
    },

    async purge(ids: number[]) {
      await api.purge(ids)
      this.clearSel()
      await Promise.all([this.load(), this.loadCapacity(), this.refreshTrashCount()])
      this.ui().toast(ids.length > 1 ? `${ids.length} éléments supprimés définitivement` : 'Élément supprimé définitivement', 'trash')
    },

    download(node: FileNode) {
      if (node.type === 'folder') return
      window.open(api.contentUrl(node.id), '_blank')
    },

    downloadArchive(ids: number[]) {
      if (!ids.length) return
      window.open(api.archiveUrl(ids), '_blank')
    },

    // Poll a background task until it finishes, then run onDone.
    pollTask(id: string, onDone: () => void) {
      const started = Date.now()
      const tick = async () => {
        try {
          const t = await api.taskStatus(id)
          if (t.status === 'done') return onDone()
          if (t.status === 'failed') {
            this.ui().toast('Tâche échouée' + (t.error ? ` : ${t.error}` : ''), 'x')
            return
          }
        } catch {
          /* keep polling */
        }
        if (Date.now() - started < 120000) setTimeout(tick, 500)
      }
      setTimeout(tick, 300)
    },

    async compress(ids: number[]) {
      if (!ids.length) return
      const task = await api.compress(this.currentParentParam, ids)
      this.ui().toast('Compression en cours…', 'file-archive')
      this.pollTask(task.id, async () => {
        await Promise.all([this.load(), this.loadCapacity()])
        this.ui().toast('Archive créée', 'file-archive')
      })
    },

    openOffice(node: FileNode) {
      // The launch page auto-submits the WOPI POST form and hosts the editor in
      // an iframe; opening it as a top-level navigation carries the session cookie.
      window.open(api.officeUrl(node.id), '_blank')
    },

    async extract(node: FileNode) {
      const task = await api.extract(node.id, this.currentParentParam)
      this.ui().toast('Extraction en cours…', 'file-archive')
      this.pollTask(task.id, async () => {
        await Promise.all([this.load(), this.loadCapacity()])
        this.ui().toast('Archive extraite', 'file-archive')
      })
    },

    /* Chunked resumable upload */
    async upload(files: File[]) {
      const parent = this.view === 'drive' ? this.currentParentParam : 'root'
      if (this.view !== 'drive') this.ui().toast('Importation dans la racine de Mon Drive', 'info')

      await Promise.all(files.map((file) => this.uploadOne(file, parent)))

      await Promise.all([this.load(), this.loadCapacity()])
      setTimeout(() => {
        if (this.uploads.length && this.uploads.every((u) => u.done)) this.uploads = []
      }, 2400)
    },

    async uploadOne(file: File, parent: string) {
      this.uploads.push({ id: ++uploadSeq, name: file.name, size: file.size, progress: 0, done: false })
      // Mutate the reactive array element (not the raw object we just pushed),
      // otherwise Vue never sees the progress changes.
      const up = this.uploads[this.uploads.length - 1]
      const pct = (bytes: number) => (file.size ? Math.min(99, Math.round((bytes / file.size) * 100)) : 99)
      try {
        const init = await api.initUpload(parent, file.name, file.size)
        const { session_id, chunk_size, num_chunks } = init
        let uploaded = 0
        for (let i = 0; i < num_chunks; i++) {
          const start = i * chunk_size
          const end = Math.min(file.size, start + chunk_size)
          await api.putChunk(session_id, i, file.slice(start, end), (sent) => {
            up.progress = pct(uploaded + sent)
          })
          uploaded = end
          up.progress = pct(uploaded)
        }
        await api.completeUpload(session_id)
        up.progress = 100
      } catch (e: any) {
        this.ui().toast(`Échec de l’import de « ${file.name} »` + (e?.message ? ` : ${e.message}` : ''), 'x')
      } finally {
        up.done = true
      }
    },
  },
})
