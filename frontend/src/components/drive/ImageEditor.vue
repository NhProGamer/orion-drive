<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RotateCcw, RotateCw, FlipHorizontal, FlipVertical, Crop, Check, Undo2, RefreshCcw, Save, X } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { useFilesStore } from '@/stores/files'

const { t } = useI18n()

const props = defineProps<{ node: FileNode; url: string }>()
const emit = defineEmits<{ done: [] }>()

const files = useFilesStore()
const view = ref<HTMLCanvasElement | null>(null)
const stage = ref<HTMLElement | null>(null)
const saving = ref(false)
const error = ref(false)

// Destructive-with-history: each op bakes a fresh canvas and pushes it, so
// `stack` is the undo history and its last item is the current image.
let stack: HTMLCanvasElement[] = []
const depth = ref(0)

const cropping = ref(false)
// Crop rectangle in normalised coordinates (0..1 of the image), resolution-free.
const crop = ref({ x: 0.1, y: 0.1, w: 0.8, h: 0.8 })

const lossy = /\.(jpe?g|webp)$/i.test(props.node.name)
const quality = ref(92)

const cur = () => stack[stack.length - 1]
const changed = computed(() => depth.value > 1)
const dirty = computed(() => changed.value || (lossy && quality.value !== 92))

onMounted(() => {
  const el = new Image()
  el.onload = () => {
    const cv = document.createElement('canvas')
    cv.width = el.naturalWidth
    cv.height = el.naturalHeight
    cv.getContext('2d')!.drawImage(el, 0, 0)
    stack = [cv]
    depth.value = 1
    render()
  }
  el.onerror = () => (error.value = true)
  el.src = props.url // same-origin (cookie) → the canvas stays untainted for export
})

// Full-resolution canvases are large (w×h×4 bytes each), so cap the undo history
// and release the backing store of any canvas we drop — otherwise a big photo
// edited many times retains hundreds of MB.
const UNDO_CAP = 12
function freeCanvas(cv?: HTMLCanvasElement) {
  if (cv) {
    cv.width = 0
    cv.height = 0
  }
}
function push(cv: HTMLCanvasElement) {
  stack.push(cv)
  // Drop the oldest EDIT beyond the cap (keep stack[0] = the original for reset).
  while (stack.length > UNDO_CAP) freeCanvas(stack.splice(1, 1)[0])
  depth.value = stack.length
  render()
}

// render just paints the current image; the crop rectangle is a DOM overlay.
function render() {
  const dst = view.value
  const src = cur()
  if (!dst || !src) return
  dst.width = src.width
  dst.height = src.height
  dst.getContext('2d')!.drawImage(src, 0, 0)
}

function rotate(delta: number) {
  const src = cur()
  const cv = document.createElement('canvas')
  cv.width = src.height
  cv.height = src.width
  const ctx = cv.getContext('2d')!
  ctx.translate(cv.width / 2, cv.height / 2)
  ctx.rotate((delta * Math.PI) / 180)
  ctx.drawImage(src, -src.width / 2, -src.height / 2)
  push(cv)
}

function flip(axis: 'h' | 'v') {
  const src = cur()
  const cv = document.createElement('canvas')
  cv.width = src.width
  cv.height = src.height
  const ctx = cv.getContext('2d')!
  ctx.translate(axis === 'h' ? cv.width : 0, axis === 'v' ? cv.height : 0)
  ctx.scale(axis === 'h' ? -1 : 1, axis === 'v' ? -1 : 1)
  ctx.drawImage(src, 0, 0)
  push(cv)
}

function undo() {
  if (stack.length > 1) {
    freeCanvas(stack.pop())
    depth.value = stack.length
    render()
  }
}
function reset() {
  for (const cv of stack.slice(1)) freeCanvas(cv)
  stack = stack.slice(0, 1)
  depth.value = 1
  cropping.value = false
  render()
}

function toggleCrop() {
  cropping.value = !cropping.value
  if (cropping.value) crop.value = { x: 0.08, y: 0.08, w: 0.84, h: 0.84 }
}

