<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import {
  Plus, FolderPlus, Upload, HardDrive, Users, Trash2, Database,
  Search, Grid3x3, List, Sun, Moon, ChevronRight, X, Folder, Eye,
  Download, Pencil, Star, RotateCcw, Info, Share2, Lock, Unlock,
  Link as LinkIcon, FileArchive,
} from 'lucide-vue-next'
import { useFilesStore, type View } from '@/stores/files'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import type { FileNode } from '@/lib/api'
import { fmtSize } from '@/lib/format'
import FolderChip from './FolderChip.vue'
import FileCard from './FileCard.vue'
import FileRow from './FileRow.vue'
import PreviewPanel from './PreviewPanel.vue'
import StoragePanel from './StoragePanel.vue'
import UploadsPanel from './UploadsPanel.vue'
import TweaksPanel from './TweaksPanel.vue'
import Toasts from './Toasts.vue'
import ShareDialog from './ShareDialog.vue'
import ContextMenu, { type MenuItem } from './ContextMenu.vue'

const files = useFilesStore()
const ui = useUiStore()
const auth = useAuthStore()

const searchInput = ref<HTMLInputElement>()
const fileInput = ref<HTMLInputElement>()
const dialogInput = ref<HTMLInputElement>()

type Dialog =
  | { type: 'rename'; id: number; value: string }
  | { type: 'folder'; value: string }
  | { type: 'purge'; ids: number[] }
  | null
const dialog = ref<Dialog>(null)
const menu = ref<{ x: number; y: number; items: MenuItem[] } | null>(null)
const newMenuOpen = ref(false)
const dragDepth = ref(0)
const shareNode = ref<FileNode | null>(null)

const searchTerm = ref('')
let searchTimer: number | undefined
watch(searchTerm, (v) => {
  clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => files.setQuery(v), 200)
})

const locationOf = (n: FileNode) => {
  if (n.parent_id == null) return files.view === 'shared' ? 'Partagés avec moi' : 'Mon Drive'
  const crumb = files.path.find((c) => c.id === n.parent_id)
  return crumb ? crumb.name : 'Mon Drive'
}

/* Navigation */
function gotoView(v: View) {
  searchTerm.value = ''
  files.gotoView(v)
  closeMenus()
}

/* Menus */
function closeMenus() {
  menu.value = null
  newMenuOpen.value = false
}

function onCtx(node: FileNode, ev: MouseEvent) {
  ev.preventDefault()
  if (!files.sel.includes(node.id)) files.select(node)
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
      { id: 'newfolder', label: 'Nouveau dossier', icon: FolderPlus },
      { id: 'import', label: 'Importer des fichiers', icon: Upload },
    ],
  }
}

function ctxItems(): MenuItem[] {
  if (files.view === 'trash') {
    return [
      { id: 'restore', label: 'Restaurer', icon: RotateCcw },
      { sep: true },
      { id: 'purge', label: 'Supprimer définitivement', icon: Trash2, danger: true },
    ]
  }
  const sel = files.selNodes
  const multi = sel.length > 1
  const n = sel[0]
  const items: MenuItem[] = []
  if (!multi && n) {
    if (n.type === 'folder') items.push({ id: 'open', label: 'Ouvrir', icon: Folder })
    else items.push({ id: 'preview', label: 'Aperçu', icon: Eye })
    if (n.type === 'file') items.push({ id: 'download', label: 'Télécharger', icon: Download })
  } else {
    items.push({ id: 'download', label: 'Télécharger', icon: Download })
  }
  if (!files.readOnly && n && (multi || n.type === 'folder')) {
    items.push({ id: 'archive', label: 'Télécharger en archive', icon: FileArchive })
  }
  if (!multi && n && n.type === 'file') {
    items.push({ id: 'share', label: 'Partager', icon: Share2 })
    items.push({ id: 'directlink', label: 'Copier le lien direct', icon: LinkIcon })
  }
  if (!files.readOnly && n) {
    items.push({ sep: true })
    if (!multi) {
      items.push({ id: 'rename', label: 'Renommer', icon: Pencil })
      items.push({ id: 'star', label: n.starred ? 'Ne plus suivre' : 'Suivre', icon: Star })
      items.push({ id: 'lock', label: n.locked ? 'Déverrouiller' : 'Verrouiller', icon: n.locked ? Unlock : Lock })
    }
    items.push({ sep: true })
    items.push({ id: 'trash', label: 'Déplacer vers la corbeille', icon: Trash2, danger: true })
  }
  return items
}

