<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Download, Lock, TriangleAlert, Sun, Moon, Folder, FileText, ChevronRight,
  FolderArchive, FolderOpen, Upload, FolderPlus, Pencil, Trash2, UploadCloud, Check, Eye, X,
} from 'lucide-vue-next'
import { api, type ShareView as ShareViewData, type ShareEntry } from '@/lib/api'
import { kindFromName, fmtSize, previewKind, canThumbnail } from '@/lib/format'
import { uploadInChunks } from '@/lib/upload'
import { metaFor } from '@/lib/icons'
import { useUiStore } from '@/stores/ui'
import { bannerFor } from '@/lib/branding'

const route = useRoute()
const { t } = useI18n()
const ui = useUiStore()
const token = String(route.params.token)

const data = ref<ShareViewData | null>(null)
const notFound = ref(false)
const password = ref('')
const error = ref('')

// Folder browsing state.
const opened = ref(false)
const entries = ref<ShareEntry[]>([])
const curPath = ref('')

// Write/deposit state.
const contributor = ref('')
type UploadItem = { name: string; pct: number; done: boolean; error: boolean }
const uploads = ref<UploadItem[]>([])
const dragover = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

const canWrite = computed(() => data.value?.permission === 'write')
const isDeposit = computed(() => data.value?.permission === 'deposit')
// Edit label only when the share grants write AND the format is editable on the
// document server; otherwise it opens view-only.
function officeLabel(name: string): string {
  return canWrite.value && ui.canEditOffice(name) ? t('shareView.editOffice') : t('shareView.viewOffice')
}

function openOffice(path?: string) {
  window.open(api.shareOfficeUrl(token, path, password.value || undefined), '_blank')
}

const meta = computed(() => (data.value ? metaFor(kindFromName(data.value.name)) : metaFor('file')))
const unavailable = computed(() => data.value && (data.value.expired || data.value.exhausted))

// Show the file's own thumbnail on the landing card when the type can be
// previewed; fall back to the type icon if the image fails to load.
const thumbFailed = ref(false)
const showThumb = computed(() => !!data.value && !data.value.is_dir && data.value.previewable && !thumbFailed.value)

// In-page inline viewer (image / video / audio / pdf / text). Uncounted preview;
// the explicit download button still meters. `viewer` holds the open file.
const viewer = ref<{ path: string; name: string } | null>(null)
const viewerKind = computed(() => (viewer.value ? previewKind(viewer.value.name) : 'none'))
const viewerSrc = computed(() =>
  viewer.value ? api.shareInlineUrl(token, viewer.value.path || undefined, password.value || undefined) : ''
)
function canPreview(name: string): boolean {
  return ['image', 'video', 'audio', 'pdf', 'text'].includes(previewKind(name))
}
function openPreview(path: string, name: string) {
  viewer.value = { path, name }
}
// Row icon: the file's thumbnail when the type supports one, else its kind icon.
function entryIconFailed(e: ShareEntry): boolean {
  return thumbFailedPaths.value.has(e.path)
}
const thumbFailedPaths = ref<Set<string>>(new Set())
function markThumbFailed(path: string) {
  thumbFailedPaths.value = new Set(thumbFailedPaths.value).add(path)
}
const crumbs = computed(() => {
  const parts = curPath.value ? curPath.value.split('/') : []
  const acc: { name: string; path: string }[] = [{ name: data.value?.name || '', path: '' }]
  let p = ''
  for (const seg of parts) {
    p = p ? `${p}/${seg}` : seg
    acc.push({ name: seg, path: p })
  }
  return acc
})

onMounted(async () => {
  ui.loadOffice()
  try {
    data.value = await api.shareView(token)
    // Read/write folders auto-open the listing; a deposit share is blind (no
    // listing) and a password-protected share waits for the password first.
    if (data.value.is_dir && !data.value.has_password && !unavailable.value && !isDeposit.value) openList('')
  } catch {
    notFound.value = true
  }
})

