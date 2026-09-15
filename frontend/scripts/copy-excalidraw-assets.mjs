// Excalidraw loads its hand-drawn fonts at runtime and falls back to a public
// CDN when it cannot find them locally — which a deployment with a strict
// Content-Security-Policy blocks, and which a self-hosted drive should not need
// anyway. Copying them into public/ makes OrionDrive serve them itself (see
// ASSET_PATH in src/lib/board/mount.ts).
//
// Xiaolai is skipped: it is the CJK family and, at ~13 MB, larger than the rest
// of the application put together. Excalidraw only reaches for it when a drawing
// actually contains CJK text, and falls back to a system font when it 404s.
import { cp, mkdir, readdir, rm } from 'node:fs/promises'
import { existsSync } from 'node:fs'

const SOURCE = new URL('../node_modules/@excalidraw/excalidraw/dist/prod/fonts/', import.meta.url)
const TARGET = new URL('../public/excalidraw-assets/fonts/', import.meta.url)
const SKIP = new Set(['Xiaolai'])

if (!existsSync(SOURCE)) {
  console.error('excalidraw fonts not found; run npm install first')
  process.exit(1)
}

await rm(TARGET, { recursive: true, force: true })
await mkdir(TARGET, { recursive: true })

let copied = 0
for (const entry of await readdir(SOURCE, { withFileTypes: true })) {
  if (SKIP.has(entry.name)) continue
  await cp(new URL(entry.name, SOURCE), new URL(entry.name, TARGET), { recursive: true })
  copied += 1
}

console.log(`excalidraw assets: copied ${copied} font families to public/excalidraw-assets/fonts`)
