import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { BoardCollab, type CollabHandlers } from '@/lib/board/collab'
import type { ClientMessage, ServerMessage, WireElement } from '@/lib/board/protocol'

/** Minimal WebSocket stand-in: records what was sent, injects what "arrives". */
class FakeSocket {
  static last: FakeSocket | null = null
  static readonly OPEN = 1

  readyState = 1
  sent: ClientMessage[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null

  constructor(public url: string) {
    FakeSocket.last = this
  }

  send(raw: string) {
    this.sent.push(JSON.parse(raw))
  }

  close() {
    this.readyState = 3
  }

  /** Delivers a server message to the client under test. */
  deliver(msg: ServerMessage) {
    this.onmessage?.({ data: JSON.stringify(msg) })
  }
}

function el(id: string, version: number, nonce = 1): WireElement {
  return { id, version, versionNonce: nonce, type: 'rectangle' }
}

/** Handlers that record every callback, so assertions can read them back. */
function recorder() {
  return {
    onInit: vi.fn(),
    onRemoteElements: vi.fn(),
    onRemoteFiles: vi.fn(),
    onBackground: vi.fn(),
    onPeers: vi.fn(),
    onPointer: vi.fn(),
    onWritable: vi.fn(),
    onSaved: vi.fn(),
    onStatus: vi.fn(),
    onFatal: vi.fn(),
  } satisfies CollabHandlers
}

let originalWebSocket: unknown

beforeEach(() => {
  vi.useFakeTimers()
  originalWebSocket = (globalThis as { WebSocket?: unknown }).WebSocket
  ;(globalThis as { WebSocket: unknown }).WebSocket = FakeSocket
})

afterEach(() => {
  vi.useRealTimers()
  ;(globalThis as { WebSocket: unknown }).WebSocket = originalWebSocket
  FakeSocket.last = null
})

/** Starts a session and returns it with its socket and handlers. */
function session() {
  const handlers = recorder()
  const collab = new BoardCollab('wss://drive.test/ws', handlers)
  collab.start()
  const socket = FakeSocket.last as FakeSocket
  socket.onopen?.()
  return { collab, socket, handlers }
}

/** Flushes the coalescing timers the client uses before sending. */
function flush() {
  vi.advanceTimersByTime(200)
}

describe('BoardCollab', () => {
  it('relays only elements whose revision changed', () => {
    const { collab, socket } = session()

    collab.pushElements([el('a', 1), el('b', 1)])
    flush()
    expect(socket.sent).toEqual([{ t: 'update', elements: [el('a', 1), el('b', 1)] }])

    // Unchanged elements are not resent, even though onChange reports the whole
    // scene on every keystroke.
    socket.sent = []
    collab.pushElements([el('a', 1), el('b', 1)])
    flush()
    expect(socket.sent).toEqual([])

    // A new revision of one element goes out on its own.
    collab.pushElements([el('a', 2), el('b', 1)])
    flush()
    expect(socket.sent).toEqual([{ t: 'update', elements: [el('a', 2)] }])
  })

  it('does not echo back elements received from other peers', () => {
    const { collab, socket, handlers } = session()

    socket.deliver({ t: 'update', from: 'peer1', elements: [el('a', 4)] })
    expect(handlers.onRemoteElements).toHaveBeenCalledWith([el('a', 4)])

    // Excalidraw fires onChange after the remote update is applied; that must
    // not bounce the same revision back to the relay.
    collab.pushElements([el('a', 4)])
    flush()
    expect(socket.sent).toEqual([])

    // A local edit on top of it does go out.
    collab.pushElements([el('a', 5)])
    flush()
    expect(socket.sent).toEqual([{ t: 'update', elements: [el('a', 5)] }])
  })

  it('adopts the scene the relay replays on join', () => {
    const { collab, socket, handlers } = session()

    socket.deliver({
      t: 'init',
      self: 'me',
      can_write: true,
      scene: { elements: [el('a', 3)], background: '#101010', files: { f1: { mimeType: 'image/png' } } },
      peers: [{ id: 'me', name: 'Alice', color: '#fff', write: true }],
    })

    expect(handlers.onWritable).toHaveBeenCalledWith(true)
    expect(handlers.onInit).toHaveBeenCalled()
    expect(handlers.onPeers).toHaveBeenCalledWith([{ id: 'me', name: 'Alice', color: '#fff', write: true }])

    // Everything in the replayed scene counts as already relayed.
    collab.pushElements([el('a', 3)])
    collab.pushFiles({ f1: { mimeType: 'image/png' } })
    collab.pushBackground('#101010')
    flush()
    expect(socket.sent).toEqual([])
  })

  it('coalesces cursor moves into a single frame', () => {
    const { collab, socket } = session()

    collab.pushPointer(1, 1, 'active')
    collab.pushPointer(2, 2, 'active')
    collab.pushPointer(3, 4, 'down', ['a'])
    flush()

    expect(socket.sent).toEqual([{ t: 'pointer', x: 3, y: 4, state: 'down', selected: ['a'] }])
  })

  it('sends a binary asset once', () => {
    const { collab, socket } = session()

    collab.pushFiles({ f1: { mimeType: 'image/png' } })
    expect(socket.sent).toEqual([{ t: 'files', files: { f1: { mimeType: 'image/png' } } }])

    socket.sent = []
    collab.pushFiles({ f1: { mimeType: 'image/png' } })
    expect(socket.sent).toEqual([])
  })

  it('reports a revoked session as fatal and stops reconnecting', () => {
    const { socket, handlers } = session()

    socket.deliver({ t: 'error', message: 'access to this board has been revoked' })

    expect(handlers.onFatal).toHaveBeenCalledWith('access to this board has been revoked')
    expect(handlers.onStatus).toHaveBeenLastCalledWith('closed')

    // A fatal refusal would only repeat, so no reconnect is attempted.
    const before = FakeSocket.last
    vi.advanceTimersByTime(60_000)
    expect(FakeSocket.last).toBe(before)
  })

  it('reconnects with a growing backoff after an unexpected close', () => {
    const { socket, handlers } = session()
    const first = socket

    socket.onclose?.()
    expect(handlers.onStatus).toHaveBeenLastCalledWith('reconnecting')
    expect(FakeSocket.last).toBe(first)

    vi.advanceTimersByTime(500)
    expect(FakeSocket.last).not.toBe(first)
  })

  it('drops queued edits while offline instead of replaying a stale batch', () => {
    const { collab, socket } = session()

    socket.readyState = 3 // closed under us
    collab.pushElements([el('a', 1)])
    flush()
    expect(socket.sent).toEqual([])

    // The revision was never relayed, so it is still pending once the socket is
    // back and the next diff includes it.
    socket.readyState = 1
    collab.pushElements([el('a', 1)])
    flush()
    expect(socket.sent).toEqual([{ t: 'update', elements: [el('a', 1)] }])
  })

  it('stops cleanly', () => {
    const { collab, socket, handlers } = session()
    collab.stop()
    expect(socket.readyState).toBe(3)
    expect(handlers.onStatus).toHaveBeenLastCalledWith('closed')

    vi.advanceTimersByTime(60_000)
    expect(FakeSocket.last).toBe(socket)
  })
})
