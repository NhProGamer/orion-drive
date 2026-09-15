/**
 * Live collaboration client for the whiteboard: owns the WebSocket, diffs the
 * local scene against what has already been relayed, and reconciles what other
 * peers send back into the canvas.
 *
 * The one invariant that keeps this loop from feeding on itself: every element
 * version this client has either sent or received is recorded in `known`, and
 * only elements whose (version, versionNonce) differ from that record are sent
 * out. Applying a remote update therefore never bounces back as a local change.
 */
import type { ClientMessage, ServerMessage, WireElement, WireFile, WirePeer } from './protocol'

/** How long to coalesce local element changes before relaying them. */
const UPDATE_INTERVAL = 80

/** How long to coalesce cursor moves. Pointer frames are the chattiest traffic. */
const POINTER_INTERVAL = 60

/** Reconnection backoff bounds. */
const RECONNECT_MIN = 500
const RECONNECT_MAX = 15000

/** A peer's live cursor, as relayed. */
export interface RemotePointer {
  x: number
  y: number
  state: string
  selected: string[]
}

/** Everything the UI layer reacts to. */
export interface CollabHandlers {
  /** Full scene received on (re)join. */
  onInit: (scene: { elements: WireElement[]; background: string; files: Record<string, WireFile> }, self: string) => void
  /** Elements changed by other peers. */
  onRemoteElements: (elements: WireElement[]) => void
  /** Binary assets added by other peers. */
  onRemoteFiles: (files: WireFile[]) => void
  /** Canvas background changed by another peer. */
  onBackground: (color: string) => void
  /** Participant list changed. */
  onPeers: (peers: WirePeer[]) => void
  /** A peer moved its cursor. */
  onPointer: (peerID: string, pointer: RemotePointer) => void
  /** Write permission granted or revoked (a share downgraded mid-session). */
  onWritable: (canWrite: boolean) => void
  /** The scene was snapshotted server-side. */
  onSaved: () => void
  /** Connection state, for the status indicator. */
  onStatus: (status: CollabStatus) => void
  /** Fatal error: the session is over and will not reconnect. */
  onFatal: (message: string) => void
}

export type CollabStatus = 'connecting' | 'live' | 'reconnecting' | 'closed'

/** The version pair that identifies one revision of an element. */
interface VersionMark {
  version: number
  nonce: number
}

export class BoardCollab {
  private socket: WebSocket | null = null
  private readonly url: string
  private readonly handlers: CollabHandlers

  /** Element revisions already sent or received; the echo guard. */
  private known = new Map<string, VersionMark>()
  /** Asset ids already relayed, so a scene of images is not resent on change. */
  private sentFiles = new Set<string>()
  private lastBackground = ''

  private pendingElements = new Map<string, WireElement>()
  private updateTimer: number | null = null
  private pendingPointer: { x: number; y: number; state: string; selected?: string[] } | null = null
  private pointerTimer: number | null = null

  private reconnectDelay = RECONNECT_MIN
  private reconnectTimer: number | null = null
  private closedByUs = false
  private fatal = false

  constructor(url: string, handlers: CollabHandlers) {
    this.url = url
    this.handlers = handlers
  }

  /** Opens the session. Reconnects on its own until `stop()` is called. */
  start() {
    this.closedByUs = false
    this.connect()
  }