function menuAction(id: string) {
  const sel = files.selNodes
  menu.value = null
  switch (id) {
    case 'open': if (sel[0]) files.openFolder(sel[0]); break
    case 'preview': if (sel[0]) files.previewId = sel[0].id; break
    case 'download': doDownload(sel); break
    case 'rename': startRename(); break
    case 'star': if (sel[0]) files.toggleStar(sel[0]); break
    case 'share': if (sel[0]) shareNode.value = sel[0]; break
    case 'directlink': if (sel[0]) files.createDirectLink(sel[0]); break
    case 'archive': files.downloadArchive([...files.sel]); break
    case 'lock': if (sel[0]) files.setLock(sel[0], !sel[0].locked); break
    case 'trash': files.trash([...files.sel]); break
    case 'restore': files.restore([...files.sel]); break
    case 'purge': dialog.value = { type: 'purge', ids: [...files.sel] }; break
    case 'newfolder': openNewFolder(); break
    case 'import': triggerUpload(); break
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
  dialog.value = { type: 'folder', value: 'Nouveau dossier' }
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
    await files.createFolder(d.value.trim() || 'Nouveau dossier')
  } else if (d.type === 'purge') {
    await files.purge(d.ids)
  }
  dialog.value = null
}

/* Upload */
function triggerUpload() {
  closeMenus()
  fileInput.value?.click()
}
function onFileInput(e: Event) {
  const input = e.target as HTMLInputElement
  const list = Array.from(input.files || [])
  input.value = ''
  if (list.length) files.upload(list)
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
  const list = Array.from(e.dataTransfer?.files || [])
  if (list.length) files.upload(list)
}

/* Keyboard */
function isTyping(e: KeyboardEvent) {
  const t = e.target as HTMLElement
  return !!(t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable))
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    if (dialog.value) dialog.value = null
    else if (menu.value || newMenuOpen.value) closeMenus()
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
  return files.view === 'drive' ? c[c.length - 1].name : 'Mon Drive'
})

