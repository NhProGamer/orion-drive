/**
 * React host for the Excalidraw canvas. Excalidraw ships as a React component,
 * so this is the one React island in an otherwise Vue application; the Vue side
 * mounts it through `mount.ts` and only passes plain data in.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { Excalidraw, LiveCollaborationTrigger, reconcileElements, restoreElements, CaptureUpdateAction } from '@excalidraw/excalidraw'
import { BoardCollab, type CollabStatus } from './collab'
import type { WireElement, WireFile, WirePeer } from './protocol'

/** Strings come pre-translated from the Vue side, which owns vue-i18n. */
export interface BoardLabels {
  connecting: string
  reconnecting: string
  offline: string
  saved: string
  readOnly: string
  aloneOnBoard: string
  /** Peer count, with a `{count}` placeholder. */
  peers: string
}

export interface BoardCanvasProps {
  /** WebSocket URL of the relay session. */
  url: string
  /** 'dark' | 'light', mirroring the app's theme. */
  theme: 'dark' | 'light'
  /** Forces view mode regardless of the permission the relay grants. */
  viewOnly?: boolean
  labels: BoardLabels
}

type ExcalidrawAPI = Parameters<NonNullable<Parameters<typeof Excalidraw>[0]['excalidrawAPI']>>[0]
type Collaborators = Map<string, Record<string, unknown>>