  /** Closes the session and drops every pending timer. */
  stop() {
    this.closedByUs = true
    this.clearTimers()
    if (this.reconnectTimer !== null) {
      window.clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    const socket = this.socket
    this.socket = null
    if (socket) {
      socket.onclose = null
      socket.onerror = null
      socket.onmessage = null
      socket.close()
    }
    this.handlers.onStatus('closed')
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
    this.socket = socket

    socket.onopen = () => {
      this.reconnectDelay = RECONNECT_MIN
      this.handlers.onStatus('live')
    }
    socket.onmessage = (event) => {
      if (typeof event.data !== 'string') return
      let msg: ServerMessage
      try {
        msg = JSON.parse(event.data)
      } catch {
        return
      }
      this.handle(msg)
    }
    socket.onclose = () => {
      if (this.socket === socket) this.socket = null
      this.scheduleReconnect()
    }
    socket.onerror = () => {
      // `onclose` always follows, which is where reconnection is handled.
    }
  }

  private scheduleReconnect() {
    if (this.closedByUs || this.fatal) return
    this.clearTimers()
    this.handlers.onStatus('reconnecting')
    this.reconnectTimer = window.setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, this.reconnectDelay)
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, RECONNECT_MAX)
  }

  private handle(msg: ServerMessage) {
    switch (msg.t) {
      case 'init': {
        // A reconnect replays the whole scene: forget what we believed was
        // relayed and adopt the server's view, which is authoritative.
        this.known.clear()
        this.sentFiles.clear()
        for (const el of msg.scene.elements) this.mark(el)
        for (const id of Object.keys(msg.scene.files ?? {})) this.sentFiles.add(id)
        this.lastBackground = msg.scene.background
        this.handlers.onWritable(msg.can_write !== false)
        this.handlers.onInit(
          { elements: msg.scene.elements ?? [], background: msg.scene.background, files: msg.scene.files ?? {} },
          msg.self,
        )
        if (msg.peers) this.handlers.onPeers(msg.peers)
        break
      }
      case 'update': {
        const elements = msg.elements ?? []
        for (const el of elements) this.mark(el)
        if (elements.length) this.handlers.onRemoteElements(elements)
        break
      }
      case 'files': {
        const files = Object.entries(msg.files ?? {})
        const added: WireFile[] = []
        for (const [id, file] of files) {
          this.sentFiles.add(id)
          added.push({ ...file, id: (file.id as string) ?? id })
        }
        if (added.length) this.handlers.onRemoteFiles(added)
        break
      }
      case 'state':
        this.lastBackground = msg.background
        this.handlers.onBackground(msg.background)
        break
      case 'pointer':
        this.handlers.onPointer(msg.from, {
          x: msg.x ?? 0,
          y: msg.y ?? 0,
          state: msg.state ?? 'active',
          selected: msg.selected ?? [],
        })
        break
      case 'peers':
        this.handlers.onPeers(msg.peers ?? [])
        break
      case 'readonly':
        this.handlers.onWritable(msg.can_write === true)
        break
      case 'saved':
        this.handlers.onSaved()
        break
      case 'error':
        // The server closes right after an error frame, and retrying would only
        // hit the same refusal (revoked share, board full, unreadable scene).
        this.fatal = true
        this.handlers.onFatal(msg.message)
        this.stop()
        break
    }
  }

  /** Records an element revision as relayed, in either direction. */
  private mark(el: WireElement) {
    if (!el || typeof el.id !== 'string') return
    this.known.set(el.id, { version: el.version ?? 0, nonce: el.versionNonce ?? 0 })
  }

  /**
   * Relays whatever changed in the local scene. Called on every Excalidraw
   * change: the diff against `known` is what keeps this cheap, since a change
   * to one shape does not resend the other thousand.
   */
  pushElements(elements: readonly WireElement[]) {
    for (const el of elements) {
      if (!el || typeof el.id !== 'string') continue
      const prev = this.known.get(el.id)
      if (prev && prev.version === (el.version ?? 0) && prev.nonce === (el.versionNonce ?? 0)) continue
      this.pendingElements.set(el.id, el)
    }
    if (this.pendingElements.size && this.updateTimer === null) {
      this.updateTimer = window.setTimeout(() => {
        this.updateTimer = null
        this.flushElements()
      }, UPDATE_INTERVAL)
    }
  }

  private flushElements() {
    if (!this.pendingElements.size) return
    const batch = [...this.pendingElements.values()]
    this.pendingElements.clear()
    if (!this.send({ t: 'update', elements: batch })) {
      // Offline: drop the batch rather than queue it. The reconnect replays the
      // server's scene and the next diff resends whatever is still ours.
      return
    }
    for (const el of batch) this.mark(el)
  }

  /** Relays binary assets the local user added. */
  pushFiles(files: Record<string, WireFile>) {
    const fresh: Record<string, WireFile> = {}
    let count = 0
    for (const [id, file] of Object.entries(files ?? {})) {
      if (this.sentFiles.has(id)) continue
      fresh[id] = file
      count += 1
    }
    if (!count) return
    if (this.send({ t: 'files', files: fresh })) {
      for (const id of Object.keys(fresh)) this.sentFiles.add(id)
    }
  }

  /** Relays a canvas background change. */
  pushBackground(color: string) {
    if (!color || color === this.lastBackground) return
    this.lastBackground = color
    this.send({ t: 'state', background: color })
  }

  /** Relays the local cursor, coalesced so a fast mouse cannot flood the room. */
  pushPointer(x: number, y: number, state: string, selected?: string[]) {
    this.pendingPointer = { x, y, state, selected }
    if (this.pointerTimer !== null) return
    this.pointerTimer = window.setTimeout(() => {
      this.pointerTimer = null
      const p = this.pendingPointer
      this.pendingPointer = null
      if (p) this.send({ t: 'pointer', x: p.x, y: p.y, state: p.state, selected: p.selected })
    }, POINTER_INTERVAL)
  }

  private send(msg: ClientMessage): boolean {
    const socket = this.socket
    if (!socket || socket.readyState !== WebSocket.OPEN) return false
    try {
      socket.send(JSON.stringify(msg))
      return true
    } catch {
      return false
    }
  }

  private clearTimers() {
    if (this.updateTimer !== null) {
      window.clearTimeout(this.updateTimer)
      this.updateTimer = null
    }
    if (this.pointerTimer !== null) {
      window.clearTimeout(this.pointerTimer)
      this.pointerTimer = null
    }
    this.pendingElements.clear()
    this.pendingPointer = null
  }
}
