<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'

const { t } = useI18n()
const props = defineProps<{ url: string }>()

const host = ref<HTMLElement | null>(null)
const error = ref(false)
// foliate-js is loaded lazily so it stays out of the main bundle. Its
// <foliate-view> element auto-detects the format and paginates by default.
let view: any = null

onMounted(async () => {
  try {
    await import('foliate-js/view.js') // registers the <foliate-view> custom element
    const res = await fetch(props.url, { credentials: 'include' })
    if (!res.ok) throw new Error('fetch failed')
    const blob = await res.blob()
    const file = new File([blob], 'book.epub', { type: 'application/epub+zip' })
    view = document.createElement('foliate-view') as any
    view.style.cssText = 'display:block;width:100%;height:100%'
    host.value?.append(view)
    await view.open(file)
  } catch {
    error.value = true
  }
})

onBeforeUnmount(() => {
  try {
    view?.close?.()
    view?.remove?.()
  } catch {
    /* ignore */
  }
})

// goLeft/goRight respect the book's reading direction (LTR: left = previous).
function prev() {
  view?.goLeft?.()
}
function next() {
  view?.goRight?.()
}
</script>

<template>
  <div class="ov-epub">
    <button class="ov-epub-nav left" :title="t('epub.prev')" @click="prev"><ChevronLeft :size="22" /></button>
    <div ref="host" class="ov-epub-host"></div>
    <button class="ov-epub-nav right" :title="t('epub.next')" @click="next"><ChevronRight :size="22" /></button>
    <div v-if="error" class="ov-empty" style="position: absolute; inset: 0; justify-content: center">
      {{ t('epub.error') }}
    </div>
  </div>
</template>