export default function BoardCanvas({ url, theme, viewOnly, labels }: BoardCanvasProps) {
  const [api, setApi] = useState<ExcalidrawAPI | null>(null)
  const [status, setStatus] = useState<CollabStatus>('connecting')
  const [canWrite, setCanWrite] = useState(false)
  const [peers, setPeers] = useState<WirePeer[]>([])
  const [justSaved, setJustSaved] = useState(false)
  const [fatal, setFatal] = useState('')

  const collab = useRef<BoardCollab | null>(null)
  // Collaborator entries are rebuilt from the peer list and patched by pointer
  // frames, then handed to Excalidraw whole — it replaces the map on each call.
  const collaborators = useRef<Collaborators>(new Map())
  const selfID = useRef('')
  const centred = useRef(false)
  const savedTimer = useRef(0)

  /** Pushes the current collaborator map into the canvas. */
  const syncCollaborators = useCallback(
    (target: ExcalidrawAPI) => {
      target.updateScene({ collaborators: new Map(collaborators.current) as never })
    },
    [],
  )

  useEffect(() => {
    if (!api) return

    const session = new BoardCollab(url, {
      onInit: (scene, self) => {
        selfID.current = self
        // Merged rather than replaced: on a reconnect the canvas may hold
        // strokes drawn while the socket was down, and reconciliation keeps
        // them instead of letting the replayed scene wipe them out.
        const remote = restoreElements(scene.elements as never, null)
        api.updateScene({
          elements: reconcileElements(api.getSceneElementsIncludingDeleted(), remote as never, api.getAppState()),
          appState: { viewBackgroundColor: scene.background },
          captureUpdate: CaptureUpdateAction.NEVER,
        })
        const files = Object.entries(scene.files ?? {}).map(([id, file]) => ({
          ...(file as WireFile),
          id: (file.id as string) ?? id,
        }))
        if (files.length) api.addFiles(files as never)
        // Frame the drawing once per mount, not on every reconnect — that would
        // yank the viewport out from under whoever is drawing.
        if (!centred.current && scene.elements.length) {
          centred.current = true
          api.scrollToContent(undefined, { fitToContent: true })
        }
      },

      onRemoteElements: (elements) => {
        const local = api.getSceneElementsIncludingDeleted()
        const remote = restoreElements(elements as never, null)
        api.updateScene({
          elements: reconcileElements(local, remote as never, api.getAppState()),
          captureUpdate: CaptureUpdateAction.NEVER,
        })
      },

      onRemoteFiles: (files) => api.addFiles(files as never),

      onBackground: (color) =>
        api.updateScene({
          appState: { viewBackgroundColor: color },
          captureUpdate: CaptureUpdateAction.NEVER,
        }),

      onPeers: (list) => {
        const next: Collaborators = new Map()
        for (const peer of list) {
          if (peer.id === selfID.current) continue
          const previous = collaborators.current.get(peer.id)
          next.set(peer.id, {
            ...(previous ?? {}),
            id: peer.id,
            username: peer.name,
            color: { background: peer.color, stroke: peer.color },
          })
        }
        collaborators.current = next
        setPeers(list)
        syncCollaborators(api)
      },

      onPointer: (peerID, pointer) => {
        const existing = collaborators.current.get(peerID)
        if (!existing) return // a cursor from a peer we have not been told about yet
        collaborators.current.set(peerID, {
          ...existing,
          pointer: { x: pointer.x, y: pointer.y, tool: 'pointer' },
          button: pointer.state === 'down' ? 'down' : 'up',
          selectedElementIds: Object.fromEntries((pointer.selected ?? []).map((id) => [id, true])),
        })
        syncCollaborators(api)
      },

      onWritable: setCanWrite,
      // A transient confirmation: left pinned, it would still read "saved"
      // while newer strokes are waiting for the next snapshot.
      onSaved: () => {
        setJustSaved(true)
        window.clearTimeout(savedTimer.current)
        savedTimer.current = window.setTimeout(() => setJustSaved(false), 2000)
      },
      onStatus: setStatus,
      onFatal: setFatal,
    })

    collab.current = session
    session.start()
    return () => {
      session.stop()
      window.clearTimeout(savedTimer.current)
      collab.current = null
    }
  }, [api, url, syncCollaborators])

  const onChange = useCallback(
    (elements: readonly unknown[], appState: { viewBackgroundColor: string }, files: Record<string, unknown>) => {
      const session = collab.current
      if (!session || !canWrite) return
      session.pushElements(elements as readonly WireElement[])
      session.pushBackground(appState.viewBackgroundColor)
      session.pushFiles(files as Record<string, WireFile>)
    },
    [canWrite],
  )

  const onPointerUpdate = useCallback(
    (payload: { pointer: { x: number; y: number }; button: 'down' | 'up' }) => {
      const session = collab.current
      if (!session || !api) return
      const selected = Object.keys(api.getAppState().selectedElementIds ?? {})
      session.pushPointer(payload.pointer.x, payload.pointer.y, payload.button === 'down' ? 'down' : 'active', selected)
    },
    [api],
  )

  const others = peers.filter((p) => p.id !== selfID.current)
  const statusLabel =
    fatal ||
    (status === 'connecting' && labels.connecting) ||
    (status === 'reconnecting' && labels.reconnecting) ||
    (status === 'closed' && labels.offline) ||
    ''

  return (
    <div className="board-host">
      <Excalidraw
        excalidrawAPI={setApi}
        theme={theme}
        viewModeEnabled={viewOnly || !canWrite}
        onChange={onChange as never}
        onPointerUpdate={onPointerUpdate as never}
        UIOptions={{ canvasActions: { loadScene: false, saveToActiveFile: false, export: false } }}
        renderTopRightUI={() => (
          <LiveCollaborationTrigger
            isCollaborating={others.length > 0}
            onSelect={() => undefined}
          />
        )}
      />
      <div className="board-bar">
        {statusLabel && <span className={fatal ? 'board-chip board-chip-error' : 'board-chip'}>{statusLabel}</span>}
        {!statusLabel && justSaved && <span className="board-chip board-chip-ok">{labels.saved}</span>}
        {(viewOnly || !canWrite) && status === 'live' && <span className="board-chip">{labels.readOnly}</span>}
        <span className="board-peers" title={others.map((p) => p.name).join(', ')}>
          {others.length === 0
            ? labels.aloneOnBoard
            : labels.peers.replace('{count}', String(others.length))}
        </span>
      </div>
    </div>
  )
}
