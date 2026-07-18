<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Plus, FolderPlus, Upload, HardDrive, Trash2, Database, Shield, Menu,
  Search, Grid3x3, List, Sun, Moon, ChevronRight, X, Folder, Eye, SlidersHorizontal,
  Download, Pencil, Star, RotateCcw, Info, Share2, Lock, Unlock,
  Link as LinkIcon, FileArchive, FolderInput, FileText, Server, FolderUp, LogOut, Check,
  Sheet, Presentation,
} from 'lucide-vue-next'
import { useFilesStore, type View } from '@/stores/files'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import type { FileNode } from '@/lib/api'
import { fmtSize, isArchive } from '@/lib/format'
import { bannerFor } from '@/lib/branding'
import LanguageMenu from '@/components/LanguageMenu.vue'
import SearchFiltersBar from './SearchFiltersBar.vue'
import FolderChip from './FolderChip.vue'
import FileCard from './FileCard.vue'
import FileRow from './FileRow.vue'
import PreviewPanel from './PreviewPanel.vue'
import PreviewOverlay from './PreviewOverlay.vue'
import StoragePanel from './StoragePanel.vue'
import SharesPanel from './SharesPanel.vue'
import UploadsPanel from './UploadsPanel.vue'
import Toasts from './Toasts.vue'
import ShareDialog from './ShareDialog.vue'
import WebdavDialog from './WebdavDialog.vue'
import MoveDialog from './MoveDialog.vue'
import ContextMenu, { type MenuItem } from './ContextMenu.vue'

const { t } = useI18n()
const files = useFilesStore()
const ui = useUiStore()
const auth = useAuthStore()

const searchInput = ref<HTMLInputElement>()
const fileInput = ref<HTMLInputElement>()
const dirInput = ref<HTMLInputElement>()
const dialogInput = ref<HTMLInputElement>()

type Dialog =
  | { type: 'rename'; id: number; value: string }
  | { type: 'folder'; value: string }
  | { type: 'office'; value: string; ext: string }
  | { type: 'purge'; ids: number[] }
  | { type: 'emptytrash' }
  | null
const dialog = ref<Dialog>(null)
const menu = ref<{ x: number; y: number; items: MenuItem[] } | null>(null)
const newMenuOpen = ref(false)
const accountMenuOpen = ref(false)
const dragDepth = ref(0)
const shareNode = ref<FileNode | null>(null)
const webdavOpen = ref(false)
const moveNodes = ref<FileNode[] | null>(null)
// Off-canvas sidebar drawer (mobile only; ignored on wide layouts via CSS).
const sidebarOpen = ref(false)
// Search filter bar toggle (lets users filter without typing a query).
const filtersOpen = ref(false)

const searchTerm = ref('')
let searchTimer: number | undefined
watch(searchTerm, (v) => {
  clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => files.setQuery(v), 200)
})

const locationOf = (n: FileNode) => {
  if (n.parent_id == null) return t('shell.myDrive')
  const crumb = files.path.find((c) => c.id === n.parent_id)
  return crumb ? crumb.name : t('shell.myDrive')
}

/* Navigation */
function gotoView(v: View) {
  searchTerm.value = ''
  files.gotoView(v)
  closeMenus()
  sidebarOpen.value = false
}

/* Menus */
function closeMenus() {
  menu.value = null
  newMenuOpen.value = false
  accountMenuOpen.value = false
}
// Close the mobile drawer (called from its in-drawer actions).
function closeSidebar() {
  sidebarOpen.value = false
}

function onCtx(node: FileNode, ev: MouseEvent) {
  ev.preventDefault()
  // Right-click selects the item (without opening the details panel).
  if (!files.sel.includes(node.id)) files.sel = [node.id]
  menu.value = {
    x: Math.min(ev.clientX, window.innerWidth - 240),
    y: Math.min(ev.clientY, window.innerHeight - 300),
    items: ctxItems(),
  }
  newMenuOpen.value = false
}

function bgCtx(ev: MouseEvent) {
  if (files.view !== 'drive' || files.searching) return
  files.clearSel()
  menu.value = {
    x: Math.min(ev.clientX, window.innerWidth - 240),
    y: Math.min(ev.clientY, window.innerHeight - 140),
    items: [
      { id: 'newfolder', label: t('shell.newFolder'), icon: FolderPlus },
      { id: 'import', label: t('shell.importFiles'), icon: Upload },
      { id: 'importfolder', label: t('shell.importFolder'), icon: FolderUp },
      ...(officeCreateItems.value.length ? [{ sep: true } as MenuItem] : []),
      ...officeCreateItems.value.map(
        (it): MenuItem => ({ id: `office:${it.ext}`, label: t(it.labelKey), icon: it.icon }),
      ),
    ],
  }
}

