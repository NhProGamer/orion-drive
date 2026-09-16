/**
 * Collaborative Markdown session: owns the Yjs document, the awareness (who is
 * where) and the socket that carries both.
 *
 * The server never reads the CRDT — it only relays frames between peers — so
 * convergence is entirely this class's job, handled by y-protocols' standard
 * sync: on connect we send a state vector, peers reply with what we are
 * missing, and every local change is broadcast as an update.
 */
import * as Y from 'yjs'
import { Awareness } from 'y-protocols/awareness'
import * as awarenessProtocol from 'y-protocols/awareness'
import * as syncProtocol from 'y-protocols/sync'
import * as decoding from 'lib0/decoding'
import * as encoding from 'lib0/encoding'
import {
  CHANNEL_AWARENESS,
  CHANNEL_SYNC,
  type ClientMessage,
  type DocPeer,
  type DocStatus,
  type ServerMessage,
} from './protocol'

/** Reconnection backoff bounds. */
const RECONNECT_MIN = 500
const RECONNECT_MAX = 15000

export interface InitPayload {
  self: string
  canWrite: boolean
  writer: boolean
  /** Markdown to seed the document with, or null when peers will supply it. */
  seed: string | null
  peers: DocPeer[]
}

export interface DocHandlers {
  onInit: (info: InitPayload) => void
  onPeers: (peers: DocPeer[]) => void
  onWriter: (isWriter: boolean) => void
  onWritable: (canWrite: boolean) => void
  onSaved: () => void
  onStatus: (status: DocStatus) => void
  onFatal: (message: string) => void
}

export class DocSession {
  /** The shared document the editor binds to. */
  readonly doc = new Y.Doc()
  readonly awareness: Awareness

  private socket: WebSocket | null = null
  private readonly url: string
  private readonly handlers: DocHandlers

  /**
   * Whether this replica holds real content — it applied the seed, or a peer
   * answered its sync. The relay refuses an empty document from a replica that
   * cannot claim this, which is what stops a still-loading tab from truncating
   * the file.
   */
  private isSynced = false

  private reconnectDelay = RECONNECT_MIN
  private reconnectTimer: number | null = null
  private closedByUs = false
  private fatal = false

  constructor(url: string, handlers: DocHandlers) {
    this.url = url
    this.handlers = handlers
    this.awareness = new Awareness(this.doc)

    this.doc.on('update', this.onDocUpdate)
    this.awareness.on('update', this.onAwarenessUpdate)
  }

  get synced(): boolean {
    return this.isSynced
  }

  /** Marks the replica as holding the seeded content. */
  markSeeded() {
    this.isSynced = true
  }

  /** Publishes the local user's name and colour to the other cursors. */
  setLocalUser(name: string, color: string) {
    this.awareness.setLocalStateField('user', { name, color })
  }

  start() {
    this.closedByUs = false
    this.connect()
  }

  stop() {
    this.closedByUs = true
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.doc.off('update', this.onDocUpdate)
    this.awareness.off('update', this.onAwarenessUpdate)
    // Tell the others this cursor is gone before the socket closes.
    awarenessProtocol.removeAwarenessStates(this.awareness, [this.doc.clientID], 'session closed')
    const socket = this.socket
    this.socket = null
    if (socket) {
      socket.onclose = null
      socket.onerror = null
      socket.onmessage = null
      socket.close()
    }
    this.awareness.destroy()
    this.handlers.onStatus('closed')
  }

  /** Sends Markdown for the relay to persist. Only the writer should call it. */
  save(text: string) {
    this.send({ t: 'save', text, synced: this.isSynced })
  }

  private connect() {
    this.handlers.onStatus(this.reconnectDelay === RECONNECT_MIN ? 'connecting' : 'reconnecting')
    let socket: WebSocket
    try {
      socket = new WebSocket(this.url)
    } catch {
      this.scheduleReconnect()
      return
    }
    socket.binaryType = 'arraybuffer'
    this.socket = socket

    socket.onopen = () => {
      this.reconnectDelay = RECONNECT_MIN
      this.handlers.onStatus('live')
      this.requestSync()
    }
    socket.onmessage = (event) => {
      if (typeof event.data === 'string') this.handleControl(event.data)
      else this.handleBinary(new Uint8Array(event.data as ArrayBuffer))
    }
    socket.onclose = () => {
      if (this.socket === socket) this.socket = null
      // Peers reached through the dropped socket are gone as far as we know;
      // their cursors must not linger until the reconnect re-announces them.
      awarenessProtocol.removeAwarenessStates(
        this.awareness,
        [...this.awareness.getStates().keys()].filter((id) => id !== this.doc.clientID),
        'disconnected',
      )
      this.scheduleReconnect()
    }
    socket.onerror = () => {
      // `onclose` always follows, which is where reconnection is handled.
    }
  }

