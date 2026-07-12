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

/** Per-kind icon, label and tint class, ported from the prototype. */
export const kindMeta: Record<string, KindMeta> = {
  folder: { icon: Folder, label: 'Dossier', tint: 'folder' },
  doc: { icon: FileText, label: 'Document', tint: 'doc' },
  text: { icon: FileText, label: 'Texte', tint: 'doc' },
  pdf: { icon: FileText, label: 'PDF', tint: 'pdf' },
  sheet: { icon: Table, label: 'Feuille de calcul', tint: 'sheet' },
  slides: { icon: Presentation, label: 'Présentation', tint: 'media' },
  image: { icon: Image, label: 'Image', tint: 'sheet' },
  video: { icon: Video, label: 'Vidéo', tint: 'media' },
  audio: { icon: Music, label: 'Audio', tint: 'media' },
  archive: { icon: Archive, label: 'Archive', tint: 'neutral' },
  code: { icon: Code, label: 'Code', tint: 'doc' },
  config: { icon: SlidersHorizontal, label: 'Configuration', tint: 'neutral' },
  disc: { icon: Disc, label: 'Image disque', tint: 'neutral' },
  design: { icon: Pencil, label: 'Fichier design', tint: 'neutral' },
  file: { icon: File, label: 'Fichier', tint: 'neutral' },
}

export function metaFor(kind: string): KindMeta {
  return kindMeta[kind] || kindMeta.file
}