function ctxItems(): MenuItem[] {
  if (files.view === 'trash') {
    return [
      { id: 'restore', label: t('common.restore'), icon: RotateCcw },
      { sep: true },
      { id: 'purge', label: t('shell.deletePermanently'), icon: Trash2, danger: true },
    ]
  }
  const sel = files.selNodes
  const multi = sel.length > 1
  const n = sel[0]
  const items: MenuItem[] = []
  if (!multi && n) {
    if (n.type === 'folder') items.push({ id: 'open', label: t('common.open'), icon: Folder })
    else items.push({ id: 'preview', label: t('shell.preview'), icon: Eye })
    if (n.type === 'file') items.push({ id: 'download', label: t('common.download'), icon: Download })
    // Touch has no hover/side-panel, so surface details and multi-select here.
    if (ui.coarse) {
      items.push({ id: 'details', label: t('shell.details'), icon: Info })
      if (!files.selectionMode) items.push({ id: 'selectItem', label: t('shell.select'), icon: Check })
    }
  } else {
    items.push({ id: 'download', label: t('common.download'), icon: Download })
  }
  if (!files.readOnly && n && (multi || n.type === 'folder')) {
    items.push({ id: 'archive', label: t('shell.downloadAsArchive'), icon: FileArchive })
  }
  if (!files.readOnly && n) {
    items.push({ id: 'compress', label: t('shell.compressToZip'), icon: FileArchive })
  }
  if (!files.readOnly && !multi && n && n.type === 'file' && isArchive(n.name)) {
    items.push({ id: 'extract', label: t('shell.extractHere'), icon: FolderInput })
  }
  if (!files.readOnly && !multi && n && n.type === 'file' && ui.canEditOffice(n.name)) {
    items.push({ id: 'office', label: t('shell.editWithOffice'), icon: FileText })
  }
  if (!multi && n && auth.canShare) {
    items.push({ id: 'share', label: t('common.share'), icon: Share2 })
    if (n.type === 'file') items.push({ id: 'directlink', label: t('shell.copyDirectLink'), icon: LinkIcon })
  }
  if (!files.readOnly && n) {
    items.push({ sep: true })
    items.push({ id: 'move', label: t('shell.moveTo'), icon: FolderInput })
    if (!multi) {
      items.push({ id: 'rename', label: t('common.rename'), icon: Pencil })
      items.push({ id: 'star', label: n.starred ? t('shell.unstar') : t('shell.star'), icon: Star })
      items.push({ id: 'lock', label: n.locked ? t('shell.unlock') : t('shell.lock'), icon: n.locked ? Unlock : Lock })
    }
    items.push({ sep: true })
    items.push({ id: 'trash', label: t('shell.moveToTrash'), icon: Trash2, danger: true })
  }
  return items
}

function menuAction(id: string) {
  const sel = files.selNodes
  menu.value = null
  if (id.startsWith('office:')) {
    const ext = id.slice('office:'.length)
    const typ = OFFICE_NEW_TYPES.find((t) => t.exts.includes(ext))
    if (typ) newOffice({ ext, nameKey: typ.nameKey })
    return
  }
  switch (id) {
    case 'open': if (sel[0]) files.openFolder(sel[0]); break
    case 'preview': if (sel[0]) files.openNode(sel[0]); break
    case 'details': if (sel[0]) files.previewId = sel[0].id; break
    case 'selectItem': if (sel[0]) files.enterSelection(sel[0]); break
    case 'download': doDownload(sel); break
    case 'rename': startRename(); break
    case 'star': if (sel[0]) files.toggleStar(sel[0]); break
    case 'share': if (sel[0]) shareNode.value = sel[0]; break
    case 'directlink': if (sel[0]) files.createDirectLink(sel[0]); break
    case 'archive': files.downloadArchive([...files.sel]); break
    case 'compress': files.compress([...files.sel]); break
    case 'extract': if (sel[0]) files.extract(sel[0]); break
    case 'office': if (sel[0]) files.openOffice(sel[0]); break
    case 'lock': if (sel[0]) files.setLock(sel[0], !sel[0].locked); break
    case 'move': if (sel.length) moveNodes.value = [...sel]; break
    case 'trash': files.trash([...files.sel]); break
    case 'restore': files.restore([...files.sel]); break
    case 'purge': dialog.value = { type: 'purge', ids: [...files.sel] }; break
    case 'newfolder': openNewFolder(); break
    case 'import': triggerUpload(); break
    case 'importfolder': triggerFolderUpload(); break
  }
}

