<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  Download, Lock, TriangleAlert, Sun, Moon, Folder, FileText, ChevronRight,
  FolderArchive, FolderOpen, Upload, FolderPlus, Pencil, Trash2, UploadCloud, Check,
} from 'lucide-vue-next'
import { api, type ShareView as ShareViewData, type ShareEntry } from '@/lib/api'
import { kindFromName, fmtSize } from '@/lib/format'
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
function downloadFolderArchive() {
  window.location.href = api.shareArchiveUrl(token, curPath.value, password.value || undefined)
}

// --- Write / deposit ---------------------------------------------------------

async function uploadFiles(files: FileList | File[], path: string) {
  for (const file of Array.from(files)) {
    const item = reactive<UploadItem>({ name: file.name, pct: 0, done: false, error: false })
    uploads.value.push(item)
    try {
      const init = await api.shareInitUpload(token, {
        path,
        name: file.name,
        size: file.size,
        contributor: contributor.value || undefined,
        password: password.value || undefined,
      })
      let sent = 0
      for (let i = 0; i < init.num_chunks; i++) {
        const start = i * init.chunk_size
        const blob = file.slice(start, Math.min(start + init.chunk_size, file.size))
        await api.shareChunk(token, init.session_id, i, blob)
        sent += blob.size
        item.pct = file.size ? Math.round((sent / file.size) * 100) : 100
      }
      await api.shareComplete(token, init.session_id)
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
    <button class="icon-btn" style="position: fixed; top: 16px; right: 16px" @click="ui.toggleTheme">
      <component :is="ui.theme === 'dark' ? Sun : Moon" :size="16" />
    </button>

    <input ref="fileInput" type="file" multiple hidden @change="onFilePicked" />

    <div class="login-card" :style="{ gap: '20px', maxWidth: opened || isDeposit ? '620px' : undefined, width: opened || isDeposit ? '92vw' : undefined }">
      <img class="brand-banner" :src="bannerFor(ui.theme)" alt="OrionDrive" />

      <template v-if="notFound">
        <TriangleAlert :size="40" style="color: var(--danger)" />
        <h1>{{ t('shareView.linkNotFound') }}</h1>
        <p>{{ t('shareView.linkNotFoundDesc') }}</p>
      </template>

      <template v-else-if="data">
        <!-- Deposit (blind drop box): upload only, no listing. -->
        <template v-if="isDeposit && !unavailable">
          <div style="text-align: center">
            <h1 style="font-size: 20px">{{ data.name }}</h1>
            <p class="mono">{{ t('shareView.depositInto', { owner: data.owner }) }}</p>
          </div>
          <label v-if="data.has_password" style="width: 100%; display: flex; flex-direction: column; gap: 6px">
            <span class="tweak-label" style="letter-spacing: 0.06em; display: flex; align-items: center; gap: 6px">
              <Lock :size="12" />{{ t('shareView.password') }}
            </span>
            <input v-model="password" class="input" type="password" :placeholder="t('shareView.required')" />
          </label>
          <label style="width: 100%; display: flex; flex-direction: column; gap: 6px">
            <span class="tweak-label" style="letter-spacing: 0.06em">{{ t('shareView.yourName') }}</span>
            <input v-model="contributor" class="input" type="text" :placeholder="t('shareView.yourNamePlaceholder')" />
          </label>
          <div
            class="drop-zone"
            :class="{ over: dragover }"
            @click="pickFiles"
            @dragover.prevent="dragover = true"
            @dragleave="dragover = false"
            @drop.prevent="onDrop"
          >
            <UploadCloud :size="32" class="tint-neutral" />
            <p>{{ t('shareView.dropHere') }}</p>
          </div>
          <div v-if="uploads.length" class="upload-list">
            <div v-for="(u, i) in uploads" :key="i" class="upload-item">
              <component :is="u.error ? TriangleAlert : u.done ? Check : Upload" :size="14"
                :style="{ color: u.error ? 'var(--danger)' : u.done ? 'var(--success)' : 'var(--fg-2)' }" />
              <span class="upload-name">{{ u.name }}</span>
              <span class="mono" style="font-size: 12px">{{ u.error ? '!' : u.pct + '%' }}</span>
            </div>
          </div>
        </template>

        <!-- Folder browser (read / write) -->
        <template v-else-if="data.is_dir && opened">
          <div
            class="share-browser"
            @dragover.prevent="canWrite && (dragover = true)"
            @dragleave="dragover = false"
            @drop.prevent="canWrite && onDrop($event)"
          >
            <div class="share-crumbs">
              <template v-for="(c, i) in crumbs" :key="c.path">
                <ChevronRight v-if="i > 0" :size="13" class="tint-neutral" />
                <button class="crumb-btn" @click="openList(c.path)">{{ i === 0 ? data.name : c.name }}</button>
              </template>
            </div>

            <div v-if="canWrite" class="share-toolbar">
              <button class="btn btn-secondary btn-sm" @click="newFolder"><FolderPlus :size="14" />{{ t('shareView.newFolder') }}</button>
              <button class="btn btn-secondary btn-sm" @click="pickFiles"><Upload :size="14" />{{ t('shareView.importFiles') }}</button>
            </div>

            <div class="share-list" :class="{ 'drop-over': dragover && canWrite }">
              <div v-if="!entries.length" class="share-empty">{{ t('shareView.emptyFolder') }}</div>
              <div v-for="e in entries" :key="e.path" class="share-row">
                <button class="share-row-main" @click="e.is_dir ? openList(e.path) : downloadFile(e)">
                  <component :is="e.is_dir ? Folder : FileText" :size="16" :class="e.is_dir ? 'tint-folder' : 'tint-neutral'" />
                  <span class="share-name">{{ e.name }}</span>
                  <span class="mono share-size">{{ e.is_dir ? '' : fmtSize(e.size) }}</span>
                </button>
                <button
                  v-if="!e.is_dir && ui.canViewOffice(e.name)"
                  class="share-row-act"
                  :title="officeLabel(e.name)"
                  @click.stop="openOffice(e.path)"
                >
                  <FileText :size="14" />
                </button>
                <template v-if="canWrite">
                  <button class="share-row-act" :title="t('common.rename')" @click.stop="renameEntry(e)"><Pencil :size="14" /></button>
                  <button class="share-row-act danger" :title="t('common.delete')" @click.stop="deleteEntry(e)"><Trash2 :size="14" /></button>
                </template>
                <component :is="e.is_dir ? ChevronRight : Download" :size="15" class="tint-neutral share-row-tail"
                  @click="e.is_dir ? openList(e.path) : downloadFile(e)" />
              </div>
            </div>

            <div v-if="uploads.length" class="upload-list">
              <div v-for="(u, i) in uploads" :key="i" class="upload-item">
                <component :is="u.error ? TriangleAlert : u.done ? Check : Upload" :size="14"
                  :style="{ color: u.error ? 'var(--danger)' : u.done ? 'var(--success)' : 'var(--fg-2)' }" />
                <span class="upload-name">{{ u.name }}</span>
                <span class="mono" style="font-size: 12px">{{ u.error ? '!' : u.pct + '%' }}</span>
              </div>
            </div>

            <p v-if="error" style="color: var(--danger); font-size: 12.5px; margin: 0">{{ error }}</p>
            <button class="btn btn-secondary" style="width: 100%" @click="downloadFolderArchive">
              <FolderArchive :size="15" />{{ t('shareView.downloadFolderZip') }}
            </button>
          </div>
        </template>

        <!-- Landing (file share, or locked folder awaiting password) -->
        <template v-else>
          <div class="preview-visual" style="width: 100%; margin: 0; height: 120px">
            <component :is="data.is_dir ? Folder : meta.icon" :size="44" :class="data.is_dir ? 'tint-folder' : 'tint-' + meta.tint" />
          </div>
          <div style="text-align: center">
            <h1 style="font-size: 20px">{{ data.name }}</h1>
            <p class="mono">
              {{ data.is_dir ? t('shareView.sharedFolder') : fmtSize(data.size) }} · {{ t('shareView.sharedBy', { owner: data.owner }) }}
            </p>
          </div>

          <template v-if="unavailable">
            <p style="color: var(--danger)">
              {{ data.expired ? t('shareView.linkExpired') : t('shareView.downloadLimitReached') }}
            </p>
          </template>
          <template v-else>
            <label v-if="data.has_password" style="width: 100%; display: flex; flex-direction: column; gap: 6px">
              <span class="tweak-label" style="letter-spacing: 0.06em; display: flex; align-items: center; gap: 6px">
                <Lock :size="12" />{{ t('shareView.password') }}
              </span>
              <input v-model="password" class="input" type="password" :placeholder="t('shareView.required')"
                @keyup.enter="data.is_dir ? openList('') : download()" />
            </label>
            <p v-if="error" style="color: var(--danger); font-size: 12.5px; margin: 0">{{ error }}</p>
            <button
              v-if="!data.is_dir && ui.canViewOffice(data.name)"
              class="btn btn-secondary"
              style="width: 100%; height: 42px"
              @click="openOffice()"
            >
              <FileText :size="16" />{{ officeLabel(data.name) }}
            </button>
            <button class="btn btn-primary" style="width: 100%; height: 42px" @click="data.is_dir ? openList('') : download()">
              <component :is="data.is_dir ? FolderOpen : Download" :size="16" />
              {{ data.is_dir ? t('shareView.openFolder') : t('common.download') }}
            </button>
          </template>
        </template>
      </template>

      <template v-else>
        <p>{{ t('common.loading') }}</p>
      </template>
    </div>
  </div>
</template>