async function openList(p: string) {
  error.value = ''
  try {
    const res = await api.shareList(token, p, password.value || undefined)
    entries.value = res.entries
    curPath.value = p
    opened.value = true
  } catch (e: any) {
    if (e?.code === 42900) error.value = t('shareView.tooManyRequests')
    else error.value = e?.code === 401 ? t('shareView.passwordIncorrect') : e?.message || t('shareView.accessDenied')
  }
}

function downloadFile(entry: ShareEntry) {
  window.location.href = api.shareContentUrl(token, entry.path, password.value || undefined)
}
// Clicking a folder opens it; a viewable file previews in-page; anything else
// downloads.
function onEntry(entry: ShareEntry) {
  if (entry.is_dir) openList(entry.path)
  else if (canPreview(entry.name)) openPreview(entry.path, entry.name)
  else downloadFile(entry)
}
function downloadFolderArchive() {
  window.location.href = api.shareArchiveUrl(token, curPath.value, password.value || undefined)
}

// --- Write / deposit ---------------------------------------------------------

async function uploadFiles(files: FileList | File[], path: string) {
  for (const file of Array.from(files)) {
    const item = reactive<UploadItem>({ name: file.name, pct: 0, done: false, error: false })
    uploads.value.push(item)
    try {
      await uploadInChunks(
        file,
        {
          init: () =>
            api.shareInitUpload(token, {
              path,
              name: file.name,
              size: file.size,
              contributor: contributor.value || undefined,
              password: password.value || undefined,
            }),
          putChunk: (sid, i, blob) => api.shareChunk(token, sid, i, blob),
          complete: (sid) => api.shareComplete(token, sid),
        },
        (loaded) => {
          item.pct = file.size ? Math.round((loaded / file.size) * 100) : 100
        },
      )
      item.pct = 100
      item.done = true
    } catch {
      item.error = true
    }
  }
  // Refresh the listing (write only — a deposit share cannot list).
  if (opened.value && canWrite.value) openList(curPath.value)
}

function pickFiles() {
  fileInput.value?.click()
}
function onFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) uploadFiles(input.files, isDeposit.value ? '' : curPath.value)
  input.value = ''
}
function onDrop(e: DragEvent) {
  dragover.value = false
  const files = e.dataTransfer?.files
  if (files?.length) uploadFiles(files, isDeposit.value ? '' : curPath.value)
}

async function newFolder() {
  const name = window.prompt(t('shareView.newFolderPrompt'))
  if (!name) return
  try {
    await api.shareCreateFolder(token, curPath.value, name, password.value || undefined)
    openList(curPath.value)
  } catch (e: any) {
    error.value = e?.message || t('shareView.accessDenied')
  }
}
async function renameEntry(entry: ShareEntry) {
  const name = window.prompt(t('shareView.renamePrompt'), entry.name)
  if (!name || name === entry.name) return
  try {
    await api.shareRename(token, entry.path, name, password.value || undefined)
    openList(curPath.value)
  } catch (e: any) {
    error.value = e?.message || t('shareView.accessDenied')
  }
}
async function deleteEntry(entry: ShareEntry) {
  if (!window.confirm(t('shareView.deleteConfirm', { name: entry.name }))) return
  try {
    await api.shareDeleteItem(token, entry.path, password.value || undefined)
    openList(curPath.value)
  } catch (e: any) {
    error.value = e?.message || t('shareView.accessDenied')
  }
}