/* Actions */
function doDownload(nodes: FileNode[]) {
  nodes.filter((n) => n.type === 'file').forEach((n) => files.download(n))
}
function startRename() {
  const n = files.selNodes[0]
  if (!n) return
  dialog.value = { type: 'rename', id: n.id, value: n.name }
  focusDialog()
}
function openNewFolder() {
  closeMenus()
  sidebarOpen.value = false
  dialog.value = { type: 'folder', value: t('shell.newFolder') }
  focusDialog()
}

// Blank-document types offered in the New menu, filtered to the formats the
// document server can create (WOPI editnew), preferring OOXML over ODF.
const OFFICE_NEW_TYPES = [
  { exts: ['docx', 'odt'], icon: FileText, labelKey: 'shell.newDocument', nameKey: 'shell.untitledDocument' },
  { exts: ['xlsx', 'ods'], icon: Sheet, labelKey: 'shell.newSpreadsheet', nameKey: 'shell.untitledSpreadsheet' },
  { exts: ['pptx', 'odp'], icon: Presentation, labelKey: 'shell.newPresentation', nameKey: 'shell.untitledPresentation' },
]
const officeCreateItems = computed(() =>
  OFFICE_NEW_TYPES.map((typ) => {
    const ext = typ.exts.find((e) => ui.officeNew.includes(e))
    return ext ? { ...typ, ext } : null
  }).filter((x): x is NonNullable<typeof x> => x !== null),
)
function newOffice(item: { ext: string; nameKey: string }) {
  closeMenus()
  sidebarOpen.value = false
  dialog.value = { type: 'office', value: `${t(item.nameKey)}.${item.ext}`, ext: item.ext }
  focusDialog()
}
function focusDialog() {
  nextTick(() => {
    dialogInput.value?.focus()
    dialogInput.value?.select()
  })
}
async function confirmDialog() {
  const d = dialog.value
  if (!d) return
  if (d.type === 'rename') {
    const v = d.value.trim()
    if (v) await files.rename(d.id, v)
  } else if (d.type === 'folder') {
    await files.createFolder(d.value.trim() || t('shell.newFolder'))
  } else if (d.type === 'office') {
    const name = d.value.trim()
    if (name) {
      const node = await files.createOffice(name)
      if (node) files.openOffice(node)
    }
  } else if (d.type === 'purge') {
    await files.purge(d.ids)
  } else if (d.type === 'emptytrash') {
    await files.emptyTrash()
  }
  dialog.value = null
}

/* Upload */
function triggerUpload() {
  closeMenus()
  sidebarOpen.value = false
  fileInput.value?.click()
}
function onFileInput(e: Event) {
  const input = e.target as HTMLInputElement
  const list = Array.from(input.files || [])
  input.value = ''
  if (list.length) files.upload(list)
}
function triggerFolderUpload() {
  closeMenus()
  sidebarOpen.value = false
  dirInput.value?.click()
}
function onDirInput(e: Event) {
  const input = e.target as HTMLInputElement
  // A directory <input> reports each file's path in webkitRelativePath.
  const entries = Array.from(input.files || []).map((file) => ({
    file,
    path: (file as any).webkitRelativePath || file.name,
  }))
  input.value = ''
  if (entries.length) files.uploadTree(entries)
}

