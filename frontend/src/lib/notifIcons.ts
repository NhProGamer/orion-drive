import {
  Info, Trash2, RotateCcw, FolderPlus, Download, X, FolderInput, Check, Share2,
  Link as LinkIcon,
} from 'lucide-vue-next'
import type { LucideIcon } from 'lucide-vue-next'

// Maps a toast/notification icon name to its Lucide component.
const ICONS: Record<string, LucideIcon> = {
  info: Info,
  trash: Trash2,
  restore: RotateCcw,
  'folder-plus': FolderPlus,
  download: Download,
  move: FolderInput,
  x: X,
  check: Check,
  share: Share2,
  link: LinkIcon,
}

export function notifIcon(name: string): LucideIcon {
  return ICONS[name] || Info
}