// Single-file share download. A lightweight `check=1` request validates the
// share (204) and surfaces password/expiry errors inline; the actual download is
// then a plain browser navigation — never a fetch of the whole (possibly large)
// file, and it counts as exactly one download.
async function download() {
  error.value = ''
  const url = api.shareContentUrl(token, undefined, password.value || undefined)
  const probe = url + (url.includes('?') ? '&' : '?') + 'check=1'
  try {
    const r = await fetch(probe)
    if (r.status === 401) return void (error.value = t('shareView.passwordIncorrect'))
    if (r.status === 429) return void (error.value = t('shareView.tooManyRequests'))
    if (r.status === 403) return void (error.value = t('shareView.linkUnavailable'))
    if (!r.ok) return void (error.value = t('shareView.downloadFailed'))
  } catch {
    /* network hiccup on the probe — fall through and try the download anyway */
  }
  window.location.href = url
}
</script>

<template>
  <div class="login-screen">
    <button class="icon-btn sv-theme" @click="ui.toggleTheme">
      <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
    </button>

    <input ref="fileInput" type="file" multiple hidden @change="onFilePicked" />

    <div class="sv-card" :class="{ wide: opened || isDeposit }">
      <img class="brand-banner sv-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />

      <template v-if="notFound">
        <div class="sv-state">
          <TriangleAlert :size="40" class="tint-danger" />
          <h1 class="sv-title">{{ t('shareView.linkNotFound') }}</h1>
          <p class="sv-meta">{{ t('shareView.linkNotFoundDesc') }}</p>
        </div>
      </template>

      <template v-else-if="data">
        <!-- Deposit (blind drop box): upload only, no listing. -->
        <template v-if="isDeposit && !unavailable">
          <div class="sv-head">
            <h1 class="sv-title">{{ data.name }}</h1>
            <p class="sv-meta">{{ t('shareView.depositInto', { owner: data.owner }) }}</p>
          </div>
          <label v-if="data.has_password" class="sv-field">
            <span class="sv-field-lbl"><Lock :size="12" />{{ t('shareView.password') }}</span>
            <input v-model="password" class="input" type="password" :placeholder="t('shareView.required')" />
          </label>
          <label class="sv-field">
            <span class="sv-field-lbl">{{ t('shareView.yourName') }}</span>
            <input v-model="contributor" class="input" type="text" :placeholder="t('shareView.yourNamePlaceholder')" />
          </label>
          <div
            class="sv-drop"
            :class="{ over: dragover }"
            @click="pickFiles"
            @dragover.prevent="dragover = true"
            @dragleave="dragover = false"
            @drop.prevent="onDrop"
          >
            <UploadCloud :size="30" class="tint-neutral" />
            <b>{{ t('shareView.dropHere') }}</b>
          </div>
          <div v-if="uploads.length" class="sv-up">
            <div v-for="(u, i) in uploads" :key="i" class="sv-upi">
              <component :is="u.error ? TriangleAlert : u.done ? Check : Upload" :size="14"
                :class="u.error ? 'tint-danger' : u.done ? 'tint-success' : 'tint-neutral'" />
              <span class="nm">{{ u.name }}</span>
              <span class="pc mono">{{ u.error ? '!' : u.pct + '%' }}</span>
            </div>
          </div>
        </template>

        <!-- Folder browser (read / write) -->
        <template v-else-if="data.is_dir && opened">
          <div
            class="sv-folder"
            @dragover.prevent="canWrite && (dragover = true)"
            @dragleave="dragover = false"
            @drop.prevent="canWrite && onDrop($event)"
          >
            <div class="sv-crumbs">
              <template v-for="(c, i) in crumbs" :key="c.path">
                <ChevronRight v-if="i > 0" :size="13" class="tint-neutral" />
                <button class="sv-crumb" :class="{ here: i === crumbs.length - 1 }" @click="openList(c.path)">
                  {{ i === 0 ? data.name : c.name }}
                </button>
              </template>
            </div>

            <div v-if="canWrite" class="sv-toolbar">
              <button class="btn btn-secondary btn-sm" @click="newFolder"><FolderPlus :size="14" />{{ t('shareView.newFolder') }}</button>
              <button class="btn btn-secondary btn-sm" @click="pickFiles"><Upload :size="14" />{{ t('shareView.importFiles') }}</button>
            </div>

            <div class="sv-list" :class="{ over: dragover && canWrite }">
              <div v-if="!entries.length" class="sv-empty">{{ t('shareView.emptyFolder') }}</div>
              <div v-for="e in entries" :key="e.path" class="sv-row">
                <button class="sv-row-main" @click="onEntry(e)">
                  <img
                    v-if="!e.is_dir && canThumbnail(e.name) && !entryIconFailed(e)"
                    class="sv-thumb"
                    :src="api.shareThumbUrl(token, e.path)"
                    alt=""
                    loading="lazy"
                    @error="markThumbFailed(e.path)"
                  />
                  <span v-else class="sv-ic" :class="{ folder: e.is_dir }">
                    <component :is="e.is_dir ? Folder : metaFor(kindFromName(e.name)).icon" :size="16" />
                  </span>
                  <span class="sv-rn">{{ e.name }}</span>
                  <span class="sv-rs mono">{{ e.is_dir ? '' : fmtSize(e.size) }}</span>
                </button>
                <button v-if="!e.is_dir && ui.canViewOffice(e.name)" class="sv-act" :title="officeLabel(e.name)" @click.stop="openOffice(e.path)">
                  <FileText :size="14" />
                </button>
                <template v-if="canWrite">
                  <button class="sv-act" :title="t('common.rename')" @click.stop="renameEntry(e)"><Pencil :size="14" /></button>
                  <button class="sv-act danger" :title="t('common.delete')" @click.stop="deleteEntry(e)"><Trash2 :size="14" /></button>
                </template>
                <component :is="e.is_dir ? ChevronRight : Download" :size="15" class="sv-tail" @click="e.is_dir ? openList(e.path) : downloadFile(e)" />
              </div>
            </div>

            <div v-if="uploads.length" class="sv-up">
              <div v-for="(u, i) in uploads" :key="i" class="sv-upi">
                <component :is="u.error ? TriangleAlert : u.done ? Check : Upload" :size="14"
                  :class="u.error ? 'tint-danger' : u.done ? 'tint-success' : 'tint-neutral'" />
                <span class="nm">{{ u.name }}</span>
                <span class="pc mono">{{ u.error ? '!' : u.pct + '%' }}</span>
              </div>
            </div>

            <p v-if="error" class="sv-error">{{ error }}</p>
            <button class="btn btn-secondary sv-btn" @click="downloadFolderArchive">
              <FolderArchive :size="15" />{{ t('shareView.downloadFolderZip') }}
            </button>
          </div>
        </template>

        <!-- Landing (file share, or locked folder awaiting password) -->
        <template v-else>
          <div class="sv-file">
            <div class="sv-preview">
              <img v-if="showThumb" :src="api.shareThumbUrl(token)" :alt="data.name" class="sv-poster" @error="thumbFailed = true" />
              <component v-else :is="data.is_dir ? Folder : meta.icon" :size="46" :class="data.is_dir ? 'tint-folder' : 'tint-' + meta.tint" />
            </div>
            <h1 class="sv-title">{{ data.name }}</h1>
            <p class="sv-meta">
              <span>{{ data.is_dir ? t('shareView.sharedFolder') : fmtSize(data.size) }}</span>
              <span class="sv-dot">·</span>
              <span>{{ t('shareView.sharedBy', { owner: data.owner }) }}</span>
            </p>

            <p v-if="unavailable" class="sv-error">
              {{ data.expired ? t('shareView.linkExpired') : t('shareView.downloadLimitReached') }}
            </p>
            <template v-else>
              <label v-if="data.has_password" class="sv-field">
                <span class="sv-field-lbl"><Lock :size="12" />{{ t('shareView.password') }}</span>
                <input v-model="password" class="input" type="password" :placeholder="t('shareView.required')"
                  @keyup.enter="data.is_dir ? openList('') : download()" />
              </label>
              <p v-if="error" class="sv-error">{{ error }}</p>
              <div class="sv-actions">
                <button class="btn btn-primary sv-btn" @click="data.is_dir ? openList('') : download()">
                  <component :is="data.is_dir ? FolderOpen : Download" :size="16" />
                  {{ data.is_dir ? t('shareView.openFolder') : t('common.download') }}
                </button>
                <button v-if="!data.is_dir && canPreview(data.name)" class="btn btn-secondary sv-btn" @click="openPreview('', data.name)">
                  <Eye :size="16" />{{ t('shareView.preview') }}
                </button>
                <button v-if="!data.is_dir && ui.canViewOffice(data.name)" class="btn btn-secondary sv-btn" @click="openOffice()">
                  <FileText :size="16" />{{ officeLabel(data.name) }}
                </button>
              </div>
            </template>
          </div>
        </template>
      </template>

      <template v-else>
        <p class="sv-loading">{{ t('common.loading') }}</p>
      </template>
    </div>

    <!-- In-page inline viewer overlay -->
    <div v-if="viewer" class="share-viewer" @click.self="viewer = null">
      <button class="icon-btn share-viewer-close" :title="t('common.close')" @click="viewer = null">
        <X :size="18" />
      </button>
      <img v-if="viewerKind === 'image'" :src="viewerSrc" :alt="viewer.name" class="share-viewer-media" />
      <video v-else-if="viewerKind === 'video'" :src="viewerSrc" controls autoplay class="share-viewer-media" />
      <audio v-else-if="viewerKind === 'audio'" :src="viewerSrc" controls autoplay />
      <iframe v-else-if="viewerKind === 'pdf' || viewerKind === 'text'" :src="viewerSrc" class="share-viewer-frame" />
      <a
        class="btn btn-secondary share-viewer-dl"
        :href="api.shareContentUrl(token, viewer.path || undefined, password || undefined)"
      >
        <Download :size="15" />{{ t('common.download') }}
      </a>
    </div>
  </div>
