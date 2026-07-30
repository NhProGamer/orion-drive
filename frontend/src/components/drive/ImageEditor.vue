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
const saving = ref(false)

// Editing is destructive-with-history: each operation bakes a fresh canvas and
// pushes it, so `stack` is the undo history and `stack[last]` the current image.
// This keeps rendering trivial and makes crop/rotate/flip compose cleanly.
let stack: HTMLCanvasElement[] = []
const depth = ref(0) // reactive mirror of stack length (drives undo/reset/save state)

const cropping = ref(false)
// Selection rectangle in current-image pixel coordinates while cropping.
const sel = ref<{ x: number; y: number; w: number; h: number } | null>(null)
let dragging = false
let anchor = { x: 0, y: 0 }

const lossy = /\.(jpe?g|webp)$/i.test(props.node.name)
const quality = ref(92) // percent, only meaningful for JPEG/WebP

const cur = () => stack[stack.length - 1]
const changed = computed(() => depth.value > 1)
// Saveable when transformed, or when only the export quality was lowered
// (re-compressing a JPEG/WebP to shrink it is a valid edit on its own).
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
  // Same-origin (with cookie) — the canvas stays untainted so we can export it.
  el.src = props.url
})

function push(cv: HTMLCanvasElement) {
  stack.push(cv)
  depth.value = stack.length
  render()
}

// render draws the current image into the visible canvas (CSS scales it to fit),
// plus the crop selection overlay when cropping.
function render() {
  const dst = view.value
  const src = cur()
  if (!dst || !src) return
  dst.width = src.width
  dst.height = src.height
  const ctx = dst.getContext('2d')!
  ctx.drawImage(src, 0, 0)
  if (cropping.value && sel.value) {
    const s = sel.value
    ctx.save()
    ctx.fillStyle = 'rgba(0,0,0,0.5)' // dim everything outside the selection
    ctx.beginPath()
    ctx.rect(0, 0, dst.width, dst.height)
    ctx.rect(s.x, s.y, s.w, s.h)
    ctx.fill('evenodd')
    ctx.strokeStyle = '#fff'
    ctx.lineWidth = Math.max(2, dst.width / 400)
    ctx.strokeRect(s.x, s.y, s.w, s.h)
    ctx.restore()
  }
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
    stack.pop()
    depth.value = stack.length
    render()
  }
}
function reset() {
  stack = stack.slice(0, 1)
  depth.value = 1
  cropping.value = false
  sel.value = null
  render()
}

// --- Crop selection (mouse in display coords → current-image pixel coords) ---
function toPixel(e: MouseEvent) {
  const dst = view.value!
  const r = dst.getBoundingClientRect()
  const x = ((e.clientX - r.left) / r.width) * dst.width
  const y = ((e.clientY - r.top) / r.height) * dst.height
  return { x: Math.max(0, Math.min(dst.width, x)), y: Math.max(0, Math.min(dst.height, y)) }
}
function onDown(e: MouseEvent) {
  if (!cropping.value) return
  dragging = true
  anchor = toPixel(e)
  sel.value = { x: anchor.x, y: anchor.y, w: 0, h: 0 }
}
function onMove(e: MouseEvent) {
  if (!cropping.value || !dragging) return
  const p = toPixel(e)
  sel.value = {
    x: Math.min(anchor.x, p.x),
    y: Math.min(anchor.y, p.y),
    w: Math.abs(p.x - anchor.x),
    h: Math.abs(p.y - anchor.y),
  }
  render()
}
function onUp() {
  dragging = false
}
function toggleCrop() {
  cropping.value = !cropping.value
  if (!cropping.value) sel.value = null
  render()
}
const canApplyCrop = computed(() => !!sel.value && sel.value.w >= 2 && sel.value.h >= 2)
function applyCrop() {
  const s = sel.value
  const src = cur()
  if (!s) return
  const w = Math.round(s.w)
  const h = Math.round(s.h)
  const cv = document.createElement('canvas')
  cv.width = w
  cv.height = h
  cv.getContext('2d')!.drawImage(src, Math.round(s.x), Math.round(s.y), w, h, 0, 0, w, h)
  cropping.value = false
  sel.value = null
  push(cv)
}

function mimeFor(name: string): string {
  const ext = name.toLowerCase().split('.').pop()
  if (ext === 'jpg' || ext === 'jpeg') return 'image/jpeg'
  if (ext === 'webp') return 'image/webp'
  return 'image/png'
}

async function save() {
  const src = cur()
  if (!src) return
  saving.value = true
  const blob: Blob | null = await new Promise((r) => src.toBlob(r, mimeFor(props.node.name), quality.value / 100))
  if (blob) {
    await api.saveBlob(props.node.id, blob)
    await Promise.all([files.load(), files.loadCapacity()])
    files.ui().toast(t('imageEditor.saved'), 'check')
  }
  saving.value = false
  emit('done')
}
</script>

<template>
  <div class="ov-imgedit">
    <div class="ov-imgedit-canvas">
      <canvas
        ref="view"
        :class="{ cropping }"
        @mousedown="onDown"
        @mousemove="onMove"
        @mouseup="onUp"
        @mouseleave="onUp"
      ></canvas>
    </div>
    <div class="ov-imgedit-bar">
      <button class="icon-btn" :title="t('imageEditor.rotateLeft')" @click="rotate(-90)"><RotateCcw :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.rotateRight')" @click="rotate(90)"><RotateCw :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.flipHorizontal')" @click="flip('h')"><FlipHorizontal :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.flipVertical')" @click="flip('v')"><FlipVertical :size="18" /></button>
      <button class="icon-btn" :class="{ active: cropping }" :title="t('imageEditor.crop')" @click="toggleCrop"><Crop :size="18" /></button>
      <template v-if="cropping">
        <button class="btn btn-secondary ov-imgedit-crop" :disabled="!canApplyCrop" @click="applyCrop">
          <Check :size="15" />{{ t('imageEditor.applyCrop') }}
        </button>
      </template>

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
