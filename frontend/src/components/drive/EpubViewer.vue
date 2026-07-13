<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'

const props = defineProps<{ url: string }>()

const host = ref<HTMLElement | null>(null)
const error = ref(false)
// epub.js is loaded lazily so it is not in the main bundle.
let book: any = null
let rendition: any = null

onMounted(async () => {
  try {
    const mod: any = await import('epubjs')
    const ePub = mod.default || mod
    const buf = await (await fetch(props.url, { credentials: 'include' })).arrayBuffer()
    book = ePub(buf)
    rendition = book.renderTo(host.value, {
      width: '100%',
      height: '100%',
      flow: 'paginated',
      spread: 'none',
    })
    await rendition.display()
  } catch {
    error.value = true
  }
})

onBeforeUnmount(() => {
  try {
    rendition?.destroy()
    book?.destroy()
  } catch {
    /* ignore */
  }
})

function prev() {
  rendition?.prev()
}
function next() {
  rendition?.next()
}
</script>

<template>
  <div class="ov-epub">
    <button class="ov-epub-nav left" title="Précédent" @click="prev"><ChevronLeft :size="22" /></button>
    <div ref="host" class="ov-epub-host"></div>
    <button class="ov-epub-nav right" title="Suivant" @click="next"><ChevronRight :size="22" /></button>
    <div v-if="error" class="ov-empty" style="position: absolute; inset: 0; justify-content: center">
      Impossible de lire cet ePub.
    </div>
  </div>
</template>