</template>

<style scoped>
/* ── Share page card ─────────────────────────────────────────────── */
.sv-theme {
  position: fixed;
  top: 16px;
  right: 16px;
}
.sv-card {
  width: 92vw;
  max-width: 420px;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-xl);
  box-shadow: 0 30px 70px -24px oklch(0.05 0.02 285 / 0.75);
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  transition: max-width var(--t-med);
}
.sv-card.wide {
  max-width: 620px;
}
.sv-banner {
  align-self: center;
  height: 30px;
  width: auto;
}
.sv-state,
.sv-head {
  text-align: center;
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: center;
}
.sv-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--fg-0);
  margin: 0;
  line-height: 1.3;
  word-break: break-word;
}
.sv-meta {
  font-size: 13px;
  color: var(--fg-2);
  margin: 0;
  display: flex;
  gap: 7px;
  justify-content: center;
  align-items: center;
  flex-wrap: wrap;
}
.sv-meta .sv-dot {
  color: var(--fg-3);
}
.sv-error {
  color: var(--danger);
  font-size: 12.5px;
  margin: 0;
  text-align: center;
}
.sv-loading {
  color: var(--fg-2);
  text-align: center;
}
.sv-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  text-align: left;
}
.sv-field-lbl {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-3);
  font-weight: 600;
}