onMounted(() => {
  ui.init()
  files.init()
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
    <!-- Sidebar -->
    <aside class="sidebar">
      <div class="logo">
        <span class="glyph">◆</span>
        <span class="logo-word"><em>Orion</em><strong>Drive</strong></span>
      </div>
      <div class="new-wrap">
        <button class="btn btn-primary btn-new" @click.stop="newMenuOpen = !newMenuOpen">
          <Plus :size="16" />Nouveau
        </button>
        <div v-if="newMenuOpen" class="menu new-menu" @click.stop>
          <button class="menu-item" @click="openNewFolder"><FolderPlus :size="16" />Nouveau dossier</button>
          <button class="menu-item" @click="triggerUpload"><Upload :size="16" />Importer des fichiers</button>
        </div>
      </div>
      <nav aria-label="Navigation principale">
        <button class="nav-item" :class="{ active: files.view === 'drive' }" @click="gotoView('drive')"><HardDrive :size="18" />Mon Drive</button>
        <button class="nav-item" :class="{ active: files.view === 'shared' }" @click="gotoView('shared')"><Users :size="18" />Partagés avec moi</button>
        <button class="nav-item" :class="{ active: files.view === 'trash' }" @click="gotoView('trash')">
          <Trash2 :size="18" />Corbeille<span v-if="files.trashCount" class="nav-count">{{ files.trashCount }}</span>
        </button>
        <div class="nav-sep"></div>
        <button class="nav-item" :class="{ active: files.view === 'storage' }" @click="gotoView('storage')"><Database :size="18" />Stockage</button>
      </nav>
      <div class="quota">
        <div class="quota-bar"><div class="quota-fill" :style="{ width: files.quotaPct + '%' }"></div></div>
        <span class="quota-text">{{ fmtSize(files.quota.used) }} / {{ fmtSize(files.quota.total) }}</span>
        <button class="quota-link" @click="gotoView('storage')">Gérer le stockage</button>
      </div>
    </aside>

    <div class="main">
      <!-- Topbar -->
      <header class="topbar">
        <label class="searchbox">
          <Search :size="16" />
          <input ref="searchInput" v-model="searchTerm" type="search" placeholder="Rechercher dans OrionDrive" aria-label="Rechercher" />
          <kbd>/</kbd>
        </label>
        <div class="topbar-right">
          <div class="segmented" role="group" aria-label="Mode d'affichage">
            <button class="icon-btn" :class="{ active: ui.mode === 'grid' }" title="Grille" @click="ui.setMode('grid')"><Grid3x3 :size="16" /></button>
            <button class="icon-btn" :class="{ active: ui.mode === 'list' }" title="Liste" @click="ui.setMode('list')"><List :size="16" /></button>
          </div>
          <button class="icon-btn" :title="ui.theme === 'dark' ? 'Thème clair' : 'Thème sombre'" @click="ui.toggleTheme">
            <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
          </button>
          <button class="icon-btn" title="Réglages d'affichage" @click="ui.tweaksOpen = !ui.tweaksOpen"><Info :size="16" /></button>
          <div class="avatar" :title="auth.me?.nick">
            <img v-if="auth.me?.avatar" :src="auth.me.avatar" alt="" />
            <template v-else>{{ auth.initials }}</template>
          </div>
        </div>
      </header>

      <div class="workspace">
        <main class="content" @click.self="files.clearSel" @contextmenu.self.prevent="bgCtx">
          <!-- Header -->
          <div class="content-head">
            <template v-if="files.sel.length">
              <div class="selbar">
                <button class="icon-btn" title="Annuler la sélection" @click="files.clearSel"><X :size="16" /></button>
                <span class="selbar-label">{{ files.sel.length }} sélectionné{{ files.sel.length > 1 ? 's' : '' }}</span>
                <span class="selbar-spacer"></span>
                <template v-if="files.view === 'trash'">
                  <button class="icon-btn" title="Restaurer" @click="files.restore([...files.sel])"><RotateCcw :size="16" /></button>
                  <button class="icon-btn" title="Supprimer définitivement" @click="dialog = { type: 'purge', ids: [...files.sel] }"><Trash2 :size="16" /></button>
                </template>
                <template v-else>
                  <button class="icon-btn" title="Télécharger" @click="doDownload(files.selNodes)"><Download :size="16" /></button>
                  <button v-if="files.sel.length === 1 && files.selNodes[0]?.type === 'file'" class="icon-btn" title="Partager" @click="shareNode = files.selNodes[0]"><Share2 :size="16" /></button>
                  <button v-if="!files.readOnly && files.sel.length === 1" class="icon-btn" title="Renommer" @click="startRename"><Pencil :size="16" /></button>
                  <button v-if="!files.readOnly" class="icon-btn" title="Corbeille" @click="files.trash([...files.sel])"><Trash2 :size="16" /></button>
                </template>
              </div>
            </template>
            <template v-else>
              <h1 v-if="files.searching" class="view-title">Résultats</h1>
              <h1 v-else-if="files.crumbs.length === 1" class="view-title">{{ files.viewLabel }}</h1>
              <div v-else class="crumbs">
                <template v-for="(c, i) in files.crumbs" :key="i">
                  <span v-if="i > 0" class="crumb-sep"><ChevronRight :size="14" /></span>
                  <button class="crumb" :class="{ current: i === files.crumbs.length - 1 }" @click="files.crumbTo(i)">{{ c.name }}</button>
                </template>
              </div>
              <span v-if="files.view !== 'storage'" class="head-count">{{ files.nodes.length }} élément{{ files.nodes.length > 1 ? 's' : '' }}</span>
            </template>
          </div>

          <!-- Storage view -->
          <StoragePanel v-if="files.view === 'storage'" :files="files.storageFiles" :total="files.quota.total" @open="files.previewId = $event.id" />

          <template v-else>
            <div v-if="files.view === 'trash' && files.nodes.length" class="notice">
              <Info :size="16" />Les éléments de la corbeille sont supprimés définitivement après 30 jours.
            </div>

            <!-- Grid -->
            <template v-if="ui.mode === 'grid'">
              <section v-if="files.folders.length" class="section">
                <span class="eyebrow">Dossiers</span>
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
                <span class="eyebrow">Fichiers</span>
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
                <span>Nom</span>
                <span>{{ files.view === 'shared' ? 'Partagé par' : 'Propriétaire' }}</span>
                <span>Modifié</span>
                <span>Taille</span>
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
              <component :is="files.searching ? Search : files.view === 'trash' ? Trash2 : files.view === 'shared' ? Users : Upload" :size="36" />
              <span class="empty-title">
                <template v-if="files.searching">Aucun résultat pour « {{ files.q.trim() }} »</template>
                <template v-else-if="files.view === 'trash'">La corbeille est vide</template>
                <template v-else-if="files.view === 'shared'">Rien de partagé pour l'instant</template>
                <template v-else>Dossier vide</template>
              </span>
              <span class="empty-sub">
                <template v-if="files.searching">Essaie un autre terme ou change de vue.</template>
                <template v-else-if="files.view === 'trash'">Les éléments supprimés apparaîtront ici.</template>
                <template v-else-if="files.view === 'shared'">Les fichiers partagés avec toi apparaîtront ici.</template>
                <template v-else>Glisse-dépose des fichiers ici, ou utilise le bouton Nouveau.</template>
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

    <ContextMenu v-if="menu" :x="menu.x" :y="menu.y" :items="menu.items" @action="menuAction" />

    <!-- Dialogs -->
    <div v-if="dialog" class="overlay" @click.self="dialog = null">
      <div class="dialog">
        <template v-if="dialog.type === 'rename'">
          <h2>Renommer</h2>
          <input ref="dialogInput" v-model="dialog.value" class="input" type="text" @keyup.enter="confirmDialog" />
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">Annuler</button>
            <button class="btn btn-primary" @click="confirmDialog">Renommer</button>
          </div>
        </template>
        <template v-else-if="dialog.type === 'folder'">
          <h2>Nouveau dossier</h2>
          <input ref="dialogInput" v-model="dialog.value" class="input" type="text" placeholder="Nom du dossier" @keyup.enter="confirmDialog" />
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">Annuler</button>
            <button class="btn btn-primary" @click="confirmDialog">Créer</button>
          </div>
        </template>
        <template v-else-if="dialog.type === 'purge'">
          <h2>Supprimer définitivement ?</h2>
          <p>{{ dialog.ids.length > 1 ? dialog.ids.length + ' éléments seront supprimés' : 'Cet élément sera supprimé' }} définitivement. Cette action est irréversible.</p>
          <div class="dialog-actions">
            <button class="btn btn-ghost" @click="dialog = null">Annuler</button>
            <button class="btn btn-danger" @click="confirmDialog">Supprimer</button>
          </div>
        </template>
      </div>
    </div>

    <ShareDialog v-if="shareNode" :node="shareNode" @close="shareNode = null" />

    <UploadsPanel v-if="files.uploads.length" :uploads="files.uploads" />
    <Toasts />
    <TweaksPanel v-if="ui.tweaksOpen" @close="ui.tweaksOpen = false" />

    <div v-if="dragDepth > 0" class="drop-overlay">
      <div class="drop-card">
        <Upload :size="32" />
        <span>Dépose tes fichiers pour les importer</span>
        <span class="mono" style="font-size: 11px; color: var(--accent)">{{ dropTargetName }}</span>
      </div>
    </div>

    <input ref="fileInput" type="file" multiple style="display: none" @change="onFileInput" />
  </div>
</template>