/* Drag & drop */
function hasFiles(e: DragEvent) {
  return !!e.dataTransfer && Array.from(e.dataTransfer.types || []).includes('Files')
}
function onDragEnter(e: DragEvent) {
  if (hasFiles(e)) dragDepth.value++
}
function onDragLeave(e: DragEvent) {
  if (hasFiles(e) && dragDepth.value > 0) dragDepth.value--
}
function onDrop(e: DragEvent) {
  dragDepth.value = 0
  const dt = e.dataTransfer
  if (!dt) return
  // Capture directory entries synchronously (the items list is cleared once the
  // handler returns), then walk them asynchronously.
  const roots = dt.items
    ? Array.from(dt.items)
        .map((it) => (it.webkitGetAsEntry ? it.webkitGetAsEntry() : null))
        .filter(Boolean)
    : []
  if (roots.some((r: any) => r?.isDirectory)) {
    collectEntries(roots as any[]).then((entries) => {
      if (entries.length) files.uploadTree(entries)
    })
    return
  }
  const list = Array.from(dt.files || [])
  if (list.length) files.upload(list)
}

// Recursively read dropped FileSystemEntry roots into {file, path} pairs.
async function collectEntries(roots: any[]): Promise<{ file: File; path: string }[]> {
  const out: { file: File; path: string }[] = []
  const walk = async (entry: any, prefix: string): Promise<void> => {
    if (entry.isFile) {
      const file: File = await new Promise((res, rej) => entry.file(res, rej))
      out.push({ file, path: prefix + entry.name })
    } else if (entry.isDirectory) {
      const reader = entry.createReader()
      const readBatch = (): Promise<any[]> => new Promise((res, rej) => reader.readEntries(res, rej))
      for (let batch = await readBatch(); batch.length; batch = await readBatch()) {
        for (const child of batch) await walk(child, prefix + entry.name + '/')
      }
    }
  }
  for (const r of roots) await walk(r, '')
  return out
}

/* Keyboard */
function isTyping(e: KeyboardEvent) {
  const t = e.target as HTMLElement
  return !!(t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable))
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (dialog.value) dialog.value = null
    else if (menu.value || newMenuOpen.value || accountMenuOpen.value) closeMenus()
    else if (files.previewId) files.previewId = null
    else files.clearSel()
  } else if ((e.key === 'Delete' || e.key === 'Backspace') && files.sel.length && !isTyping(e)) {
    if (files.view === 'trash') dialog.value = { type: 'purge', ids: [...files.sel] }
    else if (!files.readOnly) files.trash([...files.sel])
  } else if (e.key === '/' && !isTyping(e)) {
    e.preventDefault()
    searchInput.value?.focus()
  }
}

const dropTargetName = computed(() => {
  const c = files.crumbs
  return files.view === 'drive' ? c[c.length - 1].name : t('shell.myDrive')
})