/* ── Landing (single file) ───────────────────────────────────────── */
.sv-file {
  display: flex;
  flex-direction: column;
  gap: 14px;
  text-align: center;
}
.sv-preview {
  height: 172px;
  border-radius: var(--r-lg);
  border: 1px solid var(--border-subtle);
  background: var(--bg-inset);
  display: grid;
  place-items: center;
  overflow: hidden;
}
.sv-preview .sv-poster {
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.sv-file .sv-title {
  font-size: 17px;
}
.sv-actions {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.sv-btn {
  width: 100%;
  height: 42px;
  justify-content: center;
}

/* ── Folder browser ──────────────────────────────────────────────── */
.sv-folder {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.sv-crumbs {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-wrap: wrap;
  font-size: 13px;
}
.sv-crumb {
  background: none;
  border: 0;
  color: var(--fg-2);
  font: inherit;
  font-size: 13px;
  cursor: pointer;
  padding: 3px 6px;
  border-radius: var(--r-sm);
  transition: var(--t-fast);
}
.sv-crumb:hover {
  background: var(--bg-2);
  color: var(--fg-0);
}
.sv-crumb.here {
  color: var(--fg-0);
  font-weight: 600;
}
.sv-toolbar {
  display: flex;
  gap: 8px;
}
.sv-list {
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  overflow: hidden;
  transition: border-color var(--t-fast);
}
.sv-list.over {
  border-color: var(--accent);
}
.sv-empty {
  text-align: center;
  color: var(--fg-3);
  font-size: 13px;
  padding: 22px 0;
}
.sv-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  transition: background var(--t-fast);
}
.sv-row + .sv-row {
  border-top: 1px solid var(--border-subtle);
}
.sv-row:hover {
  background: var(--bg-2);
}
.sv-row-main {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  background: none;
  border: 0;
  cursor: pointer;
  padding: 0;
  text-align: left;
}
.sv-thumb {
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  object-fit: cover;
  flex: none;
  border: 1px solid var(--border-subtle);
}
.sv-ic {
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  display: grid;
  place-items: center;
  flex: none;
  background: var(--bg-2);
  color: var(--fg-2);
}
.sv-ic.folder {
  color: var(--fg-1);
}
.sv-rn {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 13.5px;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sv-rs {
  font-size: 12px;
  color: var(--fg-3);
  flex: none;
}
.sv-act {
  flex: none;
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  border: 0;
  background: transparent;
  color: var(--fg-3);
  display: grid;
  place-items: center;
  cursor: pointer;
  transition: var(--t-fast);
}
.sv-act:hover {
  background: var(--bg-3);
  color: var(--fg-0);
}
.sv-act.danger:hover {
  background: var(--danger-bg);
  color: var(--danger);
}
.sv-tail {
  flex: none;
  color: var(--fg-3);
  opacity: 0.55;
  cursor: pointer;
  transition: var(--t-fast);
}
.sv-row:hover .sv-tail {
  opacity: 1;
  color: var(--fg-1);
}

/* ── Deposit ─────────────────────────────────────────────────────── */
.sv-drop {
  border: 1.5px dashed var(--border-strong);
  border-radius: var(--r-lg);
  background: var(--bg-inset);
  padding: 30px 20px;
  text-align: center;
  color: var(--fg-2);
  cursor: pointer;
  transition: var(--t-fast);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}
.sv-drop b {
  color: var(--fg-0);
  font-weight: 600;
  font-size: 14px;
}
.sv-drop:hover,
.sv-drop.over {
  border-color: var(--accent);
  background: var(--accent-bg);
  color: var(--fg-1);
}

/* ── Upload progress list (deposit + folder) ─────────────────────── */
.sv-up {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.sv-upi {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  background: var(--bg-2);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-sm);
  padding: 7px 10px;
}
.sv-upi .nm {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-1);
}
.sv-upi .pc {
  font-size: 11.5px;
  color: var(--fg-3);
  flex: none;
}

.share-viewer {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  padding: 3.5rem 1.5rem 1.5rem;
  background: rgba(0, 0, 0, 0.82);
  backdrop-filter: blur(4px);
}
.share-viewer-close {
  position: fixed;
  top: 16px;
  right: 16px;
  color: #fff;
}
.share-viewer-media {
  max-width: 92vw;
  max-height: 78vh;
  object-fit: contain;
  border-radius: 8px;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
}
.share-viewer-frame {
  width: min(92vw, 900px);
  height: 78vh;
  border: 0;
  border-radius: 8px;
  background: #fff;
}
.share-viewer-dl {
  text-decoration: none;
}
</style>
