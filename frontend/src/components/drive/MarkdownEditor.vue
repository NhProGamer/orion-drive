<script setup lang="ts">
/**
 * Collaborative Markdown editor. Several people type in the same document at
 * once; their browsers merge the text with a CRDT and the elected writer sends
 * the result back to the server, which snapshots it into the file.
 */
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DocPeer, DocStatus } from '@/lib/doc/protocol'
import type { DocSession } from '@/lib/doc/session'
import type { EditorHandle } from '@/lib/doc/editor'

const props = defineProps<{
  /** WebSocket URL of the document's session. */
  url: string
  /** Opens the document read-only regardless of what the session grants. */
  viewOnly?: boolean
}>()

/** Exposed so the parent can show the raw Markdown without a second request. */
const emit = defineEmits<{ markdown: [string] }>()

const { t } = useI18n()

const host = ref<HTMLElement | null>(null)
const status = ref<DocStatus>('connecting')
const canWrite = ref(false)
const peers = ref<DocPeer[]>([])
const selfID = ref('')
const justSaved = ref(false)
const failed = ref('')

let session: DocSession | null = null
let editor: EditorHandle | null = null
let isWriter = false
let saveTimer = 0
let savedTimer = 0

/** How long to coalesce edits before the writer persists the document. */
const SAVE_DEBOUNCE = 1200

/**
 * Persists the document. Only the peer the server elected writes, so two
 * editors never race on the same version — and it saves on any change, local or
 * remote, since it is writing on everyone's behalf.
 */
function scheduleSave() {
  if (!isWriter || !canWrite.value || props.viewOnly) return
  window.clearTimeout(saveTimer)
  saveTimer = window.setTimeout(() => {
    if (!editor || !session) return
    const text = editor.getMarkdown()
    session.save(text)
    emit('markdown', text)
  }, SAVE_DEBOUNCE)
}

onMounted(async () => {
  if (!host.value) return
  try {
    const [{ DocSession }, { createEditor }] = await Promise.all([
      import('@/lib/doc/session'),
      import('@/lib/doc/editor'),
    ])

    editor = await createEditor(host.value, {
      readonly: true, // until the session says otherwise
      placeholder: t('doc.placeholder'),
    })

    session = new DocSession(props.url, {
      onInit: (info) => {
        selfID.value = info.self
        canWrite.value = info.canWrite
        isWriter = info.writer
        peers.value = info.peers
        const me = info.peers.find((p) => p.id === info.self)
        session?.setLocalUser(me?.name || t('doc.you'), me?.color || '#7c6cf0')
        editor?.connect(session!, info.seed)
        editor?.setReadonly(!info.canWrite || !!props.viewOnly)
        if (info.seed !== null) emit('markdown', info.seed)
        // The document is live from here on; every change reaches the writer.
        session?.doc.on('update', scheduleSave)
      },
      onPeers: (list) => (peers.value = list),
      onWriter: (writer) => {
        isWriter = writer
        // Taking over mid-session means flushing what the previous writer may
        // not have persisted yet.
        if (writer) scheduleSave()
      },
      onWritable: (allowed) => {
        canWrite.value = allowed
        editor?.setReadonly(!allowed || !!props.viewOnly)
      },
      onSaved: () => {
        justSaved.value = true
        window.clearTimeout(savedTimer)
        savedTimer = window.setTimeout(() => (justSaved.value = false), 2000)
      },
      onStatus: (s) => (status.value = s),
      onFatal: (message) => (failed.value = message),
    })
    session.start()
  } catch {
    failed.value = t('doc.loadFailed')
  }
})

watch(
  () => props.viewOnly,
  (viewOnly) => editor?.setReadonly(!canWrite.value || !!viewOnly),
)

onUnmounted(async () => {
  window.clearTimeout(saveTimer)
  window.clearTimeout(savedTimer)
  session?.doc.off('update', scheduleSave)
  session?.stop()
  session = null
  const handle = editor
  editor = null
  await handle?.destroy()
})

const others = () => peers.value.filter((p) => p.id !== selfID.value)
</script>

<template>
  <div class="ov-doc">
    <div v-if="failed" class="ov-empty">{{ failed }}</div>
    <div v-show="!failed" ref="host" class="ov-doc-host"></div>
    <div class="doc-bar">
      <span v-if="status === 'connecting'" class="board-chip">{{ t('doc.connecting') }}</span>
      <span v-else-if="status === 'reconnecting'" class="board-chip">{{ t('doc.reconnecting') }}</span>
      <span v-else-if="status === 'closed'" class="board-chip">{{ t('doc.offline') }}</span>
      <span v-else-if="justSaved" class="board-chip board-chip-ok">{{ t('doc.saved') }}</span>
      <span v-if="(viewOnly || !canWrite) && status === 'live'" class="board-chip">{{ t('doc.readOnly') }}</span>
      <span class="board-peers" :title="others().map((p) => p.name).join(', ')">
        {{ others().length === 0 ? t('doc.alone') : t('doc.peers', { count: others().length }) }}
      </span>
    </div>
  </div>
</template>
