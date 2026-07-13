<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { X, Download, Save, Eye, Pencil, FileQuestion } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { previewKind, isMarkdown } from '@/lib/format'
import { renderMarkdown } from '@/lib/markdown'
import { useFilesStore } from '@/stores/files'

const props = defineProps<{ node: FileNode }>()
const emit = defineEmits<{ close: [] }>()

const files = useFilesStore()
const kind = computed(() => previewKind(props.node.name))
const src = computed(() => api.inlineUrl(props.node.id))
const markdown = computed(() => isMarkdown(props.node.name))

const text = ref('')
const original = ref('')
const loading = ref(false)
const mode = ref<'edit' | 'rendered'>('edit')
const dirty = computed(() => text.value !== original.value)
const rendered = computed(() => renderMarkdown(text.value))

async function loadText() {
  if (kind.value !== 'text') return
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
watch(() => props.node.id, loadText, { immediate: true })

async function save() {
  await files.saveText(props.node.id, text.value)
  original.value = text.value
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && !dirty.value) emit('close')
  if ((e.metaKey || e.ctrlKey) && e.key === 's' && kind.value === 'text') {
    e.preventDefault()
    if (dirty.value) save()
  }
}
onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div class="ov-backdrop" @click.self="!dirty && emit('close')">
    <div class="ov-panel">
      <header class="ov-head">
        <span class="ov-title">{{ node.name }}</span>
        <div class="ov-actions">
          <template v-if="kind === 'text'">
            <button v-if="markdown" class="icon-btn" :title="mode === 'edit' ? 'Aperçu' : 'Éditer'"
              @click="mode = mode === 'edit' ? 'rendered' : 'edit'">
              <component :is="mode === 'edit' ? Eye : Pencil" :size="16" />
            </button>
            <button class="btn btn-primary ov-save" :disabled="!dirty" @click="save">
              <Save :size="15" />Enregistrer
            </button>
          </template>
          <a class="icon-btn" title="Télécharger" :href="api.contentUrl(node.id)"><Download :size="16" /></a>
          <button class="icon-btn" title="Fermer" @click="emit('close')"><X :size="16" /></button>
        </div>
      </header>

      <div class="ov-body">
        <img v-if="kind === 'image'" :src="src" :alt="node.name" class="ov-media" />
        <video v-else-if="kind === 'video'" :src="src" controls class="ov-media"></video>
        <audio v-else-if="kind === 'audio'" :src="src" controls class="ov-audio"></audio>
        <iframe v-else-if="kind === 'pdf'" :src="src" class="ov-frame" title="PDF"></iframe>

        <template v-else-if="kind === 'text'">
          <div v-if="loading" class="ov-empty">Chargement…</div>
          <div v-else-if="markdown && mode === 'rendered'" class="ov-markdown" v-html="rendered"></div>
          <textarea v-else v-model="text" class="ov-editor" spellcheck="false"></textarea>
        </template>

        <div v-else class="ov-empty">
          <FileQuestion :size="48" />
          <p>Aucun aperçu disponible pour ce type de fichier.</p>
          <a class="btn btn-secondary" :href="api.contentUrl(node.id)"><Download :size="15" />Télécharger</a>
        </div>
      </div>
    </div>
  </div>
</template>
