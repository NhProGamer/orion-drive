<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RotateCcw, RotateCw, FlipHorizontal, FlipVertical, Save, X } from 'lucide-vue-next'
import { api, type FileNode } from '@/lib/api'
import { useFilesStore } from '@/stores/files'

const { t } = useI18n()

const props = defineProps<{ node: FileNode; url: string }>()
const emit = defineEmits<{ done: [] }>()

const files = useFilesStore()
const canvas = ref<HTMLCanvasElement | null>(null)
const saving = ref(false)
let img: HTMLImageElement | null = null

const rotation = ref(0) // 0 | 90 | 180 | 270
const flipH = ref(false)
const flipV = ref(false)

onMounted(() => {
  const el = new Image()
  el.onload = () => {
    img = el
    draw()
  }
  // Same-origin (with cookie) — the canvas stays untainted so we can export it.
  el.src = props.url
})

function draw() {
  const cv = canvas.value
  if (!cv || !img) return
  const rot = rotation.value % 360
  const swap = rot === 90 || rot === 270
  const w = img.naturalWidth
  const h = img.naturalHeight
  cv.width = swap ? h : w
  cv.height = swap ? w : h
  const ctx = cv.getContext('2d')!
  ctx.clearRect(0, 0, cv.width, cv.height)
  ctx.save()
  ctx.translate(cv.width / 2, cv.height / 2)
  ctx.rotate((rot * Math.PI) / 180)
  ctx.scale(flipH.value ? -1 : 1, flipV.value ? -1 : 1)
  ctx.drawImage(img, -w / 2, -h / 2)
  ctx.restore()
}

function rotate(delta: number) {
  rotation.value = (rotation.value + delta + 360) % 360
  draw()
}
function toggleFlipH() {
  flipH.value = !flipH.value
  draw()
}
function toggleFlipV() {
  flipV.value = !flipV.value
  draw()
}

const changed = () => rotation.value !== 0 || flipH.value || flipV.value

function mimeFor(name: string): string {
  const ext = name.toLowerCase().split('.').pop()
  if (ext === 'jpg' || ext === 'jpeg') return 'image/jpeg'
  if (ext === 'webp') return 'image/webp'
  return 'image/png'
}

async function save() {
  const cv = canvas.value
  if (!cv) return
  saving.value = true
  const blob: Blob | null = await new Promise((r) => cv.toBlob(r, mimeFor(props.node.name), 0.92))
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
    <div class="ov-imgedit-canvas"><canvas ref="canvas"></canvas></div>
    <div class="ov-imgedit-bar">
      <button class="icon-btn" :title="t('imageEditor.rotateLeft')" @click="rotate(-90)"><RotateCcw :size="18" /></button>
      <button class="icon-btn" :title="t('imageEditor.rotateRight')" @click="rotate(90)"><RotateCw :size="18" /></button>
      <button class="icon-btn" :class="{ active: flipH }" :title="t('imageEditor.flipHorizontal')" @click="toggleFlipH"><FlipHorizontal :size="18" /></button>
      <button class="icon-btn" :class="{ active: flipV }" :title="t('imageEditor.flipVertical')" @click="toggleFlipV"><FlipVertical :size="18" /></button>
      <span class="ov-imgedit-spacer"></span>
      <button class="btn btn-secondary" @click="emit('done')"><X :size="15" />{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" :disabled="!changed() || saving" @click="save"><Save :size="15" />{{ t('common.save') }}</button>
    </div>
  </div>
</template>
