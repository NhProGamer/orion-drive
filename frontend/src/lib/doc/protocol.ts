/**
 * Wire format of the collaborative Markdown socket. Mirrors pkg/livedoc.
 *
 * Two channels share one connection: JSON text frames carry control messages
 * (who is here, who writes, seed the document, it was saved), and binary frames
 * carry the CRDT's own updates, which the server relays without reading them.
 */

/** Channel tag prefixing every binary frame. */
export const CHANNEL_SYNC = 0
export const CHANNEL_AWARENESS = 1

/** One participant, as advertised by the relay. */
export interface DocPeer {
  id: string
  name: string
  color: string
  write: boolean
}

/** Control message received from the relay. */
export type ServerMessage =
  | {
      t: 'init'
      self: string
      can_write?: boolean
      writer?: boolean
      /** The stored Markdown, sent only to the peer that opened an empty room. */
      seed?: string
      peers?: DocPeer[]
    }
  | { t: 'peers'; peers: DocPeer[] }
  | { t: 'writer'; writer?: boolean }
  | { t: 'readonly'; can_write?: boolean }
  | { t: 'saved' }
  | { t: 'error'; message: string }

/** Control message sent to the relay. */
export type ClientMessage = { t: 'save'; text: string; synced: boolean }

export type DocStatus = 'connecting' | 'live' | 'reconnecting' | 'closed'
