/**
 * The Markdown WYSIWYG itself: Milkdown's Crepe editor, bound to a
 * collaborative session.
 *
 * Kept behind its own module so every heavy import — ProseMirror, CodeMirror,
 * the CRDT bindings and their stylesheets — lands in one lazily-loaded chunk
 * that a user who never opens a Markdown file never downloads.
 */
import { Crepe } from '@milkdown/crepe'
import { collab, collabServiceCtx, type CollabService } from '@milkdown/plugin-collab'
import type { DocSession } from './session'

import '@milkdown/crepe/theme/common/style.css'
import '@milkdown/crepe/theme/nord.css'

/** A mounted editor, driven by the Vue component that owns it. */
export interface EditorHandle {
  /**
   * Binds the CRDT document and starts collaborating. `seed` is the stored
   * Markdown when this peer is the one that must populate an empty document.
   */
  connect: (session: DocSession, seed: string | null) => void
  setReadonly: (readonly: boolean) => void
  getMarkdown: () => string
  destroy: () => Promise<void>
}

export interface EditorOptions {
  readonly: boolean
  placeholder: string
}

export async function createEditor(root: HTMLElement, opts: EditorOptions): Promise<EditorHandle> {
  const crepe = new Crepe({
    root,
    defaultValue: '',
    features: {
      // LaTeX pulls in KaTeX for a use case a drive's notes rarely need, and
      // the AI feature would call out to a third-party model.
      [Crepe.Feature.Latex]: false,
      [Crepe.Feature.AI]: false,
    },
    featureConfigs: {
      [Crepe.Feature.Placeholder]: { text: opts.placeholder },
    },
  })

  crepe.editor.use(collab)
  await crepe.create()
  crepe.setReadonly(opts.readonly)

  let service: CollabService | null = null
  crepe.editor.action((ctx) => {
    service = ctx.get(collabServiceCtx)
  })

  return {
    connect: (session, seed) => {
      if (!service) return
      service.bindDoc(session.doc).setAwareness(session.awareness)
      if (seed !== null) {
        // applyTemplate only writes when the document is still empty, so a peer
        // that was told to seed but raced with an incoming sync cannot end up
        // duplicating the content.
        service.applyTemplate(seed)
        session.markSeeded()
      }
      service.connect()
    },
    setReadonly: (readonly) => crepe.setReadonly(readonly),
    getMarkdown: () => crepe.getMarkdown(),
    destroy: async () => {
      service?.disconnect()
      await crepe.destroy()
    },
  }
}