  private scheduleReconnect() {
    if (this.closedByUs || this.fatal) return
    this.handlers.onStatus('reconnecting')
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, this.reconnectDelay)
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, RECONNECT_MAX)
  }

  /** Announces our state vector and cursor so peers can fill in the gaps. */
  private requestSync() {
    const sync = encoding.createEncoder()
    encoding.writeVarUint(sync, CHANNEL_SYNC)
    syncProtocol.writeSyncStep1(sync, this.doc)
    this.sendBinary(encoding.toUint8Array(sync))

    if (this.awareness.getLocalState() !== null) {
      const state = encoding.createEncoder()
      encoding.writeVarUint(state, CHANNEL_AWARENESS)
      encoding.writeVarUint8Array(
        state,
        awarenessProtocol.encodeAwarenessUpdate(this.awareness, [this.doc.clientID]),
      )
      this.sendBinary(encoding.toUint8Array(state))
    }
  }

  private handleBinary(payload: Uint8Array) {
    const decoder = decoding.createDecoder(payload)
    const channel = decoding.readVarUint(decoder)
    if (channel === CHANNEL_SYNC) {
      const reply = encoding.createEncoder()
      encoding.writeVarUint(reply, CHANNEL_SYNC)
      // `this` as the origin marks what arrived from the wire, so the update
      // handler below does not echo it straight back out.
      const type = syncProtocol.readSyncMessage(decoder, reply, this.doc, this)
      if (type === syncProtocol.messageYjsSyncStep2 || type === syncProtocol.messageYjsUpdate) {
        this.isSynced = true
      }
      // Length 1 means the channel tag only: nothing to answer.
      if (encoding.length(reply) > 1) this.sendBinary(encoding.toUint8Array(reply))
      return
    }
    if (channel === CHANNEL_AWARENESS) {
      awarenessProtocol.applyAwarenessUpdate(this.awareness, decoding.readVarUint8Array(decoder), this)
    }
  }

  private handleControl(raw: string) {
    let msg: ServerMessage
    try {
      msg = JSON.parse(raw)
    } catch {
      return
    }
    switch (msg.t) {
      case 'init':
        this.handlers.onInit({
          self: msg.self,
          canWrite: msg.can_write !== false,
          writer: msg.writer === true,
          seed: typeof msg.seed === 'string' ? msg.seed : null,
          peers: msg.peers ?? [],
        })
        break
      case 'peers':
        this.handlers.onPeers(msg.peers ?? [])
        break
      case 'writer':
        this.handlers.onWriter(msg.writer === true)
        break
      case 'readonly':
        this.handlers.onWritable(msg.can_write === true)
        break
      case 'saved':
        this.handlers.onSaved()
        break
      case 'error':
        // The relay closes right after, and retrying would hit the same
        // refusal (revoked share, document full).
        this.fatal = true
        this.handlers.onFatal(msg.message)
        this.stop()
        break
    }
  }

  private onDocUpdate = (update: Uint8Array, origin: unknown) => {
    if (origin === this) return // already came from a peer
    const enc = encoding.createEncoder()
    encoding.writeVarUint(enc, CHANNEL_SYNC)
    syncProtocol.writeUpdate(enc, update)
    this.sendBinary(encoding.toUint8Array(enc))
  }

  private onAwarenessUpdate = (
    changes: { added: number[]; updated: number[]; removed: number[] },
    origin: unknown,
  ) => {
    if (origin === this) return
    const changed = [...changes.added, ...changes.updated, ...changes.removed]
    if (!changed.length) return
    const enc = encoding.createEncoder()
    encoding.writeVarUint(enc, CHANNEL_AWARENESS)
    encoding.writeVarUint8Array(enc, awarenessProtocol.encodeAwarenessUpdate(this.awareness, changed))
    this.sendBinary(encoding.toUint8Array(enc))
  }

  private send(msg: ClientMessage) {
    const socket = this.socket
    if (!socket || socket.readyState !== WebSocket.OPEN) return
    try {
      socket.send(JSON.stringify(msg))
    } catch {
      // The close handler takes it from here.
    }
  }

  private sendBinary(payload: Uint8Array) {
    const socket = this.socket
    if (!socket || socket.readyState !== WebSocket.OPEN) return
    try {
      socket.send(payload)
    } catch {
      // Same: the close handler reconnects and re-syncs.
    }
  }
}