// --- Crop box drag (handles resize an edge/corner; the interior moves it) ---
type Dir = 'move' | 'nw' | 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w'
const MIN = 0.05 // smallest crop fraction per axis
function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v))
}
function startDrag(e: PointerEvent, dir: Dir) {
  e.preventDefault()
  e.stopPropagation()
  const rect = stage.value!.getBoundingClientRect()
  const start = { ...crop.value }
  const sx = e.clientX
  const sy = e.clientY
  const move = (ev: PointerEvent) => {
    const dx = (ev.clientX - sx) / rect.width
    const dy = (ev.clientY - sy) / rect.height
    let { x, y, w, h } = start
    if (dir === 'move') {
      x = clamp(start.x + dx, 0, 1 - w)
      y = clamp(start.y + dy, 0, 1 - h)
    } else {
      if (dir.includes('w')) {
        const nx = clamp(start.x + dx, 0, start.x + start.w - MIN)
        w = start.x + start.w - nx
        x = nx
      }
      if (dir.includes('e')) w = clamp(start.w + dx, MIN, 1 - start.x)
      if (dir.includes('n')) {
        const ny = clamp(start.y + dy, 0, start.y + start.h - MIN)
        h = start.y + start.h - ny
        y = ny
      }
      if (dir.includes('s')) h = clamp(start.h + dy, MIN, 1 - start.y)
    }
    crop.value = { x, y, w, h }
  }
  const up = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', up)
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', up)
}

function applyCrop() {
  const src = cur()
  const c = crop.value
  const x = Math.round(c.x * src.width)
  const y = Math.round(c.y * src.height)
  const w = Math.max(1, Math.round(c.w * src.width))
  const h = Math.max(1, Math.round(c.h * src.height))
  const cv = document.createElement('canvas')
  cv.width = w
  cv.height = h
  cv.getContext('2d')!.drawImage(src, x, y, w, h, 0, 0, w, h)
  cropping.value = false
  push(cv)
}

const boxStyle = computed(() => ({
  left: crop.value.x * 100 + '%',
  top: crop.value.y * 100 + '%',
  width: crop.value.w * 100 + '%',
  height: crop.value.h * 100 + '%',
}))

function mimeFor(name: string): string {
  const ext = name.toLowerCase().split('.').pop()
  if (ext === 'jpg' || ext === 'jpeg') return 'image/jpeg'
  if (ext === 'webp') return 'image/webp'
  return 'image/png'
}

async function save() {
  const src = cur()
  if (!src || saving.value) return
  saving.value = true
  try {
    const blob: Blob | null = await new Promise((r) => src.toBlob(r, mimeFor(props.node.name), quality.value / 100))
    if (!blob) throw new Error('export failed')
    await api.saveBlob(props.node.id, blob)
    await Promise.all([files.load(), files.loadCapacity()])
    files.ui().toast(t('imageEditor.saved'), 'check')
    emit('done')
  } catch {
    files.ui().toast(t('imageEditor.saveFailed'), 'x')
  } finally {
    saving.value = false
  }
}

const dirs: Dir[] = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w']
</script>

<template>
  <div class="ov-imgedit">
    <div class="ov-imgedit-canvas">
      <div ref="stage" class="ov-imgedit-stage" :class="{ cropping }">
        <canvas ref="view"></canvas>
        <div v-if="cropping" class="ov-crop" :style="boxStyle" @pointerdown="startDrag($event, 'move')">
          <span v-for="d in dirs" :key="d" class="ov-crop-h" :class="'h-' + d" @pointerdown="startDrag($event, d)"></span>
        </div>
      </div>
    </div>

    <div class="ov-imgedit-bar">
      <button class="icon-btn" :title="t('imageEditor.rotateLeft')" @click="rotate(-90)"><RotateCcw :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.rotateRight')" @click="rotate(90)"><RotateCw :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.flipHorizontal')" @click="flip('h')"><FlipHorizontal :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.flipVertical')" @click="flip('v')"><FlipVertical :size="18" /></button>
      <button class="icon-btn" :class="{ active: cropping }" :title="t('imageEditor.crop')" @click="toggleCrop"><Crop :size="18" /></button>
      <button v-if="cropping" class="btn btn-secondary ov-imgedit-crop" @click="applyCrop">
        <Check :size="15" />{{ t('imageEditor.applyCrop') }}
      </button>

      <span class="ov-imgedit-spacer"></span>

      <label v-if="lossy" class="ov-imgedit-quality" :title="t('imageEditor.quality')">
        {{ t('imageEditor.quality') }}
        <input type="range" min="50" max="100" v-model.number="quality" />
        <span>{{ quality }}%</span>
      </label>
      <button class="icon-btn" :disabled="!changed" :title="t('imageEditor.undo')" @click="undo"><Undo2 :size="18" /></button>
      <button class="icon-btn" :disabled="!changed" :title="t('imageEditor.reset')" @click="reset"><RefreshCcw :size="18" /></button>
      <button class="btn btn-secondary" @click="emit('done')"><X :size="15" />{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" :disabled="!dirty || saving" @click="save"><Save :size="15" />{{ t('common.save') }}</button>
    </div>
  </div>
</template>
