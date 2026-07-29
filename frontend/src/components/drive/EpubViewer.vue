<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, ChevronRight } from 'lucide-vue-next'

const { t } = useI18n()
// `name` carries the real filename so foliate can detect name-based formats
// (CBZ / FB2); content-sniffed formats (EPUB / MOBI / AZW3) work regardless.
const props = defineProps<{ url: string; name?: string }>()

const host = ref<HTMLElement | null>(null)
const error = ref(false)
// foliate-js is loaded lazily so it stays out of the main bundle. Its
// <foliate-view> element auto-detects the format and paginates by default.
let view: any = null

// RemoteBlob is a lazy, Blob-like view over a remote file. foliate's readers
// (the ZIP loader for EPUB/CBZ, the MOBI/AZW3 record reader) only touch a file
// through `.size` and `.slice(a, b).arrayBuffer()`, so backing those with HTTP
// range requests streams the book: only the index and the parts actually read
// are fetched, never the whole file up front. `<foliate-view>.open()` detects
// the `arrayBuffer` method and drives format detection off `.name`/`.type`.
// (FB2 is a single XML document with no random access, so foliate reads it whole
// through one range request — correct, just not incremental.)
class RemoteBlob {
  url: string
  size: number
  name: string
  type = ''
  constructor(url: string, size: number, name: string) {
    this.url = url
    this.size = size
    this.name = name
  }
  private async range(start: number, end: number): Promise<ArrayBuffer> {
    const res = await fetch(this.url, {
      credentials: 'include',
      headers: { Range: `bytes=${start}-${end - 1}` },
    })
    if (!res.ok) throw new Error(`range ${start}-${end}: ${res.status}`)
    return res.arrayBuffer()
  }
  arrayBuffer(): Promise<ArrayBuffer> {
    return this.range(0, this.size)
  }
  slice(start = 0, end = this.size) {
    return { size: end - start, arrayBuffer: () => this.range(start, end) }
  }
}

function extOf(name?: string): string {
  return (name || '').toLowerCase().split('.').pop() || ''
}

// buildComicBook opens a CBZ/CBT through the ZIP loader (streamed when src is a
// RemoteBlob) and forces single-page layout. Comic archives declare no page
// spread, so foliate's fixed-layout renderer would otherwise pair pages into
// two-up spreads and push the cover to the right, leaving a phantom blank page
// before it. spread:'none' renders one centred image per page.
async function buildComicBook(src: RemoteBlob | File, name: string) {
  const zip: any = await import('foliate-js/vendor/zip.js')
  zip.configure({ useWebWorkers: false })
  const reader = new zip.ZipReader(new zip.BlobReader(src))
  const entries = await reader.getEntries()
  const map = new Map<string, any>(entries.map((e: any) => [e.filename, e]))
  const load = (f: (e: any, ...a: any[]) => any) => (n: string, ...a: any[]) =>
    map.has(n) ? f(map.get(n), ...a) : null
  const loader = {
    entries,
    loadText: load((e) => e.getData(new zip.TextWriter())),
    loadBlob: load((e, type) => e.getData(new zip.BlobWriter(type))),
    getSize: (n: string) => map.get(n)?.uncompressedSize ?? 0,
  }
  const { makeComicBook } = await import('foliate-js/comic-book.js')
  const book: any = await makeComicBook(loader, { name })
  book.rendition = { ...(book.rendition || {}), spread: 'none' }
  return book
}

// probeSize issues a 1-byte range GET (the content route is GET-only, so HEAD
// is not available): a 206 + Content-Range confirms range support and reveals
// the total size. Returns 0 when the server does not stream.
async function probeSize(url: string): Promise<number> {
  const res = await fetch(url, { credentials: 'include', headers: { Range: 'bytes=0-0' } })
  const cr = res.headers.get('Content-Range') // e.g. "bytes 0-0/12345"
  if (res.status !== 206 || !cr) return 0
  const size = Number(cr.split('/')[1])
  return Number.isFinite(size) && size > 0 ? size : 0
}

// downloadBook fetches the whole file as a fallback for servers without range
// support, handing foliate a plain File.
async function downloadBook(): Promise<File> {
  const res = await fetch(props.url, { credentials: 'include' })
  if (!res.ok) throw new Error('fetch failed')
  const blob = await res.blob()
  return new File([blob], props.name || 'book.epub')
}

onMounted(async () => {
  try {
    await import('foliate-js/view.js') // registers the <foliate-view> custom element
    view = document.createElement('foliate-view') as any
    view.style.cssText = 'display:block;width:100%;height:100%'
    host.value?.append(view)

    // Stream every format through range requests when the server supports them;
    // otherwise fall back to a full download. foliate reads only what it needs.
    const name = props.name || 'book.epub'
    const size = await probeSize(props.url).catch(() => 0)
    const src: RemoteBlob | File = size > 0 ? new RemoteBlob(props.url, size, name) : await downloadBook()
    // Comics need explicit single-page layout; other formats open as-is and
    // foliate's makeBook detects them from the blob's magic bytes and name.
    const ext = extOf(props.name)
    const book = ext === 'cbz' || ext === 'cbt' ? await buildComicBook(src, name) : src
    await view.open(book)
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