onMounted(() => {
  files.init()
  ui.watchPointer()
  ui.loadOffice()
  document.addEventListener('keydown', onKey)
})
onUnmounted(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <div
    class="app"
    @click="closeMenus"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent
    @dragleave.prevent="onDragLeave"
    @drop.prevent="onDrop"
  >
    <!-- Mobile drawer backdrop -->
    <div v-if="sidebarOpen" class="sidebar-backdrop" @click="closeSidebar"></div>

    <!-- Sidebar -->
    <aside class="sidebar" :class="{ open: sidebarOpen }">
      <div class="logo">
        <img class="brand-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />
      </div>
      <div class="new-wrap">
        <button class="btn btn-primary btn-new" @click.stop="newMenuOpen = !newMenuOpen">
          <Plus :size="16" />{{ t('shell.new') }}
        </button>
        <div v-if="newMenuOpen" class="menu new-menu" @click.stop>
          <button class="menu-item" @click="openNewFolder"><FolderPlus :size="16" />{{ t('shell.newFolder') }}</button>
          <button class="menu-item" @click="triggerUpload"><Upload :size="16" />{{ t('shell.importFiles') }}</button>
          <button class="menu-item" @click="triggerFolderUpload"><FolderUp :size="16" />{{ t('shell.importFolder') }}</button>
          <template v-if="officeCreateItems.length">
            <div class="menu-sep"></div>
            <button v-for="it in officeCreateItems" :key="it.ext" class="menu-item" @click="newOffice(it)">
              <component :is="it.icon" :size="16" />{{ t(it.labelKey) }}
            </button>
          </template>
        </div>
      </div>
      <nav :aria-label="t('shell.mainNav')">
        <button class="nav-item" :class="{ active: files.view === 'drive' }" @click="gotoView('drive')"><HardDrive :size="18" />{{ t('shell.myDrive') }}</button>
        <button class="nav-item" :class="{ active: files.view === 'shares' }" @click="gotoView('shares')"><Share2 :size="18" />{{ t('shell.myShares') }}</button>
        <button class="nav-item" :class="{ active: files.view === 'trash' }" @click="gotoView('trash')">
          <Trash2 :size="18" />{{ t('shell.trash') }}<span v-if="files.trashCount" class="nav-count">{{ files.trashCount }}</span>
        </button>
        <div class="nav-sep"></div>
        <button class="nav-item" :class="{ active: files.view === 'storage' }" @click="gotoView('storage')"><Database :size="18" />{{ t('shell.storage') }}</button>
        <button class="nav-item" @click="webdavOpen = true; closeSidebar()"><Server :size="18" />{{ t('shell.webdavAccess') }}</button>
      </nav>
      <div class="quota">
        <div class="quota-bar"><div class="quota-fill" :style="{ width: files.quotaPct + '%' }"></div></div>
        <span class="quota-text">{{ fmtSize(files.quota.used) }} / {{ fmtSize(files.quota.total) }}</span>
        <button class="quota-link" @click="gotoView('storage')">{{ t('shell.manageStorage') }}</button>
      </div>
    </aside>

    <div class="main">
      <!-- Topbar -->
      <header class="topbar">
        <button class="icon-btn topbar-burger" :aria-label="t('shell.mainNav')" @click.stop="sidebarOpen = !sidebarOpen">
          <Menu :size="18" />
        </button>
        <label class="searchbox">
          <Search :size="16" />
          <input ref="searchInput" v-model="searchTerm" type="search" :placeholder="t('shell.searchPlaceholder')" :aria-label="t('common.search')" />
          <kbd>/</kbd>
        </label>
        <div class="topbar-right">
          <button
            class="icon-btn"
            :class="{ active: filtersOpen || files.hasFilters }"
            :title="t('search.filters')"
            @click="filtersOpen = !filtersOpen"
          >
            <SlidersHorizontal :size="16" />
          </button>
          <div class="segmented" role="group" :aria-label="t('shell.displayMode')">
            <button class="icon-btn" :class="{ active: ui.mode === 'grid' }" :title="t('shell.gridView')" @click="ui.setMode('grid')"><Grid3x3 :size="16" /></button>
            <button class="icon-btn" :class="{ active: ui.mode === 'list' }" :title="t('shell.listView')" @click="ui.setMode('list')"><List :size="16" /></button>
          </div>
          <button v-if="auth.isAdmin" class="icon-btn" :title="t('shell.administration')" @click="$router.push('/admin')"><Shield :size="16" /></button>
          <LanguageMenu />
          <button class="icon-btn" :title="ui.theme === 'dark' ? t('shell.lightTheme') : t('shell.darkTheme')" @click="ui.toggleTheme">
            <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
          </button>
          <div class="account-wrap">
            <button class="avatar" :title="auth.me?.nick" @click.stop="accountMenuOpen = !accountMenuOpen">
              <img v-if="auth.me?.avatar" :src="auth.me.avatar" alt="" />
              <template v-else>{{ auth.initials }}</template>
            </button>
            <div v-if="accountMenuOpen" class="menu account-menu" @click.stop>
              <div class="account-id">
                <span class="account-name">{{ auth.me?.nick }}</span>
                <span class="account-email">{{ auth.me?.email }}</span>
              </div>
              <button class="menu-item" @click="auth.logout()"><LogOut :size="16" />{{ t('shell.logout') }}</button>
            </div>
          </div>
        </div>
      </header>

      <div class="workspace">
        <main class="content" @click.self="files.clearSel" @contextmenu.prevent="bgCtx">
          <!-- Header -->
          <div class="content-head">
            <template v-if="files.sel.length">
              <div class="selbar">
                <button class="icon-btn" :title="t('shell.clearSelection')" @click="files.clearSel"><X :size="16" /></button>
                <span class="selbar-label">{{ t('shell.selectedCount', files.sel.length) }}</span>
                <span class="selbar-spacer"></span>
                <template v-if="files.view === 'trash'">
                  <button class="icon-btn" :title="t('common.restore')" @click="files.restore([...files.sel])"><RotateCcw :size="16" /></button>
                  <button class="icon-btn" :title="t('shell.deletePermanently')" @click="dialog = { type: 'purge', ids: [...files.sel] }"><Trash2 :size="16" /></button>
                </template>
                <template v-else>
                  <button class="icon-btn" :title="t('common.download')" @click="doDownload(files.selNodes)"><Download :size="16" /></button>
                  <button v-if="files.sel.length === 1 && files.selNodes[0]?.type === 'file'" class="icon-btn" :title="t('common.share')" @click="shareNode = files.selNodes[0]"><Share2 :size="16" /></button>
                  <button v-if="!files.readOnly && files.sel.length === 1" class="icon-btn" :title="t('common.rename')" @click="startRename"><Pencil :size="16" /></button>
                  <button v-if="!files.readOnly" class="icon-btn" :title="t('shell.moveTo')" @click="moveNodes = [...files.selNodes]"><FolderInput :size="16" /></button>
                  <button v-if="!files.readOnly" class="icon-btn" :title="t('shell.trash')" @click="files.trash([...files.sel])"><Trash2 :size="16" /></button>
                </template>
              </div>
            </template>
            <template v-else>
              <h1 v-if="files.searching" class="view-title">{{ t('shell.results') }}</h1>
              <h1 v-else-if="files.crumbs.length === 1" class="view-title">{{ files.viewLabel }}</h1>
              <div v-else class="crumbs">
                <template v-for="(c, i) in files.crumbs" :key="i">
                  <span v-if="i > 0" class="crumb-sep"><ChevronRight :size="14" /></span>
                  <button class="crumb" :class="{ current: i === files.crumbs.length - 1 }" @click="files.crumbTo(i)">{{ c.name }}</button>
                </template>
              </div>
              <span v-if="files.view !== 'storage' && files.view !== 'shares'" class="head-count">{{ t('shell.itemCount', files.nodes.length) }}</span>
              <button
                v-if="files.view === 'trash' && files.nodes.length"
                class="btn btn-secondary"
                style="margin-left: auto"
                @click="dialog = { type: 'emptytrash' }"
              >
                <Trash2 :size="15" />{{ t('shell.emptyTrash') }}
              </button>
            </template>
          </div>

          <!-- Search filters -->
          <SearchFiltersBar v-if="(filtersOpen || files.searching) && files.view === 'drive'" />

          <!-- Storage view -->
          <StoragePanel v-if="files.view === 'storage'" :files="files.storageFiles" :total="files.quota.total" @open="files.previewId = $event.id" />

          <!-- My shares view -->
          <SharesPanel v-else-if="files.view === 'shares'" :shares="files.shares" />

          <template v-else>
            <div v-if="files.view === 'trash' && files.nodes.length" class="notice">
              <Info :size="16" />{{ t('shell.trashRetentionNotice') }}
            </div>

            <!-- Grid -->
            <template v-if="ui.mode === 'grid'">
              <section v-if="files.folders.length" class="section">
                <span class="eyebrow">{{ t('shell.folders') }}</span>
                <div class="folder-grid" @click.self="files.clearSel">
                  <FolderChip
                    v-for="n in files.folders"
                    :key="n.id"
                    :node="n"
                    :selected="files.sel.includes(n.id)"
                    @select="files.select"
                    @open="files.openNode"
                    @menu="onCtx"
                  />
                </div>
              </section>
              <section v-if="files.files.length" class="section">
                <span class="eyebrow">{{ t('shell.files') }}</span>
                <div class="file-grid" @click.self="files.clearSel">
                  <FileCard
                    v-for="n in files.files"
                    :key="n.id"
                    :node="n"
                    :selected="files.sel.includes(n.id)"
                    @select="files.select"
                    @open="files.openNode"
                    @menu="onCtx"
                  />
                </div>
              </section>
            </template>

            <!-- List -->
            <div v-else-if="files.nodes.length" class="listing">
              <div class="list-head">
                <span>{{ t('common.name') }}</span>
                <span>{{ t('shell.owner') }}</span>
                <span>{{ t('shell.modified') }}</span>
                <span>{{ t('shell.size') }}</span>
                <span></span>
              </div>
              <FileRow
                v-for="n in files.orderedNodes"
                :key="n.id"
                :node="n"
                :selected="files.sel.includes(n.id)"
                @select="files.select"
                @open="files.openNode"
                @menu="onCtx"
              />
            </div>

            <!-- Empty -->
            <div v-if="!files.nodes.length && !files.loading" class="empty">
              <component :is="files.searching ? Search : files.view === 'trash' ? Trash2 : Upload" :size="36" />
              <span class="empty-title">
                <template v-if="files.searching">{{ t('shell.noResults', { q: files.q.trim() }) }}</template>
                <template v-else-if="files.view === 'trash'">{{ t('shell.trashEmpty') }}</template>
                <template v-else>{{ t('shell.folderEmpty') }}</template>
              </span>
              <span class="empty-sub">
                <template v-if="files.searching">{{ t('shell.noResultsSub') }}</template>
                <template v-else-if="files.view === 'trash'">{{ t('shell.trashEmptySub') }}</template>
                <template v-else>{{ t('shell.folderEmptySub') }}</template>
              </span>
            </div>
          </template>
        </main>

        <PreviewPanel
          v-if="files.previewNode"
          :node="files.previewNode"
          :location="locationOf(files.previewNode)"
          @close="files.previewId = null"
          @download="files.previewNode && files.download(files.previewNode)"
          @star="files.previewNode && files.toggleStar(files.previewNode)"
        />
      </div>
    </div>

    <PreviewOverlay v-if="files.overlayNode" :node="files.overlayNode" @close="files.closeOverlay()" />

    <ContextMenu v-if="menu" :x="menu.x" :y="menu.y" :items="menu.items" :sheet="ui.coarse" @action="menuAction" @close="menu = null" />

    <!-- Dialogs -->
    <div v-if="dialog" class="overlay" @click.self="dialog = null">
      <div class="dialog">
        <template v-if="dialog.type === 'rename'">
          <h2>{{ t('common.rename') }}</h2>
          <input ref="dialogInput" v-model="dialog.value" class="input" type="text" @keyup.enter="confirmDialog" />
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">{{ t('common.cancel') }}</button>
            <button class="btn btn-primary" @click="confirmDialog">{{ t('common.rename') }}</button>
          </div>
        </template>
        <template v-else-if="dialog.type === 'folder'">
          <h2>{{ t('shell.newFolder') }}</h2>
          <input ref="dialogInput" v-model="dialog.value" class="input" type="text" :placeholder="t('shell.folderNamePlaceholder')" @keyup.enter="confirmDialog" />
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">{{ t('common.cancel') }}</button>
            <button class="btn btn-primary" @click="confirmDialog">{{ t('common.create') }}</button>
          </div>
        </template>
        <template v-else-if="dialog.type === 'office'">
          <h2>{{ t('shell.newDocumentTitle') }}</h2>
          <input ref="dialogInput" v-model="dialog.value" class="input" type="text" @keyup.enter="confirmDialog" />
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">{{ t('common.cancel') }}</button>
            <button class="btn btn-primary" @click="confirmDialog">{{ t('common.create') }}</button>
          </div>
        </template>
        <template v-else-if="dialog.type === 'purge'">
          <h2>{{ t('shell.deletePermanentlyConfirm') }}</h2>
          <p>{{ t('shell.purgeWarning', dialog.ids.length) }}</p>
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">{{ t('common.cancel') }}</button>
            <button class="btn btn-danger" @click="confirmDialog">{{ t('common.delete') }}</button>
          </div>
        </template>
        <template v-else-if="dialog.type === 'emptytrash'">
          <h2>{{ t('shell.emptyTrashConfirm') }}</h2>
          <p>{{ t('shell.emptyTrashWarning') }}</p>
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">{{ t('common.cancel') }}</button>
            <button class="btn btn-danger" @click="confirmDialog">{{ t('shell.emptyTrash') }}</button>
          </div>
        </template>
      </div>
    </div>

    <ShareDialog v-if="shareNode" :node="shareNode" @close="shareNode = null" />
    <WebdavDialog v-if="webdavOpen" @close="webdavOpen = false" />
    <MoveDialog v-if="moveNodes" :nodes="moveNodes" @close="moveNodes = null" @moved="moveNodes = null" />

    <UploadsPanel v-if="files.uploads.length" :uploads="files.uploads" />
    <Toasts />

    <div v-if="dragDepth > 0" class="drop-overlay">
      <div class="drop-card">
        <Upload :size="32" />
        <span>{{ t('shell.dropToUpload') }}</span>
        <span class="mono" style="font-size: 11px; color: var(--accent)">{{ dropTargetName }}</span>
      </div>
    </div>

    <input ref="fileInput" type="file" multiple style="display: none" @change="onFileInput" />
    <input ref="dirInput" type="file" webkitdirectory multiple style="display: none" @change="onDirInput" />
  </div>
</template>
