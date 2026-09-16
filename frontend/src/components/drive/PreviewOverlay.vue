<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { X, Download, Save, Eye, Pencil, FileQuestion, Crop, Maximize2, Minimize2, Code2 } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { previewKind, isMarkdown } from '@/lib/format'
import { renderMarkdown } from '@/lib/markdown'
import { useFilesStore } from '@/stores/files'
import { useUiStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useFullscreen } from '@/composables/useFullscreen'
import EpubViewer from './EpubViewer.vue'
import ArchiveViewer from './ArchiveViewer.vue'
import ImageEditor from './ImageEditor.vue'

// The whiteboard drags in React and Excalidraw, so it stays in its own chunk:
// only a user who actually opens a board downloads them.
const BoardEditor = defineAsyncComponent(() => import('./BoardEditor.vue'))

// Same for the Markdown editor: ProseMirror, CodeMirror and the CRDT bindings
// only load once someone opens a Markdown file.
const MarkdownEditor = defineAsyncComponent(() => import('./MarkdownEditor.vue'))

const props = defineProps<{ node: FileNode }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()

const files = useFilesStore()
const ui = useUiStore()
const auth = useAuthStore()
const office = computed(() => ui.canEditOffice(props.node.name))
const kind = computed(() => previewKind(props.node.name))
const src = computed(() => api.inlineUrl(props.node.id))
const markdown = computed(() => isMarkdown(props.node.name))
// A Markdown file opens in the collaborative WYSIWYG when the server relays
// live sessions; otherwise it falls back to the plain textarea below.
const liveMarkdown = computed(() => markdown.value && auth.liveDocsEnabled)
// The raw Markdown behind the WYSIWYG, for the source view. It comes from the
// editor itself (the seed, then every autosave), so opening the source costs no
// extra request.
const docText = ref('')
const sourceMode = ref(false)
const editingImage = ref(false)

// A whiteboard is the one preview worth handing the whole screen: it is a
// canvas people draw on together, not something you glance at.
const panel = ref<HTMLElement | null>(null)
const { active: isFullscreen, supported: canFullscreen, toggle: toggleFullscreen } = useFullscreen(panel)

const text = ref('')
const original = ref('')
const loading = ref(false)
const mode = ref<'edit' | 'rendered'>('edit')
const dirty = computed(() => text.value !== original.value)
const rendered = computed(() => renderMarkdown(text.value))

async function loadText() {
  // A live document is seeded through its socket, so fetching it here would
  // only duplicate the work.
  if (kind.value !== 'text' || liveMarkdown.value) return
  loading.value = true
  try {
    const res = await fetch(src.value, { credentials: 'include' })
    text.value = original.value = await res.text()
    mode.value = markdown.value ? 'rendered' : 'edit'
  } catch {
    text.value = original.value = ''
  } finally {
    loading.value = false
  }
}
watch(() => props.node.id, () => {
  editingImage.value = false
  sourceMode.value = false
  docText.value = ''
  loadText()
}, { immediate: true })

async function save() {
  await files.saveText(props.node.id, text.value)
  original.value = text.value
}

