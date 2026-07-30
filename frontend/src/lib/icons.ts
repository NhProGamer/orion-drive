import {
  Folder,
  FileText,
  Table,
  Presentation,
  Image,
  Video,
  Music,
  Archive,
  Code,
  SlidersHorizontal,
  Disc,
  Pencil,
  File,
  type LucideIcon,
} from 'lucide-vue-next'

export interface KindMeta {
  icon: LucideIcon
  label: string
  tint: string
}

/** Per-kind icon, i18n label key and tint class. `label` is an i18n key
 *  (kinds.*) resolved with t() at render time, so labels are localised. */
const kindMeta: Record<string, KindMeta> = {
  folder: { icon: Folder, label: 'kinds.folder', tint: 'folder' },
  doc: { icon: FileText, label: 'kinds.doc', tint: 'doc' },
  text: { icon: FileText, label: 'kinds.text', tint: 'doc' },
  pdf: { icon: FileText, label: 'kinds.pdf', tint: 'pdf' },
  sheet: { icon: Table, label: 'kinds.sheet', tint: 'sheet' },
  slides: { icon: Presentation, label: 'kinds.slides', tint: 'media' },
  image: { icon: Image, label: 'kinds.image', tint: 'sheet' },
  video: { icon: Video, label: 'kinds.video', tint: 'media' },
  audio: { icon: Music, label: 'kinds.audio', tint: 'media' },
  archive: { icon: Archive, label: 'kinds.archive', tint: 'neutral' },
  code: { icon: Code, label: 'kinds.code', tint: 'doc' },
  config: { icon: SlidersHorizontal, label: 'kinds.config', tint: 'neutral' },
  disc: { icon: Disc, label: 'kinds.disc', tint: 'neutral' },
  design: { icon: Pencil, label: 'kinds.design', tint: 'neutral' },
  file: { icon: File, label: 'kinds.file', tint: 'neutral' },
}

export function metaFor(kind: string): KindMeta {
  return kindMeta[kind] || kindMeta.file
}
