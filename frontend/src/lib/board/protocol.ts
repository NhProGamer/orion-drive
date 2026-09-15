/**
 * Wire format of the collaborative whiteboard socket. Mirrors pkg/board:
 * plain JSON, because the server merges element versions itself (which is what
 * lets a late joiner and the saved file stay in sync).
 */

/** One Excalidraw element, relayed untouched. Only these fields are read here. */
export interface WireElement {
  id: string
  version: number
  versionNonce: number
  isDeleted?: boolean
  [key: string]: unknown
}

/** A binary asset (pasted or dropped image), relayed untouched. */
export interface WireFile {
  id?: string
  mimeType?: string
  dataURL?: string
  [key: string]: unknown
}

export interface WirePeer {
  id: string
  name: string
  color: string
  write: boolean
}

export interface WireScene {
  elements: WireElement[]
  background: string
  files: Record<string, WireFile>
}

/** Message received from the relay. */
export type ServerMessage =
  | { t: 'init'; self: string; can_write?: boolean; scene: WireScene; peers?: WirePeer[] }
  | { t: 'peers'; peers: WirePeer[] }
  | { t: 'update'; from: string; elements: WireElement[] }
  | { t: 'pointer'; from: string; x?: number; y?: number; state?: string; selected?: string[] }
  | { t: 'files'; from: string; files: Record<string, WireFile> }
  | { t: 'state'; from?: string; background: string }
  | { t: 'readonly'; can_write?: boolean }
  | { t: 'saved' }
  | { t: 'error'; message: string }

/** Message sent to the relay. */
export type ClientMessage =
  | { t: 'update'; elements: WireElement[] }
  | { t: 'pointer'; x: number; y: number; state: string; selected?: string[] }
  | { t: 'files'; files: Record<string, WireFile> }
  | { t: 'state'; background: string }