function onKey(e: KeyboardEvent) {
  // Escape leaves fullscreen first (the browser handles that itself), so it
  // must not also close the panel underneath.
  if (e.key === 'Escape' && !dirty.value && !isFullscreen.value) emit('close')
  if ((e.metaKey || e.ctrlKey) && e.key === 's' && kind.value === 'text') {
    e.preventDefault()
    if (dirty.value) save()
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <!-- Drag events stop here. The drive shell listens for drops on its root to
       upload files, so a drop meant for the whiteboard canvas would otherwise
       land in the folder behind as well. They are also prevented, since without
       that the browser treats an unclaimed drop as "navigate to this file". -->
  <div
    class="ov-backdrop"
    :class="{ 'ov-backdrop-bare': kind === 'board' }"
    @click.self="!dirty && emit('close')"
    @dragenter.stop.prevent
    @dragover.stop.prevent
    @dragleave.stop.prevent
    @drop.stop.prevent
  >
    <div ref="panel" class="ov-panel" :class="{ 'ov-panel-viewport': kind === 'board' }">
      <header class="ov-head">
        <span class="ov-title">{{ node.name }}</span>
        <div class="ov-actions">
          <button
            v-if="liveMarkdown"
            class="icon-btn"
            :title="sourceMode ? t('doc.wysiwyg') : t('doc.source')"
            @click="sourceMode = !sourceMode"
          >
            <component :is="sourceMode ? Pencil : Code2" :size="16" />
          </button>
          <template v-if="kind === 'text' && !liveMarkdown">
            <button v-if="markdown" class="icon-btn" :title="mode === 'edit' ? t('previewOverlay.preview') : t('common.edit')"
              @click="mode = mode === 'edit' ? 'rendered' : 'edit'">
              <component :is="mode === 'edit' ? Eye : Pencil" :size="16" />
            </button>
            <button class="btn btn-primary ov-save" :disabled="!dirty" @click="save">
              <Save :size="15" />{{ t('common.save') }}
            </button>
          </template>
          <button
            v-if="kind === 'board' && canFullscreen"
            class="icon-btn"
            :title="isFullscreen ? t('board.exitFullscreen') : t('board.fullscreen')"
            @click="toggleFullscreen()"
          >
            <component :is="isFullscreen ? Minimize2 : Maximize2" :size="16" />
          </button>
          <button v-if="kind === 'image' && !editingImage" class="icon-btn" :title="t('previewOverlay.editImage')" @click="editingImage = true">
            <Crop :size="16" />
          </button>
          <a class="icon-btn" :title="t('common.download')" :href="api.contentUrl(node.id)"><Download :size="16" /></a>
          <button class="icon-btn" :title="t('common.close')" @click="emit('close')"><X :size="16" /></button>
        </div>
      </header>

      <div class="ov-body">
        <ImageEditor v-if="kind === 'image' && editingImage" :node="node" :url="src" @done="editingImage = false" />
        <img v-else-if="kind === 'image'" :src="src" :alt="node.name" class="ov-media" />
        <video v-else-if="kind === 'video'" :src="src" controls class="ov-media"></video>
        <audio v-else-if="kind === 'audio'" :src="src" controls class="ov-audio"></audio>
        <iframe v-else-if="kind === 'pdf'" :src="src" class="ov-frame" title="PDF"></iframe>
        <BoardEditor v-else-if="kind === 'board'" :url="api.boardSocketUrl(node.id)" :key="node.id" />
        <EpubViewer v-else-if="kind === 'epub'" :url="src" :name="node.name" :key="node.id" />
        <ArchiveViewer v-else-if="kind === 'archive'" :node="node" :key="node.id" />

        <template v-else-if="liveMarkdown">
          <!-- Kept mounted while the source is shown: unmounting would drop the
               live session, and with it this peer's place in the document. -->
          <pre v-show="sourceMode" class="ov-source">{{ docText }}</pre>
          <MarkdownEditor
            v-show="!sourceMode"
            :url="api.docSocketUrl(node.id)"
            :key="node.id"
            @markdown="docText = $event"
          />
        </template>

        <template v-else-if="kind === 'text'">
          <div v-if="loading" class="ov-empty">{{ t('common.loading') }}</div>
          <div v-else-if="markdown && mode === 'rendered'" class="ov-markdown" v-html="rendered"></div>
          <textarea v-else v-model="text" class="ov-editor" spellcheck="false"></textarea>
        </template>

        <div v-else class="ov-empty">
          <FileQuestion :size="48" />
          <p>{{ t('previewOverlay.noPreview') }}</p>
          <div style="display: flex; gap: 8px">
            <button v-if="office" class="btn btn-primary" @click="files.openOffice(node)"><Pencil :size="15" />{{ t('previewOverlay.editWithOffice') }}</button>
            <a class="btn btn-secondary" :href="api.contentUrl(node.id)"><Download :size="15" />{{ t('common.download') }}</a>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
